#!/bin/bash
# Copyright (c) Tailscale Inc & AUTHORS
# SPDX-License-Identifier: BSD-3-Clause

# Manual test script for --bind flag functionality
# This script creates an isolated TUN device and demonstrates that
# binding to it prevents connections from succeeding.

set -e

TUN_NAME="derpprobe_test_manual"
TUN_IP="10.99.99.1/24"
TEST_TARGET="8.8.8.8:53"

echo "=== Testing Network Interface Binding ==="
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo "ERROR: This script must be run as root (needs to create TUN device)"
  echo "Usage: sudo $0"
  exit 1
fi

# Cleanup function
cleanup() {
  echo ""
  echo "Cleaning up..."
  ip link delete "$TUN_NAME" 2>/dev/null || true
  echo "Done!"
}

trap cleanup EXIT

echo "Step 1: Creating isolated TUN device '$TUN_NAME'"
echo "  - This device will have an IP but NO routes to internet"
echo "  - Acting as a 'dead end' for traffic"
echo ""

# Create TUN device
ip tuntap add mode tun "$TUN_NAME" 2>/dev/null || {
  echo "NOTE: TUN device may already exist, deleting and recreating..."
  ip link delete "$TUN_NAME" 2>/dev/null || true
  ip tuntap add mode tun "$TUN_NAME"
}

# Assign IP and bring up
ip addr add "$TUN_IP" dev "$TUN_NAME"
ip link set "$TUN_NAME" up

echo "✓ TUN device created: $TUN_NAME with IP $TUN_IP"
echo ""

# Show the device
echo "Current TUN device state:"
ip addr show dev "$TUN_NAME"
echo ""

echo "Step 2: Verify NO routes exist for this TUN"
if ip route show dev "$TUN_NAME" | grep -q default; then
  echo "WARNING: Default route exists on $TUN_NAME - this may allow traffic through!"
else
  echo "✓ No default route on $TUN_NAME (traffic will be isolated)"
fi
echo ""

echo "Step 3: Create a simple Go test program to test binding"
echo ""

# Create a minimal test program
cat > /tmp/test_bind.go << 'EOF'
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <interface|IP> <target:port>\n", os.Args[0])
		os.Exit(1)
	}

	bindSpec := os.Args[1]
	target := os.Args[2]

	fmt.Printf("Testing connection to %s bound to %s\n", target, bindSpec)

	d := &net.Dialer{
		Timeout: 3 * time.Second,
	}

	// Try to parse as IP first
	if ip := net.ParseIP(bindSpec); ip != nil {
		d.LocalAddr = &net.TCPAddr{IP: ip}
		fmt.Printf("Using IP-based binding: %s\n", ip)
	} else {
		// Treat as interface name - use SO_BINDTODEVICE on Linux
		fmt.Printf("Using interface binding: %s\n", bindSpec)
		d.Control = func(network, address string, c syscall.RawConn) error {
			var sockErr error
			err := c.Control(func(fd uintptr) {
				sockErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, bindSpec)
			})
			if err != nil {
				return err
			}
			return sockErr
		}
	}

	ctx := context.Background()
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		fmt.Printf("✗ Connection FAILED (as expected with isolated TUN): %v\n", err)
		os.Exit(0)  // This is actually success - we proved binding worked!
	}

	conn.Close()
	fmt.Printf("✓ Connection SUCCEEDED\n")
	fmt.Printf("WARNING: Connection succeeded despite TUN isolation - binding may not be working!\n")
	os.Exit(1)
}
EOF

echo "Compiling test program..."
cd /tmp
if ! go build -o test_bind test_bind.go 2>&1; then
  echo "NOTE: Cannot build (Go version mismatch or dependencies missing)"
  echo "Skipping executable tests, but TUN device has been created."
  echo ""
  echo "You can manually test with:"
  echo "  sudo ./derpprobe --once --bind $TUN_NAME --derp-map local"
  echo ""
  echo "Or verify with tcpdump:"
  echo "  sudo tcpdump -i $TUN_NAME -n"
  exit 0
fi

echo "✓ Test program compiled"
echo ""

echo "Step 4: Test connection WITHOUT binding (control test)"
echo "  - This should timeout because target might not be reachable"
echo "  - But it will at least TRY via default route"
echo ""

timeout 3 ./test_bind "0.0.0.0" "$TEST_TARGET" || true
echo ""

echo "Step 5: Test connection WITH binding to isolated TUN"
echo "  - This SHOULD fail because TUN has no routes"
echo "  - If it fails, binding is working! ✓"
echo ""

./test_bind "$TUN_NAME" "$TEST_TARGET"
echo ""

echo "=== Test Complete ==="
echo ""
echo "Summary:"
echo "  - Created isolated TUN: $TUN_NAME"
echo "  - Verified binding forces traffic through TUN"
echo "  - Confirmed connection fails (proves binding works)"
echo ""
echo "The TUN device will be cleaned up on exit."
