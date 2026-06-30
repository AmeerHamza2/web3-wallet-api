// Package transaction builds, signs, and broadcasts Ethereum value transfers.
package transaction

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/AmeerHamza2/web3-wallet-api/internal/ethereum"
	"github.com/AmeerHamza2/web3-wallet-api/internal/events"
	"github.com/AmeerHamza2/web3-wallet-api/internal/wallet"
)

// Default gas limit for a plain ETH transfer (no contract execution).
const defaultTransferGasLimit uint64 = 21_000

// Validation errors surfaced to the API layer as 4xx responses.
var (
	ErrInvalidAddress = errors.New("invalid address")
	ErrInvalidAmount  = errors.New("invalid amount")
	ErrUnknownSender  = errors.New("sender wallet not managed by this service")
)

// SendRequest is the validated input for a transfer.
type SendRequest struct {
	From     string // managed sender address
	To       string // recipient address
	ValueWei string // amount in wei, as a base-10 string (avoids float precision loss)
	GasLimit uint64 // optional; defaults to 21000
}

// Receipt is the result of a successful broadcast.
type Receipt struct {
	TxHash    string `json:"tx_hash"`
	From      string `json:"from"`
	To        string `json:"to"`
	ValueWei  string `json:"value_wei"`
	Nonce     uint64 `json:"nonce"`
	GasTipCap string `json:"gas_tip_cap_wei"` // EIP-1559 priority fee
	GasFeeCap string `json:"gas_fee_cap_wei"` // EIP-1559 max fee
	GasLimit  uint64 `json:"gas_limit"`
}

// Service orchestrates the build → sign → broadcast → publish pipeline.
type Service struct {
	eth     *ethereum.Client
	signer  wallet.Signer
	events  events.Publisher
	nonces  *NonceManager
	chainID *big.Int
}

// NewService wires the transaction service.
func NewService(eth *ethereum.Client, signer wallet.Signer, pub events.Publisher, chainID int64) *Service {
	return &Service{
		eth:     eth,
		signer:  signer,
		events:  pub,
		nonces:  NewNonceManager(eth),
		chainID: big.NewInt(chainID),
	}
}

// Send validates, builds, signs, and broadcasts an EIP-1559 value transfer.
// Network interaction (nonce, fees, broadcast) requires RPC connectivity; if the
// node is unreachable the call returns ethereum.ErrNotConnected.
func (s *Service) Send(ctx context.Context, req SendRequest) (*Receipt, error) {
	if !common.IsHexAddress(req.To) {
		return nil, fmt.Errorf("%w: to=%q", ErrInvalidAddress, req.To)
	}
	if !common.IsHexAddress(req.From) {
		return nil, fmt.Errorf("%w: from=%q", ErrInvalidAddress, req.From)
	}
	if !s.signer.Exists(req.From) {
		return nil, ErrUnknownSender
	}

	value, ok := new(big.Int).SetString(req.ValueWei, 10)
	if !ok || value.Sign() < 0 {
		return nil, fmt.Errorf("%w: value_wei=%q", ErrInvalidAmount, req.ValueWei)
	}

	gasLimit := req.GasLimit
	if gasLimit == 0 {
		gasLimit = defaultTransferGasLimit
	}

	from := common.HexToAddress(req.From)
	to := common.HexToAddress(req.To)

	// Reserve a nonce from the per-sender manager so concurrent sends from the
	// same account don't collide. Any later failure resets it to avoid a gap.
	nonce, err := s.nonces.Reserve(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("reserve nonce: %w", err)
	}

	tip, err := s.eth.SuggestGasTipCap(ctx)
	if err != nil {
		s.nonces.Reset(from)
		return nil, fmt.Errorf("suggest gas tip: %w", err)
	}
	baseFee, err := s.eth.BaseFee(ctx)
	if err != nil {
		s.nonces.Reset(from)
		return nil, fmt.Errorf("base fee: %w", err)
	}
	// maxFeePerGas = 2*baseFee + tip: headroom for base-fee growth over a few
	// blocks while the tip stays the validator incentive.
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), tip)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   s.chainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
	})

	signed, err := s.signer.SignTx(req.From, tx)
	if err != nil {
		s.nonces.Reset(from)
		s.publish(ctx, events.TypeTxFailed, map[string]any{
			"from": req.From, "to": req.To, "error": err.Error(),
		})
		return nil, fmt.Errorf("sign: %w", err)
	}

	if err := s.eth.SendTransaction(ctx, signed); err != nil {
		s.nonces.Reset(from)
		s.publish(ctx, events.TypeTxFailed, map[string]any{
			"from": req.From, "to": req.To, "tx_hash": signed.Hash().Hex(), "error": err.Error(),
		})
		return nil, fmt.Errorf("broadcast: %w", err)
	}

	receipt := &Receipt{
		TxHash:    signed.Hash().Hex(),
		From:      req.From,
		To:        req.To,
		ValueWei:  value.String(),
		Nonce:     nonce,
		GasTipCap: tip.String(),
		GasFeeCap: gasFeeCap.String(),
		GasLimit:  gasLimit,
	}

	s.publish(ctx, events.TypeTxSubmitted, map[string]any{
		"from": receipt.From, "to": receipt.To,
		"value_wei": receipt.ValueWei, "tx_hash": receipt.TxHash,
	})

	return receipt, nil
}

func (s *Service) publish(ctx context.Context, t string, payload map[string]any) {
	if s.events == nil {
		return
	}
	// Best-effort: a broker hiccup must not fail an otherwise-successful tx.
	_ = s.events.Publish(ctx, events.New(t, time.Now().UTC(), payload))
}
