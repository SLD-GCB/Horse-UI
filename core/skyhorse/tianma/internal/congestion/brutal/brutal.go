// Package brutal is Hysteria's fixed-rate congestion control with an adaptive
// governor on top: when the measured ACK rate collapses, the send rate backs
// off instead of hammering a throttled or lossy path.
package brutal

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/apernet/quic-go/congestion"
	"github.com/apernet/quic-go/monotime"

	"tianma/internal/congestion/common"
)

const (
	pktInfoSlotCount           = 5
	minSampleCount             = 50
	minAckRate                 = 0.8
	congestionWindowMultiplier = 2

	debugEnv           = "TIANMA_BRUTAL_DEBUG"
	debugPrintInterval = 2

	// Governor thresholds. A path that acks less than 90% of what we send is
	// being shaped or is congested; below 98% there is no headroom to grow.
	backoffThreshold = 0.90
	recoverThreshold = 0.98
	recoverDivisor   = 20
)

var _ congestion.CongestionControl = &BrutalSender{}

// BrutalSender is a fixed-rate sender with a bounded adaptive governor.
type BrutalSender struct {
	rttStats                congestion.RTTStatsProvider
	configuredBps           atomic.Uint64
	bps                     atomic.Uint64
	ackRateBits             atomic.Uint64
	maxDatagramSize         congestion.ByteCount
	pacer                   *common.Pacer
	disableLossCompensation bool
	disableAdaptive         bool

	pktInfoSlots [pktInfoSlotCount]pktInfo

	debug                 bool
	lastAckPrintTimestamp int64
}

type pktInfo struct {
	Timestamp int64
	AckCount  uint64
	LossCount uint64
}

func NewBrutalSender(bps uint64, disableLossCompensation bool) *BrutalSender {
	debug, _ := strconv.ParseBool(os.Getenv(debugEnv))
	sender := &BrutalSender{
		maxDatagramSize:         congestion.InitialPacketSize,
		disableLossCompensation: disableLossCompensation,
		debug:                   debug,
	}
	sender.configuredBps.Store(bps)
	sender.bps.Store(bps)
	sender.ackRateBits.Store(math.Float64bits(1))
	sender.pacer = common.NewPacer(func() congestion.ByteCount {
		return congestion.ByteCount(sender.bps.Load())
	})
	return sender
}

// Rate reports the current send rate in bytes per second.
func (b *BrutalSender) Rate() uint64 { return b.bps.Load() }

// ConfiguredRate reports the rate the panel asked for.
func (b *BrutalSender) ConfiguredRate() uint64 { return b.configuredBps.Load() }

// AckRate reports the measured ack ratio.
func (b *BrutalSender) AckRate() float64 {
	return math.Float64frombits(b.ackRateBits.Load())
}

func (b *BrutalSender) SetRTTStatsProvider(rttStats congestion.RTTStatsProvider) {
	b.rttStats = rttStats
}

func (b *BrutalSender) TimeUntilSend(bytesInFlight congestion.ByteCount) monotime.Time {
	return b.pacer.TimeUntilSend()
}

func (b *BrutalSender) HasPacingBudget(now monotime.Time) bool {
	return b.pacer.Budget(now) >= b.maxDatagramSize
}

func (b *BrutalSender) CanSend(bytesInFlight congestion.ByteCount) bool {
	return bytesInFlight <= b.GetCongestionWindow()
}

func (b *BrutalSender) GetCongestionWindow() congestion.ByteCount {
	rtt := b.rttStats.SmoothedRTT()
	if rtt <= 0 {
		return 10240
	}
	ackRate := b.AckRate()
	cwnd := congestion.ByteCount(float64(b.bps.Load()) * rtt.Seconds() * congestionWindowMultiplier / ackRate)
	if cwnd < b.maxDatagramSize {
		cwnd = b.maxDatagramSize
	}
	return cwnd
}

func (b *BrutalSender) OnPacketSent(sentTime monotime.Time, bytesInFlight congestion.ByteCount,
	packetNumber congestion.PacketNumber, bytes congestion.ByteCount, isRetransmittable bool,
) {
	b.pacer.SentPacket(sentTime, bytes)
}

func (b *BrutalSender) OnPacketAcked(number congestion.PacketNumber, ackedBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount, eventTime monotime.Time,
) {
}

func (b *BrutalSender) OnCongestionEvent(number congestion.PacketNumber, lostBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount,
) {
}

func (b *BrutalSender) OnCongestionEventEx(priorInFlight congestion.ByteCount, eventTime monotime.Time, ackedPackets []congestion.AckedPacketInfo, lostPackets []congestion.LostPacketInfo) {
	currentTimestamp := int64(time.Duration(eventTime) / time.Second)
	slot := currentTimestamp % pktInfoSlotCount
	if b.pktInfoSlots[slot].Timestamp == currentTimestamp {
		b.pktInfoSlots[slot].LossCount += uint64(len(lostPackets))
		b.pktInfoSlots[slot].AckCount += uint64(len(ackedPackets))
	} else {
		b.pktInfoSlots[slot].Timestamp = currentTimestamp
		b.pktInfoSlots[slot].AckCount = uint64(len(ackedPackets))
		b.pktInfoSlots[slot].LossCount = uint64(len(lostPackets))
	}
	b.updateAckRate(currentTimestamp)
}

func (b *BrutalSender) SetMaxDatagramSize(size congestion.ByteCount) {
	b.maxDatagramSize = size
	b.pacer.SetMaxDatagramSize(size)
}

func (b *BrutalSender) updateAckRate(currentTimestamp int64) {
	if b.disableLossCompensation {
		b.ackRateBits.Store(math.Float64bits(1))
		b.bps.Store(b.configuredBps.Load())
		return
	}

	minTimestamp := currentTimestamp - pktInfoSlotCount
	var ackCount, lossCount uint64
	for _, info := range b.pktInfoSlots {
		if info.Timestamp < minTimestamp {
			continue
		}
		ackCount += info.AckCount
		lossCount += info.LossCount
	}
	if ackCount+lossCount < minSampleCount {
		b.ackRateBits.Store(math.Float64bits(1))
		return
	}

	rate := float64(ackCount) / float64(ackCount+lossCount)
	if rate < minAckRate {
		rate = minAckRate
	}
	b.ackRateBits.Store(math.Float64bits(rate))
	b.govern(rate)
}

// govern is the anti-interference part: a shaped path shows up as a low ack
// rate, so the sender lowers its own ceiling instead of continuing to push at
// the configured rate.
func (b *BrutalSender) govern(rate float64) {
	if b.disableAdaptive {
		return
	}
	configured := b.configuredBps.Load()
	current := b.bps.Load()

	switch {
	case rate < backoffThreshold:
		target := uint64(float64(current) * (rate - 0.05))
		floor := configured / 5
		if floor < 64*1024 {
			floor = 64 * 1024
		}
		if target < floor {
			target = floor
		}
		b.bps.Store(target)
	case rate > recoverThreshold && current < configured:
		step := configured / recoverDivisor
		next := current + step
		if next > configured {
			next = configured
		}
		b.bps.Store(next)
	}

	if b.debug {
		fmt.Printf("[BrutalSender] ack=%.2f rate=%d configured=%d\n", rate, b.bps.Load(), configured)
	}
}

func (b *BrutalSender) InSlowStart() bool  { return false }
func (b *BrutalSender) InRecovery() bool   { return false }
func (b *BrutalSender) MaybeExitSlowStart() {}
func (b *BrutalSender) OnRetransmissionTimeout(packetsRetransmitted bool) {}
