// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package prober

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// DialConfig holds configuration for binding network connections
// to a specific interface or source IP address.
type DialConfig struct {
	// BindAddr is the local address to bind to. Can be nil for default behavior.
	BindAddr net.Addr
}

// NewDialConfig creates a DialConfig from a bind specification string.
// The spec can be either:
// - An IP address (e.g., "192.168.1.100")
// - A network interface name (e.g., "en0", "eth0")
//
// If spec is empty, returns nil (use default dialing).
func NewDialConfig(spec string) (*DialConfig, error) {
	if spec == "" {
		return nil, nil
	}

	addr, err := resolveBindAddr(spec)
	if err != nil {
		return nil, err
	}

	return &DialConfig{BindAddr: addr}, nil
}

// resolveBindAddr resolves a bind specification to a net.Addr.
// It tries to parse as an IP address first, then as an interface name.
func resolveBindAddr(spec string) (net.Addr, error) {
	// Try parsing as IP address
	if ip := net.ParseIP(spec); ip != nil {
		if ip.To4() != nil {
			return &net.TCPAddr{IP: ip}, nil
		}
		return &net.TCPAddr{IP: ip}, nil
	}

	// Treat as interface name and resolve to IP
	iface, err := net.InterfaceByName(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid bind spec %q: not an IP address and interface not found: %w", spec, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("getting addresses for interface %q: %w", spec, err)
	}

	// Prefer IPv4, then IPv6
	var ipv6Addr net.IP
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		// Skip loopback addresses
		if ipnet.IP.IsLoopback() {
			continue
		}

		if ipv4 := ipnet.IP.To4(); ipv4 != nil {
			return &net.TCPAddr{IP: ipv4}, nil
		}

		// Keep first IPv6 as fallback
		if ipv6Addr == nil && ipnet.IP.To16() != nil {
			ipv6Addr = ipnet.IP
		}
	}

	if ipv6Addr != nil {
		return &net.TCPAddr{IP: ipv6Addr}, nil
	}

	return nil, fmt.Errorf("no suitable IP address found on interface %q", spec)
}

// MakeDialer creates a net.Dialer configured with the bind address.
func (dc *DialConfig) MakeDialer() *net.Dialer {
	if dc == nil {
		return &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
	}

	return &net.Dialer{
		LocalAddr: dc.BindAddr,
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

// MakeHTTPTransport creates an http.Transport configured with the bind address.
func (dc *DialConfig) MakeHTTPTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if dc != nil && dc.BindAddr != nil {
		tr.DialContext = dc.MakeDialer().DialContext
	}
	return tr
}

// MakeListenConfig creates a net.ListenConfig for UDP binding.
// For UDP, we need to listen on the specific interface/address.
func (dc *DialConfig) MakeListenConfig() *net.ListenConfig {
	if dc == nil {
		return &net.ListenConfig{}
	}

	lc := &net.ListenConfig{}

	// Platform-specific control function will be added if needed
	// For now, we'll use the address-based approach which is more portable
	return lc
}

// GetUDPListenAddr returns the address to use for UDP ListenPacket.
// This is used for STUN probes.
func (dc *DialConfig) GetUDPListenAddr() string {
	if dc == nil || dc.BindAddr == nil {
		return ":0"
	}

	// Extract IP from the bind address
	switch addr := dc.BindAddr.(type) {
	case *net.TCPAddr:
		return net.JoinHostPort(addr.IP.String(), "0")
	case *net.UDPAddr:
		return net.JoinHostPort(addr.IP.String(), "0")
	default:
		return ":0"
	}
}

// WrapDialContext wraps an existing DialContext function with local address binding.
// This is useful for modifying existing dialers.
func (dc *DialConfig) WrapDialContext(baseDialContext func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if dc == nil || dc.BindAddr == nil {
		return baseDialContext
	}

	dialer := dc.MakeDialer()
	return dialer.DialContext
}
