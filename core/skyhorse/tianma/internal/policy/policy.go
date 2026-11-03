// Package policy decides which destinations a SkyHorse node is allowed to
// reach. It is the SSRF boundary: without it any authenticated user can ask the
// node to connect to loopback, the LAN, or a cloud metadata endpoint.
package policy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

// Config is the destination policy.
type Config struct {
	AllowPrivate  bool     `json:"allow_private"`
	AllowLoopback bool     `json:"allow_loopback"`
	AllowedPorts  []int    `json:"allowed_ports"`
	DeniedCIDRs   []string `json:"denied_cidrs"`
	AllowedCIDRs  []string `json:"allowed_cidrs"`
}

// Policy is a compiled destination policy.
type Policy struct {
	allowPrivate  bool
	allowLoopback bool
	ports         map[uint16]bool
	denied        []netip.Prefix
	allowed       []netip.Prefix
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// New compiles the configuration.
func New(config Config) (*Policy, error) {
	compiled := &Policy{
		allowPrivate:  config.AllowPrivate,
		allowLoopback: config.AllowLoopback,
	}
	if len(config.AllowedPorts) > 0 {
		compiled.ports = make(map[uint16]bool, len(config.AllowedPorts))
		for _, port := range config.AllowedPorts {
			if port <= 0 || port > 65535 {
				return nil, fmt.Errorf("policy: invalid port %d", port)
			}
			compiled.ports[uint16(port)] = true
		}
	}
	for _, raw := range config.DeniedCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("policy: bad denied cidr %q: %w", raw, err)
		}
		compiled.denied = append(compiled.denied, prefix)
	}
	for _, raw := range config.AllowedCIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("policy: bad allowed cidr %q: %w", raw, err)
		}
		compiled.allowed = append(compiled.allowed, prefix)
	}
	return compiled, nil
}

// Check validates a destination and returns the address to dial. Domain names
// are resolved here so a rebinding answer cannot slip past the check.
func (p *Policy) Check(ctx context.Context, host string, port uint16) (netip.Addr, error) {
	if p.ports != nil && !p.ports[port] {
		return netip.Addr{}, fmt.Errorf("policy: port %d is not allowed", port)
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		if err := p.checkIP(ip); err != nil {
			return netip.Addr{}, err
		}
		return ip, nil
	}

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("policy: resolve %s: %w", host, err)
	}
	if len(addresses) == 0 {
		return netip.Addr{}, fmt.Errorf("policy: %s has no address", host)
	}
	for _, address := range addresses {
		if err := p.checkIP(address); err != nil {
			return netip.Addr{}, fmt.Errorf("policy: %s resolves to a blocked address: %w", host, err)
		}
	}
	return addresses[0], nil
}

func (p *Policy) checkIP(ip netip.Addr) error {
	if !ip.IsValid() {
		return fmt.Errorf("policy: invalid address")
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}

	if matchPrefix(p.denied, ip) {
		return fmt.Errorf("policy: %s is denied", ip)
	}
	if len(p.allowed) > 0 && !matchPrefix(p.allowed, ip) {
		return fmt.Errorf("policy: %s is not in the allowed cidrs", ip)
	}

	switch {
	case ip.IsUnspecified():
		return fmt.Errorf("policy: unspecified address")
	case ip.IsMulticast():
		return fmt.Errorf("policy: multicast address")
	case ip.IsLinkLocalUnicast():
		return fmt.Errorf("policy: link-local address")
	case ip.IsLoopback() && !p.allowLoopback:
		return fmt.Errorf("policy: loopback address")
	case (ip.IsPrivate() || cgnat.Contains(ip)) && !p.allowPrivate:
		return fmt.Errorf("policy: private address")
	}
	return nil
}

func matchPrefix(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
