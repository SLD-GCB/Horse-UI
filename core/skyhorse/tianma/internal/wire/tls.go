package wire

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"

	quic "github.com/apernet/quic-go"
)

// ServerTLS builds the QUIC listener's TLS configuration.
func ServerTLS(certificatePath, keyPath string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h3"},
	}, nil
}

// ClientTLS builds the dialer's TLS configuration with a pinned self signed
// certificate.
func ClientTLS(serverName string, pin []byte, insecure bool) (*tls.Config, error) {
	config := &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h3"},
	}
	switch {
	case len(pin) > 0:
		config.InsecureSkipVerify = true
		config.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("tianma: server presented no certificate")
			}
			leaf, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
			if subtle.ConstantTimeCompare(sum[:], pin) != 1 {
				return errors.New("tianma: certificate pin mismatch")
			}
			return nil
		}
	case insecure:
		config.InsecureSkipVerify = true
	default:
		return nil, errors.New("tianma: a certificate pin is required")
	}
	return config, nil
}

// Token derives the per-connection auth token from the TLS exporter. It is
// bound to this QUIC connection, so a relay that terminates TLS elsewhere
// cannot replay it.
func Token(state *tls.ConnectionState, uuid, password string) ([]byte, error) {
	return state.ExportKeyingMaterial(uuid, []byte(password), 32)
}

// QUICConfig returns the transport parameters. The windows are sized for
// video and bulk transfer at evening-peak RTTs.
func QUICConfig() *quic.Config {
	return &quic.Config{
		MaxIdleTimeout:                 60 * time.Second,
		KeepAlivePeriod:                15 * time.Second,
		MaxIncomingStreams:             1024,
		InitialStreamReceiveWindow:     1 << 20,
		MaxStreamReceiveWindow:         8 << 20,
		InitialConnectionReceiveWindow: 2 << 20,
		MaxConnectionReceiveWindow:     64 << 20,
		InitialPacketSize:              1452,
		EnableDatagrams:                true,
		MaxDatagramFrameSize:           1200,
		AssumePeerMaxDatagramFrameSize: 1200,
	}
}
