package chain

import (
	"errors"
	"testing"
)

// Add refuses once the queue is at capacity, and draining makes room again.
func TestMempoolCap(t *testing.T) {
	m := NewMempool()
	for i := 0; i < maxMempool; i++ {
		if err := m.Add(Transaction{}); err != nil {
			t.Fatalf("add %d of %d: %v", i, maxMempool, err)
		}
	}
	if err := m.Add(Transaction{}); !errors.Is(err, ErrMempoolFull) {
		t.Fatalf("add past cap: got %v, want ErrMempoolFull", err)
	}
	if got := m.Len(); got != maxMempool {
		t.Errorf("len %d, want %d", got, maxMempool)
	}

	m.Drain(1)
	if err := m.Add(Transaction{}); err != nil {
		t.Errorf("add after drain: %v", err)
	}
}
