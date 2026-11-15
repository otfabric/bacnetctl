// SPDX-License-Identifier: MIT

package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/otfabric/bacnetctl/internal/format"
)

func TestTerminalSafe(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"AHU-1 Supply Temp", "AHU-1 Supply Temp"},
		{`C:\path "quoted" 'single'`, `C:\path "quoted" 'single'`},
		{"Küche 20 °C 😀 \uFFFD", "Küche 20 °C 😀 \uFFFD"},
		{"\x00", `\x00`},
		{"a\nb\r\tc", `a\nb\r\tc`},
		{"\a\b\f\v", `\a\b\f\v`},
		{"\x1b[2J\x1b]0;title\a", `\x1b[2J\x1b]0;title\a`},
		{"\x1f \x20", `\x1f  `},
		{"\x7f", `\x7f`},
		{"\u0080\u0085\u009b\u009f\u00a0", `\u0080\u0085\u009b\u009f` + "\u00a0"},
		{"\u2029\u202a\u202b\u202c\u202d\u202e\u202f", "\u2029" + `\u202a\u202b\u202c\u202d\u202e` + "\u202f"},
		{"\u2065\u2066\u2067\u2068\u2069\u206a", "\u2065" + `\u2066\u2067\u2068\u2069` + "\u206a"},
		{"bad \xff\x9b octets \xc3", `bad \xff\x9b octets \xc3`},
		{"\u202eNAME", `\u202eNAME`},
	} {
		if got := TerminalSafe(c.in); got != c.want {
			t.Errorf("TerminalSafe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Every C0 control, DEL and every C1 control is escaped.
	for r := rune(0); r < 0xA0; r++ {
		if r >= 0x20 && r < 0x7F {
			continue
		}
		got := TerminalSafe(string(r))
		if !strings.HasPrefix(got, `\`) || hasControl(got) {
			t.Errorf("U+%04X: %q", r, got)
		}
	}
}

// hasControl reports whether s holds a byte below 0x20 other than a line feed, DEL, a C1
// control, a bidirectional control or an octet that is not UTF-8.
func hasControl(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r != '\n' && control(r) {
			return true
		}
	}
	return false
}

// controls holds a control of every class TerminalSafe escapes: a screen clear, a window
// title, BEL, CR, LF, TAB, DEL, a C1 CSI, a right-to-left override and an isolate.
const controls = "AHU\x1b[2J\x1b]0;title\a\r\nline\tx\x7f\u009b\u202eNAME\u2066"

// Every text and table rendering of a result that holds a string from a device or a client
// shows its controls as escapes: no control octet but the layout's own line feeds, and as
// many lines as with a plain string. JSON keeps the string as it is.
func TestPeerStringsInTextAndTable(t *testing.T) {
	str := func(s string) *Value { return &Value{Type: TypeCharacterString, Value: s, Text: s} }
	for _, c := range []struct {
		name   string
		result func(s string) any
	}{
		{"discover", func(s string) any {
			return &DiscoverResult{Mode: "unicast", Target: "t", Names: true, Count: 1,
				Devices: []Device{{Instance: 1, Address: "a", Name: s, VendorName: s, ModelName: s}}}
		}},
		{"whohas", func(s string) any {
			return &WhoHasResult{Mode: "unicast", Query: "object device:1", Count: 1, Objects: []FoundObject{{ObjectRef: ObjectRef{Object: "device:1"}, Name: s}}}
		}},
		{"read one value", func(s string) any {
			return &ReadResult{Target: "t", Count: 1, Rows: []PropertyRow{{ObjectRef: ObjectRef{Object: "device:1"}, PropertyRef: PropertyRef{Property: "object-name"}, Value: str(s)}}}
		}},
		{"read", func(s string) any {
			return &ReadResult{Target: "t", Count: 2, Rows: []PropertyRow{
				{ObjectRef: ObjectRef{Object: "device:1"}, PropertyRef: PropertyRef{Property: "object-name"}, Value: str(s)},
				{ObjectRef: ObjectRef{Object: "device:1"}, PropertyRef: PropertyRef{Property: "description"}, Value: str(s)},
			}}
		}},
		{"read list", func(s string) any {
			list := &Value{Type: TypeList, Value: []Value{*str(s), *str(s)}, Text: "[" + s + ", " + s + "]"}
			return &ReadResult{Target: "t", Count: 1, Rows: []PropertyRow{{ObjectRef: ObjectRef{Object: "device:1"}, PropertyRef: PropertyRef{Property: "x"}, Value: list}}}
		}},
		{"describe", func(s string) any {
			return &DescribeResult{Target: "t", ObjectRef: ObjectRef{Object: "device:1"}, Name: s, Count: 1,
				Properties: []PropertyRow{{PropertyRef: PropertyRef{Property: "object-name"}, Value: str(s)}}}
		}},
		{"objects", func(s string) any {
			return &ObjectsResult{Target: "t", Device: "device:1", Total: 1, Count: 1, Values: true,
				Objects: []ObjectRow{{ObjectRef: ObjectRef{Object: "ai:1"}, Name: s, PresentValue: str(s), Description: s}}}
		}},
		{"device", func(s string) any {
			return &DeviceResult{Target: "t", ObjectRef: ObjectRef{Object: "device:1"}, Name: s, Description: s, Location: s,
				VendorName: s, ModelName: s, SerialNumber: s, FirmwareRevision: s, ApplicationVersion: s}
		}},
		{"write", func(s string) any {
			return &WriteResult{Target: "t", ObjectRef: ObjectRef{Object: "av:1"}, PropertyRef: PropertyRef{Property: "description"}, Value: *str(s), Status: "written"}
		}},
		{"monitor", func(s string) any {
			return &COVRecord{Timestamp: "2026-11-15T12:00:00.000Z", ObjectRef: ObjectRef{Object: "av:1"},
				Values: []PropertyValue{{PropertyRef: PropertyRef{Property: "present-value"}, Value: *str(s)}}}
		}},
		{"events", func(s string) any {
			return &EventRecord{Timestamp: "2026-11-15T12:00:00.000Z", Device: "device:1", ObjectRef: ObjectRef{Object: "ai:2"},
				EventType: "out-of-range", NotifyType: "alarm", ToState: "high-limit", Message: s}
		}},
		{"trend", func(s string) any {
			return &TrendResult{Target: "t", ObjectRef: ObjectRef{Object: "trend-log:1"}, Count: 2, Records: []LogRecord{
				{Position: 1, Kind: "value", Value: str(s)},
				{Position: 2, Kind: "values", Values: []Value{*str(s), *str(s)}},
			}}
		}},
		{"server run write", func(s string) any {
			return &ServerRecord{Timestamp: "2026-11-15T12:00:00.000Z", Event: ServerWrite, ObjectRef: &ObjectRef{Object: "av:1"},
				PropertyRef: &PropertyRef{Property: "description"}, Value: str(s), Status: ServerAllowed}
		}},
		{"server run listening", func(s string) any {
			return &ServerRecord{Timestamp: "2026-11-15T12:00:00.000Z", Event: ServerListening, Name: s, Address: "a"}
		}},
		{"decode", func(s string) any {
			return &DecodeResult{Length: 1, Layers: []Layer{{Layer: "service", Summary: "i-have", Fields: []Field{{Name: "name", Value: s}}}}}
		}},
	} {
		for _, f := range []format.OutputFormat{format.FormatText, format.FormatTable} {
			render := func(s string) string {
				var b strings.Builder
				if err := format.Write(&b, f, c.result(s)); err != nil {
					t.Fatalf("%s %s: %v", c.name, f, err)
				}
				return b.String()
			}
			got, plain := render(controls), render("AHU plain string")
			if hasControl(got) {
				t.Errorf("%s %s: a control character in the output:\n%q", c.name, f, got)
			}
			if n, want := strings.Count(got, "\n"), strings.Count(plain, "\n"); n != want {
				t.Errorf("%s %s: %d lines, want %d:\n%s", c.name, f, n, want, got)
			}
			if !strings.Contains(got, `\x1b[2J`) {
				t.Errorf("%s %s: the escaped string is not shown:\n%s", c.name, f, got)
			}
		}
		raw, err := json.Marshal(c.result(controls))
		if err != nil {
			t.Fatal(err)
		}
		var back any
		if err := json.Unmarshal(raw, &back); err != nil || !strings.Contains(fmt.Sprint(back), controls) {
			t.Errorf("%s json: the string is not kept as it is: %s", c.name, raw)
		}
	}
}
