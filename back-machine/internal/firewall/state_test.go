package firewall

import (
	"testing"
	"time"
)

func TestWriteGuardSnapshotIncludesIdleAllowedPort(t *testing.T) {
	s := &Service{dataDir: t.TempDir()}
	states := map[string]*portCapacity{
		"tcp/10000": newPortCapacity("tcp", 0),
	}
	visible := map[string]bool{"tcp/10000": true}
	s.writeGuardSnapshot(true, states, map[int]int{}, map[int]int{}, visible, map[string]bool{})

	snap := s.GuardState()
	if len(snap.Ports) != 1 {
		t.Fatalf("ports=%d, want 1", len(snap.Ports))
	}
	p := snap.Ports[0]
	if p.Port != 10000 || p.Protocol != "tcp" || p.Current != 0 || p.Status != "normal" {
		t.Fatalf("unexpected guard port: %+v", p)
	}
}

func TestWriteGuardSnapshotHidesUnlistedPort(t *testing.T) {
	s := &Service{dataDir: t.TempDir()}
	states := map[string]*portCapacity{
		"tcp/10000": newPortCapacity("tcp", 0),
	}
	s.writeGuardSnapshot(true, states, map[int]int{}, map[int]int{}, map[string]bool{}, map[string]bool{})

	if got := len(s.GuardState().Ports); got != 0 {
		t.Fatalf("ports=%d, want 0", got)
	}
}

func TestWriteGuardSnapshotClosedTimes(t *testing.T) {
	s := &Service{dataDir: t.TempDir()}
	st := newPortCapacity("tcp", 0)
	st.closedAt = time.Unix(1_700_000_000, 0)
	states := map[string]*portCapacity{"tcp/10000": st}
	visible := map[string]bool{"tcp/10000": true}

	s.writeGuardSnapshot(true, states, map[int]int{}, map[int]int{}, visible, map[string]bool{})
	p := s.GuardState().Ports[0]
	if p.ClosedAt != st.closedAt.Unix() || p.RecoverAt != st.closedAt.Add(5*time.Minute).Unix() || p.Locked {
		t.Fatalf("unexpected closed snapshot: %+v", p)
	}

	s.writeGuardSnapshot(true, states, map[int]int{}, map[int]int{}, visible, map[string]bool{"tcp/10000": true})
	p = s.GuardState().Ports[0]
	if p.RecoverAt != 0 || !p.Locked {
		t.Fatalf("locked port should not recover: %+v", p)
	}
}
