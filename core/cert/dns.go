package cert

import (
	"fmt"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/providers/dns/alidns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/dnspod"
)

// FieldSpec 证书申请表单字段描述。
type FieldSpec struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

// ProviderSpec 证书签发方式描述。
type ProviderSpec struct {
	Name   string      `json:"name"`
	Label  string      `json:"label"`
	Fields []FieldSpec `json:"fields"`
}

// Providers 返回支持的签发方式（供前端渲染表单）。
func Providers() []ProviderSpec {
	return []ProviderSpec{
		{Name: "http", Label: "HTTP-01（需 80 端口可达）", Fields: nil},
		{
			Name:  "cloudflare",
			Label: "Cloudflare (DNS)",
			Fields: []FieldSpec{
				{Key: "auth_token", Label: "API Token", Required: true, Secret: true},
			},
		},
		{
			Name:  "alidns",
			Label: "阿里云 DNS",
			Fields: []FieldSpec{
				{Key: "access_key_id", Label: "AccessKey ID", Required: true},
				{Key: "access_key_secret", Label: "AccessKey Secret", Required: true, Secret: true},
			},
		},
		{
			Name:  "dnspod",
			Label: "DNSPod（腾讯云）",
			Fields: []FieldSpec{
				{Key: "login_token", Label: "Login Token（格式 id,token）", Required: true, Secret: true},
			},
		},
	}
}

// newDNSProvider 按名称构造 DNS-01 provider。
func newDNSProvider(name string, creds map[string]string) (challenge.Provider, error) {
	switch name {
	case "cloudflare":
		cfg := cloudflare.NewDefaultConfig()
		cfg.AuthToken = creds["auth_token"]
		cfg.AuthEmail = creds["email"]
		cfg.AuthKey = creds["api_key"]
		if cfg.AuthToken == "" && (cfg.AuthEmail == "" || cfg.AuthKey == "") {
			return nil, fmt.Errorf("Cloudflare 需要 API Token（或 Email + API Key）")
		}
		return cloudflare.NewDNSProviderConfig(cfg)

	case "alidns":
		cfg := alidns.NewDefaultConfig()
		cfg.APIKey = creds["access_key_id"]
		cfg.SecretKey = creds["access_key_secret"]
		if cfg.APIKey == "" || cfg.SecretKey == "" {
			return nil, fmt.Errorf("阿里云 DNS 需要 AccessKey ID 与 Secret")
		}
		return alidns.NewDNSProviderConfig(cfg)

	case "dnspod":
		cfg := dnspod.NewDefaultConfig()
		cfg.LoginToken = creds["login_token"]
		if cfg.LoginToken == "" {
			return nil, fmt.Errorf("DNSPod 需要 Login Token")
		}
		return dnspod.NewDNSProviderConfig(cfg)

	default:
		return nil, fmt.Errorf("不支持的签发方式: %s", name)
	}
}
