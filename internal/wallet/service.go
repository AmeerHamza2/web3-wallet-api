// Package wallet manages Ethereum accounts and key custody via go-ethereum's
// keystore (Web3 Secret Storage: scrypt-derived AES-128-CTR with a Keccak-256
// MAC). Keys are decrypted only transiently for a single signing operation and
// are never returned over the API. The Signer interface is where a KMS/HSM
// backend would replace the on-disk keystore.
package wallet

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ErrWalletNotFound is returned when an address is not present in the keystore.
var ErrWalletNotFound = errors.New("wallet not found")

// Wallet is the public (non-sensitive) view of an account.
type Wallet struct {
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
}

// Signer is the minimal capability the transaction layer needs from a key
// custodian. Implementing this against KMS/HSM is the production upgrade path.
type Signer interface {
	SignTx(address string, tx *types.Transaction) (*types.Transaction, error)
	Exists(address string) bool
}

// Service is a keystore-backed wallet manager. Safe for concurrent use.
type Service struct {
	ks         *keystore.KeyStore
	passphrase string
	chainID    *big.Int

	mu      sync.RWMutex
	created map[common.Address]time.Time // best-effort creation timestamps
}

// NewService opens (or creates) a keystore rooted at dir, using the standard
// (stronger) scrypt parameters. Tests call newService with the light ones.
func NewService(dir, passphrase string, chainID int64) (*Service, error) {
	return newService(dir, passphrase, chainID, keystore.StandardScryptN, keystore.StandardScryptP)
}

func newService(dir, passphrase string, chainID int64, scryptN, scryptP int) (*Service, error) {
	if passphrase == "" {
		return nil, errors.New("keystore passphrase must not be empty")
	}
	ks := keystore.NewKeyStore(dir, scryptN, scryptP)
	return &Service{
		ks:         ks,
		passphrase: passphrase,
		chainID:    big.NewInt(chainID),
		created:    make(map[common.Address]time.Time),
	}, nil
}

// Create generates a fresh account and persists it encrypted to disk.
func (s *Service) Create() (Wallet, error) {
	acct, err := s.ks.NewAccount(s.passphrase)
	if err != nil {
		return Wallet{}, fmt.Errorf("create account: %w", err)
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.created[acct.Address] = now
	s.mu.Unlock()
	return Wallet{Address: acct.Address.Hex(), CreatedAt: now}, nil
}

// List returns all accounts currently in the keystore.
func (s *Service) List() []Wallet {
	accts := s.ks.Accounts()
	out := make([]Wallet, 0, len(accts))
	for _, a := range accts {
		s.mu.RLock()
		ts := s.created[a.Address]
		s.mu.RUnlock()
		out = append(out, Wallet{Address: a.Address.Hex(), CreatedAt: ts})
	}
	return out
}

// Exists reports whether the keystore holds the given address.
func (s *Service) Exists(address string) bool {
	if !common.IsHexAddress(address) {
		return false
	}
	return s.ks.HasAddress(common.HexToAddress(address))
}

// SignTx signs tx with the key for address, decrypting it only for this call.
//
// We use the EIP-155 replay-protected signer bound to the configured chain ID,
// so a transaction signed for Sepolia cannot be replayed on mainnet.
func (s *Service) SignTx(address string, tx *types.Transaction) (*types.Transaction, error) {
	if !common.IsHexAddress(address) {
		return nil, fmt.Errorf("%w: invalid address %q", ErrWalletNotFound, address)
	}
	acct := accounts.Account{Address: common.HexToAddress(address)}
	if !s.ks.HasAddress(acct.Address) {
		return nil, ErrWalletNotFound
	}
	// Unlock-sign-relock keeps the decrypted key resident for the minimum window.
	if err := s.ks.Unlock(acct, s.passphrase); err != nil {
		return nil, fmt.Errorf("unlock account: %w", err)
	}
	defer s.ks.Lock(acct.Address) //nolint:errcheck // best-effort relock
	signed, err := s.ks.SignTx(acct, tx, s.chainID)
	if err != nil {
		return nil, fmt.Errorf("sign tx: %w", err)
	}
	return signed, nil
}
