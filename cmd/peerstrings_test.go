// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
)

// deviceString holds a screen clear, a window title, BEL, CR, LF, a C1 CSI and a
// right-to-left override, as a device can send them in any character string.
const deviceString = "AHU\x1b[2J\x1b]0;title\a\r\nNEXT\u009b\u202eNAME"

// noControls fails the test when out holds a control character other than the line feeds
// of the layout, or a bidirectional control.
func noControls(t *testing.T, what, out string) {
	t.Helper()
	if !utf8.ValidString(out) {
		t.Errorf("%s: the output is not UTF-8: %q", what, out)
	}
	for i, r := range out {
		if (r < 0x20 && r != '\n') || r == 0x7F || (r >= 0x80 && r <= 0x9F) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			t.Errorf("%s: U+%04X at %d of the output:\n%q", what, r, i, out)
			return
		}
	}
	if strings.Contains(out, "\nNEXT") {
		t.Errorf("%s: the line feed of the device string starts a line:\n%s", what, out)
	}
}

// The strings a device sends reach text and table output with their control characters
// escaped; JSON keeps them as they are.
func TestDeviceStringsAreEscaped(t *testing.T) {
	text := bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: deviceString}}
	dev := bactest.Start(t,
		bactest.DeviceProperty(bacnet.PropertyObjectName, text),
		bactest.DeviceProperty(bacnet.PropertyDescription, text),
	)
	escaped := `AHU\x1b[2J\x1b]0;title\a\r\nNEXT\u009b\u202eNAME`
	for _, args := range [][]string{
		{"device"},
		{"device", "--format", "table"},
		{"read", "device", "object-name"},
		{"read", "device", "object-name,description", "--format", "table"},
		{"describe", "device"},
		{"objects", "--values"},
		{"objects", "--values", "--format", "table"},
	} {
		r := on(t, dev, args...).want(t, 0).has(t, escaped)
		noControls(t, strings.Join(args, " "), r.stdout)
	}
	for _, args := range [][]string{
		{"discover", "--target", dev.Target(), "--local-port", "0", "--names"},
		{"whohas", "--object", "device:1234", "--target", dev.Target(), "--local-port", "0"},
		{"whohas", "--object", "device:1234", "--target", dev.Target(), "--local-port", "0", "--format", "table"},
	} {
		r := run(t, args...).want(t, 0).has(t, escaped)
		noControls(t, strings.Join(args, " "), r.stdout)
	}

	var res types.DeviceResult
	on(t, dev, "device", "--format", "json").want(t, 0).decode(t, &res)
	if res.Name != deviceString || res.Description != deviceString {
		t.Errorf("JSON: name %q, description %q", res.Name, res.Description)
	}
}
