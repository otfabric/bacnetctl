// SPDX-License-Identifier: MIT

package cmd

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestMonitor(t *testing.T) {
	dev := bactest.Start(t)

	// The device sends the current value when it accepts a subscription.
	r := on(t, dev, "monitor", "analog-value:1", "--count", "1", "--duration", "30").want(t, 0)
	if !strings.Contains(r.stdout, "analog-value:1      present-value=20  status-flags={}\n") || strings.Count(r.stdout, "\n") != 1 {
		t.Errorf("text: %q", r.stdout)
	}
	r.errHas(t, "subscribed to analog-value:1 at "+dev.Target()+" (lifetime 300 s)", dev.Target()+": 1 notification in ")
	// When monitoring ends the subscription is cancelled on the device.
	if dev.Subscriptions() != 0 || dev.Count(bactest.SubscribeCOV) != 2 {
		t.Errorf("%d subscriptions left after %v", dev.Subscriptions(), dev.Requests())
	}

	// Two objects, and a change while monitoring runs: JSON Lines.
	wait := start(t, "monitor", "--device", dev.Target(), "av:1", "ai:1", "--count", "3", "--duration", "30", "--lifetime", "120", "--format", "json")
	dev.AwaitSubscriptions(t, 2)
	dev.SetPresentValue(ai1, bacnet.RealValue(22.75))
	lines := wait().want(t, 0).lines(t)
	if len(lines) != 3 {
		t.Fatalf("%d records, want 3: %v", len(lines), lines)
	}
	last := lines[2]
	values := last["values"].([]any)
	pv := values[0].(map[string]any)
	if last["source"] != "cov" || last["device"] != "device:1234" || last["object"] != "analog-input:1" || last["object_type"] != 0.0 ||
		last["instance"] != 1.0 || last["time_remaining"] != 120.0 || len(values) != 2 || pv["property"] != "present-value" ||
		pv["property_id"] != 85.0 || pv["value"].(map[string]any)["value"] != 22.75 || pv["value"].(map[string]any)["type"] != "real" {
		t.Errorf("record: %v", last)
	}
	if ts, err := time.Parse(time.RFC3339Nano, last["timestamp"].(string)); err != nil || time.Since(ts) > time.Minute || !strings.HasSuffix(last["timestamp"].(string), "Z") {
		t.Errorf("timestamp %v: %v", last["timestamp"], err)
	}
	if flags := values[1].(map[string]any); flags["property"] != "status-flags" || len(flags["value"].(map[string]any)["bits"].([]any)) != 0 {
		t.Errorf("status flags: %v", flags)
	}
	if dev.Subscriptions() != 0 {
		t.Errorf("%d subscriptions left", dev.Subscriptions())
	}

	// A write of another client shows up; confirmed notifications are acknowledged.
	wait = start(t, "monitor", "--device", dev.Target(), "bv:1", "--confirmed", "--count", "2", "--duration", "30", "--format", "table")
	dev.AwaitSubscriptions(t, 1)
	on(t, dev, "write", "bv:1", "pv", "enum:0").want(t, 0)
	wait().want(t, 0).has(t, "TIME", "OBJECT", "PROPERTY", "VALUE", "binary-value:1", "present-value", "active (1)", "inactive (0)", "status-flags")

	// One property, with an increment.
	r = on(t, dev, "monitor", "ai:1", "--property", "present-value", "--increment", "0.5", "--count", "1", "--duration", "30", "--format", "json").want(t, 0)
	if rec := r.lines(t)[0]; rec["object"] != "analog-input:1" || dev.Count(bactest.SubscribeCOVProp) != 2 {
		t.Errorf("property subscription: %v after %v", rec, dev.Requests())
	}
	// "device" alone cannot be subscribed to here: the device says so.
	on(t, dev, "monitor", "device").want(t, 6).silent(t).errHas(t, "optional-functionality-not-supported")
	on(t, dev, "monitor", "ai:9").want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "monitor", "ai:1", "--property", "high-limit").want(t, 6).errHas(t, "unknown-property")
	// One subscription that fails cancels the ones before it.
	on(t, dev, "monitor", "ai:1", "ai:9").want(t, 6)
	if dev.Subscriptions() != 0 {
		t.Errorf("%d subscriptions left after a failed monitor", dev.Subscriptions())
	}
	// --duration ends monitoring by itself.
	on(t, dev, "monitor", "ai:1", "--duration", "1").want(t, 0).errHas(t, "1 notification in 1.0 s")

	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"monitor", "ai:1"}, fast...)...).want(t, 5)

	for name, args := range map[string][]string{
		"object":                   {"nonsense"},
		"property":                 {"ai:1", "--property", "nonsense"},
		"two properties":           {"ai:1", "--property", "pv,units"},
		"increment":                {"ai:1", "--property", "pv", "--increment", "lots"},
		"negative increment":       {"ai:1", "--property", "pv", "--increment=-1"},
		"increment alone":          {"ai:1", "--increment", "1"},
		"increment and poll":       {"ai:1", "--property", "pv", "--increment", "1", "--poll", "100"},
		"confirmed and poll":       {"ai:1", "--confirmed", "--poll", "100"},
		"format":                   {"ai:1", "--format", "csv"},
		"no device":                nil,
		"device behind half a mac": {"ai:1", "--mac", "0a"},
	} {
		full := append([]string{"monitor", "--device", dev.Target()}, args...)
		if args == nil {
			full = []string{"monitor", "ai:1"}
		}
		if r := run(t, full...); r.code != 3 || r.stdout != "" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
}

