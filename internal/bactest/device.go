// SPDX-License-Identifier: MIT

// Package bactest is a small simulated BACnet/IP device for tests: a UDP socket on the
// loopback interface that answers the services bacnetctl uses from a model of a dozen
// objects. It is built from the codecs of go-bacnet and shares no code with its client.
//
// The device answers to the address a request came from, also where a real device would
// broadcast (I-Am, I-Have): a test needs no port 47808 and no broadcast.
package bactest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// DefaultInstance is the device instance of the simulated device.
const DefaultInstance = 1234

// Routed device: the address of the second device, which the simulated device routes to
// when it was started with Router.
const (
	RoutedNetwork  = 2001
	RoutedMAC      = 0x0A
	RoutedInstance = 2001010
)

// Names of the services in Requests and in the options.
const (
	WhoIs               = "who-is"
	WhoHas              = "who-has"
	ReadProperty        = "read-property"
	ReadPropertyMulti   = "read-property-multiple"
	WriteProperty       = "write-property"
	WritePropertyMulti  = "write-property-multiple"
	CreateObject        = "create-object"
	DeleteObject        = "delete-object"
	SubscribeCOV        = "subscribe-cov"
	SubscribeCOVProp    = "subscribe-cov-property"
	GetEventInformation = "get-event-information"
	GetAlarmSummary     = "get-alarm-summary"
	AcknowledgeAlarm    = "acknowledge-alarm"
	ReadRange           = "read-range"
	AtomicReadFile      = "atomic-read-file"
	AtomicWriteFile     = "atomic-write-file"
	Reinitialize        = "reinitialize-device"
	CommControl         = "device-communication-control"
	TimeSync            = "time-synchronization"
	UTCTimeSync         = "utc-time-synchronization"
	WhoIsRouter         = "who-is-router-to-network"
	RegisterForeign     = "register-foreign-device"
	ReadBDT             = "read-broadcast-distribution-table"
	ReadFDT             = "read-foreign-device-table"
)

var confirmedNames = map[uint8]string{
	apdu.ServiceReadProperty: ReadProperty, apdu.ServiceReadPropertyMultiple: ReadPropertyMulti,
	apdu.ServiceWriteProperty: WriteProperty, apdu.ServiceSubscribeCOV: SubscribeCOV,
	apdu.ServiceSubscribeCOVProperty: SubscribeCOVProp, apdu.ServiceGetEventInformation: GetEventInformation,
	apdu.ServiceGetAlarmSummary: GetAlarmSummary, apdu.ServiceAcknowledgeAlarm: AcknowledgeAlarm,
	apdu.ServiceReadRange: ReadRange, apdu.ServiceAtomicReadFile: AtomicReadFile,
	apdu.ServiceAtomicWriteFile: AtomicWriteFile, apdu.ServiceReinitializeDevice: Reinitialize,
	apdu.ServiceDeviceCommunicationControl: CommControl,
	apdu.ServiceWritePropertyMultiple:      WritePropertyMulti,
	apdu.ServiceCreateObject:               CreateObject,
	apdu.ServiceDeleteObject:               DeleteObject,
}

// Option changes how the simulated device behaves.
type Option func(*Device)

// Silent makes the device answer nothing at all, as an address without a device.
func Silent() Option { return func(d *Device) { d.silent = true } }

// Mute makes the device carry out the named services without answering them: what a lost
// acknowledgement looks like to a client.
func Mute(services ...string) Option {
	return func(d *Device) {
		for _, s := range services {
			d.mute[s] = true
		}
	}
}

// Without makes the device reject the named services as unrecognized: a device that does
// not implement them.
func Without(services ...string) Option {
	return func(d *Device) {
		for _, s := range services {
			d.without[s] = true
		}
	}
}

// MaxAPDU makes the device abort an answer that is longer than n octets: a device with a
// small buffer and no segmentation.
func MaxAPDU(n int) Option { return func(d *Device) { d.maxAPDU = n } }

// NoWildcard makes the device answer unknown-object for the wildcard Device instance
// (device:4194303), as BACpypes3 does.
func NoWildcard() Option { return func(d *Device) { d.local.noWildcard = true } }

// Password makes the device ask for this password in device management requests.
func Password(p string) Option { return func(d *Device) { d.local.password = p } }

// Router makes the device a BACnet router to network 2001, with a second device behind it
// at MAC 0A, and a BBMD that accepts foreign devices.
func Router() Option {
	return func(d *Device) {
		d.routed = newModel(RoutedInstance, "Zone Controller 10")
		d.bbmd = true
	}
}

