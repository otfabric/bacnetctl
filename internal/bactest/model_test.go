// SPDX-License-Identifier: MIT

package bactest

import (
	"errors"
	"math"
	"testing"
	"time"

	bacnet "github.com/otfabric/go-bacnet"
)

// The day of week of the dates of the model is that of the calendar, also before the 10th.
func TestDateTimeWeekday(t *testing.T) {
	for day := 1; day <= 31; day++ {
		want := uint8(time.Date(2026, time.October, day, 0, 0, 0, 0, time.UTC).Weekday())
		if want == 0 {
			want = 7 // BACnet: 1 is Monday, 7 is Sunday
		}
		got := dateTime(uint8(day), 1, 2)
		if got.Date.Weekday != want || got.Date.Day != uint8(day) || got.Date.Month != 10 || got.Date.Year != 126 {
			t.Errorf("2026-10-%02d: %+v, weekday want %d", day, got.Date, want)
		}
		if got.Time != (bacnet.Time{Hour: 1, Minute: 2}) {
			t.Errorf("2026-10-%02d: time %+v", day, got.Time)
		}
	}
}

func TestLatin1(t *testing.T) {
	for in, want := range map[string]string{
		"abc":     "abc",
		"Filterü": "Filter\xfc",
		"ÿ":       "\xff",
		"Ā€😀x":    "???x",
		"":        "",
		"a b":     "a\xa0b",
		"Āend":    "?end",
	} {
		v := latin1(in)
		if v.Kind != bacnet.ValueCharacterString || v.Character.Encoding != 5 || v.Character.Value != want {
			t.Errorf("latin1(%q) = %+v, want %q", in, v, want)
		}
	}
}

func TestBitsUnused(t *testing.T) {
	for n, unused := range map[int]uint8{0: 0, 1: 7, 8: 0, 9: 7, 49: 7, 65: 7, 16: 0} {
		b := bits(make([]bool, n)...).BitString
		if b.UnusedBits != unused || len(b.Bytes)*8-int(b.UnusedBits) != n {
			t.Errorf("%d bits: %+v", n, b)
		}
	}
}

func TestPasswordOK(t *testing.T) {
	open := &model{}
	if !open.passwordOK(nil) || !open.passwordOK(&bacnet.CharacterString{Value: "x"}) {
		t.Error("a device without a password refused a request")
	}
	m := &model{password: "secret"}
	for given, ok := range map[string]bool{"secret": true, "Secret": false, "secre": false, "secret ": false, "": false} {
		if got := m.passwordOK(&bacnet.CharacterString{Value: given}); got != ok {
			t.Errorf("password %q: %v", given, got)
		}
	}
	if m.passwordOK(nil) {
		t.Error("no password accepted")
	}
}

func TestDecodeRPMWideReference(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	object := []byte{0x0c, 0x00, 0x00, 0x00, 0x01}
	for _, c := range []struct {
		name string
		refs []byte
		ok   bool
	}{
		{"property", []byte{0x09, 0x55}, true},
		{"property of four octets", []byte{0x0c, 0xff, 0xff, 0xff, 0xff}, true},
		{"property of five octets", []byte{0x0d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55}, false},
		{"index of four octets", []byte{0x09, 0x55, 0x1c, 0xff, 0xff, 0xff, 0xff}, true},
		{"index of five octets", []byte{0x09, 0x55, 0x1d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x08}, false},
	} {
		payload := append(append(append(append([]byte{}, object...), 0x1e), c.refs...), 0x1f)
		specs, err := decodeRPM(payload, limits)
		if c.ok {
			if err != nil || len(specs) != 1 || len(specs[0].Properties) != 1 {
				t.Errorf("%s: %+v, %v", c.name, specs, err)
			}
			continue
		}
		if !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: %+v, %v; want ErrMalformed", c.name, specs, err)
		}
	}
	specs, err := decodeRPM(append(append([]byte{}, object...), 0x1e, 0x09, 0x55, 0x1c, 0xff, 0xff, 0xff, 0xff, 0x1f), limits)
	if err != nil || *specs[0].Properties[0].ArrayIndex != math.MaxUint32 {
		t.Errorf("array index 0xFFFFFFFF: %+v, %v", specs, err)
	}
	if _, err := decodeRPM([]byte{0x1e, 0x09, 0x55, 0x1f}, limits); err == nil {
		t.Error("properties without an object accepted")
	}
}

func TestDevicePropertyOption(t *testing.T) {
	d := Start(t, DeviceProperty(bacnet.PropertyVendorIdentifier, bacnet.UnsignedValue(1<<40)))
	v, ok := d.Value(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: DefaultInstance}, bacnet.PropertyVendorIdentifier)
	if !ok || v.Kind != bacnet.ValueUnsigned || v.Unsigned != 1<<40 {
		t.Errorf("vendor identifier: %+v, %v", v, ok)
	}
	// A property the device did not have is added.
	d = Start(t, DeviceProperty(propLocation+1000, bacnet.UnsignedValue(1)))
	if _, ok := d.Value(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: DefaultInstance}, propLocation+1000); !ok {
		t.Error("an added property cannot be read")
	}
}
