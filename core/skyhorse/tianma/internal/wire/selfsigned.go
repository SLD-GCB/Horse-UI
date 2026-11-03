package wire

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

// SelfSigned reports whether the certificate is signed by itself. A self-signed
// certificate is a strong fingerprint unless the transport is obfuscated.
func SelfSigned(certificatePath string) (bool, error) {
	data, err := os.ReadFile(certificatePath)
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false, fmt.Errorf("wire: no certificate in %s", certificatePath)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}
	return certificate.CheckSignatureFrom(certificate) == nil, nil
}
