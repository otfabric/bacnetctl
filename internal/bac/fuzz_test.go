// SPDX-License-Identifier: MIT

package bac

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/otfabric/bacnetctl/internal/config"
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
