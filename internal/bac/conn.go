// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

// DefaultTimeout is the APDU timeout when --timeout is 0.
const DefaultTimeout = 3 * time.Second

// DefaultBBMDTTL is the time-to-live of a foreign device registration when --bbmd-ttl is 0.
const DefaultBBMDTTL = 60 * time.Second

// Timeout returns the APDU timeout of a --timeout value in milliseconds.
func Timeout(ms uint32) time.Duration {
	if ms == 0 {
		return DefaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

// Session is the BACnet/IP client of one command.
type Session struct {
	*client.Client
	// Timeout is the APDU timeout: how long one request waits for its answer.
	Timeout time.Duration
	// Retries is how often an unanswered read is sent again.
	Retries int
}

// CheckSocket checks everything about cfg that can be checked without opening a socket.
func CheckSocket(cfg *config.SocketConfig) error {
	if cfg.BBMD != "" {
		if _, err := config.ParseHostPort(cfg.BBMD); err != nil {
			return fmt.Errorf("--bbmd: %w", err)
		}
		if cfg.BBMDTTL == 1 {
			return fmt.Errorf("--bbmd-ttl must be at least 2 seconds")
		}
	}
	if cfg.Interface != "" {
		if _, err := net.InterfaceByName(cfg.Interface); err != nil {
			return fmt.Errorf("--interface %q: no such network interface", cfg.Interface)
		}
	}
	return nil
}

// CheckConn checks the socket settings and the device address of cfg.
func CheckConn(cfg *config.ConnConfig) error {
	if err := CheckSocket(&cfg.SocketConfig); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Device) == "" {
		return fmt.Errorf("no device given: set --device host[:port] (or BACNETCTL_DEVICE)")
	}
	if _, err := config.ParseHostPort(cfg.Device); err != nil {
		return fmt.Errorf("--device: %w", err)
	}
	_, _, err := remoteStation(cfg)
	return err
}

// remoteStation returns the network and MAC of a device behind a router, or ok false for
// a device that is addressed directly.
func remoteStation(cfg *config.ConnConfig) (addr bacnet.Address, ok bool, err error) {
	mac := strings.TrimSpace(cfg.MAC)
	if cfg.Network == 0 && mac == "" {
		return bacnet.Address{}, false, nil
	}
	if cfg.Network == 0 || mac == "" {
		return bacnet.Address{}, false, fmt.Errorf("--network and --mac go together: a device behind a router is addressed by both")
	}
	if cfg.Network == 0xFFFF {
		return bacnet.Address{}, false, fmt.Errorf("--network must be 1-65534")
	}
	raw, err := ParseHex(mac)
	if err != nil || len(raw) == 0 {
		return bacnet.Address{}, false, fmt.Errorf("--mac %q: use hex octets, for example 0a or c0a80114bac0", cfg.MAC)
	}
	m, err := bacnet.NewMAC(raw)
	if err != nil {
		return bacnet.Address{}, false, fmt.Errorf("--mac %q: at most %d octets", cfg.MAC, bacnet.MaxMACLength)
	}
	return bacnet.RemoteStation(cfg.Network, m), true, nil
}

// ResolveEndpoint turns host[:port] into a BACnet/IP endpoint. A host name is resolved to
// its first IPv4 address.
func ResolveEndpoint(ctx context.Context, raw string) (bip.Endpoint, error) {
	hp, err := config.ParseHostPort(raw)
	if err != nil {
		return bip.Endpoint{}, errs.InvalidInput(errs.CodeInvalidAddress, err.Error(), err)
	}
	if addr, err := netip.ParseAddr(hp.Host); err == nil {
		if !addr.Is4() {
			e := fmt.Errorf("invalid address %q: BACnet/IP is IPv4", raw)
			return bip.Endpoint{}, errs.InvalidInput(errs.CodeInvalidAddress, e.Error(), e)
		}
		return bip.NewEndpoint(netip.AddrPortFrom(addr, hp.Port)), nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", hp.Host)
	if err != nil || len(addrs) == 0 {
		if err == nil {
			err = fmt.Errorf("no IPv4 address")
		}
		return bip.Endpoint{}, errs.New(errs.KindTransport, errs.CodeTransportResolveFailed,
			fmt.Sprintf("cannot resolve %q to an IPv4 address: %v", hp.Host, err), err)
	}
	return bip.NewEndpoint(netip.AddrPortFrom(addrs[0].Unmap(), hp.Port)), nil
}

// Open opens the BACnet/IP socket of a command. With --bbmd it registers as a foreign
// device and waits until the BBMD has accepted the registration. logw receives the
// diagnostics of --debug. extra options are applied last.
func Open(ctx context.Context, cfg *config.SocketConfig, logw io.Writer, extra ...client.Option) (*Session, error) {
	if err := CheckSocket(cfg); err != nil {
		return nil, errs.WrapValidation(err)
	}
	timeout := Timeout(cfg.Timeout)
	opts := []client.Option{client.WithTransactionOptions(timeout, int(cfg.Retries), 0)}
	if cfg.LocalPort == 0 {
		// A free port: fine for everything that is answered to the address that asked.
		opts = append(opts, client.WithLocalAddr("0.0.0.0:0"))
	} else {
		opts = append(opts, client.WithPort(int(cfg.LocalPort)))
	}
	if cfg.Interface != "" {
		opts = append(opts, client.WithInterface(cfg.Interface))
	}
	var bbmd bip.Endpoint
	if cfg.BBMD != "" {
		ep, err := ResolveEndpoint(ctx, cfg.BBMD)
		if err != nil {
			return nil, err
		}
		bbmd = ep
		ttl := DefaultBBMDTTL
		if cfg.BBMDTTL != 0 {
			ttl = time.Duration(cfg.BBMDTTL) * time.Second
		}
		opts = append(opts, client.WithForeignDevice(client.ForeignDeviceConfig{BBMD: ep, TTL: ttl}))
	}
	if cfg.Debug && logw != nil {
		opts = append(opts, client.WithLogger(slog.New(slog.NewTextHandler(logw, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	}
	c, err := client.New(append(opts, extra...)...)
	if err != nil {
		return nil, bindError(cfg.LocalPort, err)
	}
	s := &Session{Client: c, Timeout: timeout, Retries: int(cfg.Retries)}
	if cfg.Debug && logw != nil {
		_, _ = fmt.Fprintf(logw, "debug: BACnet/IP socket bound to %s\n", c.LocalEndpoint())
	}
	if bbmd.IsValid() {
		if err := s.awaitRegistration(ctx, bbmd); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return s, nil
}

// registrationPoll is how often the registration state is looked at: the client reports
// it as a state, not as an event.
const registrationPoll = 10 * time.Millisecond

func (s *Session) awaitRegistration(ctx context.Context, bbmd bip.Endpoint) error {
	deadline := time.NewTimer(s.Budget())
	defer deadline.Stop()
	tick := time.NewTicker(registrationPoll)
	defer tick.Stop()
	for !s.ForeignDeviceRegistered() {
		select {
		case <-ctx.Done():
			return WrapError(ctx.Err())
		case <-deadline.C:
			return errs.New(errs.KindTransport, errs.CodeForeignDeviceFailed,
				fmt.Sprintf("foreign device registration failed: the BBMD at %s did not accept it within %s (is it a BBMD, and does it accept foreign devices?)",
					bbmd, s.Budget()), bacnet.ErrTimeout)
		case <-tick.C:
		}
	}
	return nil
}

// Budget is the longest one read can take: the APDU timeout for the request and each of
// its retransmissions.
func (s *Session) Budget() time.Duration {
	return s.Timeout * time.Duration(s.Retries+1)
}

// Target returns the device cfg addresses: the device at --device, or the station
// --network/--mac behind the router at --device.
func (s *Session) Target(ctx context.Context, cfg *config.ConnConfig) (client.Target, error) {
	ep, err := ResolveEndpoint(ctx, cfg.Device)
	if err != nil {
		return client.Target{}, err
	}
	remote, routed, err := remoteStation(cfg)
	if err != nil {
		return client.Target{}, errs.WrapValidation(err)
	}
	if routed {
		return client.Target{Address: remote, Endpoint: ep}, nil
	}
	return EndpointTarget(ep), nil
}

// EndpointTarget is the device that is directly reachable at ep.
func EndpointTarget(ep bip.Endpoint) client.Target {
	ip := ep.Addr.Addr().As4()
	port := ep.Addr.Port()
	mac := bacnet.MustMAC([]byte{ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)})
	return client.Target{Address: bacnet.LocalStation(mac), Endpoint: ep}
}

// TargetText names a target in output: the address of the device, and for a device behind
// a router its network and MAC.
func TargetText(t client.Target) string {
	if t.Address.Scope() == bacnet.AddressRemoteStation {
		return fmt.Sprintf("%s net %d mac %s", t.Endpoint, t.Address.Network(), t.Address.MAC())
	}
	return t.Endpoint.String()
}

// DeviceID learns the Device object identifier of a target. It first reads
// object-identifier of the wildcard device instance, which needs no Who-Is. Not every
// device implements the wildcard (BACpypes3 answers unknown-object): when the device
// refuses that read, a Who-Is is sent to it and the instance is taken from its I-Am.
func (s *Session) DeviceID(ctx context.Context, target client.Target) (bacnet.ObjectIdentifier, error) {
	v, err := s.ReadProperty(ctx, target, bacnet.AnyDevice(), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
	if err != nil {
		var refused *bacnet.ErrorResponse
		var rejected *bacnet.RejectError
		if errors.As(err, &refused) || errors.As(err, &rejected) {
			if id, ok := s.deviceIDByWhoIs(ctx, target); ok {
				return id, nil
			}
		}
		return bacnet.ObjectIdentifier{}, WrapError(err)
	}
	id, err := bacnet.AsObjectID(v)
	if err != nil || id.Type != bacnet.ObjectTypeDevice {
		return bacnet.ObjectIdentifier{}, errs.New(errs.KindProtocol, errs.CodeProtocolViolation,
			"protocol error: the device did not answer with its Device object identifier", err)
	}
	return id, nil
}

// deviceIDByWhoIs asks the target who it is and waits for its I-Am. A device that answers
// by broadcast to port 47808 is only heard when the socket is bound there (--local-port
// 47808); otherwise the wait runs out and the caller reports the refused wildcard read.
func (s *Session) deviceIDByWhoIs(ctx context.Context, target client.Target) (bacnet.ObjectIdentifier, bool) {
	opts := client.DiscoveryOptions{}
	want := target.Address
	if want.Scope() == bacnet.AddressRemoteStation {
		opts.Address = want
	} else {
		want = EndpointTarget(target.Endpoint).Address
	}
	if err := s.SendWhoIs(ctx, target.Endpoint, false, opts); err != nil {
		return bacnet.ObjectIdentifier{}, false
	}
	deadline := time.Now().Add(s.Budget())
	for {
		for _, d := range s.Devices() {
			if d.Address.Equal(want) {
				return d.DeviceID(), true
			}
		}
		if time.Now().After(deadline) {
			return bacnet.ObjectIdentifier{}, false
		}
		select {
		case <-ctx.Done():
			return bacnet.ObjectIdentifier{}, false
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// ResolveObject replaces the wildcard device ("device" without instance) by the Device
// object of the target.
func (s *Session) ResolveObject(ctx context.Context, target client.Target, id bacnet.ObjectIdentifier) (bacnet.ObjectIdentifier, error) {
	if id != bacnet.AnyDevice() {
		return id, nil
	}
	return s.DeviceID(ctx, target)
}

// ParseInstance parses a device instance number of --low and --high.
func ParseInstance(flag, s string) (*uint32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n > bacnet.MaxObjectInstance {
		return nil, fmt.Errorf("--%s %q: a device instance is a number from 0 to %d", flag, s, bacnet.MaxObjectInstance)
	}
	v := uint32(n)
	return &v, nil
}

// InstanceRange parses --low and --high: both or neither.
func InstanceRange(low, high string) (lo, hi *uint32, err error) {
	if lo, err = ParseInstance("low", low); err != nil {
		return nil, nil, err
	}
	if hi, err = ParseInstance("high", high); err != nil {
		return nil, nil, err
	}
	if (lo == nil) != (hi == nil) {
		return nil, nil, fmt.Errorf("--low and --high go together")
	}
	if lo != nil && *lo > *hi {
		return nil, nil, fmt.Errorf("--low %d is above --high %d", *lo, *hi)
	}
	return lo, hi, nil
}

// ParseBroadcastNetwork parses --network of the discovery commands: "" for the default,
// "all" (or 65535) for the global broadcast, or one network number.
func ParseBroadcastNetwork(s string) (bacnet.Address, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return bacnet.Address{}, nil
	case "all", "global", "65535", "0xffff":
		return bacnet.GlobalBroadcast(), nil
	}
	n, err := strconv.ParseUint(s, 0, 16)
	if err != nil || n == 0 {
		return bacnet.Address{}, fmt.Errorf("--network %q: use a network number 1-65534, or all", s)
	}
	return bacnet.RemoteBroadcast(uint16(n)), nil
}

// wsaeaddrinuse is the "address already in use" error of Windows sockets, which the
// syscall package does not map to EADDRINUSE.
const wsaeaddrinuse = 10048

func isAddrInUse(err error) bool {
	return errors.Is(err, errAddrInUse) || isErrno(err, wsaeaddrinuse) ||
		strings.Contains(strings.ToLower(err.Error()), "address already in use")
}

func bindError(port uint16, err error) error {
	switch {
	case errors.Is(err, bacnet.ErrInvalidConfig):
		return errs.InvalidInput(errs.CodeInvalidConfig, fmt.Sprintf("invalid input: %v", err), err)
	case isAddrInUse(err):
		return errs.New(errs.KindTransport, errs.CodeTransportPortInUse, fmt.Sprintf(
			"UDP port %d is in use: another BACnet program on this host (a workstation, a gateway, another bacnetctl) holds it. "+
				"Stop that program, or use --local-port 0 for work that needs no broadcasts (read, write, discover --target)", port), err)
	default:
		return errs.New(errs.KindTransport, errs.CodeTransportBindFailed, fmt.Sprintf("cannot open the BACnet/IP socket: %v", err), err)
	}
}
