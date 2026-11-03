package obfs

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestRotatingRoundTrip(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	wrappedClient := NewRotating(client, []byte("master"), 3600, 3600)
	wrappedServer := NewRotating(server, []byte("master"), 3600, 3600)

	payload := []byte("rotation-ok")
	if _, err := wrappedClient.WriteTo(payload, server.LocalAddr()); err != nil {
		t.Fatal(err)
	}

	buffer := make([]byte, 1024)
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := wrappedServer.ReadFrom(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer[:n], payload) {
		t.Fatalf("got %q", buffer[:n])
	}
}

func TestRotationKeyChangesPerEpoch(t *testing.T) {
	first := rotationKey([]byte("master"), 1)
	second := rotationKey([]byte("master"), 2)
	if first == second {
		t.Fatal("keys must differ per epoch")
	}
}

func TestRotationRejectsOldEpoch(t *testing.T) {
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.DialUDP("udp", nil, server.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	wrappedServer := NewRotating(server, []byte("master"), 3600, 0)
	if _, err := client.Write([]byte{0, 0, 0, 1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}); err != nil {
		t.Fatal(err)
	}
	_ = server.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buffer := make([]byte, 64)
	if _, _, err := wrappedServer.ReadFrom(buffer); err == nil {
		t.Fatal("a stale epoch must be dropped")
	}
}
