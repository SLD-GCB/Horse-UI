package firewall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PortGuardState 单个端口的动态容量快照。
type PortGuardState struct {
	Protocol string  `json:"protocol"`
	Port     int     `json:"port"`
	Current  int     `json:"current"`
	Baseline float64 `json:"baseline"`
	Floor    float64 `json:"floor"`
	Limit    float64 `json:"limit"`
	Ceiling  float64 `json:"ceiling"`
	Status   string  `json:"status"` // normal / watch / closed
}

// GuardSnapshot fwd 自动关端口的实时快照。
type GuardSnapshot struct {
	UpdatedAt int64            `json:"updated_at"`
	Healthy   bool             `json:"healthy"`
	Running   bool             `json:"running"`
	Stale     bool             `json:"stale"`
	Ports     []PortGuardState `json:"ports"`
}

func (s *Service) guardStatePath() string {
	return filepath.Join(s.dataDir, "fwd-state.json")
}

func (s *Service) writeGuardSnapshot(healthy bool, states map[string]*portCapacity, tcp, udp map[int]int) {
	snapshot := GuardSnapshot{
		UpdatedAt: time.Now().Unix(),
		Healthy:   healthy,
		Running:   true,
		Ports:     make([]PortGuardState, 0, len(states)),
	}
	for key, st := range states {
		protocol, port := splitGuardKey(key)
		if port == 0 {
			continue
		}
		current := tcp
		if protocol == "udp" {
			current = udp
		}
		status := "normal"
		if !st.closedAt.IsZero() {
			status = "closed"
		} else if st.over > 0 {
			status = "watch"
		}
		snapshot.Ports = append(snapshot.Ports, PortGuardState{
			Protocol: protocol,
			Port:     port,
			Current:  current[port],
			Baseline: st.baseline,
			Floor:    st.floor,
			Limit:    st.limit,
			Ceiling:  st.ceiling,
			Status:   status,
		})
	}
	sort.Slice(snapshot.Ports, func(i, j int) bool {
		if snapshot.Ports[i].Protocol != snapshot.Ports[j].Protocol {
			return snapshot.Ports[i].Protocol < snapshot.Ports[j].Protocol
		}
		return snapshot.Ports[i].Port < snapshot.Ports[j].Port
	})
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	path := s.guardStatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// GuardState 读取 fwd 的实时快照。fwd 不在线时返回 stale。
func (s *Service) GuardState() GuardSnapshot {
	var snapshot GuardSnapshot
	raw, err := os.ReadFile(s.guardStatePath())
	if err != nil {
		return GuardSnapshot{Stale: true}
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return GuardSnapshot{Stale: true}
	}
	snapshot.Running = true
	if time.Since(time.Unix(snapshot.UpdatedAt, 0)) > 15*time.Second {
		snapshot.Stale = true
	}
	return snapshot
}

func splitGuardKey(key string) (string, int) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			port := 0
			for _, c := range key[i+1:] {
				if c < '0' || c > '9' {
					return key[:i], 0
				}
				port = port*10 + int(c-'0')
			}
			return key[:i], port
		}
	}
	return "", 0
}
