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
	"sync"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bbmd"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// Defaults of `server run`.
const (
	DefaultServerMaxAPDU     = 1476
	DefaultServerMaxSegments = 64
	// serverWindow is the proposed window size of segmented replies (go-bacnet's default).
	serverWindow = 4
	// maxServerPassword is the longest password go-bacnet accepts, in octets.
	maxServerPassword = 20
	// serverQueue bounds the records that wait for stdout.
	serverQueue = 256
)

// BuiltInDevice names the device file of `server run` without --config.
const BuiltInDevice = "(built-in example device)"

// ServerRequest is a validated `server run` invocation.
type ServerRequest struct {
	File *DeviceFile
	// Source is the path of the device file, or BuiltInDevice.
	Source string
	Listen netip.Addr
	Port   uint16
	// BBMD is set with --bbmd: the BDT entries of the peers and the BBMD's own address
	// (Self, whose port is that of the server when SelfPort is false).
	BBMD     bool
	BDT      []bvlc.BDTEntry
	Self     netip.Addr
	SelfPort uint16
}

// ParseServer validates cfg and reads the device file. Nothing is bound.
func ParseServer(cfg *config.ServerRunConfig, password string) (ServerRequest, error) {
	req := ServerRequest{Source: BuiltInDevice, Listen: netip.IPv4Unspecified()}
	if strings.TrimSpace(cfg.DeviceFile) != "" {
		req.Source = cfg.DeviceFile
		df, err := LoadDeviceFile(cfg.DeviceFile)
		if err != nil {
			return req, err
		}
		req.File = df
	} else {
		req.File = DefaultDeviceFile()
	}
	if err := applyOverrides(req.File, cfg, req.Source == BuiltInDevice); err != nil {
		return req, err
	}
	if _, err := req.File.Build(); err != nil {
		return req, configError(req.Source, err)
	}

	if s := strings.TrimSpace(cfg.Listen); s != "" {
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() {
			return req, fmt.Errorf("--listen %q: give an IPv4 address of this host, such as 192.168.1.10 or 127.0.0.1", cfg.Listen)
		}
		req.Listen = a
	}
	req.Port = req.File.Port
	if req.Port == 0 {
		req.Port = config.DefaultPort
	}
	if s := strings.TrimSpace(cfg.Port); s != "" {
		n, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return req, fmt.Errorf("--port %q: a UDP port is 0-65535", cfg.Port)
		}
		req.Port = uint16(n)
	}
	if cfg.Interface != "" {
		if _, err := net.InterfaceByName(cfg.Interface); err != nil {
			return req, fmt.Errorf("--interface %q: no such network interface", cfg.Interface)
		}
	}
	if cfg.MaxAPDU < 50 || cfg.MaxAPDU > DefaultServerMaxAPDU {
		return req, fmt.Errorf("--max-apdu %d: use 50-%d", cfg.MaxAPDU, DefaultServerMaxAPDU)
	}
	if cfg.MaxSegments == 1 {
		return req, errors.New("--max-segments 1: use 2-255, or 0 for no segmentation")
	}
	if len(password) > maxServerPassword {
		return req, fmt.Errorf("--password: at most %d octets", maxServerPassword)
	}
	if err := parseBBMD(cfg, &req); err != nil {
		return req, err
	}
	return req, nil
}

// applyOverrides applies --instance, --name and --vendor-id to the device file.
func applyOverrides(df *DeviceFile, cfg *config.ServerRunConfig, builtIn bool) error {
	dev := -1
	for i, o := range df.Objects {
		if t, err := ParseObjectType(o.Type); err == nil && t == bacnet.ObjectTypeDevice {
			dev = i
			break
		}
	}
	entry := func() *DeviceObject {
		if dev < 0 {
			df.Objects = append(df.Objects, DeviceObject{Type: "device", Instance: df.DeviceInstance, ObjectName: df.DeviceName})
			dev = len(df.Objects) - 1
		}
		return &df.Objects[dev]
	}
	if s := strings.TrimSpace(cfg.Instance); s != "" {
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil || n >= bacnet.MaxObjectInstance {
			return fmt.Errorf("--instance %q: a device instance is a number from 0 to %d", cfg.Instance, bacnet.MaxObjectInstance-1)
		}
		df.DeviceInstance = uint32(n)
		entry().Instance = df.DeviceInstance
		if builtIn && strings.TrimSpace(cfg.Name) == "" {
			df.DeviceName = fmt.Sprintf("bacnetctl-%d", n)
			entry().ObjectName = df.DeviceName
		}
	}
	if s := strings.TrimSpace(cfg.Name); s != "" {
		df.DeviceName = s
		entry().ObjectName = s
	}
	if s := strings.TrimSpace(cfg.VendorID); s != "" {
		n, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return fmt.Errorf("--vendor-id %q: a vendor identifier is a number from 0 to 65535", cfg.VendorID)
		}
		v := uint16(n)
		entry().VendorID = &v
	}
	return nil
}

