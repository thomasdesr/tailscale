// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package prober

import (
	"fmt"
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// setControlForInterface sets up platform-specific socket binding for macOS.
// On macOS, we use IP_BOUND_IF / IPV6_BOUND_IF to bind to an interface by index.
func (dc *DialConfig) setControlForInterface(d *net.Dialer, ifname string) {
	// Get the interface index
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		// If we can't get the interface now, we'll fail when the control function runs
		d.Control = func(network, address string, c syscall.RawConn) error {
			return fmt.Errorf("interface %q not found: %w", ifname, err)
		}
		return
	}

	ifIndex := iface.Index
	d.Control = func(network, address string, c syscall.RawConn) error {
		return bindConnToInterface(c, network, address, ifIndex)
	}
}

// setControlForInterfaceListenConfig sets up platform-specific socket binding for UDP on macOS.
func (dc *DialConfig) setControlForInterfaceListenConfig(lc *net.ListenConfig, ifname string) {
	// Get the interface index
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		lc.Control = func(network, address string, c syscall.RawConn) error {
			return fmt.Errorf("interface %q not found: %w", ifname, err)
		}
		return
	}

	ifIndex := iface.Index
	lc.Control = func(network, address string, c syscall.RawConn) error {
		return bindConnToInterface(c, network, address, ifIndex)
	}
}

// bindConnToInterface binds a connection to a specific interface index on macOS.
// This is adapted from net/netns/netns_darwin.go.
func bindConnToInterface(c syscall.RawConn, network, address string, ifIndex int) error {
	// Determine if this is IPv6 based on the network type or address format
	v6 := strings.Contains(address, "]:") || strings.HasSuffix(network, "6")

	proto := unix.IPPROTO_IP
	opt := unix.IP_BOUND_IF
	if v6 {
		proto = unix.IPPROTO_IPV6
		opt = unix.IPV6_BOUND_IF
	}

	var sockErr error
	err := c.Control(func(fd uintptr) {
		sockErr = unix.SetsockoptInt(int(fd), proto, opt, ifIndex)
	})

	if err != nil {
		return fmt.Errorf("RawConn.Control: %w", err)
	}
	if sockErr != nil {
		return fmt.Errorf("setting IP_BOUND_IF (ifIndex=%d): %w", ifIndex, sockErr)
	}
	return nil
}
