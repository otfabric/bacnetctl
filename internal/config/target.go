// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// DefaultPort is the UDP port of BACnet/IP: 47808, 0xBAC0.
const DefaultPort = 47808

// HostPort is a device address as typed: a host name or IPv4 address and a UDP port.
type HostPort struct {
	Host string
	Port uint16
}

// String returns host:port.
func (h HostPort) String() string {
	return net.JoinHostPort(h.Host, strconv.Itoa(int(h.Port)))
}

// ParseHostPort parses host[:port]. A missing port is 47808. An IPv6 address is refused:
// BACnet/IP is IPv4.
func ParseHostPort(raw string) (HostPort, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return HostPort{}, fmt.Errorf("empty address")
	}
	if strings.Contains(s, "://") || strings.ContainsAny(s, "/ ") {
		return HostPort{}, fmt.Errorf("invalid address %q: use host[:port], for example 192.168.1.20 or 192.168.1.20:47809", raw)
	}
	if strings.HasPrefix(s, "[") || strings.Count(s, ":") > 1 {
		return HostPort{}, fmt.Errorf("invalid address %q: BACnet/IP is IPv4, an IPv6 address cannot be used", raw)
	}
	out := HostPort{Host: s, Port: DefaultPort}
	if host, port, ok := strings.Cut(s, ":"); ok {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return HostPort{}, fmt.Errorf("invalid address %q: the port must be 1-65535", raw)
		}
		out.Host, out.Port = host, uint16(n)
	}
	if out.Host == "" {
		return HostPort{}, fmt.Errorf("invalid address %q: missing host", raw)
	}
	return out, nil
}