// Without COV, a value is polled and printed when it changed.
func TestMonitorPoll(t *testing.T) {
	dev := bactest.Start(t)

	wait := start(t, "monitor", "--device", dev.Target(), "ai:1", "bv:1", "--poll", "20", "--count", "3", "--duration", "30", "--format", "json")
	dev.AwaitRequest(t, bactest.ReadPropertyMulti, 2)
	dev.SetPresentValue(ai1, bacnet.RealValue(30))
	r := wait().want(t, 0)
	lines := r.lines(t)
	if len(lines) != 3 || lines[0]["source"] != "poll" || lines[0]["object"] != "analog-input:1" || lines[1]["object"] != "binary-value:1" ||
		lines[2]["object"] != "analog-input:1" || lines[2]["values"].([]any)[0].(map[string]any)["value"].(map[string]any)["value"] != 30.0 {
		t.Errorf("records: %v", lines)
	}
	if _, has := lines[0]["time_remaining"]; has || dev.Count(bactest.SubscribeCOV) != 0 {
		t.Errorf("polling subscribed: %v", dev.Requests())
	}
	r.errHas(t, "3 changes in ")
	on(t, dev, "monitor", "msv:1", "--poll", "20", "--property", "state-text[2]", "--count", "1").want(t, 0).has(t, "multi-state-value:1", "state-text[2]=Auto")
	// A property that cannot be read is the error of the command.
	on(t, dev, "monitor", "ai:1", "--poll", "20", "--property", "high-limit").want(t, 6).errHas(t, "unknown-property")
	on(t, dev, "monitor", "ai:1", "--poll", "20", "--duration", "1").want(t, 0).errHas(t, "1 change in 1.0 s")
}

