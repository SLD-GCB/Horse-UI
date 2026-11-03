package client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	quic "github.com/apernet/quic-go"
	"github.com/apernet/quic-go/http3"

	"tianma/internal/proto"
	"tianma/internal/wire"
	"tianma/internal/congestion"
)

// Config is the client configuration.
type Config struct {
	Listen       string   `json:"listen"`
	Server       string   `json:"server"`
	ServerPort   int      `json:"server_port"`
	Servers      []string `json:"servers"`
	ServerName   string   `json:"server_name"`
	Pin          string   `json:"pin"` // hex encoded SPKI SHA-256
	Insecure     bool     `json:"insecure"`
	UUID         string   `json:"uuid"`
	Password     string   `json:"password"`
	ObfsPassword       string   `json:"obfs_password"`
	ObfsRotationSeconds  int    `json:"obfs_rotation_seconds"`
	ObfsToleranceSeconds int    `json:"obfs_tolerance_seconds"`
	UpMbps       int      `json:"up_mbps"`
	DownMbps     int      `json:"down_mbps"`
	BBRProfile   string   `json:"bbr_profile"`

	DisableLossCompensation bool `json:"disable_loss_compensation"`
}

// Client keeps one authenticated QUIC connection and opens a stream per relay.
type Client struct {
	addresses []string
	tlsConfig *tls.Config
	uuid      string
	password  string
	obfs      []byte
	obfsRotation  int
	obfsTolerance int
	upBps     uint64
	downBps   uint64
	bbrProfile string

	disableLossCompensation bool

	mu        sync.Mutex
	conn      *quic.Conn
	transport *http3.Transport
	quicConn  *quic.Transport
	socket    *net.UDPConn
}

// New builds a client from the configuration.
func New(config Config) (*Client, error) {
	pin, err := decodePin(config.Pin)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := wire.ClientTLS(config.ServerName, pin, config.Insecure)
	if err != nil {
		return nil, err
	}
	addresses := make([]string, 0, len(config.Servers)+1)
	for _, entry := range config.Servers {
		if entry != "" {
			addresses = append(addresses, entry)
		}
	}
	if len(addresses) == 0 {
		addresses = append(addresses, net.JoinHostPort(config.Server, strconv.Itoa(config.ServerPort)))
	}

	profile := config.BBRProfile
	if profile == "" {
		profile = "standard"
	}

	return &Client{
		addresses:               addresses,
		tlsConfig:               tlsConfig,
		uuid:                    config.UUID,
		password:                config.Password,
		obfs:                    []byte(config.ObfsPassword),
		obfsRotation:            config.ObfsRotationSeconds,
		obfsTolerance:           config.ObfsToleranceSeconds,
		upBps:                   uint64(config.UpMbps) * 125000,
		downBps:                 uint64(config.DownMbps) * 125000,
		bbrProfile:              profile,
		disableLossCompensation: config.DisableLossCompensation,
	}, nil
}

// Dial opens one relay through the tunnel.
func (c *Client) Dial(ctx context.Context, target string) (net.Conn, error) {
	address, err := proto.ParseAddr(target)
	if err != nil {
		return nil, err
	}

	conn, err := c.session(ctx)
	if err != nil {
		return nil, err
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	if err := proto.WriteTCPRequest(stream, address, randomPaddingSize()); err != nil {
		_ = stream.Close()
		return nil, err
	}

	var status [1]byte
	if _, err := io.ReadFull(stream, status[:]); err != nil {
		_ = stream.Close()
		return nil, err
	}
	if status[0] != proto.StatusOKByte {
		_ = stream.Close()
		return nil, fmt.Errorf("tianma: target %s refused", address)
	}
	return &streamConn{Stream: stream, conn: conn}, nil
}

func (c *Client) session(ctx context.Context) (*quic.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn, nil
	}

	var lastErr error
	for _, address := range c.addresses {
		conn, transport, quicTransport, socket, err := c.connect(ctx, address)
		if err != nil {
			lastErr = err
			continue
		}
		c.conn = conn
		c.transport = transport
		c.quicConn = quicTransport
		c.socket = socket
		return conn, nil
	}
	if lastErr == nil {
		lastErr = errors.New("tianma: no server address")
	}
	return nil, lastErr
}

