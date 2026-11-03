package obfs

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/blake2b"
)

const rotationLabel = "tianma-obfs-rotate"

// RotatingPacketConn derives a fresh Salamander key every rotation window from
// a master secret. The frame carries the epoch, so a leaked per-window key
// expires with the window instead of exposing the link forever.
//
// Wire format: epoch(4) | salt(8) | ciphertext
type RotatingPacketConn struct {
	net.PacketConn
	master    []byte
	interval  time.Duration
	tolerance time.Duration
}

// NewRotating wraps conn with time-based key rotation.
func NewRotating(conn net.PacketConn, master []byte, rotationSeconds, toleranceSeconds int) *RotatingPacketConn {
	if rotationSeconds <= 0 {
		rotationSeconds = 3600
	}
	if toleranceSeconds < 0 {
		toleranceSeconds = 0
	}
	return &RotatingPacketConn{
		PacketConn: conn,
		master:     append([]byte(nil), master...),
		interval:   time.Duration(rotationSeconds) * time.Second,
		tolerance:  time.Duration(toleranceSeconds) * time.Second,
	}
}

func (c *RotatingPacketConn) currentEpoch() uint32 {
	return uint32(time.Now().Unix() / int64(c.interval/time.Second))
}

func (c *RotatingPacketConn) accepts(epoch uint32) bool {
	windows := uint32(c.tolerance / c.interval)
	current := c.currentEpoch()
	return epoch+windows >= current && epoch <= current+windows
}

func rotationKey(master []byte, epoch uint32) [32]byte {
	material := make([]byte, 0, len(master)+len(rotationLabel)+4)
	material = append(material, master...)
	material = append(material, rotationLabel...)
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], epoch)
	material = append(material, encoded[:]...)
	return blake2b.Sum256(material)
}

func (c *RotatingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	epoch := c.currentEpoch()
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return 0, err
	}
	key := rotationKey(c.master, epoch)

	frame := make([]byte, 12+len(p))
	binary.BigEndian.PutUint32(frame[0:4], epoch)
	copy(frame[4:12], salt)
	for index := range p {
		frame[12+index] = p[index] ^ key[index%32]
	}
	if _, err := c.PacketConn.WriteTo(frame, addr); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *RotatingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	buffer := make([]byte, 65535)
	for {
		n, addr, err := c.PacketConn.ReadFrom(buffer)
		if err != nil {
			return 0, nil, err
		}
		if n <= 12 {
			continue
		}

		epoch := binary.BigEndian.Uint32(buffer[:4])
		if !c.accepts(epoch) {
			continue
		}
		key := rotationKey(c.master, epoch)
		payload := buffer[12:n]
		for index := range payload {
			payload[index] ^= key[index%32]
		}
		if len(payload) > len(p) {
			return 0, nil, io.ErrShortBuffer
		}
		copy(p, payload)
		return len(payload), addr, nil
	}
}