// subscription is one COV subscription of a client.
type subscription struct {
	peer      netip.AddrPort
	source    bacnet.Address
	model     *model
	process   uint32
	object    bacnet.ObjectIdentifier
	property  *bacnet.PropertyReference
	confirmed bool
	lifetime  uint32
}

// Device is a running simulated device.
type Device struct {
	conn *net.UDPConn
	// Addr is the address the device listens on.
	Addr netip.AddrPort

	mu       sync.Mutex
	changed  chan struct{}
	local    *model
	routed   *model
	bbmd     bool
	silent   bool
	mute     map[string]bool
	without  map[string]bool
	maxAPDU  int
	requests []string
	subs     []*subscription
	foreign  []bvlc.FDTEntry
	invoke   uint8
	// What the device was last told.
	timeSync    *service.TimeSynchronization
	reinit      *service.ReinitializedStateOfDevice
	commControl *service.EnableDisable
	done        chan struct{}
}

// Listen starts a simulated device on addr ("127.0.0.1:0" for a free loopback port).
func Listen(addr string, opts ...Option) (*Device, error) {
	udp, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", udp)
	if err != nil {
		return nil, err
	}
	local := conn.LocalAddr().(*net.UDPAddr)
	ip, _ := netip.AddrFromSlice(local.IP.To4())
	d := &Device{
		conn: conn, Addr: netip.AddrPortFrom(ip, uint16(local.Port)), changed: make(chan struct{}),
		local: newModel(DefaultInstance, "AHU-1 Controller"), mute: map[string]bool{}, without: map[string]bool{},
		done: make(chan struct{}),
	}
	for _, o := range opts {
		o(d)
	}
	go d.serve()
	return d, nil
}

// TB is the part of testing.TB the simulated device needs.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Cleanup(func())
}

// Start starts a simulated device on a free loopback port for the duration of a test.
func Start(t TB, opts ...Option) *Device {
	t.Helper()
	d, err := Listen("127.0.0.1:0", opts...)
	if err != nil {
		t.Fatalf("starting the simulated device: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// Close stops the device.
func (d *Device) Close() {
	_ = d.conn.Close()
	<-d.done
}

// Target is the address of the device as --device takes it.
func (d *Device) Target() string { return d.Addr.String() }

// signal wakes everything that waits for the state of the device to change. The caller
// holds d.mu.
func (d *Device) signal() {
	close(d.changed)
	d.changed = make(chan struct{})
}

// Await blocks until cond holds, and fails the test when it does not within ten seconds.
// cond is called with the device locked: it may use the accessors that say so.
func (d *Device) Await(t TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		d.mu.Lock()
		ok, ch := cond(), d.changed
		d.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-ch:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", what)
			return
		}
	}
}

// AwaitSubscriptions blocks until n COV subscriptions are active.
func (d *Device) AwaitSubscriptions(t TB, n int) {
	t.Helper()
	d.Await(t, fmt.Sprintf("%d COV subscriptions", n), func() bool { return len(d.subs) == n })
}

// AwaitRequest blocks until the device has received the named service n times.
func (d *Device) AwaitRequest(t TB, name string, n int) {
	t.Helper()
	d.Await(t, fmt.Sprintf("%d %s requests", n, name), func() bool { return d.countLocked(name) >= n })
}

func (d *Device) countLocked(name string) int {
	n := 0
	for _, r := range d.requests {
		if r == name {
			n++
		}
	}
	return n
}

// Requests returns the names of the services the device received, in order.
func (d *Device) Requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.requests...)
}

// Count returns how often the device received the named service.
func (d *Device) Count(name string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.countLocked(name)
}

// Subscriptions returns the number of active COV subscriptions.
func (d *Device) Subscriptions() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.subs)
}

// Value returns the value of a property of an object of the device.
func (d *Device) Value(object bacnet.ObjectIdentifier, property bacnet.PropertyIdentifier) (bacnet.ApplicationValue, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, e := d.local.read(object, bacnet.PropertyReference{Identifier: property})
	return v, e == nil
}

// SetPresentValue changes the present value of an object, as the process would, and
// notifies the subscribers of the object.
func (d *Device) SetPresentValue(object bacnet.ObjectIdentifier, v bacnet.ApplicationValue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if o := d.local.find(object); o != nil {
		o.set(bacnet.PropertyPresentValue, v)
		d.notify(d.local, object)
	}
}

// TimeSync returns the last time synchronization the device received.
func (d *Device) TimeSync() (service.TimeSynchronization, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timeSync == nil {
		return service.TimeSynchronization{}, false
	}
	return *d.timeSync, true
}

// Reinitialized returns the state of the last ReinitializeDevice the device accepted.
func (d *Device) Reinitialized() (service.ReinitializedStateOfDevice, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reinit == nil {
		return 0, false
	}
	return *d.reinit, true
}

