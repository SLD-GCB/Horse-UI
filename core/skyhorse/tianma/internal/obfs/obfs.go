// Package obfs implements Hysteria's Salamander obfuscation over a UDP socket.
package obfs

import (
	"crypto/rand"
	"errors"
	"io"
	"net"

	"golang.org/x/crypto/blake2b"
)

// PacketConn wraps a UDP socket and XORs every packet with a salted BLAKE2b
// hash. The salt travels in front of the packet.
type PacketConn struct {
	net.PacketConn
	key []byte
}

// New wraps conn with the given pre-shared key.
func New(conn net.PacketConn, key []byte) *PacketConn {
	return &PacketConn{PacketConn: conn, key: key}
}

func (c *PacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return 0, err
	}
	hash := saltedHash(c.key, salt)

	frame := make([]byte, 8+len(p))
	copy(frame, salt)
	for index := range p {
		frame[8+index] = p[index] ^ hash[index%32]
	}
	if _, err := c.PacketConn.WriteTo(frame, addr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *PacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buffer := make([]byte, 65535)
	n, addr, err := c.PacketConn.ReadFrom(buffer)
	if err != nil {
		return 0, nil, err
	}
	if n <= 8 {
		return 0, nil, errors.New("obfs: short packet")
	}

	salt := buffer[:8]
	hash := saltedHash(c.key, salt)
	payload := buffer[8:n]
	for index := range payload {
		payload[index] ^= hash[index%32]
	}
	if len(payload) > len(p) {
		return 0, nil, io.ErrShortBuffer
	}
	copy(p, payload)
	return len(payload), addr, nil
}

func saltedHash(key, salt []byte) [32]byte {
	material := make([]byte, 0, len(key)+len(salt))
	material = append(material, key...)
	material = append(material, salt...)
	return blake2b.Sum256(material)
}
