package main

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/screwys/igloo/internal/config"
)

func TestIsIglooDiscoveryMessage(t *testing.T) {
	if !isIglooDiscoveryMessage([]byte(" who is IglooServer? ")) {
		t.Fatal("expected discovery message to match case-insensitively")
	}
	if isIglooDiscoveryMessage([]byte("who is JellyfinServer?")) {
		t.Fatal("unexpected Jellyfin discovery message match")
	}
}

func TestAdvertisedDiscoveryAddressUsesBoundHTTPAddress(t *testing.T) {
	cfg := &config.Config{ListenAddr: "127.0.0.1:5001"}

	got := advertisedDiscoveryAddress(cfg, false, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})

	if got != "http://127.0.0.1:5001" {
		t.Fatalf("address = %q", got)
	}
}

func TestAdvertisedDiscoveryAddressUsesPublishedURL(t *testing.T) {
	cfg := &config.Config{
		ListenAddr:         ":5001",
		PublishedServerURL: "https://igloo.example.test:8443",
	}

	got := advertisedDiscoveryAddress(cfg, false, &net.UDPAddr{IP: net.ParseIP("192.168.1.20"), Port: 12345})

	if got != "https://igloo.example.test:8443" {
		t.Fatalf("address = %q", got)
	}
}

func TestDiscoveryPayload(t *testing.T) {
	cfg := &config.Config{ListenAddr: "127.0.0.1:5001"}
	payload, err := discoveryPayload(cfg, false, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	if err != nil {
		t.Fatal(err)
	}

	var got discoveryResponse
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Product != "Igloo" || got.Name != "Igloo" || got.Address != "http://127.0.0.1:5001" {
		t.Fatalf("payload = %+v", got)
	}
}
