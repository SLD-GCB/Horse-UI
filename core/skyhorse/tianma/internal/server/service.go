package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	quic "github.com/apernet/quic-go"
	"github.com/apernet/quic-go/http3"
	"github.com/apernet/quic-go/quicvarint"

	"tianma/internal/congestion"
	"tianma/internal/guard"
	"tianma/internal/policy"
	"tianma/internal/proto"
	"tianma/internal/wire"
)

// User is one account.
type User struct {
	Name     string `json:"name"`
	UUID     string `json:"uuid"`
	Password string `json:"password"`
}

// Config is the server configuration.
type Config struct {
	Listen          string `json:"listen"`
	Port            int    `json:"port"`
	CertificatePath string `json:"certificate_path"`
	KeyPath         string `json:"key_path"`
	ObfsPassword    string `json:"obfs_password"`
	ObfsRotationSeconds  int `json:"obfs_rotation_seconds"`
	ObfsToleranceSeconds int `json:"obfs_tolerance_seconds"`
	UpMbps          int    `json:"up_mbps"`
	DownMbps        int    `json:"down_mbps"`

	IgnoreClientBandwidth   bool `json:"ignore_client_bandwidth"`
	DisableLossCompensation bool `json:"disable_loss_compensation"`

	Congestion string `json:"congestion"`
	BBRProfile string `json:"bbr_profile"`

	Users []User `json:"users"`

	Policy policy.Config `json:"policy"`
	Guard  guard.Config  `json:"guard"`

	Quota       guard.QuotaConfig `json:"quota"`
	MaxMemoryMB int               `json:"max_memory_mb"`

	Masquerade MasqueradeConfig `json:"masquerade"`
}

// Server accepts QUIC connections and serves both a web site and the tunnel.
type Server struct {
	address   string
	tlsConfig *tls.Config
	users     map[string]User
	obfs      []byte
	obfsRotation  int
	obfsTolerance int

	maxTxBps                uint64
	maxRxBps                uint64
	ignoreClientBandwidth   bool
	disableLossCompensation bool
	bbrProfile              string
	policy                  *policy.Policy
	guard                   *guard.Guard
	quota                   *guard.Quota
	memory                  *guard.Memory
	masquerade              http.Handler
}

// New builds a server from the configuration.
func New(config Config) (*Server, error) {
	if config.CertificatePath == "" || config.KeyPath == "" {
		return nil, errors.New("tianma: certificate_path and key_path are required")
	}
	tlsConfig, err := wire.ServerTLS(config.CertificatePath, config.KeyPath)
	if err != nil {
		return nil, err
	}
	if config.ObfsPassword == "" {
		if selfSigned, err := wire.SelfSigned(config.CertificatePath); err == nil && selfSigned {
			log.Printf("tianma: WARNING self-signed certificate without obfs; any prober can see it")
		}
	}
	users := make(map[string]User, len(config.Users))
	for _, user := range config.Users {
		if user.UUID == "" || user.Password == "" {
			continue
		}
		users[user.UUID] = user
	}
	host := config.Listen
	if host == "" {
		host = "::"
	}
	profile := config.BBRProfile
	if profile == "" {
		profile = "standard"
	}
	compiledPolicy, err := policy.New(config.Policy)
	if err != nil {
		return nil, err
	}
	masquerade, err := NewMasquerade(config.Masquerade)
	if err != nil {
		return nil, err
	}
	return &Server{
		address:                 net.JoinHostPort(host, strconv.Itoa(config.Port)),
		tlsConfig:               tlsConfig,
		users:                   users,
		obfs:                    []byte(config.ObfsPassword),
		obfsRotation:            config.ObfsRotationSeconds,
		obfsTolerance:           config.ObfsToleranceSeconds,
		maxTxBps:                uint64(config.DownMbps) * 125000,
		maxRxBps:                uint64(config.UpMbps) * 125000,
		ignoreClientBandwidth:   config.IgnoreClientBandwidth,
		disableLossCompensation: config.DisableLossCompensation,
		bbrProfile:              profile,
		policy:                  compiledPolicy,
		guard:                   guard.New(config.Guard),
		quota:                   guard.NewQuota(config.Quota),
		memory:                  guard.NewMemory(int64(config.MaxMemoryMB) << 20),
		masquerade:              masquerade,
	}, nil
}

// Run listens until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	var err error
	var transport *quic.Transport
	var socket *net.UDPConn
	if s.obfsRotation > 0 && len(s.obfs) > 0 {
		transport, socket, err = wire.NewUDPTransportRotating(s.address, s.obfs, s.obfsRotation, s.obfsTolerance)
	} else {
		transport, socket, err = wire.NewUDPTransport(s.address, s.obfs)
	}
	if err != nil {
		return err
	}
	defer transport.Close()
	defer socket.Close()

	listener, err := transport.Listen(s.tlsConfig, wire.QUICConfig())
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Printf("tianma: quic server listening on %s", s.address)
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return nil
		}
		ip := remoteIP(conn.RemoteAddr())
		release, allowed := s.guard.AllowConnection(ip)
		if !allowed {
			_ = conn.CloseWithError(0x1, "too many connections")
			continue
		}
		go func() {
			defer release()
			s.handle(conn)
		}()
	}
}

func remoteIP(address net.Addr) string {
	if address == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return address.String()
	}
	return host
}

// relayConn adds net.Conn addressing to a QUIC stream.
type relayConn struct {
	*quic.Stream
	conn *quic.Conn
}