func TestAlarmsAndAck(t *testing.T) {
	dev := bactest.Start(t)

	on(t, dev, "alarms").want(t, 0).has(t, "1 active event at "+dev.Target()+" (get-event-information)", "OBJECT", "UNACKNOWLEDGED",
		"analog-input:2  high-limit  to-offnormal    alarm        2026-10-10 09:30:00.00 | sequence 0 | 2026-10-09 18:05:00.00")
	var res types.AlarmsResult
	on(t, dev, "alarms", "--format", "json").want(t, 0).decode(t, &res)
	if res.Service != "get-event-information" || res.Count != 1 {
		t.Fatalf("result: %+v", res)
	}
	a := res.Alarms[0]
	if a.Object != "analog-input:2" || a.EventState != "high-limit" || a.EventStateID != 3 || len(a.Unacknowledged) != 1 || a.Unacknowledged[0] != "to-offnormal" ||
		a.NotifyType != "alarm" || *a.NotifyTypeID != 0 || len(a.EventEnable) != 3 || len(a.TimeStamps) != 3 || a.TimeStamps[1] != "sequence 0" ||
		len(a.Priorities) != 3 || a.Priorities[0] != 100 {
		t.Errorf("alarm: %+v", a)
	}

	// The state and the time stamp of the event are read from the object.
	on(t, dev, "ack", "analog-input:2", "--source", "J. Smith").want(t, 0).
		has(t, `analog-input:2: high-limit transition of 2026-10-10 09:30:00.00 acknowledged as "J. Smith"`)
	on(t, dev, "alarms", "--format", "table").want(t, 0).has(t, "analog-input:2", "high-limit  -")
	// The return to normal is acknowledged with its own time stamp, given or read.
	var ack types.AckResult
	on(t, dev, "ack", "ai:2", "--state", "normal", "--event-time", "2026-10-09T18:05:00Z", "--format", "json").want(t, 0).decode(t, &ack)
	if ack.Object != "analog-input:2" || ack.State != "normal" || ack.StateID != 0 || ack.TimeStamp != "2026-10-09 18:05:00.00" || ack.Source != "bacnetctl" || ack.Status != "acknowledged" {
		t.Errorf("ack: %+v", ack)
	}
	on(t, dev, "ack", "ai:2", "--state", "fault", "--sequence", "0", "--format", "table").want(t, 0).has(t, "STATE", "fault", "sequence 0", "acknowledged")
	on(t, dev, "ack", "ai:2", "--state", "fault").want(t, 0).has(t, "fault transition of sequence 0")

	// A time stamp that is not the one of the event, and a state the object is not in.
	on(t, dev, "ack", "ai:2", "--sequence", "99").want(t, 6).errHas(t, "invalid-time-stamp")
	on(t, dev, "ack", "ai:2", "--state", "low-limit", "--sequence", "1").want(t, 6).errHas(t, "invalid-event-state")
	on(t, dev, "ack", "ai:1").want(t, 6).errHas(t, "unknown-property")
	on(t, dev, "ack", "ai:9").want(t, 6).errHas(t, "unknown-object")
	for name, args := range map[string][]string{
		"state":      {"ai:2", "--state", "alarming"},
		"both":       {"ai:2", "--sequence", "1", "--event-time", "2026-10-09T18:05:00Z"},
		"sequence":   {"ai:2", "--sequence", "x"},
		"event time": {"ai:2", "--event-time", "yesterday"},
		"source":     {"ai:2", "--source", " "},
		"object":     {"nonsense"},
	} {
		if r := run(t, append([]string{"ack", "--device", dev.Target()}, args...)...); r.code != 3 {
			t.Errorf("%s: exit code %d, stderr %q", name, r.code, r.stderr)
		}
	}
	run(t, "ack", "ai:2").want(t, 3)
	run(t, "alarms").want(t, 3)

	// A device without GetEventInformation is asked with GetAlarmSummary.
	old := bactest.Start(t, bactest.Without(bactest.GetEventInformation))
	on(t, old, "alarms").want(t, 0).has(t, "1 active event", "(get-alarm-summary)", "analog-input:2  high-limit  to-offnormal")
	res = types.AlarmsResult{}
	on(t, old, "alarms", "--format", "json").want(t, 0).decode(t, &res)
	if res.Service != "get-alarm-summary" || res.Count != 1 || res.Alarms[0].TimeStamps != nil || res.Alarms[0].NotifyTypeID != nil {
		t.Errorf("alarm summary: %+v", res)
	}
	neither := bactest.Start(t, bactest.Without(bactest.GetEventInformation, bactest.GetAlarmSummary))
	on(t, neither, "alarms").want(t, 6).errHas(t, "the device rejected the request: unrecognized-service (it does not implement this service)")
	if e := on(t, neither, "alarms", "--format", "json").want(t, 6).failure(t); e.Code != "reject_unrecognized_service" || e.BACnet.PDU != "reject" || *e.BACnet.ReasonID != 9 {
		t.Errorf("reject: %+v %+v", e, e.BACnet)
	}
	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"alarms"}, fast...)...).want(t, 5)
	// An acknowledgement that is not answered may have been carried out.
	mute := bactest.Start(t, bactest.Mute(bactest.AcknowledgeAlarm))
	on(t, mute, append([]string{"ack", "ai:2"}, fast...)...).want(t, 5).errHas(t, "outcome unknown: the AcknowledgeAlarm request was sent")
}

