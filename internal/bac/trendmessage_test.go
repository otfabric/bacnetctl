// SPDX-License-Identifier: MIT

package bac

import (
	"testing"

	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// The message of an event notification an event log recorded is converted from its
// character set as the message of `events` is: the same notification reads the same in
// `trend` and in `events`.
func TestEventLogMessageCharacterSet(t *testing.T) {
	for _, c := range []struct {
		name     string
		encoding uint8
		raw      string
		want     string
	}{
		{"UTF-8", charsetUTF8, "Humidity high", "Humidity high"},
		{"UCS-2", charsetUCS2, "\x00H\x00u\x00m\x00i\x00d\x00i\x00t\x00y\x00 \x00h\x00i\x00g\x00h", "Humidity high"},
		{"UCS-2 outside Latin-1", charsetUCS2, "\x00Z\x00\xfc\x20\xac", "Zü€"},
		{"ISO 8859-1", charsetISO88591, "Feuchte zu hoch, 30 \xb0C, K\xfchlung", "Feuchte zu hoch, 30 °C, Kühlung"},
		{"UCS-4", charsetUCS4, "\x00\x00\x00Z\x00\x01\xf6\x00", "Z😀"},
	} {
		n := service.EventNotification{
			InitiatingDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
			EventObject:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 2},
			EventType:        5,
			ToState:          3,
			MessageText:      &bacnet.CharacterString{Encoding: c.encoding, Value: c.raw},
		}
		logged := eventLogRecord(1, service.EventLogRecord{Choice: service.LogDataValues, Notification: &n})
		if logged.Event == nil {
			t.Fatalf("%s: no event in %+v", c.name, logged)
		}
		if got := logged.Event.Message; got != c.want {
			t.Errorf("%s: trend message %q, want %q", c.name, got, c.want)
		}
		if got := eventRecord(n, "127.0.0.1:47808", false, false).Message; got != logged.Event.Message {
			t.Errorf("%s: events gives %q, trend %q", c.name, got, logged.Event.Message)
		}
	}
}
