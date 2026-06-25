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
	TxHash   string `json:"tx_hash"`
	From     string `json:"from"`
	To       string `json:"to"`
	ValueWei string `json:"value_wei"`
	Nonce    uint64 `json:"nonce"`
	GasPrice string `json:"gas_price_wei"`
	GasLimit uint64 `json:"gas_limit"`
}

// Service orchestrates the build → sign → broadcast → publish pipeline.
type Service struct {
	eth     *ethereum.Client
	signer  wallet.Signer
	events  events.Publisher
	chainID *big.Int
}

// NewService wires the transaction service.
func NewService(eth *ethereum.Client, signer wallet.Signer, pub events.Publisher, chainID int64) *Service {
	return &Service{eth: eth, signer: signer, events: pub, chainID: big.NewInt(chainID)}
}

// Send validates, builds, signs, and broadcasts a value transfer. Network
// interaction (nonce, gas price, broadcast) requires RPC connectivity; if the
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

	nonce, err := s.eth.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("fetch nonce: %w", err)
	}
	gasPrice, err := s.eth.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("suggest gas price: %w", err)
	}

	// Legacy transaction type — broadly accepted on all EVM chains and the
	// simplest to reason about for a reference implementation.
	tx := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, nil)

	signed, err := s.signer.SignTx(req.From, tx)
	if err != nil {
		s.publish(ctx, events.TypeTxFailed, map[string]any{
			"from": req.From, "to": req.To, "error": err.Error(),
		})
		return nil, fmt.Errorf("sign: %w", err)
	}

	if err := s.eth.SendTransaction(ctx, signed); err != nil {
		s.publish(ctx, events.TypeTxFailed, map[string]any{
			"from": req.From, "to": req.To, "tx_hash": signed.Hash().Hex(), "error": err.Error(),
		})
		return nil, fmt.Errorf("broadcast: %w", err)
	}

	receipt := &Receipt{
		TxHash:   signed.Hash().Hex(),
		From:     req.From,
		To:       req.To,
		ValueWei: value.String(),
		Nonce:    nonce,
		GasPrice: gasPrice.String(),
		GasLimit: gasLimit,
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
