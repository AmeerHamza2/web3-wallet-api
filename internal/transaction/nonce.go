package transaction

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// pendingNoncer reports the next pending nonce for an account. *ethereum.Client
// satisfies it; tests provide a fake.
type pendingNoncer interface {
	PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
}

// NonceManager hands out sequential nonces per sender so concurrent transactions
// from the same account don't collide on the same nonce.
//
// The chain's pending nonce only advances once a transaction is mined, so firing
// several sends in quick succession from one account would otherwise reuse a
// nonce and have all-but-one rejected. The manager seeds from the chain's
// pending nonce on first use and then allocates locally. On a send failure the
// caller calls Reset so the next allocation re-syncs with the chain instead of
// leaving a gap.
type NonceManager struct {
	noncer pendingNoncer

	mu     sync.Mutex
	states map[common.Address]*nonceState
}

type nonceState struct {
	mu     sync.Mutex
	next   uint64
	synced bool
}

func NewNonceManager(noncer pendingNoncer) *NonceManager {
	return &NonceManager{noncer: noncer, states: make(map[common.Address]*nonceState)}
}

func (m *NonceManager) state(addr common.Address) *nonceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.states[addr]
	if !ok {
		st = &nonceState{}
		m.states[addr] = st
	}
	return st
}

// Reserve returns the next nonce to use for addr and advances the local counter.
// Per-address locking lets different senders reserve concurrently.
func (m *NonceManager) Reserve(ctx context.Context, addr common.Address) (uint64, error) {
	st := m.state(addr)
	st.mu.Lock()
	defer st.mu.Unlock()

	if !st.synced {
		n, err := m.noncer.PendingNonceAt(ctx, addr)
		if err != nil {
			return 0, err
		}
		st.next = n
		st.synced = true
	}
	n := st.next
	st.next++
	return n, nil
}

// Reset forces the next Reserve for addr to re-read the chain's pending nonce.
// Call it after a send fails so a reserved-but-unused nonce doesn't leave a gap.
func (m *NonceManager) Reset(addr common.Address) {
	st := m.state(addr)
	st.mu.Lock()
	st.synced = false
	st.mu.Unlock()
}