func TestEvents(t *testing.T) {
	dev := bactest.Start(t)
	port := freeUDPPort(t)
	to := netip.MustParseAddrPort("127.0.0.1:" + port)

	// The device notifies until the listener has printed what it was asked for.
	listen := func(args ...string) result {
		t.Helper()
		done := make(chan result, 1)
		go func() {
			done <- run(t, append([]string{"events", "--local-port", port, "--duration", "30"}, args...)...)
		}()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case r := <-done:
				return r
			case <-tick.C:
				if err := dev.SendEvent(to); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	r := listen("--count", "1").want(t, 0)
	r.has(t, "device:1234 analog-input:2  alarm out-of-range: normal -> high-limit  priority 100  event time 2026-10-10 09:30:00.00  ack required",
		`"Return air humidity high"`)
	r.errHas(t, "listening for event notifications on UDP port "+port, "1 event notification in ")

	rec := listen("--count", "2", "--format", "json").want(t, 0).lines(t)[1]
	for k, want := range map[string]any{
		"from": dev.Target(), "confirmed": false, "device": "device:1234", "object": "analog-input:2", "object_type": 0.0, "instance": 2.0,
		"event_type": "out-of-range", "event_type_id": 5.0, "notify_type": "alarm", "notify_type_id": 0.0, "from_state": "normal",
		"to_state": "high-limit", "to_state_id": 3.0, "priority": 100.0, "notification_class": 5.0, "ack_required": true,
		"message": "Return air humidity high", "event_time": "2026-10-10 09:30:00.00",
	} {
		if rec[k] != want {
			t.Errorf("%s: %v, want %v", k, rec[k], want)
		}
	}
	listen("--count", "1", "--format", "table").want(t, 0).has(t, "TIME", "TRANSITION", "analog-input:2", "normal -> high-limit", "Return air humidity high")
	// Nothing arrives: --duration ends listening.
	run(t, "events", "--local-port", "0", "--duration", "1").want(t, 0).silent(t).errHas(t, "0 event notifications in 1.0 s")
	run(t, "events", "--bbmd", "x y").want(t, 3)
	run(t, "events", "--format", "xml").want(t, 3)
}

func TestTrend(t *testing.T) {
	dev := bactest.Start(t)

	// The device returns three records per answer.
	on(t, dev, "trend", "trend-log:1").want(t, 0).has(t, "trend-log:1 at "+dev.Target()+": 3 records from position 1 (7 in the buffer), more follow",
		"POSITION  TIMESTAMP               KIND   VALUE  STATUS", "1         2026-10-10 10:00:00.00  value  20.75  ok", "3         2026-10-10 10:30:00.00  value  21.25")
	on(t, dev, "trend", "tl:1", "--start", "6", "--count", "2", "--format", "table").want(t, 0).
		has(t, "6         2026-10-10 11:15:00.00  value   21.75", "7         2026-10-10 11:30:00.00  status  {log-interrupted}")

	var res types.TrendResult
	on(t, dev, "trend", "tl:1", "--all", "--count", "2", "--format", "json").want(t, 0).decode(t, &res)
	if res.Object != "trend-log:1" || res.ObjectType != 20 || res.Start != 1 || res.Count != 7 || *res.Total != 7 || res.Partial {
		t.Fatalf("result: %+v", res)
	}
	first, last := res.Records[0], res.Records[6]
	if first.Position != 1 || first.Timestamp != "2026-10-10 10:00:00.00" || first.Kind != "value" || first.Value.Type != "real" || first.Value.Value != 20.75 ||
		first.StatusFlags == nil {
		t.Errorf("first record: %+v %+v", first, first.Value)
	}
	if last.Position != 7 || last.Kind != "status" || last.Value.Bits[0] != "log-interrupted" {
		t.Errorf("last record: %+v %+v", last, last.Value)
	}
	if n := dev.Count(bactest.ReadRange); n < 6 {
		t.Errorf("--all read the buffer in %d requests", n)
	}
	on(t, dev, "trend", "tl:1", "--start", "20").want(t, 0).has(t, "0 records from position 20")

	on(t, dev, "trend", "ai:1").want(t, 6).errHas(t, "property-is-not-a-list")
	on(t, dev, "trend", "tl:9").want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "trend", "tl:1", "--start", "0").want(t, 3)
	on(t, dev, "trend", "tl:1", "--count", "0").want(t, 3)
	on(t, dev, "trend", "nonsense").want(t, 3)
	run(t, "trend", "tl:1").want(t, 3)
	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"trend", "tl:1"}, fast...)...).want(t, 5)
	on(t, silent, append([]string{"trend", "tl:1", "--all"}, fast...)...).want(t, 5)
}

