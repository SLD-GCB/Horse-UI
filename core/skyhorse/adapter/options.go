// Package skyhorsebox registers the SkyHorse protocol into the sing-box kernel.
package skyhorsebox

import (
	"encoding/hex"

	"github.com/sagernet/sing-box/option"

	core "tianma/boxapi"
)

// Type is the JSON "type" shared by the panel and the kernel.
const Type = "skyhorse"

// User is one account.
type User struct {
	Name     string `json:"name,omitempty"`
	UUID     string `json:"uuid"`
	Password string `json:"password"`
}

// InboundOptions is a SkyHorse server node.
type InboundOptions struct {
	option.ListenOptions
	Users []User `json:"users,omitempty"`

	TLS *option.InboundTLSOptions `json:"tls,omitempty"`

	ObfsPassword string `json:"obfs_password"`

	ObfsRotationSeconds  int `json:"obfs_rotation_seconds,omitempty"`
	ObfsToleranceSeconds int `json:"obfs_tolerance_seconds,omitempty"`

	UpMbps   int `json:"up_mbps,omitempty"`
	DownMbps int `json:"down_mbps,omitempty"`

	IgnoreClientBandwidth   bool `json:"ignore_client_bandwidth,omitempty"`
	DisableLossCompensation bool `json:"disable_loss_compensation,omitempty"`
	BBRProfile              string `json:"bbr_profile,omitempty"`

	Policy core.PolicyConfig `json:"policy,omitempty"`
	Guard  core.GuardConfig  `json:"guard,omitempty"`

	Quota       core.QuotaConfig `json:"quota,omitempty"`
	MaxMemoryMB int              `json:"max_memory_mb,omitempty"`

	Masquerade core.MasqueradeConfig `json:"masquerade,omitempty"`
}

// OutboundOptions is a SkyHorse client node.
type OutboundOptions struct {
	option.DialerOptions
	option.ServerOptions

	UUID     string `json:"uuid"`
	Password string `json:"password"`

	TLS *option.OutboundTLSOptions `json:"tls,omitempty"`

	ObfsPassword string `json:"obfs_password"`

	ObfsRotationSeconds  int `json:"obfs_rotation_seconds,omitempty"`
	ObfsToleranceSeconds int `json:"obfs_tolerance_seconds,omitempty"`

	UpMbps   int `json:"up_mbps,omitempty"`
	DownMbps int `json:"down_mbps,omitempty"`

	BBRProfile string `json:"bbr_profile,omitempty"`

	DisableLossCompensation bool `json:"disable_loss_compensation,omitempty"`
}

// pinHex returns the first configured SPKI pin as hex, which is the form the
// protocol client expects.
func pinHex(options *option.OutboundTLSOptions) string {
	if options == nil || len(options.CertificatePublicKeySHA256) == 0 {
		return ""
	}
	return hex.EncodeToString(options.CertificatePublicKeySHA256[0])
}
