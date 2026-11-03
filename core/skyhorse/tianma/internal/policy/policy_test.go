package policy

import (
	"context"
	"testing"
)

func TestBlocksLoopbackByDefault(t *testing.T) {
	compiled, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Check(context.Background(), "127.0.0.1", 80); err == nil {
		t.Fatal("loopback must be blocked by default")
	}
}

func TestAllowsLoopbackWhenConfigured(t *testing.T) {
	compiled, err := New(Config{AllowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Check(context.Background(), "127.0.0.1", 80); err != nil {
		t.Fatal(err)
	}
}

func TestBlocksPrivateByDefault(t *testing.T) {
	compiled, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"10.1.2.3", "172.16.0.1", "192.168.1.1", "100.64.0.1", "169.254.169.254"} {
		if _, err := compiled.Check(context.Background(), target, 80); err == nil {
			t.Fatalf("%s must be blocked", target)
		}
	}
}

func TestAllowsPublic(t *testing.T) {
	compiled, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Check(context.Background(), "8.8.8.8", 443); err != nil {
		t.Fatal(err)
	}
}

func TestPortAllowList(t *testing.T) {
	compiled, err := New(Config{AllowedPorts: []int{443}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Check(context.Background(), "8.8.8.8", 80); err == nil {
		t.Fatal("port 80 must be rejected")
	}
	if _, err := compiled.Check(context.Background(), "8.8.8.8", 443); err != nil {
		t.Fatal(err)
	}
}

func TestDeniedCIDR(t *testing.T) {
	compiled, err := New(Config{DeniedCIDRs: []string{"8.8.8.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Check(context.Background(), "8.8.8.8", 443); err == nil {
		t.Fatal("denied cidr must win")
	}
}

func TestLocalhostResolvesToBlocked(t *testing.T) {
	compiled, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compiled.Check(context.Background(), "localhost", 80); err == nil {
		t.Fatal("localhost must resolve to loopback and be blocked")
	}
}
