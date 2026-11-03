package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	quic "github.com/apernet/quic-go"

	"tianma/internal/proto"
)

const maxUDPDatagram = 1150

// UDPConn is a packet connection backed by QUIC datagrams.
type UDPConn struct {
	conn    *quic.Conn
	session uint32
	packet  atomic.Uint32

	mutex     sync.Mutex
	fragments map[uint16]*fragmentBuffer

	receive   chan receivedDatagram
	closed    chan struct{}
	closeOnce sync.Once
}

type receivedDatagram struct {
	payload []byte
	addr    proto.Addr
}

type fragmentBuffer struct {
	count    uint8
	parts    [][]byte
	received uint8
	total    int
}

// DialUDP opens a datagram session on the authenticated QUIC connection.
func (c *Client) DialUDP(ctx context.Context) (*UDPConn, error) {
	conn, err := c.session(ctx)
	if err != nil {
		return nil, err
	}

	var seed [4]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, err
	}

	udp := &UDPConn{
		conn:      conn,
		session:   binary.BigEndian.Uint32(seed[:]),
		fragments: make(map[uint16]*fragmentBuffer),
		receive:   make(chan receivedDatagram, 128),
		closed:    make(chan struct{}),
	}
	go udp.receiveLoop()
	return udp, nil
}

func (c *UDPConn) receiveLoop() {
	for {
		data, err := c.conn.ReceiveDatagram(context.Background())
		if err != nil {
			return
		}
		message, err := proto.ParseUDPMessage(data)
		if err != nil || message.Session != c.session {
			continue
		}
		if message.FragmentCount <= 1 {
			c.deliver(receivedDatagram{payload: message.Payload, addr: message.Addr})
			continue
		}
		c.collect(message)
	}
}

func (c *UDPConn) collect(message proto.UDPMessage) {
	c.mutex.Lock()

	buffer := c.fragments[message.Packet]
	if buffer == nil {
		buffer = &fragmentBuffer{
			count: message.FragmentCount,
			parts: make([][]byte, message.FragmentCount),
		}
		c.fragments[message.Packet] = buffer
	}
	if int(message.FragmentID) < len(buffer.parts) && buffer.parts[message.FragmentID] == nil {
		buffer.parts[message.FragmentID] = append([]byte(nil), message.Payload...)
		buffer.received++
		buffer.total += len(message.Payload)
	}

	if buffer.received < buffer.count {
		c.mutex.Unlock()
		return
	}

	payload := make([]byte, 0, buffer.total)
	for _, part := range buffer.parts {
		payload = append(payload, part...)
	}
	delete(c.fragments, message.Packet)
	c.mutex.Unlock()

	c.deliver(receivedDatagram{payload: payload, addr: message.Addr})
}

func (c *UDPConn) deliver(datagram receivedDatagram) {
	select {
	case c.receive <- datagram:
	case <-c.closed:
	default:
	}
}

// WriteTo fragments one datagram and sends it.
func (c *UDPConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	target, err := proto.AddrFromNet(addr)
	if err != nil {
		return 0, err
	}

	maxPayload := maxUDPDatagram - 9 - len(target.String())
	if maxPayload < 64 {
		maxPayload = 64
	}
	count := (len(p) + maxPayload - 1) / maxPayload
	if count == 0 {
		count = 1
	}
	if count > 255 {
		return 0, errors.New("tianma: datagram too large")
	}

	packet := uint16(c.packet.Add(1))
	for index := 0; index < count; index++ {
		end := (index + 1) * maxPayload
		if end > len(p) {
			end = len(p)
		}
		message := proto.UDPMessage{
			Session:       c.session,
			Packet:        packet,
			FragmentID:    uint8(index),
			FragmentCount: uint8(count),
			Addr:          target,
			Payload:       p[index*maxPayload : end],
		}
		data, err := proto.AppendUDPMessage(nil, message)
		if err != nil {
			return 0, err
		}
		if err := c.conn.SendDatagram(data); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// ReadFrom returns the next complete datagram.
func (c *UDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case datagram := <-c.receive:
		n := copy(p, datagram.payload)
		return n, datagram.addr.UDPAddr(), nil
	case <-c.closed:
		return 0, nil, io.ErrClosedPipe
	}
}

// Close stops the datagram session.
func (c *UDPConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return nil
}
