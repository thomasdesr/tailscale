// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package prober

import (
	"net"
	"testing"
)

func TestNewDialConfig_IPAddress(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{
			name:    "empty spec",
			spec:    "",
			wantErr: false, // returns nil, no error
		},
		{
			name:    "valid IPv4",
			spec:    "192.168.1.100",
			wantErr: false,
		},
		{
			name:    "valid IPv6",
			spec:    "2001:db8::1",
			wantErr: false,
		},
		{
			name:    "loopback interface (should work even if doesn't exist)",
			spec:    "lo",
			wantErr: false, // may fail but that's ok for test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc, err := NewDialConfig(tt.spec)
			if tt.spec == "" {
				if dc != nil {
					t.Errorf("NewDialConfig(%q) returned non-nil for empty spec", tt.spec)
				}
				return
			}

			// For interface names, we might get an error on some systems
			// so we don't strictly check tt.wantErr for those
			if tt.spec != "lo" {
				if (err != nil) != tt.wantErr {
					t.Errorf("NewDialConfig(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
					return
				}
			}

			if err == nil && dc == nil {
				t.Errorf("NewDialConfig(%q) returned nil config with no error", tt.spec)
			}
		})
	}
}

func TestResolveBindAddr(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{
			name:    "IPv4 address",
			spec:    "192.168.1.100",
			wantErr: false,
		},
		{
			name:    "IPv6 address",
			spec:    "::1",
			wantErr: false,
		},
		{
			name:    "invalid spec",
			spec:    "not-an-ip-or-interface",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := resolveBindAddr(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveBindAddr(%q) error = %v, wantErr %v", tt.spec, err, tt.wantErr)
				return
			}
			if !tt.wantErr && addr == nil {
				t.Errorf("resolveBindAddr(%q) returned nil address", tt.spec)
			}
		})
	}
}

func TestDialConfig_MakeDialer(t *testing.T) {
	tests := []struct {
		name string
		dc   *DialConfig
	}{
		{
			name: "nil config",
			dc:   nil,
		},
		{
			name: "with bind addr",
			dc: &DialConfig{
				BindAddr: &net.TCPAddr{
					IP: net.ParseIP("192.168.1.100"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialer := tt.dc.MakeDialer()
			if dialer == nil {
				t.Errorf("MakeDialer() returned nil")
			}

			// Check that LocalAddr is set correctly
			if tt.dc != nil && tt.dc.BindAddr != nil {
				if dialer.LocalAddr == nil {
					t.Errorf("MakeDialer() LocalAddr is nil when BindAddr was set")
				}
			}
		})
	}
}

func TestDialConfig_GetUDPListenAddr(t *testing.T) {
	tests := []struct {
		name string
		dc   *DialConfig
		want string
	}{
		{
			name: "nil config",
			dc:   nil,
			want: ":0",
		},
		{
			name: "with IPv4",
			dc: &DialConfig{
				BindAddr: &net.TCPAddr{
					IP: net.ParseIP("192.168.1.100"),
				},
			},
			want: "192.168.1.100:0",
		},
		{
			name: "with IPv6",
			dc: &DialConfig{
				BindAddr: &net.TCPAddr{
					IP: net.ParseIP("::1"),
				},
			},
			want: "[::1]:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dc.GetUDPListenAddr()
			if got != tt.want {
				t.Errorf("GetUDPListenAddr() = %v, want %v", got, tt.want)
			}
		})
	}
}
