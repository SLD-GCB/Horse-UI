package cert

import (
	"crypto"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

const (
	accountKeyFile    = "acme_account.key"
	accountRegFile    = "acme_account.json"
	httpChallengePort = "80"
)

// Manager ACME 证书管理器，负责账户持久化与证书签发。
type Manager struct {
	dir   string
	email string
}

// NewManager 创建管理器，dir 为证书存放目录。
func NewManager(dir, email string) *Manager {
	return &Manager{dir: dir, email: email}
}

// Result 签发结果。
type Result struct {
	CertPath  string
	KeyPath   string
	NotBefore time.Time
	NotAfter  time.Time
}

// acmeUser 实现 lego 的 registration.User 接口。
type acmeUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.Email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func (m *Manager) loadAccount() (*acmeUser, error) {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return nil, err
	}
	u := &acmeUser{Email: m.email}

	keyPath := filepath.Join(m.dir, accountKeyFile)
	if data, err := os.ReadFile(keyPath); err == nil {
		key, err := certcrypto.ParsePEMPrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("解析 ACME 账户私钥失败: %w", err)
		}
		u.key = key
	} else {
		key, err := certcrypto.GeneratePrivateKey(certcrypto.EC256)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, certcrypto.PEMEncode(key), 0o600); err != nil {
			return nil, err
		}
		u.key = key
	}

	if data, err := os.ReadFile(filepath.Join(m.dir, accountRegFile)); err == nil {
		var reg registration.Resource
		if err := json.Unmarshal(data, &reg); err == nil {
			u.Registration = &reg
		}
	}
	return u, nil
}

func (m *Manager) saveAccount(u *acmeUser) error {
	if u.Registration == nil {
		return nil
	}
	data, err := json.Marshal(u.Registration)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.dir, accountRegFile), data, 0o600)
}

// Obtain 为指定域名申请（或续签）证书，provider 为 "http" 或 DNS 服务商名。
func (m *Manager) Obtain(domain, provider string, credentials map[string]string) (*Result, error) {
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}

	user, err := m.loadAccount()
	if err != nil {
		return nil, err
	}

	client, err := m.newClient(user, provider, credentials)
	if err != nil {
		return nil, err
	}

	if user.Registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("注册 ACME 账户失败: %w", err)
		}
		user.Registration = reg
		if err := m.saveAccount(user); err != nil {
			return nil, err
		}
	}

	res, err := client.Certificate.Obtain(certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("申请证书失败: %w", err)
	}

	outDir := filepath.Join(m.dir, domain)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	certPath := filepath.Join(outDir, "cert.pem")
	keyPath := filepath.Join(outDir, "key.pem")
	if err := os.WriteFile(certPath, res.Certificate, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, res.PrivateKey, 0o600); err != nil {
		return nil, err
	}

	result := &Result{CertPath: certPath, KeyPath: keyPath}
	if parsed, err := certcrypto.ParsePEMCertificate(res.Certificate); err == nil {
		result.NotBefore = parsed.NotBefore
		result.NotAfter = parsed.NotAfter
	}
	return result, nil
}

func (m *Manager) newClient(user *acmeUser, provider string, credentials map[string]string) (*lego.Client, error) {
	cfg := lego.NewConfig(user)
	cfg.CADirURL = lego.LEDirectoryProduction
	cfg.Certificate.KeyType = certcrypto.EC256

	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	var chal challenge.Provider
	if provider == "" || provider == "http" {
		chal = http01.NewProviderServer("", httpChallengePort)
	} else {
		chal, err = newDNSProvider(provider, credentials)
		if err != nil {
			return nil, err
		}
		if err := client.Challenge.SetDNS01Provider(chal); err != nil {
			return nil, err
		}
		return client, nil
	}

	if err := client.Challenge.SetHTTP01Provider(chal); err != nil {
		return nil, err
	}
	return client, nil
}

// ParseCertExpiry 读取证书文件的生效/过期时间。
func ParseCertExpiry(certPath string) (time.Time, time.Time, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	c, err := certcrypto.ParsePEMCertificate(data)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return c.NotBefore, c.NotAfter, nil
}
