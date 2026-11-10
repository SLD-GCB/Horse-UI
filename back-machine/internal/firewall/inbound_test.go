package firewall

import (
	"testing"

	"horseui/back-machine/internal/model"
)

func TestInboundPortsRange(t *testing.T) {
	ib := model.Inbound{Port: 24443, Extra: model.JSONText(`{"port_range":"24443-24445"}`)}
	ports := inboundPorts(ib)
	if len(ports) != 3 {
		t.Fatalf("got %v, want 3 ports", ports)
	}
}
