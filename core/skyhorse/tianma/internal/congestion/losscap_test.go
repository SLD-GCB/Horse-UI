package congestion

import (
	"testing"

	"github.com/apernet/quic-go/congestion"
	"github.com/apernet/quic-go/monotime"

	"tianma/internal/congestion/bbr"
)

type fakeSender struct {
	window congestion.ByteCount
}

func (f *fakeSender) SetRTTStatsProvider(congestion.RTTStatsProvider) {}
func (f *fakeSender) TimeUntilSend(congestion.ByteCount) monotime.Time { return 0 }
func (f *fakeSender) HasPacingBudget(monotime.Time) bool              { return true }
func (f *fakeSender) OnPacketSent(monotime.Time, congestion.ByteCount, congestion.PacketNumber, congestion.ByteCount, bool) {
}
func (f *fakeSender) CanSend(congestion.ByteCount) bool { return true }
func (f *fakeSender) MaybeExitSlowStart()               {}
func (f *fakeSender) OnPacketAcked(congestion.PacketNumber, congestion.ByteCount, congestion.ByteCount, monotime.Time) {
}
func (f *fakeSender) OnCongestionEvent(congestion.PacketNumber, congestion.ByteCount, congestion.ByteCount) {
}
func (f *fakeSender) OnCongestionEventEx(congestion.ByteCount, monotime.Time, []congestion.AckedPacketInfo, []congestion.LostPacketInfo) {
}
func (f *fakeSender) OnRetransmissionTimeout(bool)               {}
func (f *fakeSender) SetMaxDatagramSize(congestion.ByteCount)    {}
func (f *fakeSender) InSlowStart() bool                          { return false }
func (f *fakeSender) InRecovery() bool                           { return false }
func (f *fakeSender) GetCongestionWindow() congestion.ByteCount  { return f.window }

func TestLossGuardShrinksOnLoss(t *testing.T) {
	inner := &fakeSender{window: 10000}
	guard := newLossGuard(inner)

	acked := make([]congestion.AckedPacketInfo, 10)
	lost := make([]congestion.LostPacketInfo, 10)
	for index := 0; index < 5; index++ {
		guard.OnCongestionEventEx(0, 0, acked, lost)
	}

	if window := guard.GetCongestionWindow(); window >= 10000 {
		t.Fatalf("loss guard did not shrink the window: %d", window)
	}
}

func TestLossGuardKeepsWindowOnCleanPath(t *testing.T) {
	inner := &fakeSender{window: 10000}
	guard := newLossGuard(inner)

	acked := make([]congestion.AckedPacketInfo, 10)
	for index := 0; index < 5; index++ {
		guard.OnCongestionEventEx(0, 0, acked, nil)
	}

	if window := guard.GetCongestionWindow(); window != 10000 {
		t.Fatalf("clean path window = %d, want 10000", window)
	}
}

func TestParseProfile(t *testing.T) {
	profile, err := bbr.ParseProfile("aggressive")
	if err != nil {
		t.Fatal(err)
	}
	if profile != bbr.ProfileAggressive {
		t.Fatalf("profile = %q", profile)
	}
	if _, err := bbr.ParseProfile("nonsense"); err == nil {
		t.Fatal("an unknown profile must be rejected")
	}
}
