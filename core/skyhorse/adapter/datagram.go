package skyhorsebox

import (
	"net"
	"time"

	core "tianma/boxapi"
)

// packetConn adapts the client's datagram session to net.PacketConn.
type packetConn struct {
	*core.UDPConn
}

func (p *packetConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (p *packetConn) SetDeadline(t time.Time) error      { return nil }
func (p *packetConn) SetReadDeadline(t time.Time) error  { return nil }
func (p *packetConn) SetWriteDeadline(t time.Time) error { return nil }

var _ net.PacketConn = (*packetConn)(nil)