func (c *relayConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *relayConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func (s *Server) handle(conn *quic.Conn) {
	handler := &h3Handler{server: s, conn: conn}
	h3 := &http3.Server{
		Handler:          handler,
		StreamDispatcher: handler.dispatch,
	}
	err := h3.ServeQUICConn(conn)
	if err != nil {
		log.Printf("tianma: connection closed: %v", err)
	}
	_ = conn.CloseWithError(0, "")
}

type h3Handler struct {
	server *Server
	conn   *quic.Conn

	mutex         sync.Mutex
	authenticated bool
	user          User
	udpOnce       sync.Once
	streams       atomic.Int64
}

func (h *h3Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Host != proto.URLHost || request.URL.Path != proto.URLPath {
		h.masquerade(writer, request)
		return
	}

	ip := remoteIP(h.conn.RemoteAddr())
	if h.server.guard.AuthBlocked(ip) {
		h.masquerade(writer, request)
		return
	}

	uuid, token, err := proto.DecodeToken(request.Header.Get(proto.HeaderAuth))
	if err != nil {
		h.server.guard.RecordAuthFailure(ip)
		h.masquerade(writer, request)
		return
	}
	user, known := h.server.users[uuid]
	if !known {
		h.server.guard.RecordAuthFailure(ip)
		h.masquerade(writer, request)
		return
	}

	state := h.conn.ConnectionState()
	expected, err := wire.Token(&state.TLS, uuid, user.Password)
	if err != nil || subtle.ConstantTimeCompare(token, expected) != 1 {
		h.server.guard.RecordAuthFailure(ip)
		h.masquerade(writer, request)
		return
	}
	h.server.guard.ResetAuthFailures(ip)

	h.mutex.Lock()
	h.authenticated = true
	h.user = user
	h.mutex.Unlock()

	applied := false
	if !h.server.ignoreClientBandwidth {
		tx := uint64(0)
		if value := request.Header.Get(proto.HeaderCCRX); value != "" {
			if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
				tx = parsed
			}
		}
		if h.server.maxTxBps > 0 && (tx == 0 || tx > h.server.maxTxBps) {
			tx = h.server.maxTxBps
		}
		if tx > 0 {
			congestion.UseBrutal(h.conn, tx, h.server.disableLossCompensation)
			applied = true
		}
	}
	if !applied {
		congestion.UseBBR(h.conn, h.server.bbrProfile)
	}
	if h.server.maxRxBps > 0 {
		writer.Header().Set(proto.HeaderCCRX, strconv.FormatUint(h.server.maxRxBps, 10))
	}

	writer.Header().Set(proto.HeaderPadding, "0")
	writer.WriteHeader(proto.StatusAuthOK)
	log.Printf("tianma: user %s authenticated", user.Name)

	h.udpOnce.Do(func() {
		go newUDPManager(h.conn, h.server.policy, h.server.guard.MaxUDPSessions(), h.server.quota, h.server.memory, user.UUID).run(context.Background())
	})
}

// masquerade serves whatever the operator configured for unauthenticated traffic.
func (h *h3Handler) masquerade(writer http.ResponseWriter, request *http.Request) {
	h.server.masquerade.ServeHTTP(writer, request)
}

// dispatch is called after peeking the first frame type of a stream. A stream
// that starts with TCPRequest becomes a relay; everything else stays HTTP/3.
func (h *h3Handler) dispatch(frameType http3.FrameType, stream *quic.Stream, err error) (bool, error) {
	if err != nil {
		return false, nil
	}
	h.mutex.Lock()
	authenticated := h.authenticated
	h.mutex.Unlock()
	if !authenticated || uint64(frameType) != proto.FrameTypeTCPRequest {
		return false, nil
	}

	// The dispatcher only peeked the frame type; consume it so ReadTCPRequest
	// starts at the address length.
	if _, err := quicvarint.Read(quicvarint.NewReader(stream)); err != nil {
		return true, err
	}
	if h.streams.Load() >= int64(h.server.guard.MaxStreams()) {
		_ = stream.Close()
		return true, nil
	}
	h.streams.Add(1)
	go func() {
		defer h.streams.Add(-1)
		h.relay(stream)
	}()
	return true, nil
}

func (h *h3Handler) relay(stream *quic.Stream) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("tianma: panic in relay: %v", recovered)
			_ = stream.Close()
		}
	}()

	h.mutex.Lock()
	user := h.user
	h.mutex.Unlock()

	address, err := proto.ReadTCPRequest(stream)
	if err != nil {
		_ = stream.Close()
		return
	}

	if !h.server.quota.Allow(user.UUID) {
		_, _ = stream.Write([]byte{proto.StatusErrorByte})
		_ = stream.Close()
		return
	}
	const relayMemory = 64 << 10
	if !h.server.memory.Acquire(relayMemory) {
		_, _ = stream.Write([]byte{proto.StatusErrorByte})
		_ = stream.Close()
		return
	}
	defer h.server.memory.Release(relayMemory)

	destination, err := h.server.policy.Check(context.Background(), address.Host, address.Port)
	if err != nil {
		_, _ = stream.Write([]byte{proto.StatusErrorByte})
		_ = stream.Close()
		return
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	target, err := dialer.Dial("tcp", net.JoinHostPort(destination.String(), strconv.Itoa(int(address.Port))))
	if err != nil {
		_, _ = stream.Write([]byte{proto.StatusErrorByte})
		_ = stream.Close()
		return
	}
	defer target.Close()

	if _, err := stream.Write([]byte{proto.StatusOKByte}); err != nil {
		_ = stream.Close()
		return
	}

	relay := h.server.quota.Wrap(user.UUID, &relayConn{Stream: stream, conn: h.conn})

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(target, relay)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(relay, target)
		done <- struct{}{}
	}()
	<-done
	_ = stream.Close()
	_ = target.Close()
	<-done
}
