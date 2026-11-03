package skyhorsebox

import (
	"context"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"

	core "tianma/boxapi"
)

// RegisterOutbound adds the protocol to an outbound registry.
func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[OutboundOptions](registry, Type, NewOutbound)
}

// Outbound is a SkyHorse client node inside the kernel.
type Outbound struct {
	outbound.Adapter
	client *core.Client
}

// NewOutbound builds the client without dialing anything.
func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options OutboundOptions) (adapter.Outbound, error) {
	if options.Server == "" || options.ServerPort == 0 {
		return nil, E.New("skyhorse: server and server_port are required")
	}

	serverName := options.Server
	pin := ""
	if options.TLS != nil {
		if options.TLS.ServerName != "" {
			serverName = options.TLS.ServerName
		}
		pin = pinHex(options.TLS)
	}

	instance, err := core.NewClient(core.Config{
		Server:       options.Server,
		ServerPort:   int(options.ServerPort),
		ServerName:   serverName,
		Pin:          pin,
		UUID:         options.UUID,
		Password:     options.Password,
		ObfsPassword: options.ObfsPassword,
		Insecure:     options.TLS != nil && options.TLS.Insecure,
		ObfsRotationSeconds:  options.ObfsRotationSeconds,
		ObfsToleranceSeconds: options.ObfsToleranceSeconds,
		UpMbps:       options.UpMbps,
		DownMbps:     options.DownMbps,
		BBRProfile:   options.BBRProfile,
		DisableLossCompensation: options.DisableLossCompensation,
	})
	if err != nil {
		return nil, E.Cause(err, "skyhorse: client")
	}

	return &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions(Type, tag,
			[]string{"tcp", "udp"}, options.DialerOptions),
		client: instance,
	}, nil
}

// DialContext opens one relay through the tunnel.
func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return o.client.Dial(ctx, destination.String())
}

// ListenPacket opens the datagram side.
func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	udp, err := o.client.DialUDP(ctx)
	if err != nil {
		return nil, err
	}
	return &packetConn{UDPConn: udp}, nil
}

// Close releases the session.
func (o *Outbound) Close() error { return nil }
