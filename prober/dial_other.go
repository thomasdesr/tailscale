// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !darwin

package prober

import (
	"fmt"
	"net"
	"syscall"
)

// setControlForInterface is a no-op on unsupported platforms.
// The dialer will fall back to IP-based binding only.
func (dc *DialConfig) setControlForInterface(d *net.Dialer, ifname string) {
	// Platform doesn't support interface binding via socket options.
	// The dialer will use IP-based binding from the resolved interface address.
	d.Control = func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("interface binding not supported on this platform, use IP address instead")
	}
}

// setControlForInterfaceListenConfig is a no-op on unsupported platforms.
func (dc *DialConfig) setControlForInterfaceListenConfig(lc *net.ListenConfig, ifname string) {
	lc.Control = func(network, address string, c syscall.RawConn) error {
		return fmt.Errorf("interface binding not supported on this platform, use IP address instead")
	}
}
