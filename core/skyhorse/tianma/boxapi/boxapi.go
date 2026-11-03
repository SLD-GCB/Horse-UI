// Package boxapi is the public surface the sing-box adapter uses. It re-exports
// the protocol core that lives under internal/, so the kernel module can import
// it without crossing Go's internal package boundary.
package boxapi

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"

	quic "github.com/apernet/quic-go"

	"tianma/internal/client"
	"tianma/internal/congestion"
	"tianma/internal/guard"
	"tianma/internal/policy"
	"tianma/internal/proto"
	"tianma/internal/server"
	"tianma/internal/wire"
)

// Wire constants.
const (
	URLHost             = proto.URLHost
	URLPath             = proto.URLPath
	HeaderAuth          = proto.HeaderAuth
	HeaderPadding       = proto.HeaderPadding
	StatusAuthOK        = proto.StatusAuthOK
	FrameTypeTCPRequest = proto.FrameTypeTCPRequest
	StatusOKByte        = proto.StatusOKByte
	StatusErrorByte     = proto.StatusErrorByte
)

// Addr is a target address.
type Addr = proto.Addr

// Config is the client configuration.
type Config = client.Config

// Client is the protocol client.
type Client = client.Client

// UDPConn is the datagram side of the client.
type UDPConn = client.UDPConn

// NewClient builds a protocol client.
func NewClient(config Config) (*Client, error) {
	return client.New(config)
}

// ServerTLS loads the listener certificate.
func ServerTLS(certificatePath, keyPath string) (*tls.Config, error) {
	return wire.ServerTLS(certificatePath, keyPath)
}

// Token derives the exporter-bound auth token.
func Token(state *tls.ConnectionState, uuid, password string) ([]byte, error) {
	return wire.Token(state, uuid, password)
}

// QUICConfig returns the transport parameters.
func QUICConfig() *quic.Config {
	return wire.QUICConfig()
}

// NewUDPTransport opens the UDP socket with optional obfuscation.
func NewUDPTransport(address string, key []byte) (*quic.Transport, *net.UDPConn, error) {
	return wire.NewUDPTransport(address, key)
}

// ReadTCPRequest reads a TCP request frame.
func ReadTCPRequest(r io.Reader) (Addr, error) {
	return proto.ReadTCPRequest(r)
}

// DecodeToken parses the auth header value.
func DecodeToken(value string) (string, []byte, error) {
	return proto.DecodeToken(value)
}

// UDPMessage is one datagram or fragment.
type UDPMessage = proto.UDPMessage

// ParseUDPMessage decodes one datagram.
func ParseUDPMessage(buffer []byte) (UDPMessage, error) { return proto.ParseUDPMessage(buffer) }

// AppendUDPMessage encodes one datagram.
func AppendUDPMessage(dst []byte, message UDPMessage) ([]byte, error) {
	return proto.AppendUDPMessage(dst, message)
}


// ParseAddr converts "host:port" into an Addr.
func ParseAddr(target string) (Addr, error) { return proto.ParseAddr(target) }

// UseBrutal installs the adaptive fixed-rate sender on a connection.
func UseBrutal(conn *quic.Conn, bytesPerSecond uint64, disableLossCompensation bool) {
	congestion.UseBrutal(conn, bytesPerSecond, disableLossCompensation)
}
// HeaderCCRX is the bandwidth declaration header.
const HeaderCCRX = proto.HeaderCCRX

// UseBBR installs BBR with the loss guard.
func UseBBR(conn *quic.Conn, profile string) {
	congestion.UseBBR(conn, profile)
}

// PolicyConfig is the destination policy configuration.
type PolicyConfig = policy.Config

// Policy is a compiled destination policy.
type Policy = policy.Policy

// NewPolicy compiles a destination policy.
func NewPolicy(config PolicyConfig) (*Policy, error) {
	return policy.New(config)
}

// GuardConfig is the abuse boundary configuration.
type GuardConfig = guard.Config

// NewGuard compiles the abuse boundary configuration.
func NewGuard(config GuardConfig) *guard.Guard {
	return guard.New(config)
}
// Guard is the compiled abuse boundary.
type Guard = guard.Guard

// QuotaConfig is the per-user traffic and bandwidth allowance.
type QuotaConfig = guard.QuotaConfig

// Quota tracks per-user usage.
type Quota = guard.Quota

// NewQuota compiles the allowance.
func NewQuota(config QuotaConfig) *Quota {
	return guard.NewQuota(config)
}

// Memory is the global memory water level.
type Memory = guard.Memory

// NewMemory builds the water level in bytes.
func NewMemory(limit int64) *Memory {
	return guard.NewMemory(limit)
}

// MasqueradeConfig decides what an unauthenticated visitor sees.
type MasqueradeConfig = server.MasqueradeConfig

// NewMasquerade builds the handler for unauthenticated traffic.
func NewMasquerade(config MasqueradeConfig) (http.Handler, error) {
	return server.NewMasquerade(config)
}

// SelfSigned reports whether the certificate is self-signed.
func SelfSigned(path string) (bool, error) {
	return wire.SelfSigned(path)
}

// NewUDPTransportRotating opens the UDP socket with time-rotating obfuscation.
func NewUDPTransportRotating(address string, master []byte, rotationSeconds, toleranceSeconds int) (*quic.Transport, *net.UDPConn, error) {
	return wire.NewUDPTransportRotating(address, master, rotationSeconds, toleranceSeconds)
}