func parseBBMD(cfg *config.ServerRunConfig, req *ServerRequest) error {
	if !cfg.BBMD {
		switch {
		case len(cfg.BDT) > 0:
			return errors.New("--bdt needs --bbmd")
		case cfg.BBMDAddress != "":
			return errors.New("--bbmd-address needs --bbmd")
		case cfg.NoForeign:
			return errors.New("--no-foreign-devices needs --bbmd")
		}
		return nil
	}
	req.BBMD = true
	for _, raw := range cfg.BDT {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		e, err := parseBDTEntry(raw)
		if err != nil {
			return err
		}
		req.BDT = append(req.BDT, e)
	}
	switch {
	case cfg.BBMDAddress != "":
		addr, port, err := parseIPv4Port(cfg.BBMDAddress)
		if err != nil || !unicast(addr) {
			return fmt.Errorf("--bbmd-address %q: give the unicast IPv4 address of this host, host[:port]", cfg.BBMDAddress)
		}
		req.Self, req.SelfPort = addr, port
	case !req.Listen.IsUnspecified():
		req.Self = req.Listen
	case cfg.Interface != "":
		a, err := interfaceAddr(cfg.Interface)
		if err != nil {
			return err
		}
		req.Self = a
	default:
		return errors.New("--bbmd needs the BBMD's own address: give --listen with an address of this host, --interface, or --bbmd-address")
	}
	return nil
}

// unicast reports whether a is an address one host can have.
func unicast(a netip.Addr) bool {
	return !a.IsUnspecified() && !a.IsMulticast() && a != netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// parseIPv4Port parses an IPv4 address with an optional port (0 when absent).
func parseIPv4Port(s string) (netip.Addr, uint16, error) {
	s = strings.TrimSpace(s)
	if ap, err := netip.ParseAddrPort(s); err == nil {
		if !ap.Addr().Is4() {
			return netip.Addr{}, 0, errors.New("not IPv4")
		}
		return ap.Addr(), ap.Port(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return netip.Addr{}, 0, errors.New("not an IPv4 address")
	}
	return a, 0, nil
}

// parseBDTEntry parses a --bdt entry: host[:port][/mask].
func parseBDTEntry(raw string) (bvlc.BDTEntry, error) {
	bad := func() (bvlc.BDTEntry, error) {
		return bvlc.BDTEntry{}, fmt.Errorf("--bdt %q: use an IPv4 address with an optional port and mask, such as 10.0.20.1, 10.0.20.1:47808 or 10.0.20.1/255.255.255.0", raw)
	}
	host, maskText, hasMask := strings.Cut(strings.TrimSpace(raw), "/")
	addr, port, err := parseIPv4Port(host)
	if err != nil || !unicast(addr) {
		return bad()
	}
	if port == 0 {
		port = config.DefaultPort
	}
	mask := bvlc.IPv4Mask{255, 255, 255, 255}
	if hasMask {
		m, err := netip.ParseAddr(maskText)
		if err != nil || !m.Is4() {
			return bad()
		}
		mask = bvlc.IPv4Mask(m.As4())
	}
	return bvlc.NewBDTEntry(bip.NewEndpoint(netip.AddrPortFrom(addr, port)), mask)
}

// interfaceAddr is the first IPv4 address of the interface name.
func interfaceAddr(name string) (netip.Addr, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("--interface %q: no such network interface", name)
	}
	addrs, err := ifi.Addrs()
	if err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				ip, _ := netip.AddrFromSlice(ipn.IP.To4())
				return ip, nil
			}
		}
	}
	return netip.Addr{}, fmt.Errorf("--interface %q has no IPv4 address for the BBMD: give --bbmd-address", name)
}

