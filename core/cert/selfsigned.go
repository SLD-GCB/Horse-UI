package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// SelfSignedOptions 自签证书参数。
type SelfSignedOptions struct {
	// Names 证书 SAN，可填域名或 IP。
	Names []string
	// CommonName 主题名，留空取 Names 第一个。
	CommonName string
	// Days 有效期天数。
	Days int
}

// SelfSignedResult 自签证书生成结果。
type SelfSignedResult struct {
	CertPEM   []byte
	KeyPEM    []byte
	NotBefore time.Time
	NotAfter  time.Time
	Names     []string
}

// GenerateSelfSigned 生成自签证书。
// 证书同时带 IsCA，便于客户端把它作为受信任根（也可用于 mTLS 校验）。
func GenerateSelfSigned(opts SelfSignedOptions) (*SelfSignedResult, error) {
	days := opts.Days
	if days <= 0 {
		days = 3650
	}
	if days > 36500 {
		days = 36500
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	// 整理 SAN，区分域名与 IP
	var dnsNames []string
	var ipAddrs []net.IP
	seen := map[string]bool{}
	for _, raw := range opts.Names {
		for _, n := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
			n = strings.TrimSpace(n)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			if ip := net.ParseIP(n); ip != nil {
				ipAddrs = append(ipAddrs, ip)
			} else {
				dnsNames = append(dnsNames, n)
			}
		}
	}
	if len(dnsNames) == 0 && len(ipAddrs) == 0 {
		dnsNames = []string{"localhost"}
	}

	cn := strings.TrimSpace(opts.CommonName)
	if cn == "" {
		if len(dnsNames) > 0 {
			cn = dnsNames[0]
		} else {
			cn = ipAddrs[0].String()
		}
	}

	notBefore := time.Now().Add(-time.Hour)
	notAfter := notBefore.Add(time.Duration(days) * 24 * time.Hour)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"HorseUI Self-Signed"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	names := append(append([]string{}, dnsNames...), func() []string {
		out := make([]string, 0, len(ipAddrs))
		for _, ip := range ipAddrs {
			out = append(out, ip.String())
		}
		return out
	}()...)

	return &SelfSignedResult{
		CertPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:    pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		NotBefore: notBefore,
		NotAfter:  notAfter,
		Names:     names,
	}, nil
}

// CertKeyPairFromPEM 从 PEM 解析出证书与私钥，用于校验生成结果。
func CertKeyPairFromPEM(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("证书 PEM 解析失败")
	}
	return x509.ParseCertificate(block.Bytes)
}
