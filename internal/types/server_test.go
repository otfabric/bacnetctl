// SPDX-License-Identifier: MIT

package types

import (
	"strings"
	"testing"
)

func TestServerRecordText(t *testing.T) {
	one, three := 1, 3
	prio := uint8(8)
	yes, no := true, false
	idx := uint32(2)
	av1 := &ObjectRef{Object: "analog-value,1", ObjectType: 2, Instance: 1}
	pv := &PropertyRef{Property: "present-value", PropertyID: 85}
	cases := []struct {
		name string
		rec  ServerRecord
		want []string // substrings of the text output
		not  []string
	}{
		{"listening", ServerRecord{Event: ServerListening, Name: "dev", Address: "0.0.0.0:47808", Objects: &three,
			ObjectRef: &ObjectRef{Object: "device,1234"}}, []string{"listening", "device,1234", `"dev"`, "on 0.0.0.0:47808", "3 objects"}, nil},
		{"listening one object", ServerRecord{Event: ServerListening, Name: "d", Address: "a", Objects: &one}, []string{"1 object"}, []string{"objects"}},
		{"listening without count", ServerRecord{Event: ServerListening, Name: "d", Address: "a"}, []string{"0 objects"}, nil},
		{"write", ServerRecord{Event: ServerWrite, ObjectRef: av1, PropertyRef: pv, Value: &Value{Type: "real", Text: "19.5"},
			Priority: &prio, Status: ServerAllowed}, []string{"present-value=19.5", "priority 8", "allowed"}, nil},
		{"write index no priority", ServerRecord{Event: ServerWrite, ObjectRef: av1, PropertyRef: &PropertyRef{Property: "priority-array", Index: &idx},
			Value: &Value{Text: "NULL"}, Status: ServerRefused}, []string{"priority-array[2]=NULL", "refused"}, []string{"priority "}},
		{"write without value", ServerRecord{Event: ServerWrite, ObjectRef: av1, Status: ServerRefused}, []string{"refused"}, []string{"="}},
		{"create", ServerRecord{Event: ServerCreate, ObjectRef: av1}, []string{"create", "analog-value,1"}, nil},
		{"delete", ServerRecord{Event: ServerDelete, ObjectRef: av1, Status: ServerAllowed}, []string{"delete", "allowed"}, nil},
		{"reinitialize", ServerRecord{Event: ServerReinitialize, State: "warmstart"}, []string{"warmstart"}, nil},
		{"time-sync utc", ServerRecord{Event: ServerTimeSync, Time: "2026-11-06T10:00:00", UTC: &yes}, []string{"2026-11-06T10:00:00 UTC"}, nil},
		{"time-sync local", ServerRecord{Event: ServerTimeSync, Time: "2026-11-06T10:00:00", UTC: &no}, []string{"2026-11-06T10:00:00"}, []string{"UTC"}},
		{"time-sync unset", ServerRecord{Event: ServerTimeSync, Time: "t"}, []string{"t"}, []string{"UTC"}},
		{"gap", ServerRecord{Event: ServerDelete, ObjectRef: av1, Status: ServerAllowed, Gap: true}, []string{"records were lost"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.rec.Timestamp = "2026-11-06T10:00:00.000Z"
			text, err := c.rec.MarshalTextOutput()
			if err != nil {
				t.Fatal(err)
			}
			row := strings.Join(c.rec.TableRows()[0], " ")
			for _, w := range c.want {
				if !strings.Contains(text, w) {
					t.Errorf("text %q lacks %q", text, w)
				}
				if !strings.Contains(row, w) && w != c.rec.Event {
					t.Errorf("table row %q lacks %q", row, w)
				}
			}
			for _, n := range c.not {
				if strings.Contains(text, n) {
					t.Errorf("text %q has %q", text, n)
				}
			}
			if text != strings.TrimRight(text, " ") {
				t.Errorf("trailing space in %q", text)
			}
		})
	}
	if got := (&ServerRecord{}).TableHeaders(); len(got) != 4 {
		t.Fatalf("headers %v", got)
	}
	// A record without an object shows a dash in the table.
	if row := (&ServerRecord{Event: ServerReinitialize, State: "coldstart"}).TableRows()[0]; !strings.Contains(row[2], "-") {
		t.Fatalf("object column %q", row[2])
	}
}

func TestServerSummary(t *testing.T) {
	s := ServerSummary{Device: "device,1234", Address: "0.0.0.0:47808", Records: 1, Duration: 1500}
	if got := s.String(); got != "device,1234 on 0.0.0.0:47808: served for 1.5 s, 1 record" {
		t.Fatalf("summary %q", got)
	}
	s.Records, s.Dropped = 4, 2
	if got := s.String(); !strings.Contains(got, "4 records") || !strings.Contains(got, "(2 not printed") {
		t.Fatalf("summary %q", got)
	}
}
