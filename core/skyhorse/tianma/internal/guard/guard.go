// Package guard holds the abuse boundaries: per-source connection limits, auth
// failure throttling, and per-connection stream/session caps.
package guard

import (
	"sync"
	"time"
)

// Config is the abuse boundary configuration. Zero values pick safe defaults.
type Config struct {
	MaxConnectionsPerIP         int `json:"max_connections_per_ip"`
	MaxAuthFailures             int `json:"max_auth_failures"`
	AuthFailureWindowSeconds    int `json:"auth_failure_window_seconds"`
	AuthBlockSeconds            int `json:"auth_block_seconds"`
	MaxStreamsPerConnection     int `json:"max_streams_per_connection"`
	MaxUDPSessionsPerConnection int `json:"max_udp_sessions_per_connection"`
}

// Guard enforces the configured limits.
type Guard struct {
	maxConnections int
	maxAuthFails   int
	window         time.Duration
	block          time.Duration
	maxStreams     int
	maxUDPSessions int

	mutex       sync.Mutex
	connections map[string]int
	failures    map[string]*failureState
}

type failureState struct {
	windowStart  time.Time
	count        int
	blockedUntil time.Time
}

// New compiles the configuration with defaults.
func New(config Config) *Guard {
	compiled := &Guard{
		maxConnections: config.MaxConnectionsPerIP,
		maxAuthFails:   config.MaxAuthFailures,
		window:         time.Duration(config.AuthFailureWindowSeconds) * time.Second,
		block:          time.Duration(config.AuthBlockSeconds) * time.Second,
		maxStreams:     config.MaxStreamsPerConnection,
		maxUDPSessions: config.MaxUDPSessionsPerConnection,
		connections:    make(map[string]int),
		failures:       make(map[string]*failureState),
	}
	if compiled.maxConnections <= 0 {
		compiled.maxConnections = 64
	}
	if compiled.maxAuthFails <= 0 {
		compiled.maxAuthFails = 20
	}
	if compiled.window <= 0 {
		compiled.window = time.Minute
	}
	if compiled.block <= 0 {
		compiled.block = 5 * time.Minute
	}
	if compiled.maxStreams <= 0 {
		compiled.maxStreams = 256
	}
	if compiled.maxUDPSessions <= 0 {
		compiled.maxUDPSessions = 256
	}
	go compiled.sweepLoop()
	return compiled
}

// sweepLoop removes stale auth failure entries so spoofed source IPs cannot
// grow the table without bound.
func (g *Guard) sweepLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		g.mutex.Lock()
		for ip, state := range g.failures {
			if now.After(state.blockedUntil) && now.Sub(state.windowStart) > g.window {
				delete(g.failures, ip)
			}
		}
		g.mutex.Unlock()
	}
}

// AllowConnection reserves one connection slot for the source IP.
func (g *Guard) AllowConnection(ip string) (func(), bool) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if g.connections[ip] >= g.maxConnections {
		return nil, false
	}
	g.connections[ip]++

	released := false
	return func() {
		g.mutex.Lock()
		defer g.mutex.Unlock()
		if released {
			return
		}
		released = true
		g.connections[ip]--
		if g.connections[ip] <= 0 {
			delete(g.connections, ip)
		}
	}, true
}

// AuthBlocked reports whether the source IP is currently throttled.
func (g *Guard) AuthBlocked(ip string) bool {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	state, ok := g.failures[ip]
	if !ok {
		return false
	}
	return time.Now().Before(state.blockedUntil)
}

// RecordAuthFailure counts one failed authentication and may block the source.
func (g *Guard) RecordAuthFailure(ip string) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	now := time.Now()
	state, ok := g.failures[ip]
	if !ok || now.Sub(state.windowStart) > g.window {
		state = &failureState{windowStart: now}
		g.failures[ip] = state
	}
	state.count++
	if state.count >= g.maxAuthFails {
		state.blockedUntil = now.Add(g.block)
		state.count = 0
		state.windowStart = now
	}
}

// ResetAuthFailures clears the failure state after a successful authentication.
func (g *Guard) ResetAuthFailures(ip string) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	delete(g.failures, ip)
}

// MaxStreams is the per-connection relay cap.
func (g *Guard) MaxStreams() int { return g.maxStreams }

// MaxUDPSessions is the per-connection UDP session cap.
func (g *Guard) MaxUDPSessions() int { return g.maxUDPSessions }
