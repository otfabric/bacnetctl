// SPDX-License-Identifier: MIT

package bac

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestU32(t *testing.T) {
	for _, c := range []struct {
		in   uint64
		want uint32
		ok   bool
	}{
		{0, 0, true},
		{math.MaxUint32, math.MaxUint32, true},
		{math.MaxUint32 + 1, 0, false},
		{1 << 40, 0, false},
		{math.MaxUint64, 0, false},
	} {
		got, err := u32(c.in, "x")
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("u32(%d) = %d, %v", c.in, got, err)
		}
		if err != nil && !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("u32(%d): %v is not ErrMalformed", c.in, err)
		}
	}
}

func TestI32(t *testing.T) {
	for _, c := range []struct {
		in   uint32
		want int32
		ok   bool
	}{
		{0, 0, true},
		{math.MaxInt32, math.MaxInt32, true},
		{math.MaxInt32 + 1, 0, false},
		{math.MaxUint32, 0, false},
	} {
		got, err := i32(c.in, "--count")
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("i32(%d) = %d, %v", c.in, got, err)
		}
		if err != nil && !strings.Contains(err.Error(), "--count") {
			t.Errorf("i32(%d): %v does not name the flag", c.in, err)
		}
	}
}

func TestOctet(t *testing.T) {
	for in, want := range map[int]uint8{-1: 0xFF, 0: 0, 1: 1, 254: 254, 255: 255, 256: 0xFF, math.MinInt: 0xFF, math.MaxInt: 0xFF} {
		if got := octet(in); got != want {
			t.Errorf("octet(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestReached(t *testing.T) {
	type tc struct {
		n     int
		limit uint32
		want  bool
	}
	cases := []tc{
		{0, 0, false},
		{100, 0, false},
		{-1, 1, false},
		{math.MinInt, math.MaxUint32, false},
		{0, 1, false},
		{1, 1, true},
		{2, 1, true},
	}
	if strconv.IntSize == 64 {
		// An int holds 2^32 only on 64-bit platforms; a variable keeps 32-bit builds compiling.
		top := int64(math.MaxUint32)
		cases = append(cases,
			tc{int(top - 1), math.MaxUint32, false},
			tc{int(top), math.MaxUint32, true},
			tc{int(top + 1), math.MaxUint32, true})
	}
	for _, c := range cases {
		if got := reached(c.n, c.limit); got != c.want {
			t.Errorf("reached(%d, %d) = %v", c.n, c.limit, got)
		}
	}
}

// A property identifier or array index of five octets is malformed, not its low bits.
func TestDecodeReadPropertyMultipleWideReference(t *testing.T) {
	object := []byte{0x0c, 0x00, 0x00, 0x00, 0x01}
	for _, c := range []struct {
		name string
		refs []byte
		ok   bool
	}{
		{"property of four octets", []byte{0x0c, 0xff, 0xff, 0xff, 0xff}, true},
		{"property of five octets", []byte{0x0d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00}, false},
		{"property of five octets with low bits of present-value", []byte{0x0d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55}, false},
		{"index of four octets", []byte{0x09, 0x55, 0x1c, 0xff, 0xff, 0xff, 0xff}, true},
		{"index of five octets", []byte{0x09, 0x55, 0x1d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x08}, false},
	} {
		payload := append(append(append(append([]byte{}, object...), 0x1e), c.refs...), 0x1f)
		specs, err := DecodeReadPropertyMultiple(payload, bacnet.DefaultDecodeLimits())
		if c.ok {
			if err != nil || len(specs) != 1 || len(specs[0].Properties) != 1 {
				t.Errorf("%s: %+v, %v", c.name, specs, err)
			}
			continue
		}
		if err == nil || !errors.Is(err, bacnet.ErrMalformed) || !strings.Contains(err.Error(), "exceeds 32 bits") {
			t.Errorf("%s: %+v, %v; want ErrMalformed for 32 bits", c.name, specs, err)
		}
	}
	specs, err := DecodeReadPropertyMultiple(append(append([]byte{}, object...), 0x1e, 0x09, 0x55, 0x1c, 0xff, 0xff, 0xff, 0xff, 0x1f), bacnet.DefaultDecodeLimits())
	if err != nil || specs[0].Properties[0].ArrayIndex == nil || *specs[0].Properties[0].ArrayIndex != math.MaxUint32 {
		t.Errorf("array index 0xFFFFFFFF: %+v, %v", specs, err)
	}

	// The decoder of frames reports the error instead of a wrong property.
	frame := append([]byte{0x81, 0x0a, 0x00, 0x00, 0x01, 0x04, 0x00, 0x05, 0x01, 0x0e}, object...)
	frame = append(frame, 0x1e, 0x0d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55, 0x1f)
	frame[2], frame[3] = byte(len(frame)>>8), byte(len(frame))
	res := Decode(frame)
	if res.Error == "" {
		t.Errorf("a 5-octet property identifier decoded without an error: %+v", res)
	}
	for _, l := range res.Layers {
		for _, f := range l.Fields {
			if strings.Contains(fmt.Sprint(f.Value), "present-value") {
				t.Errorf("a 5-octet property identifier decoded as %v", f.Value)
			}
		}
	}
}

// uintOf leaves a field out when the device sends an Unsigned wider than 32 bits.
func TestUintOfWide(t *testing.T) {
	if got := uintOf(bacnet.UnsignedValue(math.MaxUint32)); got == nil || *got != math.MaxUint32 {
		t.Errorf("uintOf(MaxUint32) = %v", got)
	}
	for _, v := range []uint64{1 << 32, 1<<32 + 999, math.MaxUint64} {
		if got := uintOf(bacnet.UnsignedValue(v)); got != nil {
			t.Errorf("uintOf(%d) = %d, want nil", v, *got)
		}
	}
	var res types.DeviceResult
	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyVendorIdentifier, bacnet.PropertySystemStatus, bacnet.PropertyMaxAPDULength, bacnet.PropertySegmentation} {
		setDeviceField(&res, service.PropertyResult{Property: bacnet.PropertyReference{Identifier: id}, Value: bacnet.UnsignedValue(1<<32 + 3)})
	}
	if res.VendorID != nil || res.SystemStatusID != nil || res.SystemStatus != "" || res.MaxAPDU != nil || res.SegmentationID != nil || res.Segmentation != "" {
		t.Errorf("values of 33 bits shown: %+v", res)
	}
}

func TestEnumNameObjectTypeWide(t *testing.T) {
	if got := EnumName(bacnet.ObjectTypeDevice, bacnet.PropertyObjectType, 8); got != "device" {
		t.Errorf("object type 8 = %q", got)
	}
	for _, v := range []uint32{math.MaxUint16 + 1, math.MaxUint16 + 1 + 8, math.MaxUint32} {
		if got := EnumName(bacnet.ObjectTypeDevice, bacnet.PropertyObjectType, v); got != "" {
			t.Errorf("object type %d = %q, want no name", v, got)
		}
	}
	if got := EnumName(bacnet.ObjectTypeDevice, bacnet.PropertyObjectType, math.MaxUint16); got == "" {
		t.Errorf("object type 65535 has no name at all")
	}
}

func TestTextUCS4InvalidCodePoints(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  []byte
		want string
	}{
		{"beyond Unicode", []byte{0x00, 0x11, 0x00, 0x00}, "�"},
		{"high bit", []byte{0x80, 0x00, 0x00, 0x41}, "�"},
		{"surrogate", []byte{0x00, 0x00, 0xD8, 0x00}, "�"},
		{"last surrogate", []byte{0x00, 0x00, 0xDF, 0xFF}, "�"},
		{"mixed", []byte{0, 0, 0, 'A', 0x00, 0x11, 0x00, 0x00, 0, 0, 0xD8, 0, 0x00, 0x10, 0xFF, 0xFF, 0, 0, 0, 'B'}, "A��\U0010FFFFB"},
		{"short tail is dropped", []byte{0, 0, 0, 'A', 0, 0}, "A"},
	} {
		got := Text(bacnet.CharacterString{Encoding: charsetUCS4, Value: string(c.raw)})
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCheckDateYear(t *testing.T) {
	for y, ok := range map[int]bool{1: false, 1899: false, 1900: true, 2026: true, 2154: true, 2155: false, 9999: false} {
		err := CheckDateYear(time.Date(y, 6, 1, 0, 0, 0, 0, time.UTC))
		if ok != (err == nil) {
			t.Errorf("year %d: %v", y, err)
		}
	}
	// The ends of the range are representable, one octet each.
	if d, _ := DateTimeOf(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)); d.Year != 0 {
		t.Errorf("1900 is year %d", d.Year)
	}
	if d, _ := DateTimeOf(time.Date(2154, 12, 31, 0, 0, 0, 0, time.UTC)); d.Year != 254 {
		t.Errorf("2154 is year %d", d.Year)
	}
	// A year a caller did not check is sent as unspecified, never wrapped.
	for _, y := range []int{1899, 2155, 2156} {
		if d, _ := DateTimeOf(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)); d.Year != 0xFF {
			t.Errorf("year %d sent as %d", y, d.Year)
		}
	}
	d, tm := DateTimeOf(time.Date(2026, 10, 4, 23, 59, 58, 990_000_000, time.UTC))
	if d != (bacnet.Date{Year: 126, Month: 10, Day: 4, Weekday: 7}) || tm != (bacnet.Time{Hour: 23, Minute: 59, Second: 58, Hundredths: 99}) {
		t.Errorf("DateTimeOf: %+v %+v", d, tm)
	}
}

func TestParseSyncTimeYearRange(t *testing.T) {
	for in, ok := range map[string]bool{
		"1900-01-01T00:00:00Z":      true,
		"2154-12-31T23:59:59Z":      true,
		"1899-12-31T23:59:59Z":      false,
		"2155-01-01T00:00:00Z":      false,
		"2154-12-31T23:30:00+01:00": true,
		// In UTC this is 2155: refused.
		"2154-12-31T23:30:00-01:00": false,
		// In UTC this is 1899: refused.
		"1900-01-01T00:30:00+01:00": false,
		// Local 2155, UTC 2154: refused too.
		"2155-01-01T00:30:00+01:00": false,
		// Local 1899, UTC 1900.
		"1899-12-31T23:30:00-01:00": false,
		"1900-01-01T00:30:00-01:00": true,
	} {
		_, err := ParseSyncTime(in)
		if ok != (err == nil) {
			t.Errorf("ParseSyncTime(%q): %v", in, err)
		}
		if err != nil && (!strings.Contains(err.Error(), "--time") || !strings.Contains(err.Error(), "1900-2154")) {
			t.Errorf("ParseSyncTime(%q): %v does not say what is accepted", in, err)
		}
	}
}

func TestParseAckEventTimeYearRange(t *testing.T) {
	for in, ok := range map[string]bool{
		"1900-01-01T00:00:00Z": true,
		"2154-12-31T23:59:59Z": true,
		"1899-12-31T23:59:59Z": false,
		"2155-01-01T00:00:00Z": false,
	} {
		req, err := ParseAck(&config.AckConfig{Time: in, Source: "test"}, "ai:2")
		if ok != (err == nil) {
			t.Errorf("--event-time %q: %v", in, err)
			continue
		}
		if err != nil {
			if !strings.Contains(err.Error(), "--event-time") || !strings.Contains(err.Error(), "1900-2154") {
				t.Errorf("--event-time %q: %v", in, err)
			}
			continue
		}
		if req.Stamp == nil || req.Stamp.Choice != service.TimeStampDateTime || req.Stamp.DateTime.Date.Year == 0xFF {
			t.Errorf("--event-time %q: stamp %+v", in, req.Stamp)
		}
	}
}

func TestParseTimeAndBitsBounds(t *testing.T) {
	if tm, err := ParseTime("23:59:59.99"); err != nil || tm != (bacnet.Time{Hour: 23, Minute: 59, Second: 59, Hundredths: 99}) {
		t.Errorf("23:59:59.99: %+v, %v", tm, err)
	}
	for _, s := range []string{"12:00:00.-1", "12:00:00.100", "12:00:00.9", "24:00:00", "12:60:00"} {
		if tm, err := ParseTime(s); err == nil {
			t.Errorf("%q accepted as %+v", s, tm)
		}
	}
	for s, unused := range map[string]uint8{"1": 7, "10100000": 0, "101": 5, "101000001": 7, "1010000010100000": 0} {
		b, err := ParseBits(s)
		if err != nil || b.UnusedBits != unused || len(b.Bytes)*8-int(b.UnusedBits) != len(s) {
			t.Errorf("%q: %+v, %v", s, b, err)
		}
	}
}
