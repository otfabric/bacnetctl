// SPDX-License-Identifier: MIT

package cmd

import (
	"math"
	"net"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/bactest"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// Enumerated values past the names bacnetctl knows are printed as their numbers by every
// command that shows them, with exit code 0. CI runs this test on linux/386 too, where
// int has 32 bits.
func TestEnumeratedValuesPastTheNames(t *testing.T) {
	dev := bactest.Start(t,
		bactest.DeviceProperty(bacnet.PropertyUnits, bacnet.EnumValue(math.MaxUint32)),
		bactest.DeviceProperty(bacnet.PropertyEventState, bacnet.EnumValue(math.MaxInt32+1)),
		bactest.DeviceProperty(bacnet.PropertySystemStatus, bacnet.EnumValue(math.MaxUint32-1)),
	)
	on(t, dev, "read", "device", "units").want(t, 0).has(t, "4294967295 (4294967295)")
	on(t, dev, "read", "device", "event-state").want(t, 0).has(t, "2147483648")
	on(t, dev, "read", "device", "units,event-state,system-status", "--format", "table").want(t, 0).
		has(t, "4294967295 (4294967295)", "2147483648", "4294967294")
	r := on(t, dev, "read", "device", "event-state", "--format", "json").want(t, 0)
	if v := r.object(t)["rows"].([]any)[0].(map[string]any)["value"].(map[string]any); v["value"] != float64(math.MaxInt32+1) || v["name"] != nil {
		t.Errorf("event-state: %v", v)
	}
	on(t, dev, "device").want(t, 0).has(t, "System status:", "4294967294")
	on(t, dev, "describe", "device").want(t, 0).has(t, "4294967295 (4294967295)", "2147483648", "4294967294")

	// An event notification from any address, with states and types past the names.
	port := freeUDPPort(t)
	done := make(chan result, 1)
	go func() {
		done <- run(t, "events", "--local-port", port, "--duration", "30", "--count", "1")
	}()
	conn, err := net.Dial("udp4", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	from := uint32(math.MaxInt32 + 1)
	payload, err := service.EncodeEventNotification(service.EventNotification{
		ProcessIdentifier: 1, InitiatingDevice: oid(bacnet.ObjectTypeDevice, 77), EventObject: ai1,
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass: 1, Priority: 1, EventType: math.MaxUint32, NotifyType: math.MaxUint32,
		FromState: &from, ToState: math.MaxUint32,
	})
	if err != nil {
		t.Fatal(err)
	}
	note := frame(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: payload})...)
	// The listener may not be bound yet: send until it has printed the one it waits for.
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case r := <-done:
			r.want(t, 0).has(t, "device:77 analog-input:1  4294967295 4294967295: 2147483648 -> 4294967295  priority 1")
			return
		case <-tick.C:
			_, _ = conn.Write(note)
		}
	}
}
