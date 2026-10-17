package process

import "sync"

// RingBuffer 定长环形日志缓冲。
type RingBuffer struct {
	mu    sync.Mutex
	lines []string
	cap   int
}

// NewRingBuffer 创建容量为 cap 的环形缓冲。
func NewRingBuffer(cap int) *RingBuffer {
	if cap <= 0 {
		cap = 500
	}
	return &RingBuffer{cap: cap}
}

// Add 追加一行日志，超出容量时丢弃最旧行。
func (r *RingBuffer) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) >= r.cap {
		n := len(r.lines) - r.cap + 1
		r.lines = append(r.lines[:0], r.lines[n:]...)
	}
	r.lines = append(r.lines, line)
}

// Lines 返回日志快照。
func (r *RingBuffer) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// Clear 清空日志。
func (r *RingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = r.lines[:0]
}
