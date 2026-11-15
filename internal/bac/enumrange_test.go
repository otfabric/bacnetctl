// SPDX-License-Identifier: MIT

package bac

import (
	"fmt"
	"math"
	"strconv"
	"testing"

	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// wideEnums are enumerated values past every name table, around the 32-bit boundary of an
// int. CI runs this package on linux/386 too, where int has 32 bits.
var wideEnums = []uint32{math.MaxInt32, math.MaxInt32 + 1, math.MaxUint32 - 1, math.MaxUint32}

// An enumerated value past the names bacnetctl has is written as its number, whatever its
// size and on every platform.
func TestEnumValuesPastTheNames(t *testing.T) {
	num := func(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
	none := func(uint32) string { return "" }
	units := uint32(len(unitNames))
	for _, c := range []struct {
		name string
		got  func(uint32) string
		// named are values that have a name, with that name; past is the first value
		// without one.
		named map[uint32]string
		past  uint32
		// unnamed is what a value without a name gives: "" or its number.
		unnamed func(uint32) string
	}{
		{"UnitName", UnitName, map[uint32]string{0: "square-meters", units - 1: unitNames[units-1]}, units, num},
		{"UnitSymbol", UnitSymbol, map[uint32]string{62: "°C", 95: ""}, units, num},
		{"EnumName units", func(v uint32) string { return EnumName(bacnet.ObjectTypeAnalogInput, bacnet.PropertyUnits, v) },
			map[uint32]string{62: "degrees-celsius"}, units, num},
		{"EnumName binary present value", func(v uint32) string {
			return EnumName(bacnet.ObjectTypeBinaryInput, bacnet.PropertyPresentValue, v)
		}, map[uint32]string{0: "inactive", 1: "active"}, 2, none},
		{"EnumName event state", func(v uint32) string { return EnumName(bacnet.ObjectTypeAnalogInput, bacnet.PropertyEventState, v) },
			map[uint32]string{0: "normal", 5: "life-safety-alarm"}, 6, none},
		{"EnumName system status", func(v uint32) string { return EnumName(bacnet.ObjectTypeDevice, bacnet.PropertySystemStatus, v) },
			map[uint32]string{0: "operational", 5: "backup-in-progress"}, 6, none},
		{"eventStateName", eventStateName, map[uint32]string{3: "high-limit"}, 6, num},
		{"namedOr event type", func(v uint32) string { return namedOr(eventTypeNames, v) },
			map[uint32]string{5: "out-of-range"}, uint32(len(eventTypeNames)), num},
		{"namedOr notify type", func(v uint32) string { return namedOr(notifyTypeNames, v) },
			map[uint32]string{2: "ack-notification"}, 3, num},
	} {
		for v, want := range c.named {
			if got := c.got(v); got != want {
				t.Errorf("%s(%d) = %q, want %q", c.name, v, got, want)
			}
		}
		for _, v := range append([]uint32{c.past}, wideEnums...) {
			if got, want := c.got(v), c.unnamed(v); got != want {
				t.Errorf("%s(%d) = %q, want %q", c.name, v, got, want)
			}
		}
	}

	for _, v := range wideEnums {
		val := ValueOf(bacnet.ObjectTypeBinaryInput, bacnet.PropertyPresentValue, bacnet.EnumValue(v))
		if val.Name != "" || val.Text != num(v) || val.Value != v {
			t.Errorf("binary present value %d: %+v", v, val)
		}
		val = ValueOf(bacnet.ObjectTypeAnalogInput, bacnet.PropertyUnits, bacnet.EnumValue(v))
		if val.Name != num(v) || val.Text != fmt.Sprintf("%d (%d)", v, v) {
			t.Errorf("units %d: %+v", v, val)
		}

		from := v
		n := service.EventNotification{
			InitiatingDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
			EventObject:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 2},
			EventType:        v, NotifyType: v, FromState: &from, ToState: v,
		}
		rec := eventRecord(n, "127.0.0.1:47808", false, false)
		if rec.EventType != num(v) || rec.NotifyType != num(v) || rec.FromState != num(v) || rec.ToState != num(v) || rec.ToStateID != v {
			t.Errorf("event notification %d: %+v", v, rec)
		}
		logged := eventLogRecord(1, service.EventLogRecord{Choice: service.LogDataValues, Notification: &n})
		if e := logged.Event; e == nil || e.EventType != num(v) || e.FromState != num(v) || e.ToState != num(v) {
			t.Errorf("event log record %d: %+v", v, logged.Event)
		}
	}
}
