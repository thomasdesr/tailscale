# Testing Network Interface Binding

This document describes how to test the `--bind` flag implementation for `derpprobe` and the underlying `DialConfig` infrastructure.

## Overview

The binding implementation allows forcing all network traffic through a specific interface or source IP. To prove it works, we use **negative testing** - binding to an isolated network interface with no internet access and verifying connections fail.

## Integration Tests

### Test Strategy

The integration tests in `dial_integration_test.go` use an **isolated TUN device** approach:

1. **Create TUN device** with an IP address (e.g., `10.77.77.1/24`)
2. **Do NOT add routes** - makes it a "dead end"
3. **Bind probes to TUN** - force traffic through isolated interface
4. **Verify connection fails** - proves binding worked ✅
5. **Control tests** - verify same connection works without binding

### Running Integration Tests

```bash
# Run all integration tests (requires root/CAP_NET_ADMIN)
sudo go test -v ./prober -run TestDialConfig_WithIsolatedTUN

# Run specific probe type test
sudo go test -v ./prober -run TestDialConfig_WithIsolatedTUN/TLS_bound
sudo go test -v ./prober -run TestDialConfig_WithIsolatedTUN/HTTP_bound
sudo go test -v ./prober -run TestDialConfig_WithIsolatedTUN/UDP_bound

# Run control tests
sudo go test -v ./prober -run TestDialConfig_WithIsolatedTUN/localhost_without_binding
sudo go test -v ./prober -run TestDialConfig_WithIsolatedTUN/binding_to_loopback

# Run all prober tests including unit tests
sudo go test -v ./prober

# Skip integration tests (no root needed)
go test -v -short ./prober
```

### Why Root/CAP_NET_ADMIN is Required

- **Creating TUN devices** requires `CAP_NET_ADMIN`
- **SO_BINDTODEVICE** (Linux) requires `CAP_NET_RAW` or root
- Tests automatically skip if insufficient permissions

### Test Coverage

| Test | Purpose | Expected Result |
|------|---------|-----------------|
| `TLS_bound_to_isolated_TUN_fails` | TLS probe bound to TUN | Connection timeout/failure |
| `HTTP_bound_to_isolated_TUN_fails` | HTTP probe bound to TUN | Connection timeout/failure |
| `UDP_bound_to_isolated_TUN_fails` | UDP/STUN probe bound to TUN | Connection timeout/failure |
| `TCP_dial_bound_to_isolated_TUN_fails` | Raw TCP dial bound to TUN | Connection timeout/failure |
| `localhost_without_binding_succeeds` | Control: normal localhost | Success ✅ |
| `binding_to_loopback_succeeds` | Positive: bind to `lo` | Success ✅ |

### Additional Tests

```bash
# Verify SO_BINDTODEVICE is set
go test -v ./prober -run TestDialConfig_SO_BINDTODEVICE_IsSet

# Compare IP vs interface binding
go test -v ./prober -run TestDialConfig_IPBinding_vs_InterfaceBinding

# Benchmark binding overhead
go test -bench=BenchmarkDialConfig_MakeDialer ./prober
```

## Manual Testing with tcpdump

For visual confirmation, use `tcpdump` to watch packets on the TUN device:

```bash
# Terminal 1: Create isolated TUN device
sudo ip tuntap add mode tun derpprobe_manual
sudo ip addr add 10.88.88.1/24 dev derpprobe_manual
sudo ip link set derpprobe_manual up
# Intentionally DO NOT add any routes

# Terminal 2: Start packet capture
sudo tcpdump -i derpprobe_manual -n

# Terminal 3: Try to connect bound to TUN (will fail but packets visible)
# This requires building derpprobe first
sudo ./derpprobe --once --bind derpprobe_manual --derp-map local

# Observe: You should see packets on derpprobe_manual interface
# But connections will fail (no routes) - proving binding works

# Cleanup
sudo ip link delete derpprobe_manual
```

## Manual Testing with Real Network Interfaces

```bash
# List available interfaces
ip link show

# Test binding to a real interface (e.g., eth0)
./derpprobe --once --bind eth0 --derp-map https://login.tailscale.com/derpmap/default

# Test binding to a specific IP
./derpprobe --once --bind 192.168.1.100 --derp-map https://login.tailscale.com/derpmap/default

# Test binding to loopback for local testing
./derpprobe --once --bind lo --derp-map local
```

## Expected Behavior

### Successful Binding

When binding works correctly:
- **With isolated TUN**: All probes fail (timeout/unreachable)
- **With real interface**: Probes use that interface's route
- **With loopback**: Can reach localhost services

### Binding Failure Indicators

If binding is broken, you would see:
- Connections succeed despite isolated TUN (uses default route instead)
- No packets appear on bound interface when using tcpdump
- No errors but traffic goes through wrong interface

## Platform Differences

### Linux
- Uses `SO_BINDTODEVICE` socket option
- Binds directly to interface by name
- Most robust implementation
- Requires `CAP_NET_RAW` or root

### macOS
- Uses `IP_BOUND_IF` / `IPV6_BOUND_IF` socket options
- Binds to interface by index (resolved from name)
- Well-tested approach from `net/netns`

### Other Platforms
- Falls back to IP-based binding only
- Interface names will fail with clear error
- IP addresses still work

## Troubleshooting

### Tests Skip with "needs CAP_NET_ADMIN"
```bash
# Run with sudo
sudo -E go test -v ./prober -run TestDialConfig_WithIsolatedTUN
```

### "Interface not found" errors
```bash
# Verify interface exists
ip link show

# Check interface name is correct
ip addr show dev lo
```

### Tests pass but suspect binding doesn't work
```bash
# Use tcpdump to verify packets on interface
sudo tcpdump -i <interface> -n

# Check if Control function is set
go test -v ./prober -run TestDialConfig_SO_BINDTODEVICE_IsSet
```

## References

- Integration tests: `prober/dial_integration_test.go`
- Platform-specific binding: `prober/dial_linux.go`, `prober/dial_darwin.go`
- Core binding logic: `prober/dial.go`
- Inspired by: `net/netns/netns_linux.go`, `net/netns/netns_darwin.go`