func TestFiles(t *testing.T) {
	dev := bactest.Start(t)
	dir := t.TempDir()
	content := string(dev.FileContent(1))

	// Fetch in small requests, into a file.
	out := filepath.Join(dir, "config.txt")
	var res types.FileResult
	on(t, dev, "file", "get", "file:1", "--output", out, "--chunk", "20", "--format", "json").want(t, 0).decode(t, &res)
	got, err := os.ReadFile(out)
	if err != nil || string(got) != content {
		t.Fatalf("downloaded %q, %v", got, err)
	}
	if res.Direction != "download" || res.Object != "file:1" || res.Length != len(content) || len(res.SHA256) != 64 || res.Path != out || res.DurationMS <= 0 {
		t.Errorf("result: %+v", res)
	}
	if n := dev.Count(bactest.AtomicReadFile); n != 5 {
		t.Errorf("98 octets in requests of 20 took %d requests", n)
	}
	on(t, dev, "file", "get", "file:1", "--output", out).want(t, 0).has(t, "file:1: 98 octets read to "+out, "sha256: "+res.SHA256)
	on(t, dev, "file", "get", "file:1", "--output", out, "--format", "table").want(t, 0).has(t, "DIRECTION", "download", "98")

	// To stdout: the content takes stdout, the result goes to stderr.
	r := on(t, dev, "file", "get", "file:1", "--output", "-").want(t, 0)
	if r.stdout != content || !strings.Contains(r.stderr, "98 octets read to stdout") {
		t.Errorf("stdout %q, stderr %q", r.stdout, r.stderr)
	}
	on(t, dev, "file", "get", "file:1", "--output", "-", "--format", "json").want(t, 0).has(t, content).errHas(t, `"direction": "download"`)

	// Send a file, then append to it.
	in := filepath.Join(dir, "new.txt")
	replacement := strings.Repeat("fan.minimum = 35\n", 20)
	if err := os.WriteFile(in, []byte(replacement), 0o600); err != nil {
		t.Fatal(err)
	}
	res = types.FileResult{}
	on(t, dev, "file", "put", "file:1", "--input", in, "--chunk", "100", "--format", "json").want(t, 0).decode(t, &res)
	if string(dev.FileContent(1)) != replacement || res.Direction != "upload" || res.Length != len(replacement) || res.Requests != 4 || res.Path != in {
		t.Errorf("uploaded %q, result %+v", dev.FileContent(1), res)
	}
	on(t, dev, "file", "put", "file:1", "--input", in, "--start", "340").want(t, 0).has(t, "file:1: 340 octets written from "+in)
	if got := dev.FileContent(1); len(got) != 680 || string(got) != replacement+replacement {
		t.Errorf("after appending: %d octets", len(got))
	}
	on(t, dev, "file", "put", "file:1", "--input", in, "--format", "table").want(t, 0).has(t, "upload", "340")

	// Refusals of the device, and of bacnetctl.
	on(t, dev, "file", "get", "file:9", "--output", out).want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "file", "put", "file:9", "--input", in).want(t, 6).errHas(t, "unknown-object", "(0 of 340 octets were acknowledged before that)")
	on(t, dev, "file", "put", "file:1", "--input", in, "--start", "5000").want(t, 6).errHas(t, "invalid-file-start-position")
	on(t, dev, "file", "get", "file:1", "--output", out, "--max-size", "100").want(t, 3).errHas(t, "larger than --max-size (100 octets)")
	if got, _ := os.ReadFile(out); string(got) != content {
		t.Error("a refused download overwrote the file")
	}
	on(t, dev, "file", "get", "file:1", "--output", out, "--max-size", "340").want(t, 0)
	on(t, dev, "file", "get", "file:1", "--output", filepath.Join(dir, "no", "such", "dir", "x")).want(t, 10).errHas(t, "cannot write")
	on(t, dev, "file", "put", "file:1", "--input", filepath.Join(dir, "missing")).want(t, 3).errHas(t, "cannot read")
	for name, args := range map[string][]string{
		"get without output": {"file", "get", "--device", dev.Target(), "file:1"},
		"get of no file":     {"file", "get", "--device", dev.Target(), "ai:1", "--output", out},
		"get chunk":          {"file", "get", "--device", dev.Target(), "file:1", "--output", out, "--chunk", "0"},
		"get large chunk":    {"file", "get", "--device", dev.Target(), "file:1", "--output", out, "--chunk", "5000"},
		"get max size":       {"file", "get", "--device", dev.Target(), "file:1", "--output", out, "--max-size", "0"},
		"get object":         {"file", "get", "--device", dev.Target(), "nonsense", "--output", out},
		"get format":         {"file", "get", "--device", dev.Target(), "file:1", "--output", "-", "--format", "xml"},
		"get stdout chunk":   {"file", "get", "--device", dev.Target(), "file:1", "--output", "-", "--chunk", "0"},
		"get no device":      {"file", "get", "file:1", "--output", out},
		"put without input":  {"file", "put", "--device", dev.Target(), "file:1"},
		"put chunk":          {"file", "put", "--device", dev.Target(), "file:1", "--input", in, "--chunk", "0"},
		"put start":          {"file", "put", "--device", dev.Target(), "file:1", "--input", in, "--start", "4000000000"},
	} {
		if r := run(t, args...); r.code != 3 || r.stdout != "" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"file", "get", "file:1", "--output", "-"}, fast...)...).want(t, 5).silent(t)
	// A write that is not acknowledged stops the transfer, and says how far it got.
	mute := bactest.Start(t, bactest.Mute(bactest.AtomicWriteFile))
	on(t, mute, append([]string{"file", "put", "file:1", "--input", in}, fast...)...).want(t, 5).
		errHas(t, "outcome unknown: the AtomicWriteFile request was sent", "(0 of 340 octets were acknowledged before that)")
}

