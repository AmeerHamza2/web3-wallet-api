// Package wallet manages Ethereum accounts and private-key custody.
//
// Key-management design (the security-critical decision in this service):
//
//   - Private keys are NEVER held in plaintext in the application. They are
//     persisted using go-ethereum's keystore, which implements the Web3 Secret
//     Storage Definition: each key is encrypted with AES-128-CTR under a key
//     derived from a passphrase via scrypt (N=2^18), with a Keccak-256 MAC to
//     detect tampering/wrong-passphrase. This is the same on-disk format used by
//     geth and the broader Ethereum ecosystem.
//
//   - The raw private key is decrypted only transiently, inside the keystore,
//     for the duration of a single signing operation, then re-locked. The key
//     material is never returned over the API.
//
//   - For this reference implementation a single service-level passphrase
//     (KEYSTORE_PASSPHRASE) encrypts all accounts. In production you would scope
//     a distinct secret per user/tenant and source it from a managed secret
//     store (AWS KMS, HashiCorp Vault) or an HSM, so that the application
//     process never sees long-lived key-encryption material. The Signer
//     interface below is the seam where an HSM/KMS backend would plug in.
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

// NewService opens (or creates) a keystore rooted at dir.
//
// It uses StandardScryptN/P — the stronger (slower) scrypt parameters
// recommended for at-rest key encryption. Tests use newService directly with
// the cheaper "Light" parameters to stay fast.
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
