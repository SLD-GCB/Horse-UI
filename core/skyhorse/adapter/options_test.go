package skyhorsebox

import (
	"encoding/hex"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestPinHex(t *testing.T) {
	pin := make([]byte, 32)
	for index := range pin {
		pin[index] = byte(index)
	}
	options := &option.OutboundTLSOptions{
		CertificatePublicKeySHA256: [][]byte{pin},
	}
	got := pinHex(options)
	want := hex.EncodeToString(pin)
	if got != want {
		t.Fatalf("pinHex = %q, want %q", got, want)
	}
}

func TestPinHexEmpty(t *testing.T) {
	if got := pinHex(nil); got != "" {
		t.Fatalf("pinHex(nil) = %q", got)
	}
	if got := pinHex(&option.OutboundTLSOptions{}); got != "" {
		t.Fatalf("pinHex(empty) = %q", got)
	}
}

func TestTypeName(t *testing.T) {
	if Type != "skyhorse" {
		t.Fatalf("Type = %q", Type)
	}
}
