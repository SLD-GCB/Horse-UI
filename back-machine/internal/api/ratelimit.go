package api

import (
	"sync"
	"time"
)

// rateLimiter 滑动窗口速率限制（按 key 计数）。
type rateLimiter struct {
	mu     sync.Mutex
	m      map[string]*rlEntry
	max    int
	window time.Duration
}

type rlEntry struct {
	count int
	start time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	if max <= 0 {
		max = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	return &rateLimiter{m: make(map[string]*rlEntry), max: max, window: window}
}

// Allow 记录一次访问并判断是否在限额内。
func (r *rateLimiter) Allow(key string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.m[key]
	if e == nil || now.Sub(e.start) >= r.window {
		r.m[key] = &rlEntry{count: 1, start: now}
		return true
	}
	e.count++
	return e.count <= r.max
}

func (r *rateLimiter) purge() {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, e := range r.m {
		if now.Sub(e.start) >= 2*r.window {
			delete(r.m, k)
		}
	}
}

// failGuard 连续登录失败计数与锁定。
type failGuard struct {
	mu      sync.Mutex
	m       map[string]*fgEntry
	lockFor time.Duration
}

type fgEntry struct {
	fails       int
	lockedUntil time.Time
	last        time.Time
}

func newFailGuard(lockFor time.Duration) *failGuard {
	if lockFor <= 0 {
		lockFor = 15 * time.Minute
	}
	return &failGuard{m: make(map[string]*fgEntry), lockFor: lockFor}
}

// Allowed 判断 key 是否被锁定，返回剩余锁定时间。
func (g *failGuard) Allowed(key string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.m[key]
	if e == nil {
		return true, 0
	}
	if d := time.Until(e.lockedUntil); d > 0 {
		return false, d
	}
	return true, 0
}

// Fail 记录一次失败；达到阈值则锁定并清零计数。
func (g *failGuard) Fail(key string, threshold int) {
	if threshold <= 0 {
		threshold = 5
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	e := g.m[key]
	if e == nil {
		e = &fgEntry{}
		g.m[key] = e
	}
	e.fails++
	e.last = time.Now()
	if e.fails >= threshold {
		e.lockedUntil = time.Now().Add(g.lockFor)
		e.fails = 0
	}
}

// Reset 清除某 key 的失败记录（登录成功时调用）。
func (g *failGuard) Reset(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.m, key)
}

func (g *failGuard) purge() {
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, e := range g.m {
		if e.lockedUntil.After(now) {
			continue // 仍在锁定中
		}
		if e.fails == 0 || now.Sub(e.last) >= 2*g.lockFor {
			delete(g.m, k)
		}
	}
}
