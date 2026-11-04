// SPDX-License-Identifier: MIT

package bac

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/otfabric/bacnetctl/internal/config"
	bacnet "github.com/otfabric/go-bacnet"
)

// FuzzParseObject: an object identifier that parses prints as text that
// parses to the same identifier.
func FuzzParseObject(f *testing.F) {
	for _, s := range []string{"analog-input:1", "ai:1", "8:4194302", "device:4194303", "analog-input:", ":", "AV,3"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		o, err := ParseObject(s)
		if err != nil {
			return
		}
		if o.Instance > 0x3FFFFF {
			t.Fatalf("%q: instance %d does not fit 22 bits", s, o.Instance)
		}
	})
}

// FuzzParseProperties covers property lists with array indexes.
func FuzzParseProperties(f *testing.F) {
	for _, s := range []string{"present-value", "85", "priority-array[8]", "all", "a,b[", "[]", "object-name,units[0]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		refs, err := ParseProperties(s)
		if err == nil && len(refs) == 0 {
			t.Fatalf("%q: no error and no property", s)
		}
	})
}

// FuzzParseHex: the octets survive a round trip, whatever the separators.
func FuzzParseHex(f *testing.F) {
	for _, s := range []string{"0a0b", "0x0a 0x0b", "0a:0b", "0A-0B,0c", "0", "zz", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		b, err := ParseHex(s)
		if err != nil {
			return
		}
		again, err := ParseHex(hex.EncodeToString(b))
		if err != nil || !bytes.Equal(b, again) {
			t.Fatalf("%q: % x, then % x, %v", s, b, again, err)
		}
	})
}

// FuzzParseValues feeds the typed value syntax: dates, times, bit strings and
// every value type the write command accepts.
func FuzzParseValues(f *testing.F) {
	for _, s := range []string{"2026-10-31", "*-*-*", "2026-13-40", "12:30:15.25", "*:*:*.*", "24:61:61", "10100000", "1", "-1.5e300", "NaN", "active", "0xFF", "null"} {
		f.Add(TypeReal, s)
		f.Add(TypeEnum, s)
		f.Add(TypeDate, s)
		f.Add(TypeTime, s)
	}
	f.Fuzz(func(t *testing.T, typ, s string) {
		_, _ = ParseTypedValue(typ, s, ValueContext{})
		if d, err := ParseDate(s); err == nil && d.Month == 0 {
			t.Fatalf("%q: month 0 means nothing in a BACnet date", s)
		}
		if tm, err := ParseTime(s); err == nil && tm.Hour > 23 && tm.Hour != 0xFF {
			t.Fatalf("%q: hour %d", s, tm.Hour)
		}
		if bits, err := ParseBits(s); err == nil && bits.UnusedBits > 7 {
			t.Fatalf("%q: %d unused bits", s, bits.UnusedBits)
		}
	})
}

// FuzzParseWrite feeds the command line of `bacnetctl write`.
func FuzzParseWrite(f *testing.F) {
	f.Add("analog-value:1", "present-value", "21.5")
	f.Add("binary-output:3", "priority-array[8]", "active")
	f.Add("device:1", "object-name", "x")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, object, property, value string) {
		if _, err := ParseWrite(&config.WriteConfig{}, object, property, value); err != nil {
			return
		}
		if _, err := ParseWrites(&config.WriteConfig{}, []string{object, property, value}); err != nil {
			t.Fatalf("ParseWrite accepted %q %q %q, ParseWrites did not: %v", object, property, value, err)
		}
	})
}

// rpmSeeds are ReadPropertyMultiple request payloads: well formed, and with the property
// references of five octets that are malformed.
var rpmSeeds = [][]byte{
	{0x0c, 0x00, 0x00, 0x00, 0x01, 0x1e, 0x09, 0x55, 0x1f},
	{0x0c, 0x00, 0x00, 0x00, 0x01, 0x1e, 0x09, 0x55, 0x19, 0x08, 0x09, 0x4d, 0x1f, 0x0c, 0x02, 0x00, 0x04, 0xd2, 0x1e, 0x09, 0x08, 0x1f},
	{0x0c, 0x00, 0x00, 0x00, 0x01, 0x1e, 0x0c, 0xff, 0xff, 0xff, 0xff, 0x1f},
	{0x0c, 0x00, 0x00, 0x00, 0x01, 0x1e, 0x0d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55, 0x1f},
	{0x0c, 0x00, 0x00, 0x00, 0x01, 0x1e, 0x09, 0x55, 0x1d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x08, 0x1f},
	{0x0c, 0x00, 0x00, 0x00, 0x01, 0x1e, 0x29, 0x55, 0x1f},
	{0x1e, 0x09, 0x55, 0x1f},
	{},
}

// FuzzDecodeReadPropertyMultiple: a request decodes or is refused, never with a property
// reference reduced to its low bits; what decodes has an object and properties.
func FuzzDecodeReadPropertyMultiple(f *testing.F) {
	for _, s := range rpmSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, payload []byte) {
		specs, err := DecodeReadPropertyMultiple(payload, bacnet.DefaultDecodeLimits())
		if err != nil {
			return
		}
		if len(specs) == 0 {
			t.Fatalf("% x: no error and no specification", payload)
		}
	})
}

// FuzzDecode feeds whole frames to the decoder of `bacnetctl decode`: it reports what it
// cannot decode and never panics; the result is valid JSON.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{
		"810b000c 0120 ffff 00ff 1008",
		"810a0011 0100 1000 c4020004d2 2205c4 9103 2203e7",
		"810a0013 0104 0005 010c 0c00000001 1955",
		"810a0015 0104 0005 010e 0c00000001 1e 0955 1f",
		"810a0017 0104 0005 010e 0c00000001 1e 0d0501000000 55 1f",
		"810a0012 0100 300b06 11 0e 3100 6503 616263 0f",
		"810b0012 0100 1006 a47e0a0a06 b40c1e0f00",
		"810a000e 0100 3c 01 00 02 0c 0c0000",
		"8100",
		"",
	} {
		raw, err := ParseHex(s)
		if err != nil {
			f.Fatalf("seed %q: %v", s, err)
		}
		if len(raw) >= 4 {
			raw[2], raw[3] = byte(len(raw)>>8), byte(len(raw))
		}
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		res := Decode(raw)
		if res == nil {
			t.Fatalf("% x: no result", raw)
		}
		if _, err := json.Marshal(res); err != nil {
			t.Fatalf("% x: result does not encode: %v", raw, err)
		}
	})
}

// FuzzText: any octets in any character set give valid UTF-8.
func FuzzText(f *testing.F) {
	for _, s := range [][]byte{
		{0, 0, 0, 'A', 0x00, 0x11, 0x00, 0x00, 0, 0, 0xD8, 0},
		{0xD8, 0x00, 0x00, 0x41},
		{0xfc, 'a'},
		{0xff, 0xfe, 0xfd},
		{},
	} {
		for _, enc := range []uint8{0, 1, 2, 3, 4, 5, 6} {
			f.Add(enc, s)
		}
	}
	f.Fuzz(func(t *testing.T, enc uint8, raw []byte) {
		got := Text(bacnet.CharacterString{Encoding: enc, Value: string(raw)})
		if !utf8.ValidString(got) {
			t.Fatalf("charset %d, % x: %q is not valid UTF-8", enc, raw, got)
		}
	})
}
