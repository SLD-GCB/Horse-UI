package congestion

import (
	"math"
	"sync/atomic"

	"github.com/apernet/quic-go/congestion"
	"github.com/apernet/quic-go/monotime"
)

// lossGuard wraps a congestion controller (BBR in practice) with a gentle loss
// response. BBR deliberately ignores loss, which is right on a clean path but
// turns into bufferbloat and unfairness when a middlebox or a shallow buffer
// drops packets. The guard only shaves the window once loss is sustained.
type lossGuard struct {
	inner congestion.CongestionControl

	ackCount  atomic.Uint64
	lossCount atomic.Uint64
	factor    atomic.Uint64 // math.Float64bits, 1.0 = no cap
}

func newLossGuard(inner congestion.CongestionControl) *lossGuard {
	guard := &lossGuard{inner: inner}
	guard.factor.Store(math.Float64bits(1))
	return guard
}

func (g *lossGuard) SetRTTStatsProvider(provider congestion.RTTStatsProvider) {
	g.inner.SetRTTStatsProvider(provider)
}

func (g *lossGuard) TimeUntilSend(bytesInFlight congestion.ByteCount) monotime.Time {
	return g.inner.TimeUntilSend(bytesInFlight)
}

func (g *lossGuard) HasPacingBudget(now monotime.Time) bool {
	return g.inner.HasPacingBudget(now)
}

func (g *lossGuard) CanSend(bytesInFlight congestion.ByteCount) bool {
	return bytesInFlight <= g.GetCongestionWindow()
}

func (g *lossGuard) GetCongestionWindow() congestion.ByteCount {
	window := g.inner.GetCongestionWindow()
	factor := math.Float64frombits(g.factor.Load())
	if factor >= 1 {
		return window
	}
	return congestion.ByteCount(float64(window) * factor)
}

func (g *lossGuard) OnPacketSent(sentTime monotime.Time, bytesInFlight congestion.ByteCount,
	packetNumber congestion.PacketNumber, bytes congestion.ByteCount, isRetransmittable bool,
) {
	g.inner.OnPacketSent(sentTime, bytesInFlight, packetNumber, bytes, isRetransmittable)
}

func (g *lossGuard) OnPacketAcked(number congestion.PacketNumber, ackedBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount, eventTime monotime.Time,
) {
	g.inner.OnPacketAcked(number, ackedBytes, priorInFlight, eventTime)
}

func (g *lossGuard) OnCongestionEvent(number congestion.PacketNumber, lostBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount,
) {
	g.inner.OnCongestionEvent(number, lostBytes, priorInFlight)
}

func (g *lossGuard) OnCongestionEventEx(priorInFlight congestion.ByteCount, eventTime monotime.Time,
	ackedPackets []congestion.AckedPacketInfo, lostPackets []congestion.LostPacketInfo,
) {
	if extended, ok := g.inner.(congestion.CongestionControlEx); ok {
		extended.OnCongestionEventEx(priorInFlight, eventTime, ackedPackets, lostPackets)
	}
	acked := uint64(len(ackedPackets))
	lost := uint64(len(lostPackets))
	g.ackCount.Add(acked)
	g.lossCount.Add(lost)

	total := g.ackCount.Load() + g.lossCount.Load()
	if total < 20 {
		return
	}

	lossRate := float64(g.lossCount.Load()) / float64(total)
	factor := 1.0
	if lossRate > 0.02 {
		factor = 1 - math.Min(0.5, lossRate*2)
	}
	g.factor.Store(math.Float64bits(factor))

	// Decay the counters so the guard follows the path instead of a lifetime
	// average.
	if total > 1000 {
		g.ackCount.Store(g.ackCount.Load() / 2)
		g.lossCount.Store(g.lossCount.Load() / 2)
	}
}

func (g *lossGuard) SetMaxDatagramSize(size congestion.ByteCount) {
	g.inner.SetMaxDatagramSize(size)
}

func (g *lossGuard) InSlowStart() bool  { return g.inner.InSlowStart() }
func (g *lossGuard) InRecovery() bool   { return g.inner.InRecovery() }
func (g *lossGuard) MaybeExitSlowStart() { g.inner.MaybeExitSlowStart() }
func (g *lossGuard) OnRetransmissionTimeout(packetsRetransmitted bool) {
	g.inner.OnRetransmissionTimeout(packetsRetransmitted)
}
