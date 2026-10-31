package svc

import (
	"encoding/json"
	"testing"

	"horseui/back-machine/internal/model"
)

func TestSkyHorseInboundAndClientConfig(t *testing.T) {
	ib := model.Inbound{
		Type:         model.ProtoSkyHorse,
		Tag:          "sky-in",
		Listen:       "127.0.0.1",
		Port:         29663,
		ObfsPassword: "obfs-test",
		UpMbps:       20,
		DownMbps:     20,
		TLS:          model.JSONText(`{"enabled":true,"server_name":"localhost","insecure":true}`),
	}
	client := model.Client{Name: "alice", UUID: "alice-1", Password: "secret"}
	cert := &model.Certificate{
		CertPath: "D:/github_code/code/Android/project/SkyHorse/tianma/e2e/cert.pem",
		KeyPath:  "D:/github_code/code/Android/project/SkyHorse/tianma/e2e/key.pem",
	}

	inbound, err := buildInbound(ib, []model.Client{client}, cert)
	if err != nil {
		t.Fatal(err)
	}
	inboundJSON, err := json.Marshal(inbound)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("inbound=%s", inboundJSON)

	config := BuildSingboxClientConfig("127.0.0.1", []model.Inbound{ib}, client, BehaviorMask{})
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("client=%s", configJSON)

	var parsed map[string]any
	if err := json.Unmarshal(configJSON, &parsed); err != nil {
		t.Fatal(err)
	}
	outbounds := parsed["outbounds"].([]any)
	found := false
	for _, raw := range outbounds {
		outbound := raw.(map[string]any)
		if outbound["type"] == "skyhorse" {
			found = true
			if outbound["uuid"] != "alice-1" || outbound["password"] != "secret" {
				t.Fatalf("bad credentials: %v", outbound)
			}
			if outbound["obfs_password"] != "obfs-test" {
				t.Fatalf("bad obfs: %v", outbound)
			}
		}
	}
	if !found {
		t.Fatalf("skyhorse outbound missing: %s", configJSON)
	}
}