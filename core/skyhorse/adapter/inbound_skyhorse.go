package skyhorsebox

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"

	quic "github.com/apernet/quic-go"
	"github.com/apernet/quic-go/http3"
	"github.com/apernet/quic-go/quicvarint"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"

	core "tianma/boxapi"
	
)

// RegisterInbound adds the protocol to an inbound registry.
func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[InboundOptions](registry, Type, NewInbound)
}

// Inbound is a SkyHorse server node inside the kernel.
type Inbound struct {
	inbound.Adapter
	logger     log.ContextLogger
	router     adapter.Router
	users      map[string]User
	tlsConfig  *tls.Config
	obfs       []byte
	listenAddr string
	obfsRotation  int
	obfsTolerance int

	maxTxBps                uint64
	maxRxBps                uint64
	ignoreClientBandwidth   bool
	disableLossCompensation bool
	bbrProfile              string
	policy                  *core.Policy
	guard                   *core.Guard
	quota                   *core.Quota
	memory                  *core.Memory
	masquerade              http.Handler

	transport *quic.Transport
	socket    *net.UDPConn
	listener  *quic.Listener
	cancel    context.CancelFunc
}

// NewInbound builds the node without binding anything.
func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options InboundOptions) (adapter.Inbound, error) {
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, E.New("skyhorse: tls is required")
	}
	if options.TLS.CertificatePath == "" || options.TLS.KeyPath == "" {
		return nil, E.New("skyhorse: tls certificate_path and key_path are required")
	}
	tlsConfig, err := core.ServerTLS(options.TLS.CertificatePath, options.TLS.KeyPath)
	if err != nil {
		return nil, E.Cause(err, "skyhorse: load tls")
	}

	if options.ObfsPassword == "" {
		if selfSigned, err := core.SelfSigned(options.TLS.CertificatePath); err == nil && selfSigned {
			logger.Warn("skyhorse: self-signed certificate without obfs; probers can see it")
		}
	}

	users := make(map[string]User, len(options.Users))
	for _, user := range options.Users {
		if user.UUID == "" || user.Password == "" {
			logger.Warn("skyhorse: skipping user ", user.Name, ": empty uuid or password")
			continue
		}
		users[user.UUID] = user
	}

	host := "::"
	if options.Listen != nil {
		host = options.Listen.Build(netip.IPv6Unspecified()).String()
	}

	compiledPolicy, err := core.NewPolicy(options.Policy)
	if err != nil {
		return nil, E.Cause(err, "skyhorse: policy")
	}
	masquerade, err := core.NewMasquerade(options.Masquerade)
	if err != nil {
		return nil, E.Cause(err, "skyhorse: masquerade")
	}

	return &Inbound{
		Adapter:    inbound.NewAdapter(Type, tag),
		logger:     logger,
		router:     router,
		users:      users,
		tlsConfig:  tlsConfig,
		obfs:       []byte(options.ObfsPassword),
		obfsRotation:  options.ObfsRotationSeconds,
		obfsTolerance: options.ObfsToleranceSeconds,
		listenAddr: net.JoinHostPort(host, strconv.Itoa(int(options.ListenPort))),

		maxTxBps:                uint64(options.DownMbps) * 125000,
		maxRxBps:                uint64(options.UpMbps) * 125000,
		ignoreClientBandwidth:   options.IgnoreClientBandwidth,
		disableLossCompensation: options.DisableLossCompensation,
		bbrProfile:              options.BBRProfile,
		policy:                  compiledPolicy,
		guard:                   core.NewGuard(options.Guard),
		quota:                   core.NewQuota(options.Quota),
		memory:                  core.NewMemory(int64(options.MaxMemoryMB) << 20),
		masquerade:              masquerade,
	}, nil
}

// Start binds the QUIC listener.
func (h *Inbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}

	var err error
	var transport *quic.Transport
	var socket *net.UDPConn
	if h.obfsRotation > 0 && len(h.obfs) > 0 {
		transport, socket, err = core.NewUDPTransportRotating(h.listenAddr, h.obfs, h.obfsRotation, h.obfsTolerance)
	} else {
		transport, socket, err = core.NewUDPTransport(h.listenAddr, h.obfs)
	}
	if err != nil {
		return E.Cause(err, "skyhorse: listen quic")
	}
	listener, err := transport.Listen(h.tlsConfig, core.QUICConfig())
	if err != nil {
		_ = transport.Close()
		_ = socket.Close()
		return E.Cause(err, "skyhorse: listen quic")
	}

	h.transport = transport
	h.socket = socket
	h.listener = listener

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	scope.Add(func() error { return h.Close() })

	go h.accept(ctx)
	return nil
}

// Close releases the listener and the socket.
func (h *Inbound) Close() error {
	if h.cancel != nil {
		h.cancel()
	}
	if h.listener != nil {
		_ = h.listener.Close()
		h.listener = nil
	}
	if h.transport != nil {
		_ = h.transport.Close()
		h.transport = nil
	}
	if h.socket != nil {
		_ = h.socket.Close()
		h.socket = nil
	}
	return nil
}

