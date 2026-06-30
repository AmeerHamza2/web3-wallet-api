// Package ethereum wraps the go-ethereum JSON-RPC client with the operations
// this service needs (balance, nonce, gas price, broadcast). Connectivity is
// optional at startup: the service boots without a reachable node and offline
// features (wallet creation, signing) keep working; chain-dependent endpoints
// return 503 until the node recovers.
package ethereum

import (
	"context"
	"errors"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ErrNotConnected indicates the upstream RPC node is unavailable.
var ErrNotConnected = errors.New("ethereum rpc not connected")

// Client is a thin, concurrency-safe wrapper over *ethclient.Client.
type Client struct {
	url     string
	chainID *big.Int

	mu        sync.RWMutex
	rpc       *ethclient.Client
	connected bool
}

// NewClient dials url but never fails hard: a dial error yields a disconnected
// client that callers can later revive via Reconnect.
func NewClient(ctx context.Context, url string, chainID int64) *Client {
	c := &Client{url: url, chainID: big.NewInt(chainID)}
	_ = c.Reconnect(ctx)
	return c
}

// Reconnect (re)establishes the RPC connection. go-ethereum's HTTP transport
// dials lazily, so a successful dial isn't proof of connectivity; we issue a
// ChainID round-trip to confirm the node is reachable before marking connected.
func (c *Client) Reconnect(ctx context.Context) error {
	rpc, err := ethclient.DialContext(ctx, c.url)
	if err != nil {
		c.setConnected(nil, false)
		return err
	}
	if _, err := rpc.ChainID(ctx); err != nil {
		rpc.Close()
		c.setConnected(nil, false)
		return err
	}
	c.setConnected(rpc, true)
	return nil
}

func (c *Client) setConnected(rpc *ethclient.Client, connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rpc = rpc
	c.connected = connected
}

// Connected reports the last known connection state.
func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Client) conn() (*ethclient.Client, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.connected || c.rpc == nil {
		return nil, ErrNotConnected
	}
	return c.rpc, nil
}

// BalanceAt returns the latest balance (in wei) of address.
func (c *Client) BalanceAt(ctx context.Context, address common.Address) (*big.Int, error) {
	rpc, err := c.conn()
	if err != nil {
		return nil, err
	}
	return rpc.BalanceAt(ctx, address, nil)
}

// PendingNonceAt returns the next nonce to use for address.
func (c *Client) PendingNonceAt(ctx context.Context, address common.Address) (uint64, error) {
	rpc, err := c.conn()
	if err != nil {
		return 0, err
	}
	return rpc.PendingNonceAt(ctx, address)
}

// SuggestGasTipCap returns a suggested EIP-1559 priority fee (tip) per gas.
func (c *Client) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	rpc, err := c.conn()
	if err != nil {
		return nil, err
	}
	return rpc.SuggestGasTipCap(ctx)
}

// BaseFee returns the base fee of the latest block, used to size an EIP-1559
// fee cap. It errors if the chain predates EIP-1559 (no base fee).
func (c *Client) BaseFee(ctx context.Context) (*big.Int, error) {
	rpc, err := c.conn()
	if err != nil {
		return nil, err
	}
	header, err := rpc.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	if header.BaseFee == nil {
		return nil, errors.New("chain does not support EIP-1559 (no base fee)")
	}
	return header.BaseFee, nil
}

// SendTransaction broadcasts a signed transaction to the network.
func (c *Client) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	rpc, err := c.conn()
	if err != nil {
		return err
	}
	return rpc.SendTransaction(ctx, tx)
}

// ChainID returns the configured chain ID.
func (c *Client) ChainID() *big.Int { return new(big.Int).Set(c.chainID) }

// Close releases the underlying connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rpc != nil {
		c.rpc.Close()
	}
}
