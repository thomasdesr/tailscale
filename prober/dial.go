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
//
// Exactly ONE of bindIP or bindInterface is set, never both.
// This ensures clear, unambiguous binding behavior.
type DialConfig struct {
	// bindIP, if non-nil, specifies binding to a specific IP address.
	// This creates a family-specific socket (IPv4 or IPv6).
	bindIP net.IP

	// bindInterface, if non-empty, specifies binding to a network interface.
	// This uses platform-specific socket options (SO_BINDTODEVICE on Linux,
	// IP_BOUND_IF on macOS) and creates dual-stack sockets supporting both
	// IPv4 and IPv6.
	bindInterface string
}

// isInterfaceBinding reports whether this is interface-based binding.
func (dc *DialConfig) isInterfaceBinding() bool {
	return dc != nil && dc.bindInterface != ""
}

// isIPBinding reports whether this is IP-based binding.
func (dc *DialConfig) isIPBinding() bool {
	return dc != nil && dc.bindIP != nil
}

// NewDialConfig creates a DialConfig from a bind specification string.
// The spec can be either:
// - An IP address (e.g., "192.168.1.100") - creates family-specific binding
// - A network interface name (e.g., "en0", "eth0") - creates dual-stack binding
//
// For interface names on supported platforms (Linux, macOS), uses platform-specific
// socket options (SO_BINDTODEVICE, IP_BOUND_IF) for robust dual-stack binding.
//
// If spec is empty, returns nil (use default dialing).
func NewDialConfig(spec string) (*DialConfig, error) {
	if spec == "" {
		return nil, nil
	}

	// Try parsing as IP address first
	if ip := net.ParseIP(spec); ip != nil {
		// IP binding: store only the IP, family-specific
		return &DialConfig{
			bindIP: ip,
		}, nil
	}

	// Must be an interface name - verify it exists
	_, err := net.InterfaceByName(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid bind spec %q: not an IP address and interface not found: %w", spec, err)
	}

	// Interface binding: store only the interface name, dual-stack
	// DO NOT store IP - it would be ambiguous (IPv4 or IPv6?) and create bugs
	return &DialConfig{
		bindInterface: spec,
	}, nil
}

// MakeDialer creates a net.Dialer configured with the bind address.
//
// For interface binding: Uses platform-specific socket options (SO_BINDTODEVICE
// on Linux, IP_BOUND_IF on macOS) for dual-stack operation.
//
// For IP binding: Uses LocalAddr for family-specific binding.
func (dc *DialConfig) MakeDialer() *net.Dialer {
	d := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if dc == nil {
		return d
	}

	switch {
	case dc.isInterfaceBinding():
		// Interface binding: use Control function for dual-stack
		dc.setControlForInterface(d, dc.bindInterface)
	case dc.isIPBinding():
		// IP binding: use LocalAddr for family-specific binding
		d.LocalAddr = &net.TCPAddr{IP: dc.bindIP}
	}

	return d
}

// MakeHTTPTransport creates an http.Transport configured with the bind address.
func (dc *DialConfig) MakeHTTPTransport() *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if dc != nil && (dc.isInterfaceBinding() || dc.isIPBinding()) {
		tr.DialContext = dc.MakeDialer().DialContext
	}
	return tr
}

// MakeListenConfig creates a net.ListenConfig for UDP binding.
//
// For interface binding: Uses platform-specific socket options (SO_BINDTODEVICE
// on Linux, IP_BOUND_IF on macOS) for dual-stack operation.
//
// For IP binding: No Control function needed; GetUDPListenAddr() provides the
// specific IP to bind to.
func (dc *DialConfig) MakeListenConfig() *net.ListenConfig {
	lc := &net.ListenConfig{}

	if dc == nil {
		return lc
	}

	// Only set Control for interface binding (dual-stack)
	// For IP binding, the specific IP comes from GetUDPListenAddr()
	if dc.isInterfaceBinding() {
		dc.setControlForInterfaceListenConfig(lc, dc.bindInterface)
	}

	return lc
}

// GetUDPListenAddr returns the address to use for UDP ListenPacket.
// This is used for STUN probes.
//
// For interface binding: Returns ":0" for dual-stack (IPv4/IPv6) operation.
// The Control function (SO_BINDTODEVICE/IP_BOUND_IF) handles the actual binding.
//
// For IP binding: Returns the specific IP, creating a family-specific socket.
func (dc *DialConfig) GetUDPListenAddr() string {
	switch {
	case dc == nil:
		return ":0"
	case dc.isInterfaceBinding():
		// Interface binding: dual-stack socket, Control handles binding
		return ":0"
	case dc.isIPBinding():
		// IP binding: family-specific socket
		return net.JoinHostPort(dc.bindIP.String(), "0")
	default:
		return ":0"
	}
}

// WrapDialContext wraps an existing DialContext function with local address binding.
// This is useful for modifying existing dialers.
func (dc *DialConfig) WrapDialContext(baseDialContext func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if dc == nil || (!dc.isInterfaceBinding() && !dc.isIPBinding()) {
		return baseDialContext
	}

	dialer := dc.MakeDialer()
	return dialer.DialContext
}
