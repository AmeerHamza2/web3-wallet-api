package transaction

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/AmeerHamza2/web3-wallet-api/internal/ethereum"
)

const sepoliaChainID = 11155111

// stubSigner is an in-memory wallet.Signer for validation tests. Validation runs
// before any signing, so SignTx is never reached in these cases.
type stubSigner struct{ known map[string]bool }

func (s stubSigner) Exists(addr string) bool { return s.known[addr] }
func (s stubSigner) SignTx(string, *types.Transaction) (*types.Transaction, error) {
	return nil, errors.New("not used in these tests")
}

func newService(t *testing.T, known ...string) *Service {
	t.Helper()
	// A client pointed at a closed port is reported as disconnected, so any code
	// path that reaches the network surfaces ethereum.ErrNotConnected.
	eth := ethereum.NewClient(context.Background(), "http://127.0.0.1:1", sepoliaChainID)
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	return NewService(eth, stubSigner{known: set}, nil, sepoliaChainID)
}

func TestSendValidation(t *testing.T) {
	const managed = "0x71C7656EC7ab88b098defB751B7401B5f6d8976F"
	svc := newService(t, managed)

	tests := []struct {
		name string
		req  SendRequest
		want error
	}{
		{"invalid to", SendRequest{From: managed, To: "nope", ValueWei: "1"}, ErrInvalidAddress},
		{"invalid from", SendRequest{From: "nope", To: managed, ValueWei: "1"}, ErrInvalidAddress},
		{"unknown sender", SendRequest{From: "0x0000000000000000000000000000000000000001", To: managed, ValueWei: "1"}, ErrUnknownSender},
		{"bad amount", SendRequest{From: managed, To: managed, ValueWei: "abc"}, ErrInvalidAmount},
		{"negative amount", SendRequest{From: managed, To: managed, ValueWei: "-5"}, ErrInvalidAmount},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Send(context.Background(), tc.req)
			if !errors.Is(err, tc.want) {
				t.Errorf("Send error = %v, want %v", err, tc.want)
			}
		})
	}
}

// A fully valid request reaches the network layer, which is down in the test
// environment, so it must surface ErrNotConnected rather than panicking.
func TestSendDegradesWhenRPCDown(t *testing.T) {
	const managed = "0x71C7656EC7ab88b098defB751B7401B5f6d8976F"
	svc := newService(t, managed)
	_, err := svc.Send(context.Background(), SendRequest{From: managed, To: managed, ValueWei: "1000"})
	if !errors.Is(err, ethereum.ErrNotConnected) {
		t.Errorf("Send error = %v, want ErrNotConnected", err)
	}
}
