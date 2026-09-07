package dyl

import "sync"

// Mempool is the queue of pending transactions shared by the validators in
// a Cluster. It does no validation of its own: a proposer drains
// transactions into a block and block application is what accepts or
// rejects them.
type Mempool struct {
	mu  sync.Mutex
	txs []Transaction
}

func NewMempool() *Mempool {
	return &Mempool{}
}

// Add appends a transaction to the back of the queue.
func (m *Mempool) Add(tx Transaction) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txs = append(m.txs, tx)
}

// Len is the number of queued transactions.
func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.txs)
}

// Drain removes and returns up to max transactions in arrival order. A max
// of zero or less takes everything currently queued.
func (m *Mempool) Drain(max int) []Transaction {
	m.mu.Lock()
	defer m.mu.Unlock()
	if max <= 0 || max > len(m.txs) {
		max = len(m.txs)
	}
	out := make([]Transaction, max)
	copy(out, m.txs[:max])
	m.txs = append(m.txs[:0:0], m.txs[max:]...)
	return out
}
