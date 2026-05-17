package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"strings"

	"github.com/screwys/igloo/internal/config"
)

const (
	iglooDiscoveryMessage   = "who is iglooserver?"
	iglooDiscoveryProduct   = "Igloo"
	iglooDiscoveryReadBytes = 1024
)

type discoveryResponse struct {
	Product string `json:"product"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

func serveDiscovery(ctx context.Context, cfg *config.Config, tlsEnabled bool) {
	conn, err := net.ListenPacket("udp4", cfg.ListenAddr)
	if err != nil {
		slog.Warn("discovery listener disabled", "addr", cfg.ListenAddr, "err", err)
		return
	}
	defer func() {
		_ = conn.Close()
	}()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	slog.Info("discovery listening", "addr", cfg.ListenAddr)
	buffer := make([]byte, iglooDiscoveryReadBytes)
	for {
		n, remote, err := conn.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Debug("discovery read failed", "err", err)
			continue
		}
		if !isIglooDiscoveryMessage(buffer[:n]) {
			continue
		}
		payload, err := discoveryPayload(cfg, tlsEnabled, remote)
		if err != nil {
			slog.Debug("discovery response build failed", "err", err)
			continue
		}
		if _, err := conn.WriteTo(payload, remote); err != nil {
			slog.Debug("discovery response send failed", "remote", remote.String(), "err", err)
		}
	}
}

func discoveryPayload(cfg *config.Config, tlsEnabled bool, remote net.Addr) ([]byte, error) {
	return json.Marshal(discoveryResponse{
		Product: iglooDiscoveryProduct,
		Name:    iglooDiscoveryProduct,
		Address: advertisedDiscoveryAddress(cfg, tlsEnabled, remote),
	})
}

func advertisedDiscoveryAddress(cfg *config.Config, tlsEnabled bool, remote net.Addr) string {
	if cfg.PublishedServerURL != "" {
		return cfg.PublishedServerURL
	}
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		return scheme + "://" + strings.TrimRight(cfg.ListenAddr, "/")
	}
	if isUnspecifiedListenHost(host) {
		host = routedLocalIP(remote)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

func isIglooDiscoveryMessage(message []byte) bool {
	return strings.EqualFold(strings.TrimSpace(string(message)), iglooDiscoveryMessage)
}

func isUnspecifiedListenHost(host string) bool {
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func routedLocalIP(remote net.Addr) string {
	udpRemote, ok := remote.(*net.UDPAddr)
	if !ok || udpRemote.IP == nil {
		return ""
	}
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: udpRemote.IP, Port: 9})
	if err != nil {
		return ""
	}
	defer func() {
		_ = conn.Close()
	}()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil {
		return ""
	}
	return local.IP.String()
}

func tlsFilesExist(cfg *config.Config) bool {
	if _, err := os.Stat(cfg.TLSCert); err != nil {
		return false
	}
	if _, err := os.Stat(cfg.TLSKey); err != nil {
		return false
	}
	return true
}
