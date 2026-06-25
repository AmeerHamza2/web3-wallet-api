package wallet

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// newTestService builds a wallet service backed by a throwaway temp keystore.
// Tests are fully offline and deterministic — no network, no fixed seed needed.
func newTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(t.TempDir(), "test-passphrase", 11155111)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestNewServiceRejectsEmptyPassphrase(t *testing.T) {
	if _, err := NewService(t.TempDir(), "", 1); err == nil {
		t.Fatal("expected error for empty passphrase, got nil")
	}
}

func TestCreateProducesValidUniqueAddresses(t *testing.T) {
	svc := newTestService(t)

	w1, err := svc.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	w2, err := svc.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !common.IsHexAddress(w1.Address) {
		t.Errorf("address %q is not a valid hex address", w1.Address)
	}
	if w1.Address == w2.Address {
		t.Error("two created wallets share an address")
	}
	if w1.CreatedAt.IsZero() {
		t.Error("CreatedAt not set")
	}
}

func TestExists(t *testing.T) {
	svc := newTestService(t)
	w, err := svc.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !svc.Exists(w.Address) {
		t.Error("Exists should be true for a created wallet")
	}
	if svc.Exists("0x0000000000000000000000000000000000000000") {
		t.Error("Exists should be false for an unknown address")
	}
	if svc.Exists("not-an-address") {
		t.Error("Exists should be false for an invalid address")
	}
}

func TestListReflectsCreatedWallets(t *testing.T) {
	svc := newTestService(t)
	for i := 0; i < 3; i++ {
		if _, err := svc.Create(); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	if got := len(svc.List()); got != 3 {
		t.Errorf("List length = %d, want 3", got)
	}
}

func TestSignTxProducesRecoverableSignature(t *testing.T) {
	svc := newTestService(t)
	w, err := svc.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	to := common.HexToAddress("0x71C7656EC7ab88b098defB751B7401B5f6d8976F")
	tx := types.NewTransaction(0, to, big.NewInt(1000), 21000, big.NewInt(1_000_000_000), nil)

	signed, err := svc.SignTx(w.Address, tx)
	if err != nil {
		t.Fatalf("SignTx: %v", err)
	}

	// Recover the sender from the signature using the EIP-155 signer and
	// confirm it matches the wallet that signed it. This proves the signature
	// is valid and chain-bound without any network access.
	signer := types.NewEIP155Signer(big.NewInt(11155111))
	sender, err := types.Sender(signer, signed)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	if sender != common.HexToAddress(w.Address) {
		t.Errorf("recovered sender = %s, want %s", sender.Hex(), w.Address)
	}
}

func TestSignTxUnknownWallet(t *testing.T) {
	svc := newTestService(t)
	tx := types.NewTransaction(0, common.Address{}, big.NewInt(1), 21000, big.NewInt(1), nil)
	if _, err := svc.SignTx("0x71C7656EC7ab88b098defB751B7401B5f6d8976F", tx); err == nil {
		t.Fatal("expected ErrWalletNotFound, got nil")
	}
}
