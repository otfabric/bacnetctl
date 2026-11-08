// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// served is a server of Serve that runs in the background.
type served struct {
	records chan *types.ServerRecord
	target  client.Target
	cancel  context.CancelFunc
	done    chan struct{}
	summary types.ServerSummary
	err     error
}

// serve runs Serve with cfg on loopback, on a free port, until the test ends or stop is
// called. It waits for the listening record.
func serve(t *testing.T, cfg config.ServerRunConfig, password string) *served {
	t.Helper()
	cfg.Listen, cfg.Port = "127.0.0.1", "0"
	if cfg.MaxAPDU == 0 {
		cfg.MaxAPDU = DefaultServerMaxAPDU
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &served{records: make(chan *types.ServerRecord, 64), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		s.summary, s.err = Serve(ctx, &cfg, password, io.Discard, func(r *types.ServerRecord) error {
			s.records <- r
			return nil
		})
	}()
	t.Cleanup(func() { s.stop(t) })
	r := s.next(t)
	if r.Event != types.ServerListening || r.ObjectRef == nil || r.Objects == nil {
		t.Fatalf("first record: %+v", r)
	}
	ep, err := netip.ParseAddrPort(r.Address)
	if err != nil {
		t.Fatal(err)
	}
	s.target = client.Target{Endpoint: bip.NewEndpoint(ep)}
	return s
}

func (s *served) next(t *testing.T) *types.ServerRecord {
	t.Helper()
	select {
	case r := <-s.records:
		return r
	case <-s.done:
		t.Fatalf("the server ended: %v", s.err)
	case <-time.After(10 * time.Second):
		t.Fatal("no record")
	}
	return nil
}

func (s *served) stop(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
}

func testClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(client.WithLocalAddr("127.0.0.1:0"), client.WithTransactionOptions(2*time.Second, 1, 0),
		client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

var pvRef = bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

// The built-in device read and written by the go-bacnet client, and the records of what
// changed it.
func TestServe(t *testing.T) {
	s := serve(t, config.ServerRunConfig{MaxSegments: DefaultServerMaxSegments}, "secret")
	c := testClient(t)
	ctx := t.Context()
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	v, err := c.ReadProperty(ctx, s.target, av, pvRef)
	if err != nil || v.Real != 21 {
		t.Fatalf("read: %+v %v", v, err)
	}
	prio := uint8(8)
	if err := c.WriteProperty(ctx, s.target, av, pvRef, bacnet.RealValue(23.5), &prio); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.ReadProperty(ctx, s.target, av, pvRef); v.Real != 23.5 {
		t.Errorf("after the write: %+v", v)
	}
	r := s.next(t)
	if r.Event != types.ServerWrite || r.Object != "analog-value:1" || r.Property != "present-value" || r.Value.String() != "23.5" ||
		r.Priority == nil || *r.Priority != 8 || r.Status != types.ServerAllowed {
		t.Errorf("write record: %+v", r)
	}

	// A created object can be deleted; both are recorded.
	ack, err := c.CreateObject(ctx, s.target, service.CreateObjectRequest{ObjectType: &av.Type})
	if err != nil {
		t.Fatal(err)
	}
	if r := s.next(t); r.Event != types.ServerCreate || r.Object != ObjectName(ack.Object) {
		t.Errorf("create record: %+v", r)
	}
	if err := c.DeleteObject(ctx, s.target, service.DeleteObjectRequest(ack)); err != nil {
		t.Fatal(err)
	}
	if r := s.next(t); r.Event != types.ServerDelete || r.Status != types.ServerAllowed {
		t.Errorf("delete record: %+v", r)
	}

	// Device management needs the password, and is recorded.
	if err := c.ReinitializeDevice(ctx, s.target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart}); err == nil {
		t.Error("reinitialize without the password was accepted")
	}
	pw := bacnet.CharacterString{Value: "secret"}
	if err := c.ReinitializeDevice(ctx, s.target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart, Password: &pw}); err != nil {
		t.Fatal(err)
	}
	if r := s.next(t); r.Event != types.ServerReinitialize || r.State != "warmstart" || r.Status != types.ServerAllowed {
		t.Errorf("reinitialize record: %+v", r)
	}
	// No backup or restore procedure: those states are refused, as go-bacnet's
	// server and recorded devices without backup support refuse them.
	for _, st := range []service.ReinitializedStateOfDevice{service.ReinitializedStartBackup, service.ReinitializedEndBackup,
		service.ReinitializedStartRestore, service.ReinitializedEndRestore, service.ReinitializedAbortRestore} {
		var er *bacnet.ErrorResponse
		err := c.ReinitializeDevice(ctx, s.target, service.ReinitializeDeviceRequest{State: st, Password: &pw})
		if !errors.As(err, &er) || er.Class != uint16(bacnet.ErrorClassServices) || er.Code != uint16(bacnet.ErrorCodeServiceRequestDenied) {
			t.Fatalf("state %d: %v", st, err)
		}
		if r := s.next(t); r.Event != types.ServerReinitialize || r.State != reinitStateName(st) || r.Status != types.ServerRefused {
			t.Errorf("refused reinitialize record: %+v", r)
		}
	}
	for _, st := range []service.ReinitializedStateOfDevice{service.ReinitializedColdstart, service.ReinitializedActivateChanges} {
		if err := c.ReinitializeDevice(ctx, s.target, service.ReinitializeDeviceRequest{State: st, Password: &pw}); err != nil {
			t.Fatalf("state %d: %v", st, err)
		}
		if r := s.next(t); r.State != reinitStateName(st) || r.Status != types.ServerAllowed {
			t.Errorf("reinitialize record: %+v", r)
		}
	}
	now := time.Now()
	d, tm := DateTimeOf(now)
	if err := c.UTCTimeSynchronization(ctx, s.target.Endpoint, false, service.TimeSynchronization{Date: d, Time: tm}); err != nil {
		t.Fatal(err)
	}
	if r := s.next(t); r.Event != types.ServerTimeSync || r.UTC == nil || !*r.UTC || !strings.HasPrefix(r.Time, now.Format("2006-01-02")) {
		t.Errorf("time sync record: %+v", r)
	}

	s.stop(t)
	if s.err != nil || s.summary.Records != 13 || s.summary.Device != "device:1234" || !strings.HasPrefix(s.summary.Address, "127.0.0.1:") {
		t.Errorf("summary: %+v %v", s.summary, s.err)
	}
}

