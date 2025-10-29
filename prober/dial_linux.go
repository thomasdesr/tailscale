// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package prober

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// interfaceName stores the interface name for binding on Linux.
// This is used by the Control function to bind sockets.
type interfaceName string

// setControlForInterface sets up platform-specific socket binding for Linux.
// On Linux, we use SO_BINDTODEVICE to bind directly to the interface name.
func (dc *DialConfig) setControlForInterface(d *net.Dialer, ifname string) {
	iface := interfaceName(ifname)
	d.Control = func(network, address string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, string(iface))
		})
		if err != nil {
			return fmt.Errorf("RawConn.Control: %w", err)
		}
		if sockErr != nil {
			return fmt.Errorf("SO_BINDTODEVICE(%q): %w", string(iface), sockErr)
		}
		return nil
	}
}

// setControlForInterfaceListenConfig sets up platform-specific socket binding for UDP.
func (dc *DialConfig) setControlForInterfaceListenConfig(lc *net.ListenConfig, ifname string) {
	iface := interfaceName(ifname)
	lc.Control = func(network, address string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, string(iface))
		})
		if err != nil {
			return fmt.Errorf("RawConn.Control: %w", err)
		}
		if sockErr != nil {
			return fmt.Errorf("SO_BINDTODEVICE(%q): %w", string(iface), sockErr)
		}
		return nil
	}
}
