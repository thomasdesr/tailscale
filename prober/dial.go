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

	// interfaceName, if non-empty, indicates we should bind to this interface
	// by name using platform-specific socket options (SO_BINDTODEVICE on Linux,
	// IP_BOUND_IF on macOS). This is preferred over IP-based binding as it
	// handles interface address changes.
	interfaceName string
}

// NewDialConfig creates a DialConfig from a bind specification string.
// The spec can be either:
// - An IP address (e.g., "192.168.1.100")
// - A network interface name (e.g., "en0", "eth0")
//
// For interface names on supported platforms (Linux, macOS), uses platform-specific
// socket options (SO_BINDTODEVICE, IP_BOUND_IF) for more robust binding.
//
// If spec is empty, returns nil (use default dialing).
func NewDialConfig(spec string) (*DialConfig, error) {
	if spec == "" {
		return nil, nil
	}

	// Try parsing as IP address first
	if ip := net.ParseIP(spec); ip != nil {
		return &DialConfig{
			BindAddr: &net.TCPAddr{IP: ip},
		}, nil
	}

	// Must be an interface name - verify it exists and get an IP for UDP binding
	iface, err := net.InterfaceByName(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid bind spec %q: not an IP address and interface not found: %w", spec, err)
	}

	// Get an IP address for fallback UDP binding (some platforms don't support Control on UDP)
	addr, err := getInterfaceAddr(iface)
	if err != nil {
		return nil, err
	}

	return &DialConfig{
		BindAddr:      addr,
		interfaceName: spec, // Store the interface name for platform-specific binding
	}, nil
}

// getInterfaceAddr returns the first suitable IP address from an interface.
func getInterfaceAddr(iface *net.Interface) (net.Addr, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("getting addresses for interface %q: %w", iface.Name, err)
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

	return nil, fmt.Errorf("no suitable IP address found on interface %q", iface.Name)
}


// MakeDialer creates a net.Dialer configured with the bind address.
// If an interface name was specified, uses platform-specific socket options
// for more robust binding (SO_BINDTODEVICE on Linux, IP_BOUND_IF on macOS).
func (dc *DialConfig) MakeDialer() *net.Dialer {
	d := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	if dc == nil {
		return d
	}

	// If we have an interface name, use platform-specific binding
	if dc.interfaceName != "" {
		dc.setControlForInterface(d, dc.interfaceName)
	} else if dc.BindAddr != nil {
		// Otherwise use IP-based binding
		d.LocalAddr = dc.BindAddr
	}

	return d
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
// If an interface name was specified, uses platform-specific socket options
// for more robust binding (SO_BINDTODEVICE on Linux, IP_BOUND_IF on macOS).
func (dc *DialConfig) MakeListenConfig() *net.ListenConfig {
	lc := &net.ListenConfig{}

	if dc == nil {
		return lc
	}

	// If we have an interface name, use platform-specific binding
	if dc.interfaceName != "" {
		dc.setControlForInterfaceListenConfig(lc, dc.interfaceName)
	}
	// Note: For UDP, we still bind to a specific IP via GetUDPListenAddr()
	// even when using interface-based binding, for maximum compatibility

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
