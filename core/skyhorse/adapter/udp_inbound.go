package skyhorsebox

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	quic "github.com/apernet/quic-go"
	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/buf"

	core "tianma/boxapi"
)

const (
	udpIdleTimeout = 90 * time.Second
	maxUDPDatagram = 1150
)

// udpHub owns the datagram side of one authenticated connection.
type udpHub struct {
	inbound *Inbound
	conn    *quic.Conn
	user    User
	source  M.Socksaddr

	mutex     sync.Mutex
	flows     map[uint32]*udpFlow
	fragments map[udpFragmentKey]*udpFragmentBuffer
	packet    atomic.Uint32
}

type udpFragmentKey struct {
	session uint32
	packet  uint16
}

type udpFragmentBuffer struct {
	count    uint8
	parts    [][]byte
	received uint8
	total    int
}

type udpDatagram struct {
	payload []byte
	addr    core.Addr
}

func newUDPHub(inbound *Inbound, conn *quic.Conn, user User, source M.Socksaddr) *udpHub {
	return &udpHub{
		inbound:   inbound,
		conn:      conn,
		user:      user,
		source:    source,
		flows:     make(map[uint32]*udpFlow),
		fragments: make(map[udpFragmentKey]*udpFragmentBuffer),
	}
}

func (h *udpHub) run() {
	go h.cleanup()
	for {
		data, err := h.conn.ReceiveDatagram(context.Background())
		if err != nil {
			h.closeAll()
			return
		}
		message, err := core.ParseUDPMessage(data)
		if err != nil {
			continue
		}
		if message.FragmentCount <= 1 {
			h.dispatch(message)
			continue
		}
		h.collect(message)
	}
}

func (h *udpHub) collect(message core.UDPMessage) {
	key := udpFragmentKey{session: message.Session, packet: message.Packet}

	h.mutex.Lock()
	buffer := h.fragments[key]
	if buffer == nil {
		buffer = &udpFragmentBuffer{count: message.FragmentCount, parts: make([][]byte, message.FragmentCount)}
		h.fragments[key] = buffer
	}
	if int(message.FragmentID) < len(buffer.parts) && buffer.parts[message.FragmentID] == nil {
		buffer.parts[message.FragmentID] = append([]byte(nil), message.Payload...)
		buffer.received++
		buffer.total += len(message.Payload)
	}
	if buffer.received < buffer.count {
		h.mutex.Unlock()
		return
	}
	payload := make([]byte, 0, buffer.total)
	for _, part := range buffer.parts {
		payload = append(payload, part...)
	}
	delete(h.fragments, key)
	h.mutex.Unlock()

	message.Payload = payload
	h.dispatch(message)
}

func (h *udpHub) dispatch(message core.UDPMessage) {
	ip, err := h.inbound.policy.Check(context.Background(), message.Addr.Host, message.Addr.Port)
	if err != nil {
		return
	}
	message.Addr = core.Addr{Host: ip.String(), Port: message.Addr.Port}

	if !h.inbound.quota.Allow(h.user.UUID) {
		return
	}
	size := int64(len(message.Payload))
	if !h.inbound.memory.Acquire(size) {
		return
	}
	defer h.inbound.memory.Release(size)
	if !h.inbound.quota.Add(h.user.UUID, size) {
		return
	}

	flow := h.flow(message.Session, message.Addr)
	if flow == nil {
		return
	}
	select {
	case flow.packets <- udpDatagram{payload: message.Payload, addr: message.Addr}:
	default:
	}
}

