package proto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// UDPMessage is one datagram or fragment. It is carried in a QUIC datagram.
type UDPMessage struct {
	Session       uint32
	Packet        uint16
	FragmentID    uint8
	FragmentCount uint8
	Addr          Addr
	Payload       []byte
}

// AppendUDPMessage encodes msg.
func AppendUDPMessage(dst []byte, msg UDPMessage) ([]byte, error) {
	encoded := msg.Addr.String()
	if len(encoded) == 0 || len(encoded) > 255 {
		return nil, fmt.Errorf("tianma: bad udp address length %d", len(encoded))
	}
	if msg.FragmentCount == 0 {
		msg.FragmentCount = 1
	}
	dst = binary.BigEndian.AppendUint32(dst, msg.Session)
	dst = binary.BigEndian.AppendUint16(dst, msg.Packet)
	dst = append(dst, msg.FragmentID, msg.FragmentCount, byte(len(encoded)))
	dst = append(dst, encoded...)
	dst = append(dst, msg.Payload...)
	return dst, nil
}

// ParseUDPMessage decodes one datagram.
func ParseUDPMessage(buffer []byte) (UDPMessage, error) {
	if len(buffer) < 9 {
		return UDPMessage{}, errors.New("tianma: short udp message")
	}
	message := UDPMessage{
		Session:       binary.BigEndian.Uint32(buffer[0:4]),
		Packet:        binary.BigEndian.Uint16(buffer[4:6]),
		FragmentID:    buffer[6],
		FragmentCount: buffer[7],
	}
	addressLength := int(buffer[8])
	if len(buffer) < 9+addressLength {
		return UDPMessage{}, errors.New("tianma: truncated udp address")
	}
	address, err := ParseAddr(string(buffer[9 : 9+addressLength]))
	if err != nil {
		return UDPMessage{}, err
	}
	message.Addr = address
	message.Payload = buffer[9+addressLength:]
	return message, nil
}

// AddrFromNet converts a net.Addr into a tunnel address.
func AddrFromNet(addr net.Addr) (Addr, error) {
	switch value := addr.(type) {
	case *net.UDPAddr:
		return ParseAddr(net.JoinHostPort(value.IP.String(), strconv.Itoa(value.Port)))
	default:
		return ParseAddr(addr.String())
	}
}

// UDPAddr converts a tunnel address into a *net.UDPAddr.
func (a Addr) UDPAddr() *net.UDPAddr {
	host := a.Host
	if ip, err := netip.ParseAddr(host); err == nil {
		return &net.UDPAddr{IP: net.IP(ip.AsSlice()), Port: int(a.Port)}
	}
	return &net.UDPAddr{IP: net.IPv4zero, Port: int(a.Port)}
}