// CheckServer checks everything about a `server run` invocation that can be checked
// without binding a socket.
func CheckServer(cfg *config.ServerRunConfig, password string) error {
	_, err := ParseServer(cfg, password)
	return err
}

// recordQueue passes records from the server's workers to the goroutine that prints
// them, without ever blocking a worker: a record that does not fit is dropped, and the
// next one carries the gap mark.
type recordQueue struct {
	ch      chan *types.ServerRecord
	mu      sync.Mutex
	lost    bool
	dropped int
}

func (q *recordQueue) push(r *types.ServerRecord) {
	q.mu.Lock()
	defer q.mu.Unlock()
	r.Gap = q.lost
	select {
	case q.ch <- r:
		q.lost = false
	default:
		q.lost = true
		q.dropped++
	}
}

// serverHooks are the hooks that report the requests that change the device.
func serverHooks(q *recordQueue, readOnly bool) server.Hooks {
	status := types.ServerAllowed
	if readOnly {
		status = types.ServerRefused
	}
	return server.Hooks{
		BeforeWrite: func(_ context.Context, obj bacnet.ObjectIdentifier, w object.Write) error {
			o, p := objRef(obj), propRef(w.Property)
			v := ValueOf(obj.Type, w.Property.Identifier, w.Value)
			rec := &types.ServerRecord{Event: types.ServerWrite, ObjectRef: &o, PropertyRef: &p, Value: &v, Status: status}
			if w.Priority != 0 {
				prio := w.Priority
				rec.Priority = &prio
			}
			q.push(rec)
			if readOnly {
				return object.Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
			}
			return nil
		},
		BeforeDelete: func(_ context.Context, obj bacnet.ObjectIdentifier) error {
			o := objRef(obj)
			q.push(&types.ServerRecord{Event: types.ServerDelete, ObjectRef: &o, Status: status})
			if readOnly {
				return object.Err(bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
			}
			return nil
		},
		Reinitialize: func(_ context.Context, state service.ReinitializedStateOfDevice) error {
			// The simulated device has no backup or restore procedure (clause 19.1):
			// it refuses those states as go-bacnet's server does without a hook,
			// and as recorded devices without backup support do.
			if state >= service.ReinitializedStartBackup && state <= service.ReinitializedAbortRestore {
				q.push(&types.ServerRecord{Event: types.ServerReinitialize, State: reinitStateName(state), Status: types.ServerRefused})
				return object.Err(bacnet.ErrorClassServices, bacnet.ErrorCodeServiceRequestDenied)
			}
			q.push(&types.ServerRecord{Event: types.ServerReinitialize, State: reinitStateName(state), Status: types.ServerAllowed})
			return nil
		},
		TimeSync: func(t service.TimeSynchronization, utc bool) {
			u := utc
			q.push(&types.ServerRecord{Event: types.ServerTimeSync, Time: DateText(t.Date) + " " + TimeText(t.Time), UTC: &u})
		},
	}
}

func reinitStateName(s service.ReinitializedStateOfDevice) string {
	for name, v := range reinitStates {
		if v == s {
			return name
		}
	}
	return strconv.Itoa(int(s))
}

// Serve serves the device of cfg until ctx ends or --duration has passed, and emits a
// record when it listens and for every request that changes the device.
func Serve(ctx context.Context, cfg *config.ServerRunConfig, password string, logw io.Writer, emit func(*types.ServerRecord) error) (summary types.ServerSummary, err error) {
	req, err := ParseServer(cfg, password)
	if err != nil {
		return summary, errs.WrapValidation(err)
	}
	q := &recordQueue{ch: make(chan *types.ServerRecord, serverQueue)}
	dev, err := req.File.build(func(id bacnet.ObjectIdentifier) {
		o := objRef(id)
		q.push(&types.ServerRecord{Event: types.ServerCreate, ObjectRef: &o})
	})
	if err != nil {
		return summary, configError(req.Source, err)
	}
	devID := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: req.File.DeviceInstance}
	summary.Device = ObjectName(devID)

	tr, err := bip.ListenUDP(bip.UDPConfig{Port: int(req.Port), LocalAddr: netip.AddrPortFrom(req.Listen, req.Port).String(), Interface: cfg.Interface})
	if err != nil {
		return summary, bindError(req.Port, err)
	}
	local := tr.Local()
	summary.Address = local.String()

	opts := []server.Option{
		server.WithTransport(tr),
		server.WithMaxAPDU(cfg.MaxAPDU),
		server.WithSegmentation(int(cfg.MaxSegments), serverWindow),
		server.WithHooks(serverHooks(q, cfg.ReadOnly)),
	}
	if password != "" {
		opts = append(opts, server.WithPassword(password))
	}
	if cfg.Debug && logw != nil {
		opts = append(opts, server.WithLogger(slog.New(slog.NewTextHandler(logw, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	}
	if req.BBMD {
		b, err := newBBMD(req, local, cfg.NoForeign)
		if err != nil {
			_ = tr.Close()
			return summary, err
		}
		opts = append(opts, server.WithBBMD(b))
	}
	s, err := server.New(dev, opts...)
	if err != nil {
		_ = tr.Close()
		if errors.Is(err, server.ErrInvalidConfig) {
			return summary, configError(req.Source, err)
		}
		return summary, bindError(req.Port, err)
	}
	defer func() { _ = s.Close() }()

	start := time.Now()
	defer func() { summary.Duration = ms(time.Since(start)) }()
	if logw != nil {
		_, _ = fmt.Fprintf(logw, "serving %s %q from %s on %s%s (Ctrl-C to stop)\n", summary.Device, req.File.DeviceName, req.Source, local, serverNotes(req, password, cfg.ReadOnly))
	}
	out := func(r *types.ServerRecord) error {
		r.Timestamp = Now()
		if err := emit(r); err != nil {
			return errs.Output(errs.CodeOutputWriteFailed, err)
		}
		summary.Records++
		return nil
	}
	o := objRef(devID)
	n := len(dev.ObjectIdentifiers())
	if err := out(&types.ServerRecord{Event: types.ServerListening, Address: local.String(), Name: req.File.DeviceName, Objects: &n, ObjectRef: &o}); err != nil {
		return summary, err
	}
	if err := s.IAm(); err != nil && cfg.Debug && logw != nil {
		_, _ = fmt.Fprintf(logw, "debug: the I-Am broadcast was not sent: %v\n", err)
	}

	stop, cancel := stopTimer(cfg.Duration)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return summary, drain(s, q, out, &summary)
		case <-stop:
			return summary, drain(s, q, out, &summary)
		case r := <-q.ch:
			if err := out(r); err != nil {
				return summary, err
			}
		}
	}
}

// drain stops the server and prints the records that are still queued.
func drain(s *server.Server, q *recordQueue, out func(*types.ServerRecord) error, summary *types.ServerSummary) error {
	_ = s.Close()
	defer func() {
		q.mu.Lock()
		summary.Dropped = q.dropped
		q.mu.Unlock()
	}()
	for {
		select {
		case r := <-q.ch:
			if err := out(r); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func newBBMD(req ServerRequest, local bip.Endpoint, noForeign bool) (*bbmd.BBMD, error) {
	port := req.SelfPort
	if port == 0 {
		port = local.Addr.Port()
	}
	self := bip.NewEndpoint(netip.AddrPortFrom(req.Self, port))
	bdt := req.BDT
	listed := false
	for _, e := range bdt {
		listed = listed || e.Endpoint == self
	}
	if !listed {
		bdt = append([]bvlc.BDTEntry{{Endpoint: self, Mask: bvlc.IPv4Mask{255, 255, 255, 255}}}, bdt...)
	}
	b, err := bbmd.New(bbmd.Config{Self: self, BDT: bdt, RejectRegistrations: noForeign})
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInvalidConfig, fmt.Sprintf("invalid input: BBMD: %v", err), err)
	}
	return b, nil
}

// serverNotes is what the start line on stderr says about the configuration. The password
// itself is never printed.
func serverNotes(req ServerRequest, password string, readOnly bool) string {
	var notes []string
	if req.BBMD {
		notes = append(notes, fmt.Sprintf("BBMD with %s", plural(len(req.BDT), "peer", "peers")))
	}
	if password != "" {
		notes = append(notes, "password required for device management")
	}
	if readOnly {
		notes = append(notes, "read-only")
	}
	if len(notes) == 0 {
		return ""
	}
	return ", " + strings.Join(notes, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
