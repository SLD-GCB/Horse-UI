package wire

import (
	"net"

	quic "github.com/apernet/quic-go"

	"tianma/internal/obfs"
)

// NewUDPTransport opens the UDP socket with larger buffers and wraps it with
// Salamander obfuscation when a key is configured.
func NewUDPTransport(address string, key []byte) (*quic.Transport, *net.UDPConn, error) {
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, nil, err
	}
	socket, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		return nil, nil, err
	}
	_ = socket.SetReadBuffer(4 << 20)
	_ = socket.SetWriteBuffer(4 << 20)

	var packetConn net.PacketConn = socket
	if len(key) > 0 {
		packetConn = obfs.New(socket, key)
	}
	return &quic.Transport{Conn: packetConn}, socket, nil
}

// NewUDPTransportRotating opens the UDP socket with time-rotating Salamander
// keys derived from the master secret.
func NewUDPTransportRotating(address string, master []byte, rotationSeconds, toleranceSeconds int) (*quic.Transport, *net.UDPConn, error) {
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, nil, err
	}
	socket, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		return nil, nil, err
	}
	_ = socket.SetReadBuffer(4 << 20)
	_ = socket.SetWriteBuffer(4 << 20)

	packetConn := obfs.NewRotating(socket, master, rotationSeconds, toleranceSeconds)
	return &quic.Transport{Conn: packetConn}, socket, nil
}
