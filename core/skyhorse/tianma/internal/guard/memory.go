package guard

import "sync/atomic"

// Memory is a global water level for relay buffers and queued datagrams.
type Memory struct {
	limit int64
	used  atomic.Int64
}

// NewMemory builds a water level; a non-positive limit disables it.
func NewMemory(limitBytes int64) *Memory {
	return &Memory{limit: limitBytes}
}

// Acquire reserves bytes and fails when the water level would be crossed.
func (m *Memory) Acquire(bytes int64) bool {
	if m == nil || m.limit <= 0 || bytes <= 0 {
		return true
	}
	if m.used.Add(bytes) > m.limit {
		m.used.Add(-bytes)
		return false
	}
	return true
}

// Release returns bytes to the pool.
func (m *Memory) Release(bytes int64) {
	if m == nil || m.limit <= 0 || bytes <= 0 {
		return
	}
	m.used.Add(-bytes)
}

// Used reports the current water level.
func (m *Memory) Used() int64 { return m.used.Load() }