func (h *udpHub) flow(session uint32, destination core.Addr) *udpFlow {
	h.mutex.Lock()
	if existing, ok := h.flows[session]; ok {
		h.mutex.Unlock()
		return existing
	}
	if len(h.flows) >= h.inbound.guard.MaxUDPSessions() {
		h.mutex.Unlock()
		return nil
	}
	flow := &udpFlow{
		hub:     h,
		session: session,
		packets: make(chan udpDatagram, 128),
		closed:  make(chan struct{}),
	}
	h.flows[session] = flow
	h.mutex.Unlock()

	flow.touch()

	metadata := adapter.InboundContext{
		Inbound:     h.inbound.Tag(),
		InboundType: h.inbound.Type(),
		Source:      h.source,
		Destination: M.ParseSocksaddr(destination.String()),
		User:        h.user.Name,
	}
	go h.inbound.router.RoutePacketConnectionEx(context.Background(), flow, metadata, func(error) {
		flow.Close()
	})
	return flow
}

func (h *udpHub) remove(session uint32) {
	h.mutex.Lock()
	delete(h.flows, session)
	h.mutex.Unlock()
}

func (h *udpHub) closeAll() {
	h.mutex.Lock()
	flows := make([]*udpFlow, 0, len(h.flows))
	for _, flow := range h.flows {
		flows = append(flows, flow)
	}
	h.flows = make(map[uint32]*udpFlow)
	h.mutex.Unlock()
	for _, flow := range flows {
		flow.Close()
	}
}

func (h *udpHub) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-udpIdleTimeout).UnixNano()
		h.mutex.Lock()
		var expired []*udpFlow
		for session, flow := range h.flows {
			if flow.last.Load() < cutoff {
				expired = append(expired, flow)
				delete(h.flows, session)
			}
		}
		h.mutex.Unlock()
		for _, flow := range expired {
			flow.Close()
		}
	}
}

func (h *udpHub) send(session uint32, destination core.Addr, payload []byte) error {
	maxPayload := maxUDPDatagram - 9 - len(destination.String())
	if maxPayload < 64 {
		maxPayload = 64
	}
	count := (len(payload) + maxPayload - 1) / maxPayload
	if count == 0 {
		count = 1
	}
	if count > 255 {
		return io.ErrShortBuffer
	}
	packet := uint16(h.packet.Add(1))
	for index := 0; index < count; index++ {
		end := (index + 1) * maxPayload
		if end > len(payload) {
			end = len(payload)
		}
		message := core.UDPMessage{
			Session:       session,
			Packet:        packet,
			FragmentID:    uint8(index),
			FragmentCount: uint8(count),
			Addr:          destination,
			Payload:       payload[index*maxPayload : end],
		}
		data, err := core.AppendUDPMessage(nil, message)
		if err != nil {
			return err
		}
		if err := h.conn.SendDatagram(data); err != nil {
			return err
		}
	}
	return nil
}

// udpFlow is one client UDP session, exposed to sing-box as a PacketConn.
type udpFlow struct {
	hub     *udpHub
	session uint32
	packets chan udpDatagram
	closed  chan struct{}
	once    sync.Once
	last    atomic.Int64
}

func (f *udpFlow) touch() { f.last.Store(time.Now().UnixNano()) }

func (f *udpFlow) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	select {
	case datagram := <-f.packets:
		f.touch()
		_, _ = buffer.Write(datagram.payload)
		return M.ParseSocksaddr(datagram.addr.String()), nil
	case <-f.closed:
		return M.Socksaddr{}, io.EOF
	}
}

func (f *udpFlow) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	f.touch()
	address, err := core.ParseAddr(destination.String())
	if err != nil {
		return err
	}
	return f.hub.send(f.session, address, buffer.Bytes())
}

func (f *udpFlow) Close() error {
	f.once.Do(func() {
		close(f.closed)
		f.hub.remove(f.session)
	})
	return nil
}

func (f *udpFlow) LocalAddr() net.Addr                { return f.hub.conn.LocalAddr() }
func (f *udpFlow) SetDeadline(t time.Time) error      { return nil }
func (f *udpFlow) SetReadDeadline(t time.Time) error  { return nil }
func (f *udpFlow) SetWriteDeadline(t time.Time) error { return nil }

func (h *udpHub) start() { go h.run() }