// CommControl returns the state of the last DeviceCommunicationControl the device accepted.
func (d *Device) CommControl() (service.EnableDisable, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.commControl == nil {
		return 0, false
	}
	return *d.commControl, true
}

// FileContent returns the content of a file object.
func (d *Device) FileContent(instance uint32) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	if o := d.local.find(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: instance}); o != nil {
		return append([]byte(nil), o.content...)
	}
	return nil
}

// SendEvent sends an unconfirmed event notification to an address: analog-input:2 going
// from normal to high-limit.
func (d *Device) SendEvent(to netip.AddrPort) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	o := d.local.find(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 2})
	from, ack := uint32(0), true
	payload, err := service.EncodeEventNotification(service.EventNotification{
		ProcessIdentifier: 1, InitiatingDevice: d.local.deviceID(), EventObject: o.id, TimeStamp: o.stamps[0],
		NotificationClass: 5, Priority: 100, EventType: 5, MessageText: &bacnet.CharacterString{Value: "Return air humidity high"},
		NotifyType: 0, AckRequired: &ack, FromState: &from, ToState: 3,
		Parameters: &service.NotificationParameters{Choice: service.NotificationOutOfRange, OutOfRange: &service.OutOfRangeParams{
			ExceedingValue: 71.25, StatusFlags: bitSet(4, 0).BitString, Deadband: 2, ExceededLimit: 65,
		}},
	})
	if err != nil {
		return err
	}
	raw := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: payload})
	return d.send(to, bacnet.Address{}, bacnet.Address{}, raw)
}

// send writes one APDU to a peer. source is the BACnet address of a device behind the
// simulated router, dest the BACnet address of a client behind another.
func (d *Device) send(to netip.AddrPort, source, dest bacnet.Address, apduBytes []byte) error {
	n := npdu.NPDU{Version: npdu.Version1, Source: source, Destination: dest, HopCount: 255, APDU: apduBytes}
	raw, err := npdu.Append(nil, n)
	if err != nil {
		return err
	}
	return d.sendFrame(to, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
}

func (d *Device) sendFrame(to netip.AddrPort, msg bvlc.Message) error {
	frame, err := bvlc.Append(nil, msg)
	if err != nil {
		return err
	}
	_, err = d.conn.WriteToUDPAddrPort(frame, to)
	return err
}

func (d *Device) serve() {
	defer close(d.done)
	buf := make([]byte, 2048)
	for {
		n, from, err := d.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		from = netip.AddrPortFrom(from.Addr().Unmap(), from.Port())
		d.handle(append([]byte(nil), buf[:n]...), from)
	}
}

func (d *Device) record(name string) {
	d.requests = append(d.requests, name)
	d.signal()
}

func (d *Device) handle(frame []byte, from netip.AddrPort) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.silent {
		return
	}
	limits := bacnet.DefaultDecodeLimits()
	msg, err := bvlc.Parse(frame, limits)
	if err != nil {
		return
	}
	switch msg.Function {
	case bvlc.FunctionRegisterForeignDevice:
		d.record(RegisterForeign)
		code := uint16(0x0030)
		if d.bbmd {
			code = 0
			d.foreign = append(d.foreign, bvlc.FDTEntry{Address: endpoint(from), TTL: msg.TTL, Remaining: msg.TTL + 30})
		}
		_ = d.sendFrame(from, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: code})
		return
	case bvlc.FunctionReadBroadcastDistributionTable:
		d.record(ReadBDT)
		if !d.bbmd {
			_ = d.sendFrame(from, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0x0020})
			return
		}
		entry, _ := bvlc.NewBDTEntry(endpoint(d.Addr), bvlc.IPv4Mask{255, 255, 255, 255})
		other, _ := bvlc.NewBDTEntry(endpoint(netip.MustParseAddrPort("10.0.20.1:47808")), bvlc.IPv4Mask{255, 255, 255, 0})
		payload, _ := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{entry, other})
		_ = d.sendFrame(from, bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTableAck, Payload: payload})
		return
	case bvlc.FunctionReadForeignDeviceTable:
		d.record(ReadFDT)
		if !d.bbmd {
			_ = d.sendFrame(from, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0x0040})
			return
		}
		payload, _ := bvlc.EncodeFDTEntries(nil, d.foreign)
		_ = d.sendFrame(from, bvlc.Message{Function: bvlc.FunctionReadForeignDeviceTableAck, Payload: payload})
		return
	case bvlc.FunctionOriginalUnicastNPDU, bvlc.FunctionOriginalBroadcastNPDU, bvlc.FunctionDistributeBroadcastToNetwork:
	default:
		return
	}
	n, _, err := npdu.Parse(msg.Payload, limits)
	if err != nil {
		return
	}
	if n.NetworkMessage {
		if n.NetMsgType == npdu.NetMsgWhoIsRouterToNetwork {
			d.record(WhoIsRouter)
			d.iAmRouter(from, n.NetMsgData)
		}
		return
	}
	pdu, err := apdu.Parse(n.APDU, limits)
	if err != nil {
		return
	}
	// Which device is meant: the one at this address, the one behind the router, or both
	// for a broadcast that reaches the network behind it.
	var targets []*model
	switch n.Destination.Scope() {
	case bacnet.AddressRemoteStation:
		if d.routed != nil && n.Destination.Network() == RoutedNetwork && n.Destination.MAC().Len() == 1 && n.Destination.MAC().Bytes()[0] == RoutedMAC {
			targets = []*model{d.routed}
		}
	case bacnet.AddressRemoteBroadcast:
		if d.routed != nil && n.Destination.Network() == RoutedNetwork {
			targets = []*model{d.routed}
		}
	case bacnet.AddressGlobalBroadcast:
		targets = []*model{d.local}
		if d.routed != nil {
			targets = append(targets, d.routed)
		}
	default:
		targets = []*model{d.local}
	}
	for _, m := range targets {
		rq := &request{d: d, m: m, from: from, client: n.Source}
		if m == d.routed {
			rq.source = bacnet.RemoteStation(RoutedNetwork, bacnet.MustMAC([]byte{RoutedMAC}))
		}
		switch pdu.Type {
		case apdu.TypeUnconfirmedRequest:
			rq.unconfirmed(pdu.UnconfirmedRequest, limits)
		case apdu.TypeConfirmedRequest:
			rq.confirmed(pdu.ConfirmedRequest, limits)
		default:
			// The acknowledgement of a confirmed notification: nothing to do.
		}
	}
}

