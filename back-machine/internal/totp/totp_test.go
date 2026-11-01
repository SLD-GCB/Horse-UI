package totp

import (
	"strings"
	"testing"
	"time"
)

func TestCodeAndValidate(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	code, err := Code(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != Digits {
		t.Fatalf("code length = %d", len(code))
	}
	step, ok := Validate(secret, code, now, 1)
	if !ok {
		t.Fatal("valid code rejected")
	}
	if step != Step(now) {
		t.Fatalf("step = %d, want %d", step, Step(now))
	}
	if _, ok := Validate(secret, "000000", now, 0); ok {
		t.Fatal("wrong code accepted")
	}
	if _, ok := Validate(secret, code, now.Add(5*time.Minute), 1); ok {
		t.Fatal("expired code accepted")
	}
}

func TestURI(t *testing.T) {
	secret, _ := GenerateSecret()
	uri := URI(secret, "HorseUI", "admin")
	if !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, secret) {
		t.Fatalf("bad uri: %s", uri)
	}
}
