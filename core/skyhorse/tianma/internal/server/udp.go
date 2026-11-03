package server

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	quic "github.com/apernet/quic-go"

	"tianma/internal/guard"
	"tianma/internal/policy"
	"tianma/internal/proto"
)

const (
	udpIdleTimeout = 90 * time.Second
	maxUDPDatagram = 1150
)

// udpManager owns the QUIC datagram side of one authenticated connection.
type udpManager struct {
	conn        *quic.Conn
	policy      *policy.Policy
	maxSessions int
	quota       *guard.Quota
	memory      *guard.Memory
	user        string

	mutex     sync.Mutex
	sessions  map[uint32]*udpSession
	fragments map[fragmentKey]*fragmentBuffer
}

type fragmentKey struct {
	session uint32
	packet  uint16
}

type fragmentBuffer struct {
	count    uint8
	parts    [][]byte
	received uint8
	total    int
}

func newUDPManager(conn *quic.Conn, compiledPolicy *policy.Policy, maxSessions int, quota *guard.Quota, memory *guard.Memory, user string) *udpManager {
	return &udpManager{
		conn:        conn,
		policy:      compiledPolicy,
		maxSessions: maxSessions,
		quota:       quota,
		memory:      memory,
		user:        user,
		sessions:    make(map[uint32]*udpSession),
		fragments:   make(map[fragmentKey]*fragmentBuffer),
	}
}

func (m *udpManager) run(ctx context.Context) {
	go m.cleanup(ctx)
	for {
		data, err := m.conn.ReceiveDatagram(ctx)
		if err != nil {
			m.closeAll()
			return
		}
		message, err := proto.ParseUDPMessage(data)
		if err != nil {
			continue
		}
		session := m.session(message.Session)
		if session == nil {
			continue
		}
		if message.FragmentCount <= 1 {
			session.deliver(message)
			continue
		}
		m.collect(session, message)
	}
}

// collect reassembles fragments from the client before sending them onward.
func (m *udpManager) collect(session *udpSession, message proto.UDPMessage) {
	key := fragmentKey{session: message.Session, packet: message.Packet}

	m.mutex.Lock()
	buffer := m.fragments[key]
	if buffer == nil {
		buffer = &fragmentBuffer{count: message.FragmentCount, parts: make([][]byte, message.FragmentCount)}
		m.fragments[key] = buffer
	}
	if int(message.FragmentID) < len(buffer.parts) && buffer.parts[message.FragmentID] == nil {
		buffer.parts[message.FragmentID] = append([]byte(nil), message.Payload...)
		buffer.received++
		buffer.total += len(message.Payload)
	}
	if buffer.received < buffer.count {
		m.mutex.Unlock()
		return
	}
	payload := make([]byte, 0, buffer.total)
	for _, part := range buffer.parts {
		payload = append(payload, part...)
	}
	delete(m.fragments, key)
	m.mutex.Unlock()

	message.Payload = payload
	session.deliver(message)
}

func (m *udpManager) session(id uint32) *udpSession {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if existing, ok := m.sessions[id]; ok {
		existing.touch()
		return existing
	}
	if m.maxSessions > 0 && len(m.sessions) >= m.maxSessions {
		return nil
	}

	socket, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil
	}
	_ = socket.SetReadBuffer(4 << 20)
	_ = socket.SetWriteBuffer(4 << 20)

	session := &udpSession{id: id, manager: m, socket: socket}
	session.touch()
	m.sessions[id] = session
	go session.readLoop()
	return session
}

func (m *udpManager) remove(id uint32) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if session, ok := m.sessions[id]; ok {
		_ = session.socket.Close()
		delete(m.sessions, id)
	}
}

func (m *udpManager) closeAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for id, session := range m.sessions {
		_ = session.socket.Close()
		delete(m.sessions, id)
	}
}

func (m *udpManager) cleanup(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		cutoff := time.Now().Add(-udpIdleTimeout).UnixNano()
		m.mutex.Lock()
		for id, session := range m.sessions {
			if session.last.Load() < cutoff {
				_ = session.socket.Close()
				delete(m.sessions, id)
			}
		}
		m.mutex.Unlock()
	}
}

type udpSession struct {
	id      uint32
	manager *udpManager
	socket  *net.UDPConn
	last    atomic.Int64
}

func (s *udpSession) touch() { s.last.Store(time.Now().UnixNano()) }

func (s *udpSession) deliver(message proto.UDPMessage) {
	s.touch()
	if !s.manager.quota.Allow(s.manager.user) {
		return
	}
	size := int64(len(message.Payload))
	if !s.manager.memory.Acquire(size) {
		return
	}
	defer s.manager.memory.Release(size)
	if !s.manager.quota.Add(s.manager.user, size) {
		return
	}
	ip, err := s.manager.policy.Check(context.Background(), message.Addr.Host, message.Addr.Port)
	if err != nil {
		return
	}
	target := net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, message.Addr.Port))
	_, _ = s.socket.WriteToUDP(message.Payload, target)
}

func (s *udpSession) readLoop() {
	buffer := make([]byte, 65535)
	var packet uint32
	for {
		_ = s.socket.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, source, err := s.socket.ReadFromUDP(buffer)
		if err != nil {
			s.manager.remove(s.id)
			return
		}
		s.touch()
		packet = (packet + 1) & 0xffff
		s.fragment(packet, source, buffer[:n])
	}
}

func (s *udpSession) fragment(packet uint32, source *net.UDPAddr, payload []byte) {
	address, err := proto.AddrFromNet(source)
	if err != nil {
		return
	}
	maxPayload := maxUDPDatagram - 9 - len(address.String())
	if maxPayload < 64 {
		maxPayload = 64
	}
	count := (len(payload) + maxPayload - 1) / maxPayload
	if count == 0 {
		count = 1
	}
	if count > 255 {
		return
	}
	for index := 0; index < count; index++ {
		end := (index + 1) * maxPayload
		if end > len(payload) {
			end = len(payload)
		}
		message := proto.UDPMessage{
			Session:       s.id,
			Packet:        uint16(packet),
			FragmentID:    uint8(index),
			FragmentCount: uint8(count),
			Addr:          address,
			Payload:       payload[index*maxPayload : end],
		}
		data, err := proto.AppendUDPMessage(nil, message)
		if err != nil {
			return
		}
		if err := s.manager.conn.SendDatagram(data); err != nil {
			return
		}
	}
}
