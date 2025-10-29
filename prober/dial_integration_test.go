// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package prober

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/tailscale/wireguard-go/tun"
	"tailscale.com/net/tstun"
)

// TestDialConfig_WithIsolatedTUN tests that binding to an isolated TUN device
// (one with no internet access) correctly prevents connections from succeeding.
// This proves that our binding implementation actually works.
func TestDialConfig_WithIsolatedTUN(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// These tests require root/CAP_NET_ADMIN to create TUN devices
	// and CAP_NET_RAW for SO_BINDTODEVICE
	tunDev, tunIP, cleanup := createIsolatedTUN(t)
	defer cleanup()

	t.Run("TLS_bound_to_isolated_TUN_fails", func(t *testing.T) {
		testTLSProbeFailsWithIsolatedTUN(t, tunDev)
	})

	t.Run("HTTP_bound_to_isolated_TUN_fails", func(t *testing.T) {
		testHTTPProbeFailsWithIsolatedTUN(t, tunDev)
	})

	t.Run("UDP_bound_to_isolated_TUN_fails", func(t *testing.T) {
		testUDPProbeFailsWithIsolatedTUN(t, tunDev, tunIP)
	})

	t.Run("TCP_dial_bound_to_isolated_TUN_fails", func(t *testing.T) {
		testTCPDialFailsWithIsolatedTUN(t, tunDev)
	})

	// Control test: verify that without binding, we can reach localhost
	t.Run("localhost_without_binding_succeeds", func(t *testing.T) {
		testLocalhostWithoutBindingSucceeds(t)
	})

	// Positive test: verify binding to loopback interface works
	t.Run("binding_to_loopback_succeeds", func(t *testing.T) {
		testBindingToLoopbackSucceeds(t)
	})
}

// createIsolatedTUN creates a TUN device with an IP address but no routes to the internet.
// This creates a "dead end" - any traffic bound to this interface will fail.
func createIsolatedTUN(t *testing.T) (tunName string, tunIP netip.Addr, cleanup func()) {
	t.Helper()

	// Use same TUN name as bandwidth probes
	const testTunName = "derpprobe_test"

	// Create TUN device
	dev, err := tun.CreateTUN(testTunName, int(tstun.DefaultTUNMTU()))
	if err != nil {
		t.Skipf("failed to create TUN device (needs CAP_NET_ADMIN): %v", err)
	}

	name, err := dev.Name()
	if err != nil {
		dev.Close()
		t.Fatalf("failed to get TUN name: %v", err)
	}

	// Assign IP to TUN but don't add any routes
	// This makes it a "dead end" - packets go in but nowhere to route them
	prefix := netip.MustParsePrefix("10.77.77.1/24")

	if err := configureTUN(prefix, name); err != nil {
		dev.Close()
		t.Skipf("failed to configure TUN (needs root): %v", err)
	}

	t.Logf("Created isolated TUN device: %s with IP %s", name, prefix.Addr())

	cleanup = func() {
		dev.Close()
	}

	return name, prefix.Addr(), cleanup
}

// testTLSProbeFailsWithIsolatedTUN verifies TLS probes fail when bound to isolated TUN
func testTLSProbeFailsWithIsolatedTUN(t *testing.T, tunDev string) {
	t.Helper()

	dc, err := NewDialConfig(tunDev)
	if err != nil {
		t.Fatalf("NewDialConfig(%q) failed: %v", tunDev, err)
	}

	// Try to connect to a public IP that should be unreachable via TUN
	// Using Cloudflare DNS as a reliable target
	probe := TLSWithDialer("1.1.1.1:443", nil, dc)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = probe.Probe(ctx)

	// We WANT this to fail - it proves binding worked
	if err == nil {
		t.Error("TLS probe unexpectedly succeeded when bound to isolated TUN - binding may not be working!")
		return
	}

	t.Logf("TLS probe correctly failed with isolated TUN: %v", err)

	// Verify the error is connection-related, not a TLS error
	// (which would indicate we actually reached the server)
	if strings.Contains(err.Error(), "certificate") || strings.Contains(err.Error(), "handshake") {
		t.Errorf("got TLS error instead of connection error - may have reached server despite TUN binding: %v", err)
	}
}

// testHTTPProbeFailsWithIsolatedTUN verifies HTTP probes fail when bound to isolated TUN
func testHTTPProbeFailsWithIsolatedTUN(t *testing.T, tunDev string) {
	t.Helper()

	dc, err := NewDialConfig(tunDev)
	if err != nil {
		t.Fatalf("NewDialConfig(%q) failed: %v", tunDev, err)
	}

	// Try to connect to a public HTTP endpoint
	probe := HTTPWithDialer("http://1.1.1.1/", "", dc)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = probe.Probe(ctx)

	// We WANT this to fail - it proves binding worked
	if err == nil {
		t.Error("HTTP probe unexpectedly succeeded when bound to isolated TUN - binding may not be working!")
		return
	}

	t.Logf("HTTP probe correctly failed with isolated TUN: %v", err)
}

// testUDPProbeFailsWithIsolatedTUN verifies UDP probes fail when bound to isolated TUN
func testUDPProbeFailsWithIsolatedTUN(t *testing.T, tunDev string, tunIP netip.Addr) {
	t.Helper()

	dc, err := NewDialConfig(tunDev)
	if err != nil {
		t.Fatalf("NewDialConfig(%q) failed: %v", tunDev, err)
	}

	// Try a STUN request to Cloudflare's STUN server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = derpProbeUDP(ctx, "1.1.1.1", 3478, dc)

	// We WANT this to fail - it proves binding worked
	if err == nil {
		t.Error("UDP probe unexpectedly succeeded when bound to isolated TUN - binding may not be working!")
		return
	}

	t.Logf("UDP probe correctly failed with isolated TUN: %v", err)
}

