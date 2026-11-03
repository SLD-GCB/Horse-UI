package guard

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// ErrQuota is returned when a user exceeds the traffic allowance.
var ErrQuota = errors.New("guard: traffic quota exceeded")

// QuotaConfig is the per-user traffic and bandwidth allowance.
type QuotaConfig struct {
	BytesPerPeriod int64 `json:"bytes_per_period"`
	PeriodSeconds  int   `json:"period_seconds"`
	MaxRateBps     int64 `json:"max_rate_bps"`
}

// Quota tracks usage per user and enforces the rate with a token bucket.
type Quota struct {
	config QuotaConfig

	mutex sync.Mutex
	users map[string]*quotaUsage
}

type quotaUsage struct {
	periodStart time.Time
	bytes       int64
	tokens      float64
	last        time.Time
}

// NewQuota compiles the allowance.
func NewQuota(config QuotaConfig) *Quota {
	if config.PeriodSeconds <= 0 {
		config.PeriodSeconds = 30 * 24 * 60 * 60
	}
	return &Quota{config: config, users: make(map[string]*quotaUsage)}
}

func (q *Quota) usage(uuid string) *quotaUsage {
	state, ok := q.users[uuid]
	if !ok {
		state = &quotaUsage{periodStart: time.Now(), last: time.Now()}
		q.users[uuid] = state
	}
	now := time.Now()
	if now.Sub(state.periodStart) >= time.Duration(q.config.PeriodSeconds)*time.Second {
		state.periodStart = now
		state.bytes = 0
	}
	return state
}

// Allow reports whether the user still has traffic budget left.
func (q *Quota) Allow(uuid string) bool {
	if q == nil || q.config.BytesPerPeriod <= 0 {
		return true
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	state := q.usage(uuid)
	return state.bytes < q.config.BytesPerPeriod
}

// Add records bytes and reports whether the user is still within budget.
func (q *Quota) Add(uuid string, bytes int64) bool {
	if q == nil || q.config.BytesPerPeriod <= 0 {
		return true
	}
	q.mutex.Lock()
	defer q.mutex.Unlock()
	state := q.usage(uuid)
	state.bytes += bytes
	return state.bytes <= q.config.BytesPerPeriod
}

// Take enforces the bandwidth allowance with a token bucket. It only sleeps on
// the configured path; with no rate limit it returns immediately.
func (q *Quota) Take(uuid string, bytes int64) {
	if q == nil || q.config.MaxRateBps <= 0 || bytes <= 0 {
		return
	}
	q.mutex.Lock()
	state := q.usage(uuid)
	now := time.Now()
	state.tokens += float64(q.config.MaxRateBps) * now.Sub(state.last).Seconds()
	state.last = now
	capacity := float64(q.config.MaxRateBps)
	if state.tokens > capacity {
		state.tokens = capacity
	}
	state.tokens -= float64(bytes)
	wait := 0.0
	if state.tokens < 0 {
		wait = -state.tokens / float64(q.config.MaxRateBps)
	}
	q.mutex.Unlock()

	if wait > 0 {
		time.Sleep(time.Duration(wait * float64(time.Second)))
	}
}

// Wrap counts and throttles both directions of a relay.
func (q *Quota) Wrap(uuid string, conn net.Conn) net.Conn {
	if q == nil {
		return conn
	}
	return &quotaConn{Conn: conn, quota: q, uuid: uuid}
}

type quotaConn struct {
	net.Conn
	quota *Quota
	uuid  string
}

func (c *quotaConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 {
		if !c.quota.Add(c.uuid, int64(n)) {
			return n, ErrQuota
		}
		c.quota.Take(c.uuid, int64(n))
	}
	return n, err
}

func (c *quotaConn) Write(buffer []byte) (int, error) {
	if !c.quota.Add(c.uuid, int64(len(buffer))) {
		return 0, ErrQuota
	}
	c.quota.Take(c.uuid, int64(len(buffer)))
	return c.Conn.Write(buffer)
}

var _ io.Reader = (*quotaConn)(nil)