func endpoint(ap netip.AddrPort) bip.Endpoint { return bip.NewEndpoint(ap) }

func (d *Device) iAmRouter(to netip.AddrPort, data []byte) {
	if d.routed == nil {
		return
	}
	if len(data) == 2 && (uint16(data[0])<<8|uint16(data[1])) != RoutedNetwork {
		return
	}
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true, NetMsgType: npdu.NetMsgIAmRouterToNetwork,
		NetMsgData: []byte{byte(RoutedNetwork >> 8), byte(RoutedNetwork & 0xFF)},
	})
	if err != nil {
		return
	}
	_ = d.sendFrame(to, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
}

// notify sends a COV notification for object to every subscriber of it. The caller holds
// d.mu.
func (d *Device) notify(m *model, object bacnet.ObjectIdentifier) {
	for _, s := range d.subs {
		if s.model == m && s.object == object {
			d.sendCOV(s)
		}
	}
}

func (d *Device) sendCOV(s *subscription) {
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	if s.property != nil {
		ref = *s.property
	}
	var payload []byte
	payload, _ = bacnet.AppendContextUnsigned(payload, 0, uint64(s.process))
	payload, _ = bacnet.AppendContextObjectID(payload, 1, s.model.deviceID())
	payload, _ = bacnet.AppendContextObjectID(payload, 2, s.object)
	payload, _ = bacnet.AppendContextUnsigned(payload, 3, uint64(s.lifetime))
	var values []bacnet.Element
	for _, r := range []bacnet.PropertyReference{ref, {Identifier: bacnet.PropertyStatusFlags}} {
		v, e := s.model.read(s.object, r)
		if e != nil {
			continue
		}
		values = append(values, bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(uint64(r.Identifier))})
		if r.ArrayIndex != nil {
			values = append(values, bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(uint64(*r.ArrayIndex))})
		}
		values = append(values, bacnet.Element{Context: true, TagNumber: 2,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: v}}}})
	}
	payload, err := bacnet.AppendContextTagged(payload, 4, values)
	if err != nil {
		return
	}
	var raw []byte
	if s.confirmed {
		d.invoke++
		raw = apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
			MaxAPDU: uint8(apdu.MaxAPDU1476), InvokeID: d.invoke, ServiceChoice: apdu.ServiceConfirmedCOVNotification, Payload: payload,
		})
	} else {
		raw = apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedCOV, Payload: payload})
	}
	var source bacnet.Address
	if s.model == d.routed {
		source = bacnet.RemoteStation(RoutedNetwork, bacnet.MustMAC([]byte{RoutedMAC}))
	}
	_ = d.send(s.peer, source, s.source, raw)
}

// Run serves until ctx ends: for the stand-alone simulator.
func (d *Device) Run(ctx context.Context) {
	<-ctx.Done()
	d.Close()
}