// testTCPDialFailsWithIsolatedTUN is a low-level test of TCP dialing
func testTCPDialFailsWithIsolatedTUN(t *testing.T, tunDev string) {
	t.Helper()

	dc, err := NewDialConfig(tunDev)
	if err != nil {
		t.Fatalf("NewDialConfig(%q) failed: %v", tunDev, err)
	}

	dialer := dc.MakeDialer()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", "1.1.1.1:80")
	if conn != nil {
		conn.Close()
	}

	// We WANT this to fail - it proves binding worked
	if err == nil {
		t.Error("TCP dial unexpectedly succeeded when bound to isolated TUN - binding may not be working!")
		return
	}

	t.Logf("TCP dial correctly failed with isolated TUN: %v", err)

	// Verify it's a timeout/network error, not something else
	if !strings.Contains(err.Error(), "timeout") &&
	   !strings.Contains(err.Error(), "network") &&
	   !strings.Contains(err.Error(), "unreachable") {
		t.Logf("Warning: unexpected error type (but still failed as expected): %v", err)
	}
}

// testLocalhostWithoutBindingSucceeds is a control test - verify we can reach localhost normally
func testLocalhostWithoutBindingSucceeds(t *testing.T) {
	t.Helper()

	// Start a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "test response")
	}))
	defer ts.Close()

	// Connect WITHOUT binding - should work
	probe := HTTP(ts.URL, "test response")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := probe.Probe(ctx)
	if err != nil {
		t.Errorf("localhost connection without binding failed (control test): %v", err)
	} else {
		t.Log("Control test passed: localhost accessible without binding")
	}
}

// testBindingToLoopbackSucceeds verifies binding to loopback interface works for local connections
func testBindingToLoopbackSucceeds(t *testing.T) {
	t.Helper()

	// This test verifies that binding to "lo" works for localhost connections
	dc, err := NewDialConfig("lo")
	if err != nil {
		t.Skipf("failed to create DialConfig for 'lo': %v", err)
	}

	// Start a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "loopback test")
	}))
	defer ts.Close()

	// Connect bound to loopback - should work for localhost
	probe := HTTPWithDialer(ts.URL, "loopback test", dc)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = probe.Probe(ctx)
	if err != nil {
		t.Errorf("localhost connection with loopback binding failed: %v", err)
	} else {
		t.Log("Positive test passed: loopback binding works for localhost")
	}
}

// TestDialConfig_SO_BINDTODEVICE_IsSet verifies the Control function is properly set
// when using interface binding on Linux.
func TestDialConfig_SO_BINDTODEVICE_IsSet(t *testing.T) {
	dc, err := NewDialConfig("lo")
	if err != nil {
		t.Skipf("NewDialConfig('lo') failed: %v", err)
	}

	dialer := dc.MakeDialer()

	if dialer.Control == nil {
		t.Error("Dialer.Control is nil when interface name was specified - SO_BINDTODEVICE won't be set")
		return
	}

	t.Log("Control function is set for interface binding")

	// Verify it has the interface name stored
	if dc.interfaceName != "lo" {
		t.Errorf("interfaceName = %q, want 'lo'", dc.interfaceName)
	}
}

// TestDialConfig_IPBinding_vs_InterfaceBinding compares the two binding methods
func TestDialConfig_IPBinding_vs_InterfaceBinding(t *testing.T) {
	// IP-based binding
	dcIP, err := NewDialConfig("127.0.0.1")
	if err != nil {
		t.Fatalf("NewDialConfig('127.0.0.1') failed: %v", err)
	}

	// Interface-based binding
	dcIface, err := NewDialConfig("lo")
	if err != nil {
		t.Skipf("NewDialConfig('lo') failed: %v", err)
	}

	dialerIP := dcIP.MakeDialer()
	dialerIface := dcIface.MakeDialer()

	// IP binding should use LocalAddr
	if dialerIP.LocalAddr == nil {
		t.Error("IP-based binding: LocalAddr is nil")
	}
	if dialerIP.Control != nil {
		t.Error("IP-based binding: Control should be nil (uses LocalAddr)")
	}

	// Interface binding should use Control function
	if dialerIface.Control == nil {
		t.Error("Interface-based binding: Control is nil (should use SO_BINDTODEVICE)")
	}

	t.Logf("IP binding: LocalAddr=%v, Control=%v", dialerIP.LocalAddr != nil, dialerIP.Control != nil)
	t.Logf("Interface binding: LocalAddr=%v, Control=%v", dialerIface.LocalAddr != nil, dialerIface.Control != nil)
}

// Benchmark the overhead of interface binding
func BenchmarkDialConfig_MakeDialer(b *testing.B) {
	b.Run("no_binding", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var dc *DialConfig
			_ = dc.MakeDialer()
		}
	})

	b.Run("IP_binding", func(b *testing.B) {
		dc, _ := NewDialConfig("127.0.0.1")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = dc.MakeDialer()
		}
	})

	b.Run("interface_binding", func(b *testing.B) {
		dc, err := NewDialConfig("lo")
		if err != nil {
			b.Skip("loopback interface not available")
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = dc.MakeDialer()
		}
	})
}
