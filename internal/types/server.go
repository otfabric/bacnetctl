// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// Events of `server run`.
const (
	ServerListening    = "listening"
	ServerWrite        = "write"
	ServerCreate       = "create"
	ServerDelete       = "delete"
	ServerReinitialize = "reinitialize"
	ServerTimeSync     = "time-sync"
)

// Statuses of a write, delete or reinitialize request in a ServerRecord.
const (
	ServerAllowed = "allowed"
	ServerRefused = "refused"
)

// ServerRecord is one thing that happened while `server run` served its device: that it
// listens, and the requests of clients that change the device.
type ServerRecord struct {
	// Timestamp is when bacnetctl printed the record, RFC 3339 with milliseconds, UTC.
	Timestamp string `json:"timestamp"`
	// Event is "listening", "write", "create", "delete", "reinitialize" or "time-sync".
	Event string `json:"event"`
	// Address is the BACnet/IP address the server is bound to (listening).
	Address string `json:"address,omitempty"`
	// Name is the name of the device (listening).
	Name string `json:"name,omitempty"`
	// Objects is the number of objects the device serves, the Device object included
	// (listening).
	Objects *int `json:"objects,omitempty"`
	// ObjectRef is the Device object (listening) or the object a request is for.
	*ObjectRef
	// PropertyRef is the property that is written.
	*PropertyRef
	// Value is what is written.
	Value *Value `json:"value,omitempty"`
	// Priority is the priority of the write, when the request carried one.
	Priority *uint8 `json:"priority,omitempty"`
	// Status is "allowed" or "refused" (write, delete). An allowed write is handed to the
	// object, which can still refuse the value.
	Status string `json:"status,omitempty"`
	// State is the state of a ReinitializeDevice request.
	State string `json:"state,omitempty"`
	// Time is the date and time of a time synchronization, as sent; UTC tells whether it
	// was UTCTimeSynchronization.
	Time string `json:"time,omitempty"`
	UTC  *bool  `json:"utc,omitempty"`
	// Gap is set when records before this one were lost because they came faster than
	// they were printed.
	Gap bool `json:"gap,omitempty"`
}

func (r *ServerRecord) object() string {
	if r.ObjectRef == nil {
		return ""
	}
	return r.Object
}

// detail is the record after its object, as text.
func (r *ServerRecord) detail() string {
	var parts []string
	switch r.Event {
	case ServerListening:
		n := 0
		if r.Objects != nil {
			n = *r.Objects
		}
		parts = append(parts, strconv.Quote(r.Name), "on "+r.Address, plural(n, "object", "objects"))
	case ServerWrite:
		if r.PropertyRef != nil && r.Value != nil {
			parts = append(parts, r.Label()+"="+r.Value.String())
		}
		if r.Priority != nil {
			parts = append(parts, fmt.Sprintf("priority %d", *r.Priority))
		}
		parts = append(parts, r.Status)
	case ServerDelete:
		parts = append(parts, r.Status)
	case ServerReinitialize:
		parts = append(parts, r.State)
		if r.Status != "" {
			parts = append(parts, r.Status)
		}
	case ServerTimeSync:
		utc := r.UTC != nil && *r.UTC
		if utc {
			parts = append(parts, r.Time+" UTC")
		} else {
			parts = append(parts, r.Time)
		}
	}
	if r.Gap {
		parts = append(parts, "(records were lost before this one)")
	}
	return strings.Join(parts, "  ")
}

// TableHeaders implements format.TableMarshaler.
func (r *ServerRecord) TableHeaders() []string { return []string{"TIME", "EVENT", "OBJECT", "DETAIL"} }

// TableRows implements format.TableMarshaler.
func (r *ServerRecord) TableRows() [][]string {
	return [][]string{{clock(r.Timestamp), fmt.Sprintf("%-12s", r.Event), fmt.Sprintf("%-18s", dash(r.object())), r.detail()}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ServerRecord) MarshalTextOutput() (string, error) {
	return strings.TrimRight(fmt.Sprintf("%s  %-12s  %-18s  %s", clock(r.Timestamp), r.Event, r.object(), r.detail()), " "), nil
}

// ServerSummary is what `server run` reports on stderr when it stops.
type ServerSummary struct {
	Device   string
	Address  string
	Records  int
	Dropped  int
	Duration float64
}

// String implements fmt.Stringer.
func (s ServerSummary) String() string {
	out := fmt.Sprintf("%s on %s: served for %s, %s", s.Device, s.Address, seconds(s.Duration), plural(s.Records, "record", "records"))
	if s.Dropped > 0 {
		out += fmt.Sprintf(" (%d not printed: they came faster than stdout took them)", s.Dropped)
	}
	return out
}