func (c *Client) connect(ctx context.Context, address string) (*quic.Conn, *http3.Transport, *quic.Transport, *net.UDPConn, error) {
	var err error
	var quicTransport *quic.Transport
	var socket *net.UDPConn
	if c.obfsRotation > 0 && len(c.obfs) > 0 {
		quicTransport, socket, err = wire.NewUDPTransportRotating(":0", c.obfs, c.obfsRotation, c.obfsTolerance)
	} else {
		quicTransport, socket, err = wire.NewUDPTransport(":0", c.obfs)
	}
	if err != nil {
		return nil, nil, nil, nil, err
	}
	remote, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		_ = quicTransport.Close()
		_ = socket.Close()
		return nil, nil, nil, nil, err
	}
	conn, err := quicTransport.DialEarly(ctx, remote, c.tlsConfig, wire.QUICConfig())
	if err != nil {
		_ = quicTransport.Close()
		_ = socket.Close()
		return nil, nil, nil, nil, err
	}

	transport := &http3.Transport{
		TLSClientConfig: c.tlsConfig,
		QUICConfig:      wire.QUICConfig(),
		Dial: func(_ context.Context, _ string, _ *tls.Config, _ *quic.Config) (*quic.Conn, error) {
			return conn, nil
		},
	}

	state := conn.ConnectionState()
	token, err := wire.Token(&state.TLS, c.uuid, c.password)
	if err != nil {
		_ = transport.Close()
		_ = conn.CloseWithError(0, "")
		_ = quicTransport.Close()
		_ = socket.Close()
		return nil, nil, nil, nil, err
	}

	request := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Scheme: "https", Host: proto.URLHost, Path: proto.URLPath},
		Header: make(http.Header),
	}
	request.Header.Set(proto.HeaderAuth, proto.EncodeToken(c.uuid, token))
	request.Header.Set(proto.HeaderPadding, randomPadding())
	if c.downBps > 0 {
		request.Header.Set(proto.HeaderCCRX, strconv.FormatUint(c.downBps, 10))
	}

	response, err := transport.RoundTrip(request)
	if err != nil {
		_ = transport.Close()
		_ = conn.CloseWithError(0, "")
		_ = quicTransport.Close()
		_ = socket.Close()
		return nil, nil, nil, nil, err
	}
	_ = response.Body.Close()
	if response.StatusCode != proto.StatusAuthOK {
		_ = transport.Close()
		_ = conn.CloseWithError(0, "")
		_ = quicTransport.Close()
		_ = socket.Close()
		return nil, nil, nil, nil, fmt.Errorf("tianma: authentication failed (status %d)", response.StatusCode)
	}

	if serverRx := response.Header.Get(proto.HeaderCCRX); serverRx != "" {
		if rate, err := strconv.ParseUint(serverRx, 10, 64); err == nil && rate > 0 {
			tx := rate
			if c.upBps > 0 && tx > c.upBps {
				tx = c.upBps
			}
			congestion.UseBrutal(conn, tx, c.disableLossCompensation)
		} else {
			congestion.UseBBR(conn, c.bbrProfile)
		}
	} else {
		congestion.UseBBR(conn, c.bbrProfile)
	}

	return conn, transport, quicTransport, socket, nil
}

// streamConn adapts a QUIC stream to net.Conn.
type streamConn struct {
	*quic.Stream
	conn *quic.Conn
}

func (c *streamConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *streamConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

func decodePin(text string) ([]byte, error) {
	if text == "" {
		return nil, nil
	}
	pin, err := hex.DecodeString(text)
	if err != nil || len(pin) != 32 {
		return nil, errors.New("tianma: pin must be 64 hex characters")
	}
	return pin, nil
}

func randomPadding() string {
	var seed [1]byte
	_, _ = rand.Read(seed[:])
	size := 64 + int(seed[0])%128
	buffer := make([]byte, size)
	_, _ = rand.Read(buffer)
	return base64.RawStdEncoding.EncodeToString(buffer)
}

func randomPaddingSize() int {
	var seed [1]byte
	_, _ = rand.Read(seed[:])
	return int(seed[0]) % 48
}
