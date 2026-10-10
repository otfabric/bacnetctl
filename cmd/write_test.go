// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func real(t testing.TB, dev *bactest.Device, id bacnet.ObjectIdentifier) float32 {
	t.Helper()
	v, ok := dev.Value(id, bacnet.PropertyPresentValue)
	if !ok || v.Kind != bacnet.ValueReal {
		t.Fatalf("present value of %v: %+v", id, v)
	}
	return v.Real
}

func TestWriteAndRelease(t *testing.T) {
	dev := bactest.Start(t)

	// A typed value needs no look at the device.
	r := on(t, dev, "write", "analog-value:1", "present-value", "real:22.5", "--priority", "8").want(t, 0)
	if r.stdout != "analog-value:1 present-value = 22.5 at priority 8 (acknowledged by the device)\n" || r.stderr != "" {
		t.Errorf("text: %q, stderr %q", r.stdout, r.stderr)
	}
	if got := real(t, dev, av1); got != 22.5 || dev.Count(bactest.ReadProperty) != 0 || dev.Count(bactest.WriteProperty) != 1 {
		t.Errorf("value %v, requests %v", got, dev.Requests())
	}
	on(t, dev, "read", "av:1", "priority-array[8]").want(t, 0).has(t, "22.5\n")

	// A lower priority is stored, and does not win.
	on(t, dev, "write", "av:1", "pv", "real:18").want(t, 0).has(t, "analog-value:1 present-value = 18 (acknowledged")
	on(t, dev, "read", "av:1", "pv,priority-array[16]").want(t, 0).has(t, "present-value       22.5\n", "priority-array[16]  18")

	// Relinquishing the higher one hands over to it, then to the relinquish default.
	r = on(t, dev, "release", "av:1", "--priority", "8").want(t, 0)
	if r.stdout != "analog-value:1 present-value: relinquished at priority 8 (acknowledged by the device)\n" || real(t, dev, av1) != 18 {
		t.Errorf("release: %q, value %v", r.stdout, real(t, dev, av1))
	}
	on(t, dev, "write", "av:1", "pv", "null", "--priority", "16", "--format", "table").want(t, 0).has(t, "STATUS", "relinquished", "null")
	if got := real(t, dev, av1); got != 20 {
		t.Errorf("value after relinquishing everything: %v", got)
	}

	// Without a type, the property is read first and says it.
	before := dev.Count(bactest.ReadProperty)
	var res types.WriteResult
	on(t, dev, "write", "av:1", "pv", "23", "--priority", "10", "--format", "json").want(t, 0).decode(t, &res)
	if res.Target != dev.Target() || res.Object != "analog-value:1" || res.ObjectType != 2 || res.Property != "present-value" || res.PropertyID != 85 ||
		res.Value.Type != "real" || res.Value.Value != 23.0 || res.Priority == nil || *res.Priority != 10 || res.Status != "written" || res.RTTMS <= 0 {
		t.Errorf("result: %+v", res)
	}
	if dev.Count(bactest.ReadProperty) != before+1 {
		t.Errorf("--type auto reads the property once: %v", dev.Requests())
	}
	// An enumerated property takes the names of its values.
	on(t, dev, "write", "bv:1", "pv", "inactive").want(t, 0).has(t, "binary-value:1 present-value = inactive (0)")
	if v, _ := dev.Value(bv1, bacnet.PropertyPresentValue); v.Enumerated != 0 {
		t.Errorf("binary value: %+v", v)
	}
	on(t, dev, "write", "bv:1", "pv", "enum:1").want(t, 0).has(t, "active (1)")
	on(t, dev, "write", "msv:1", "pv", "3").want(t, 0).has(t, "multi-state-value:1 present-value = 3 ")
	on(t, dev, "write", "av:1", "object-name", "string:Setpoint Room 4").want(t, 0).has(t, "object-name = Setpoint Room 4")
	on(t, dev, "write", "av:1", "description", "time:critical", "--type", "string").want(t, 0).has(t, "description = time:critical")
	on(t, dev, "write", "av:1", "pv", "--type", "real", "--", "12.5").want(t, 0)
	// --type names the type and skips the read.
	before = dev.Count(bactest.ReadProperty)
	on(t, dev, "write", "ai:1", "out-of-service", "true", "--type", "bool").want(t, 0).has(t, "out-of-service = true")
	on(t, dev, "write", "ai:1", "pv", "19", "--type", "real").want(t, 0)
	if dev.Count(bactest.ReadProperty) != before || real(t, dev, ai1) != 19 {
		t.Errorf("--type real: %v, value %v", dev.Requests(), real(t, dev, ai1))
	}
	on(t, dev, "read", "ai:1", "status-flags").want(t, 0).has(t, "{out-of-service}")

	// What the device refuses: exit code 6, and nothing changed.
	for value, want := range map[string]string{
		"real:99":    "value-out-of-range",
		"unsigned:5": "invalid-data-type",
		"string:x":   "invalid-data-type",
	} {
		on(t, dev, "write", "av:1", "pv", value).want(t, 6).silent(t).errHas(t, "error: the device refused the request: "+want)
	}
	on(t, dev, "write", "av:1", "pv", "real:15", "--priority", "6").want(t, 6).errHas(t, "write-access-denied")
	on(t, dev, "write", "ai:2", "pv", "real:1").want(t, 6).errHas(t, "write-access-denied")
	on(t, dev, "write", "av:1", "units", "enum:64").want(t, 6).errHas(t, "write-access-denied")
	on(t, dev, "write", "av:9", "pv", "real:1").want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "write", "av:1", "high-limit", "real:1").want(t, 6).errHas(t, "unknown-property")
	on(t, dev, "write", "av:9", "pv", "1").want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "release", "bv:9", "--priority", "8").want(t, 6).errHas(t, "unknown-object")
	if e := on(t, dev, "write", "av:1", "pv", "real:99", "--format", "json").want(t, 6).failure(t); e.Kind != "device_error" || e.Code != "value_out_of_range" || e.BACnet.Class != "property" {
		t.Errorf("error: %+v", e)
	}
	// The type of a list cannot be taken from the device.
	on(t, dev, "write", "av:1", "priority-array", "5").want(t, 3).errHas(t, "cannot tell the type", "real:5")
	// A value the type of the property cannot hold is found after the read.
	on(t, dev, "write", "av:1", "pv", "warm").want(t, 3).errHas(t, `value "warm" is not a number`)
	on(t, dev, "write", "bv:1", "pv", "sideways").want(t, 3).errHas(t, "inactive, active")

	// What is wrong with the invocation: exit code 3, and nothing was sent.
	sent := len(dev.Requests())
	for name, args := range map[string][]string{
		"priority":         {"write", "--device", dev.Target(), "av:1", "pv", "real:1", "--priority", "17"},
		"real":             {"write", "--device", dev.Target(), "av:1", "pv", "real:warm"},
		"unsigned":         {"write", "--device", dev.Target(), "av:1", "pv", "unsigned:-1"},
		"signed":           {"write", "--device", dev.Target(), "av:1", "pv", "signed:1.5"},
		"double":           {"write", "--device", dev.Target(), "av:1", "pv", "double:x"},
		"bool":             {"write", "--device", dev.Target(), "av:1", "pv", "bool:maybe"},
		"enum":             {"write", "--device", dev.Target(), "av:1", "pv", "enum:red"},
		"octets":           {"write", "--device", dev.Target(), "av:1", "pv", "octets:xyz"},
		"bits":             {"write", "--device", dev.Target(), "av:1", "pv", "bits:102"},
		"date":             {"write", "--device", dev.Target(), "av:1", "pv", "date:2026-02-30"},
		"time":             {"write", "--device", dev.Target(), "av:1", "pv", "time:25:00"},
		"object":           {"write", "--device", dev.Target(), "av:1", "pv", "object:nothing"},
		"null":             {"write", "--device", dev.Target(), "av:1", "pv", "something", "--type", "null"},
		"type":             {"write", "--device", dev.Target(), "av:1", "pv", "1", "--type", "integer"},
		"two properties":   {"write", "--device", dev.Target(), "av:1", "pv,units", "real:1"},
		"all":              {"write", "--device", dev.Target(), "av:1", "all", "real:1"},
		"index":            {"write", "--device", dev.Target(), "av:1", "pv", "real:1", "--index", "x"},
		"index twice":      {"write", "--device", dev.Target(), "av:1", "pv[1]", "real:1", "--index", "1"},
		"object id":        {"write", "--device", dev.Target(), "av", "pv", "real:1"},
		"property":         {"write", "--device", dev.Target(), "av:1", "nonsense", "real:1"},
		"no device":        {"write", "av:1", "pv", "real:1"},
		"release priority": {"release", "--device", dev.Target(), "av:1"},
		"release high":     {"release", "--device", dev.Target(), "av:1", "--priority", "17"},
		"release object":   {"release", "--device", dev.Target(), "nonsense", "--priority", "8"},
		"release property": {"release", "--device", dev.Target(), "av:1", "--priority", "8", "--property", "nonsense"},
		"release device":   {"release", "av:1", "--priority", "8"},
	} {
		r := run(t, args...)
		if r.code != 3 || r.stdout != "" || !strings.HasPrefix(r.stderr, "error: ") {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	if len(dev.Requests()) != sent {
		t.Errorf("an invalid invocation sent requests: %v", dev.Requests()[sent:])
	}
}

// Every type of the value syntax reaches the device as that type.
func TestWriteTypes(t *testing.T) {
	dev := bactest.Start(t)
	// The device refuses these types for the property, after it has decoded them: what it
	// says proves they arrived well-formed.
	for _, spec := range []string{"signed:-5", "double:1.5", "bool:true", "octets:0a1b", "bits:1011", "date:2026-10-10", "time:12:30:15.50", "object:ai:1", "unsigned:0x10"} {
		on(t, dev, "write", "av:1", "pv", spec).want(t, 6).errHas(t, "invalid-data-type")
	}
	if n := dev.Count(bactest.WriteProperty); n != 9 {
		t.Errorf("%d writes arrived, want 9", n)
	}
}

// A write that is sent and not acknowledged is neither a success nor a failure.
func TestWriteOutcomeUnknown(t *testing.T) {
	dev := bactest.Start(t, bactest.Mute(bactest.WriteProperty))

	r := on(t, dev, append([]string{"write", "av:1", "pv", "real:25"}, fast...)...).want(t, 5).silent(t)
	r.errHas(t, "error: outcome unknown: the WriteProperty request was sent, but no acknowledgement arrived in time.",
		"The device may or may not have carried it out", "it was not sent again", "Read the value back")
	// The device did carry it out, and it was asked exactly once.
	if got := real(t, dev, av1); got != 25 || dev.Count(bactest.WriteProperty) != 1 {
		t.Errorf("value %v after %d writes", got, dev.Count(bactest.WriteProperty))
	}
	e := on(t, dev, append([]string{"write", "av:1", "pv", "real:26", "--format", "json"}, fast...)...).want(t, 5).failure(t)
	if e.Kind != "timeout" || e.Code != "outcome_unknown" || !strings.Contains(e.Message, "may or may not") {
		t.Errorf("error object: %+v", e)
	}
	e = on(t, dev, append([]string{"release", "av:1", "--priority", "16", "--format", "json"}, fast...)...).want(t, 5).failure(t)
	if e.Code != "outcome_unknown" {
		t.Errorf("release: %+v", e)
	}
	// Even with retries configured a write is sent once.
	before := dev.Count(bactest.WriteProperty)
	on(t, dev, "write", "av:1", "pv", "real:27", "--timeout", "100", "--retries", "3").want(t, 5)
	if n := dev.Count(bactest.WriteProperty) - before; n != 1 {
		t.Errorf("the write was sent %d times", n)
	}
	// A read against the same device is a plain timeout, and is retried.
	silent := bactest.Start(t, bactest.Mute(bactest.ReadProperty))
	on(t, silent, "read", "av:1", "pv", "--timeout", "100", "--retries", "2").want(t, 5).errHas(t, "timeout: no answer")
	if n := silent.Count(bactest.ReadProperty); n != 3 {
		t.Errorf("the read was sent %d times, want 3", n)
	}
}

func TestTimeSync(t *testing.T) {
	dev := bactest.Start(t)

	on(t, dev, "time", "sync", "--time", "2026-01-31T12:34:56.78+01:00").want(t, 0).
		has(t, "time-synchronization sent to "+dev.Target()+": 2026-01-31 12:34:56.78 (unconfirmed")
	dev.AwaitRequest(t, bactest.TimeSync, 1)
	got, _ := dev.TimeSync()
	// 2026-01-31 is a Saturday: day of week 6.
	want := service.TimeSynchronization{Date: bacnet.Date{Year: 126, Month: 1, Day: 31, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 34, Second: 56, Hundredths: 78}}
	if got != want {
		t.Errorf("time the device received: %+v", got)
	}

	// In UTC the same instant is an hour earlier.
	var res types.TimeSyncResult
	on(t, dev, "time", "sync", "--utc", "--time", "2026-01-31T12:34:56+01:00", "--format", "json").want(t, 0).decode(t, &res)
	if res.Target != dev.Target() || res.Service != "utc-time-synchronization" || res.Time != "2026-01-31 11:34:56.00" || res.Status != "sent" {
		t.Errorf("result: %+v", res)
	}
	dev.AwaitRequest(t, bactest.UTCTimeSync, 1)
	if got, _ := dev.TimeSync(); got.Time.Hour != 11 {
		t.Errorf("UTC time the device received: %+v", got)
	}
	on(t, dev, "time", "sync", "--format", "table").want(t, 0).has(t, "TARGET", "time-synchronization", "sent")

	for name, args := range map[string][]string{
		"time":                 {"time", "sync", "--device", dev.Target(), "--time", "yesterday"},
		"no destination":       {"time", "sync"},
		"two destinations":     {"time", "sync", "--device", dev.Target(), "--broadcast"},
		"behind a router":      {"time", "sync", "--device", dev.Target(), "--network", "5", "--mac", "01"},
		"broadcast and a bbmd": {"time", "sync", "--broadcast", "--bbmd", "a b"},
	} {
		if r := run(t, args...); r.code != 3 || r.stdout != "" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
}
