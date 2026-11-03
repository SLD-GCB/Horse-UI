package guard

import "testing"

func TestQuotaTraffic(t *testing.T) {
	quota := NewQuota(QuotaConfig{BytesPerPeriod: 100})
	if !quota.Allow("alice") {
		t.Fatal("fresh user must be allowed")
	}
	if !quota.Add("alice", 60) {
		t.Fatal("60 bytes must fit")
	}
	if !quota.Add("alice", 40) {
		t.Fatal("100 bytes must fit")
	}
	if quota.Allow("alice") {
		t.Fatal("exhausted user must be denied")
	}
	if !quota.Allow("bob") {
		t.Fatal("another user must not be affected")
	}
}

func TestQuotaDisabled(t *testing.T) {
	quota := NewQuota(QuotaConfig{})
	if !quota.Allow("alice") || !quota.Add("alice", 1<<40) {
		t.Fatal("zero quota must be unlimited")
	}
}

func TestMemoryWaterLevel(t *testing.T) {
	memory := NewMemory(100)
	if !memory.Acquire(60) {
		t.Fatal("60 must fit")
	}
	if !memory.Acquire(40) {
		t.Fatal("40 more must fit")
	}
	if memory.Acquire(1) {
		t.Fatal("water level must reject")
	}
	memory.Release(100)
	if !memory.Acquire(100) {
		t.Fatal("released memory must be reusable")
	}
	if memory.Used() != 100 {
		t.Fatalf("used = %d", memory.Used())
	}
}

func TestMemoryDisabled(t *testing.T) {
	memory := NewMemory(0)
	if !memory.Acquire(1<<40) {
		t.Fatal("zero limit must be unlimited")
	}
}
