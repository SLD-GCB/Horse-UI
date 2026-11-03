package guard

import "testing"

func TestConnectionLimit(t *testing.T) {
	compiled := New(Config{MaxConnectionsPerIP: 2})

	first, ok := compiled.AllowConnection("1.2.3.4")
	if !ok {
		t.Fatal("first connection rejected")
	}
	second, ok := compiled.AllowConnection("1.2.3.4")
	if !ok {
		t.Fatal("second connection rejected")
	}
	if _, ok := compiled.AllowConnection("1.2.3.4"); ok {
		t.Fatal("third connection must be rejected")
	}

	first()
	if _, ok := compiled.AllowConnection("1.2.3.4"); !ok {
		t.Fatal("a released slot must be reusable")
	}
	second()
}

func TestAuthFailureBlocks(t *testing.T) {
	compiled := New(Config{MaxAuthFailures: 3, AuthFailureWindowSeconds: 60, AuthBlockSeconds: 60})

	for index := 0; index < 3; index++ {
		if compiled.AuthBlocked("1.2.3.4") {
			t.Fatal("blocked too early")
		}
		compiled.RecordAuthFailure("1.2.3.4")
	}
	if !compiled.AuthBlocked("1.2.3.4") {
		t.Fatal("source must be blocked after the failure budget")
	}
	if compiled.AuthBlocked("5.6.7.8") {
		t.Fatal("another source must not be blocked")
	}

	compiled.ResetAuthFailures("1.2.3.4")
	if compiled.AuthBlocked("1.2.3.4") {
		t.Fatal("reset must clear the block")
	}
}

func TestDefaults(t *testing.T) {
	compiled := New(Config{})
	if compiled.MaxStreams() <= 0 || compiled.MaxUDPSessions() <= 0 {
		t.Fatal("defaults must be positive")
	}
}