func TestDeviceManagement(t *testing.T) {
	const secret = "hunter2-do-not-print"
	dev := bactest.Start(t, bactest.Password(secret))

	// Without the confirmation nothing is sent: a usage error.
	for _, args := range [][]string{{"reinitialize", "warmstart"}, {"comm-control", "disable"}} {
		r := on(t, dev, args...).want(t, 2).silent(t)
		r.errHas(t, "changes how the device runs: confirm with --yes-i-am-sure (nothing was sent)")
	}
	if e := on(t, dev, "reinitialize", "coldstart", "--format", "json").want(t, 2).failure(t); e.Kind != "usage" || e.Code != "confirmation_required" {
		t.Errorf("error: %+v", e)
	}
	if n := len(dev.Requests()); n != 0 {
		t.Errorf("%d requests were sent without confirmation", n)
	}

	// The wrong password, and none.
	on(t, dev, "reinitialize", "warmstart", "--yes-i-am-sure", "--password", "wrong").want(t, 6).errHas(t, "password-failure").lacks(t, "wrong\n")
	on(t, dev, "comm-control", "disable", "--yes-i-am-sure").want(t, 6).errHas(t, "password-failure (error class security)")
	if _, done := dev.Reinitialized(); done {
		t.Error("the device restarted with the wrong password")
	}

	// The password comes from the flag or from the environment, and is never printed.
	r := on(t, dev, "--debug", "reinitialize", "warmstart", "--yes-i-am-sure", "--password", secret).want(t, 0).
		has(t, dev.Target()+": reinitialize-device warmstart acknowledged by the device").lacks(t, secret)
	_ = r
	if state, _ := dev.Reinitialized(); state != service.ReinitializedWarmstart {
		t.Errorf("state: %v", state)
	}
	t.Setenv("BACNETCTL_PASSWORD", secret)
	for _, args := range [][]string{{"reinitialize", "--help"}, {"comm-control", "--help"}} {
		run(t, args...).want(t, 0).has(t, "[env: BACNETCTL_PASSWORD]", "--yes-i-am-sure").lacks(t, secret)
	}
	var res types.ManageResult
	on(t, dev, "--debug", "comm-control", "disable", "--duration", "10", "--yes-i-am-sure", "--format", "json").want(t, 0).lacks(t, secret).decode(t, &res)
	if res.Target != dev.Target() || res.Request != "device-communication-control" || res.State != "disable" || *res.DurationMinutes != 10 || res.Status != "acknowledged" {
		t.Errorf("result: %+v", res)
	}
	if state, _ := dev.CommControl(); state != service.EnableDisableDisable {
		t.Errorf("state: %v", state)
	}
	on(t, dev, "comm-control", "enable", "--yes-i-am-sure", "--format", "table").want(t, 0).has(t, "REQUEST", "enable", "acknowledged")
	on(t, dev, "comm-control", "Disable_Initiation", "--yes-i-am-sure").want(t, 0).has(t, "device-communication-control disable-initiation acknowledged")
	on(t, dev, "reinitialize", "activate-changes", "--yes-i-am-sure", "--format", "table").want(t, 0).has(t, "reinitialize-device", "activate-changes")
	// The flag wins over the environment.
	on(t, dev, "reinitialize", "coldstart", "--yes-i-am-sure", "--password", "other").want(t, 6)

	for name, args := range map[string][]string{
		"state":         {"reinitialize", "--device", dev.Target(), "--yes-i-am-sure", "reboot"},
		"comm state":    {"comm-control", "--device", dev.Target(), "--yes-i-am-sure", "mute"},
		"long password": {"reinitialize", "--device", dev.Target(), "--yes-i-am-sure", "warmstart", "--password", strings.Repeat("x", 21)},
		"enable for":    {"comm-control", "--device", dev.Target(), "--yes-i-am-sure", "enable", "--duration", "5"},
		"no device":     {"reinitialize", "--yes-i-am-sure", "warmstart", "--device", ""},
	} {
		if r := run(t, args...); r.code != 3 || r.stdout != "" {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	// A restart that is not acknowledged may well have happened.
	mute := bactest.Start(t, bactest.Mute(bactest.Reinitialize, bactest.CommControl))
	e := on(t, mute, append([]string{"reinitialize", "coldstart", "--yes-i-am-sure", "--format", "json"}, fast...)...).want(t, 5).failure(t)
	if _, done := mute.Reinitialized(); e.Code != "outcome_unknown" || !done {
		t.Errorf("error %+v, restarted %v", e, done)
	}
	on(t, mute, append([]string{"comm-control", "disable", "--yes-i-am-sure"}, fast...)...).want(t, 5).errHas(t, "outcome unknown")
}