// --read-only refuses writes and deletes, and records them as refused.
func TestServeReadOnly(t *testing.T) {
	s := serve(t, config.ServerRunConfig{ReadOnly: true}, "")
	c := testClient(t)
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	err := c.WriteProperty(t.Context(), s.target, av, pvRef, bacnet.RealValue(1), nil)
	var refused *bacnet.ErrorResponse
	if !errors.As(err, &refused) || refused.Code != bacnet.ErrorCodeWriteAccessDenied {
		t.Errorf("write: %v", err)
	}
	if r := s.next(t); r.Status != types.ServerRefused || r.Priority != nil {
		t.Errorf("record: %+v", r)
	}
	ack, err := c.CreateObject(t.Context(), s.target, service.CreateObjectRequest{ObjectType: &av.Type})
	if err != nil {
		t.Fatal(err)
	}
	s.next(t)
	if err := c.DeleteObject(t.Context(), s.target, service.DeleteObjectRequest(ack)); !errors.As(err, &refused) || refused.Code != bacnet.ErrorCodeObjectDeletionNotPermitted {
		t.Errorf("delete: %v", err)
	}
	if r := s.next(t); r.Event != types.ServerDelete || r.Status != types.ServerRefused {
		t.Errorf("record: %+v", r)
	}
}

// A BBMD shares the endpoint: its table holds itself and the peers.
func TestServeBBMD(t *testing.T) {
	s := serve(t, config.ServerRunConfig{BBMD: true, BDT: []string{"10.0.0.1"}}, "")
	c := testClient(t)
	bdt, err := c.ReadBroadcastDistributionTable(t.Context(), s.target.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if len(bdt) != 2 || bdt[0].Endpoint != s.target.Endpoint || bdt[1].Endpoint.String() != "10.0.0.1:47808" {
		t.Errorf("BDT: %+v", bdt)
	}
	if notes := serverNotes(ServerRequest{BBMD: true, BDT: bdt[1:]}, "x", true); notes != ", BBMD with 1 peer, password required for device management, read-only" {
		t.Errorf("notes: %q", notes)
	}
}

func TestServeErrors(t *testing.T) {
	// Invalid input: nothing is bound.
	cfg := config.ServerRunConfig{MaxAPDU: 10}
	if _, err := Serve(t.Context(), &cfg, "", nil, nil); code(err) != errs.CodeInvalidInput {
		t.Errorf("invalid: %v", err)
	}
	// A port that is in use.
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	cfg = config.ServerRunConfig{MaxAPDU: DefaultServerMaxAPDU, Listen: "127.0.0.1", Port: strings.TrimPrefix(pc.LocalAddr().String(), "127.0.0.1:")}
	if _, err := Serve(t.Context(), &cfg, "", nil, nil); code(err) != errs.CodeTransportPortInUse {
		t.Errorf("port %d in use: %v", port, err)
	}
	// stdout that cannot be written ends the server with an output error.
	cfg = config.ServerRunConfig{MaxAPDU: DefaultServerMaxAPDU, Listen: "127.0.0.1", Port: "0"}
	_, err = Serve(t.Context(), &cfg, "", nil, func(*types.ServerRecord) error { return errors.New("pipe closed") })
	if e, ok := errs.As(err); !ok || e.Kind != errs.KindOutput {
		t.Errorf("output: %v", err)
	}
	// A BBMD the library refuses.
	if _, err := newBBMD(ServerRequest{}, bip.Endpoint{}, false); code(err) != errs.CodeInvalidConfig {
		t.Errorf("bbmd: %v", err)
	}
}

// --duration ends the server; a record that comes while stdout is slow is printed, one
// that does not fit in the queue is counted and the next one is marked.
func TestServeDurationAndQueue(t *testing.T) {
	cfg := config.ServerRunConfig{MaxAPDU: DefaultServerMaxAPDU, Listen: "127.0.0.1", Port: "0", Duration: 1, Debug: true}
	var mu sync.Mutex
	var got []*types.ServerRecord
	var log strings.Builder
	sum, err := Serve(t.Context(), &cfg, "", &log, func(r *types.ServerRecord) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r)
		return nil
	})
	if err != nil || len(got) != 1 || sum.Duration < 900 || !strings.Contains(log.String(), "serving device:1234") {
		t.Errorf("duration: %+v %v %d %q", sum, err, len(got), log.String())
	}

	q := &recordQueue{ch: make(chan *types.ServerRecord, 1)}
	q.push(&types.ServerRecord{})
	q.push(&types.ServerRecord{})
	<-q.ch
	third := &types.ServerRecord{}
	q.push(third)
	if q.dropped != 1 || !third.Gap {
		t.Errorf("queue: dropped %d, gap %v", q.dropped, third.Gap)
	}
}
