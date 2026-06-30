package transaction

import (
	"context"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

type fakeNoncer struct {
	mu      sync.Mutex
	pending map[common.Address]uint64
	calls   int
}

func (f *fakeNoncer) PendingNonceAt(_ context.Context, addr common.Address) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.pending[addr], nil
}

var addrA = common.HexToAddress("0x71C7656EC7ab88b098defB751B7401B5f6d8976F")

func TestReserveIsSequentialAndSeedsOnce(t *testing.T) {
	nc := &fakeNoncer{pending: map[common.Address]uint64{addrA: 100}}
	m := NewNonceManager(nc)

	for want := uint64(100); want < 105; want++ {
		got, err := m.Reserve(context.Background(), addrA)
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
		if got != want {
			t.Fatalf("nonce = %d, want %d", got, want)
		}
	}
	if nc.calls != 1 {
		t.Fatalf("expected the chain to be queried once (then local), got %d calls", nc.calls)
	}
}

// Concurrent reservations from one account must each get a distinct nonce.
func TestReserveConcurrentNoDuplicates(t *testing.T) {
	nc := &fakeNoncer{pending: map[common.Address]uint64{addrA: 0}}
	m := NewNonceManager(nc)

	const n = 200
	var wg sync.WaitGroup
	results := make([]uint64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := m.Reserve(context.Background(), addrA)
			if err != nil {
				t.Errorf("reserve: %v", err)
				return
			}
			results[i] = v
		}(i)
	}
	wg.Wait()

	seen := make(map[uint64]bool, n)
	for _, v := range results {
		if seen[v] {
			t.Fatalf("duplicate nonce %d handed out", v)
		}
		seen[v] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique nonces, got %d", n, len(seen))
	}
}

func TestResetResyncsFromChain(t *testing.T) {
	nc := &fakeNoncer{pending: map[common.Address]uint64{addrA: 100}}
	m := NewNonceManager(nc)

	if _, err := m.Reserve(context.Background(), addrA); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	// Simulate the chain advancing (or our local counter being wrong) and reset.
	nc.pending[addrA] = 200
	m.Reset(addrA)

	got, err := m.Reserve(context.Background(), addrA)
	if err != nil {
		t.Fatalf("reserve after reset: %v", err)
	}
	if got != 200 {
		t.Fatalf("after reset nonce = %d, want 200 (re-synced from chain)", got)
	}
}