func (h *Inbound) accept(ctx context.Context) {
	for {
		conn, err := h.listener.Accept(ctx)
		if err != nil {
			return
		}
		ip := remoteIP(conn.RemoteAddr())
		release, allowed := h.guard.AllowConnection(ip)
		if !allowed {
			_ = conn.CloseWithError(0x1, "too many connections")
			continue
		}
		go func() {
			defer release()
			h.handle(conn)
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

func (h *Inbound) handle(conn *quic.Conn) {
	handler := &h3Handler{inbound: h, conn: conn}
	h3 := &http3.Server{
		Handler:          handler,
		StreamDispatcher: handler.dispatch,
	}
	_ = h3.ServeQUICConn(conn)
	_ = conn.CloseWithError(0, "")
}

type h3Handler struct {
	inbound *Inbound
	conn    *quic.Conn

	mutex         sync.Mutex
	authenticated bool
	user          User
	udpOnce       sync.Once
	streams       atomic.Int64
}

func (h *h3Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Host != core.URLHost || request.URL.Path != core.URLPath {
		h.masquerade(writer, request)
		return
	}

	ip := remoteIP(h.conn.RemoteAddr())
	if h.inbound.guard.AuthBlocked(ip) {
		h.masquerade(writer, request)
		return
	}

	uuid, token, err := core.DecodeToken(request.Header.Get(core.HeaderAuth))
	if err != nil {
		h.inbound.guard.RecordAuthFailure(ip)
		h.masquerade(writer, request)
		return
	}
	user, known := h.inbound.users[uuid]
	if !known {
		h.inbound.guard.RecordAuthFailure(ip)
		h.masquerade(writer, request)
		return
	}

	state := h.conn.ConnectionState()
	expected, err := core.Token(&state.TLS, uuid, user.Password)
	if err != nil || subtle.ConstantTimeCompare(token, expected) != 1 {
		h.inbound.guard.RecordAuthFailure(ip)
		h.masquerade(writer, request)
		return
	}
	h.inbound.guard.ResetAuthFailures(ip)

	h.mutex.Lock()
	h.authenticated = true
	h.user = user
	h.mutex.Unlock()

	applied := false
	if !h.inbound.ignoreClientBandwidth {
		tx := uint64(0)
		if value := request.Header.Get(core.HeaderCCRX); value != "" {
			if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
				tx = parsed
			}
		}
		if h.inbound.maxTxBps > 0 && (tx == 0 || tx > h.inbound.maxTxBps) {
			tx = h.inbound.maxTxBps
		}
		if tx > 0 {
			core.UseBrutal(h.conn, tx, h.inbound.disableLossCompensation)
			applied = true
		}
	}
	if !applied {
		core.UseBBR(h.conn, h.inbound.bbrProfile)
	}
	if h.inbound.maxRxBps > 0 {
		writer.Header().Set(core.HeaderCCRX, strconv.FormatUint(h.inbound.maxRxBps, 10))
	}

	writer.WriteHeader(core.StatusAuthOK)
	h.inbound.logger.Info("skyhorse: user ", user.Name, " authenticated")

	h.udpOnce.Do(func() {
		hub := newUDPHub(h.inbound, h.conn, user, M.SocksaddrFromNet(h.conn.RemoteAddr()))
		hub.start()
	})
}

func (h *h3Handler) masquerade(writer http.ResponseWriter, request *http.Request) {
	h.inbound.masquerade.ServeHTTP(writer, request)
}

func (h *h3Handler) dispatch(frameType http3.FrameType, stream *quic.Stream, err error) (bool, error) {
	if err != nil {
		return false, nil
	}
	h.mutex.Lock()
	authenticated := h.authenticated
	user := h.user
	h.mutex.Unlock()
	if !authenticated || uint64(frameType) != core.FrameTypeTCPRequest {
		return false, nil
	}

	if _, err := quicvarint.Read(quicvarint.NewReader(stream)); err != nil {
		return true, err
	}
	if h.streams.Load() >= int64(h.inbound.guard.MaxStreams()) {
		_ = stream.Close()
		return true, nil
	}
	h.streams.Add(1)
	go func() {
		defer h.streams.Add(-1)
		h.relay(stream, user)
	}()
	return true, nil
}

func (h *h3Handler) relay(stream *quic.Stream, user User) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = stream.Close()
		}
	}()

	address, err := core.ReadTCPRequest(stream)
	if err != nil {
		_ = stream.Close()
		return
	}

	ip, err := h.inbound.policy.Check(context.Background(), address.Host, address.Port)
	if err != nil {
		_, _ = stream.Write([]byte{core.StatusErrorByte})
		_ = stream.Close()
		return
	}
	address = core.Addr{Host: ip.String(), Port: address.Port}

	if !h.inbound.quota.Allow(user.UUID) {
		_, _ = stream.Write([]byte{core.StatusErrorByte})
		_ = stream.Close()
		return
	}
	const relayMemory = 64 << 10
	if !h.inbound.memory.Acquire(relayMemory) {
		_, _ = stream.Write([]byte{core.StatusErrorByte})
		_ = stream.Close()
		return
	}
	defer h.inbound.memory.Release(relayMemory)

	if _, err := stream.Write([]byte{core.StatusOKByte}); err != nil {
		_ = stream.Close()
		return
	}

	metadata := adapter.InboundContext{
		Inbound:     h.inbound.Tag(),
		InboundType: h.inbound.Type(),
		Source:      M.SocksaddrFromNet(h.conn.RemoteAddr()),
		Destination: M.ParseSocksaddr(address.String()),
		User:        user.Name,
	}
	relay := h.inbound.quota.Wrap(user.UUID, &streamConn{Stream: stream, conn: h.conn})
	h.inbound.router.RouteConnectionEx(context.Background(), relay, metadata, func(error) {})
}

// streamConn adapts a QUIC stream to net.Conn.
type streamConn struct {
	*quic.Stream
	conn *quic.Conn
}

func (c *streamConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *streamConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
