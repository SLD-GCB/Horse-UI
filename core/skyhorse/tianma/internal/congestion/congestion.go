// Package congestion chooses the send algorithm for a SkyHorse connection.
package congestion

import (
	quic "github.com/apernet/quic-go"
	"github.com/apernet/quic-go/congestion"

	"tianma/internal/congestion/bbr"
	"tianma/internal/congestion/brutal"
)

// UseBrutal installs the adaptive fixed-rate sender.
func UseBrutal(conn *quic.Conn, bytesPerSecond uint64, disableLossCompensation bool) *brutal.BrutalSender {
	sender := brutal.NewBrutalSender(bytesPerSecond, disableLossCompensation)
	conn.SetCongestionControl(sender)
	return sender
}

// UseBBR installs BBR with the loss-aware guard.
func UseBBR(conn *quic.Conn, profileName string) congestion.CongestionControl {
	profile, err := bbr.ParseProfile(profileName)
	if err != nil {
		profile = bbr.ProfileStandard
	}
	seed := bbr.GetInitialPacketSize(conn.RemoteAddr())
	if quicSize := conn.InitialPacketSize(); quicSize > 0 && quicSize < seed {
		seed = quicSize
	}
	sender := bbr.NewBbrSender(bbr.DefaultClock{}, seed, profile)
	guard := newLossGuard(sender)
	conn.SetCongestionControl(guard)
	return guard
}
