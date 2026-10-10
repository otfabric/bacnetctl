// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
)

func TestWriteMultiple(t *testing.T) {
	dev := bactest.Start(t)

	// Typed values: one WritePropertyMultiple, nothing read.
	r := on(t, dev, "write", "av:1", "pv", "real:21", "av:1", "object-name", "string:New SP", "bv:1", "pv", "enum:0", "--priority", "8").want(t, 0)
	r.has(t, "OBJECT", "analog-value:1  present-value  21 ", "analog-value:1  object-name    New SP", "binary-value:1  present-value  inactive (0)",
		"written", "3 properties written (acknowledged by the device)")
	if dev.Count(bactest.WritePropertyMulti) != 1 || dev.Count(bactest.WriteProperty) != 0 || dev.Count(bactest.ReadProperty) != 0 {
		t.Errorf("requests: %v", dev.Requests())
	}
	if got := real(t, dev, av1); got != 21 {
		t.Errorf("analog value: %v", got)
	}
	if v, _ := dev.Value(av1, bacnet.PropertyObjectName); v.Character.Value != "New SP" {
		t.Errorf("name: %+v", v)
	}

	// Without a type each property is read first; null at a priority relinquishes.
	before := dev.Count(bactest.ReadProperty)
	var res types.WriteMultipleResult
	on(t, dev, "write", "av:1", "pv", "22.5", "bv:1", "pv", "active", "av:1", "pv", "null", "--priority", "9", "--format", "json").want(t, 0).decode(t, &res)
	if res.Target != dev.Target() || res.Service != "write-property-multiple" || res.Count != 3 || res.Failed != 0 || res.NotWritten != 0 || res.RTTMS <= 0 || len(res.Writes) != 3 {
		t.Fatalf("result: %+v", res)
	}
	if w := res.Writes[0]; w.Object != "analog-value:1" || w.Property != "present-value" || w.Value.Type != "real" || w.Value.Value != 22.5 ||
		w.Priority == nil || *w.Priority != 9 || w.Status != "written" || w.Error != nil {
		t.Errorf("first write: %+v", w)
	}
	if res.Writes[1].Value.Name != "active" || res.Writes[2].Status != "relinquished" {
		t.Errorf("writes: %+v", res.Writes)
	}
	if dev.Count(bactest.ReadProperty) != before+2 {
		t.Errorf("two untyped values are two reads: %v", dev.Requests())
	}
	// The table form of a refusal: here of the first write, so nothing is written.
	on(t, dev, "write", "av:1", "priority-array[7]", "real:19", "av:1", "description", "string:x", "--format", "table").want(t, 7).
		has(t, "priority-array[7]", "failed: write-access-denied", "not-written")

	// The device stops at the first write it refuses: exit code 7, and the rows say which.
	on(t, dev, "write", "av:1", "pv", "real:20", "--priority", "8").want(t, 0)
	res = types.WriteMultipleResult{}
	r = on(t, dev, "write", "av:1", "pv", "real:24", "ai:1", "units", "enum:3", "av:1", "description", "string:after", "--priority", "8", "--format", "json").want(t, 7)
	r.decode(t, &res)
	if res.Count != 3 || res.Failed != 1 || res.NotWritten != 1 {
		t.Fatalf("result: %+v", res)
	}
	if res.Writes[0].Status != "written" || res.Writes[1].Status != "failed" || res.Writes[2].Status != "not-written" || res.Writes[2].Error != nil {
		t.Errorf("statuses: %+v", res.Writes)
	}
	if e := res.Writes[1].Error; e == nil || e.Kind != "device_error" || e.Code != "write_access_denied" || e.BACnet == nil || e.BACnet.Class != "property" {
		t.Errorf("error of the refused write: %+v", e)
	}
	if got := real(t, dev, av1); got != 24 {
		t.Errorf("the write before the refusal stays: %v", got)
	}
	if v, _ := dev.Value(av1, bacnet.PropertyDescription); v.Character.Value == "after" {
		t.Error("a write after the refusal was carried out")
	}
	on(t, dev, "write", "av:1", "pv", "real:24", "ai:1", "units", "enum:3", "av:1", "description", "string:after", "--priority", "8").want(t, 7).
		has(t, "failed: write-access-denied", "not-written", "1 of 3 properties written: the device refused one, and did not attempt the 1 after it")

	// A refusal that is not about one write, and an untyped value that cannot be read.
	on(t, dev, "write", "av:9", "pv", "1", "av:1", "pv", "real:1").want(t, 6).silent(t).errHas(t, "unknown-object")
	on(t, dev, "write", "av:1", "pv", "warm", "bv:1", "pv", "enum:1").want(t, 3).errHas(t, "is not a number")

	// What is wrong with the invocation: nothing is sent.
	sent := len(dev.Requests())
	on(t, dev, "write", "av:1", "pv", "real:1", "bv:1", "pv").want(t, 2).errHas(t, "once or several times")
	on(t, dev, "write", "av:1", "pv", "real:1", "bv:1", "pv", "enum:1", "--index", "3").want(t, 3).errHas(t, "--index belongs to one write")
	on(t, dev, "write", "av:1", "pv", "real:1", "bv:1", "pv", "enum:red").want(t, 3)
	on(t, dev, "write", "av:1", "pv", "real:1", "nonsense", "pv", "enum:1").want(t, 3)
	if len(dev.Requests()) != sent {
		t.Errorf("a rejected invocation sent: %v", dev.Requests()[sent:])
	}
}

// A device without WritePropertyMultiple, and one that does not acknowledge it.
func TestWriteMultipleUnsupportedAndUnknownOutcome(t *testing.T) {
	old := bactest.Start(t, bactest.Without(bactest.WritePropertyMulti))
	on(t, old, "write", "av:1", "pv", "real:21", "bv:1", "pv", "enum:0").want(t, 6).silent(t).
		errHas(t, "the device rejected the request: unrecognized-service", "does not implement this service")
	if old.Count(bactest.WriteProperty) != 0 {
		t.Errorf("fell back to single writes: %v", old.Requests())
	}

	mute := bactest.Start(t, bactest.Mute(bactest.WritePropertyMulti))
	args := append([]string{"write", "av:1", "pv", "real:21", "bv:1", "pv", "enum:0", "--format", "json"}, fast...)
	if e := on(t, mute, args...).want(t, 5).failure(t); e.Kind != "timeout" || e.Code != "outcome_unknown" {
		t.Errorf("error: %+v", e)
	}
	if mute.Count(bactest.WritePropertyMulti) != 1 {
		t.Errorf("a write is sent exactly once: %v", mute.Requests())
	}
}

func TestCreateAndDelete(t *testing.T) {
	dev := bactest.Start(t)

	// The device chooses the instance.
	r := on(t, dev, "create", "analog-value", "--name", "Spare", "description=string:Operator offset", "present-value=real:5").want(t, 0)
	if r.stdout != "analog-value:2: created (acknowledged by the device)\n" || r.stderr != "" {
		t.Errorf("text: %q, stderr %q", r.stdout, r.stderr)
	}
	av2 := oid(bacnet.ObjectTypeAnalogValue, 2)
	if name, _ := dev.Value(av2, bacnet.PropertyObjectName); name.Character.Value != "Spare" || real(t, dev, av2) != 5 {
		t.Errorf("created object: name %+v", name)
	}
	on(t, dev, "objects", "--type", "av").want(t, 0).has(t, "analog-value:2", "Spare")

	// A given instance.
	var res types.ObjectChangeResult
	on(t, dev, "create", "bv:40", "--format", "json").want(t, 0).decode(t, &res)
	if res.Target != dev.Target() || res.Object != "binary-value:40" || res.ObjectType != 5 || res.Instance != 40 || res.Status != "created" || res.RTTMS <= 0 {
		t.Errorf("result: %+v", res)
	}
	on(t, dev, "create", "bv:41", "--format", "table").want(t, 0).has(t, "OBJECT", "STATUS", "binary-value:41", "created")

	// What the device refuses, and that a refused initial value leaves nothing behind.
	on(t, dev, "create", "bv:40").want(t, 6).silent(t).errHas(t, "object-identifier-already-exists")
	on(t, dev, "create", "analog-input").want(t, 6).errHas(t, "dynamic-creation-not-supported")
	if e := on(t, dev, "create", "av:50", "present-value=string:x", "--format", "json").want(t, 6).failure(t); e.Code != "invalid_data_type" || e.BACnet.Class != "property" {
		t.Errorf("error: %+v", e)
	}
	on(t, dev, "read", "av:50", "pv").want(t, 6).errHas(t, "unknown-object")

	// Delete needs its confirmation, and the device has the last word.
	sent := len(dev.Requests())
	if e := on(t, dev, "delete", "bv:40", "--format", "json").want(t, 2).failure(t); e.Kind != "usage" || e.Code != "confirmation_required" || !strings.Contains(e.Message, "binary-value:40") {
		t.Errorf("error: %+v", e)
	}
	if len(dev.Requests()) != sent {
		t.Errorf("an unconfirmed delete sent: %v", dev.Requests()[sent:])
	}
	r = on(t, dev, "delete", "bv:40", "--yes-i-am-sure").want(t, 0)
	if r.stdout != "binary-value:40: deleted (acknowledged by the device)\n" {
		t.Errorf("text: %q", r.stdout)
	}
	res = types.ObjectChangeResult{}
	on(t, dev, "delete", "bv:41", "--yes-i-am-sure", "--format", "json").want(t, 0).decode(t, &res)
	if res.Object != "binary-value:41" || res.Status != "deleted" {
		t.Errorf("result: %+v", res)
	}
	on(t, dev, "read", "bv:40", "pv").want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "delete", "bv:40", "--yes-i-am-sure").want(t, 6).errHas(t, "unknown-object")
	on(t, dev, "delete", "av:1", "--yes-i-am-sure").want(t, 6).errHas(t, "object-deletion-not-permitted")

	// What is wrong with the invocation: exit code 3, and nothing was sent.
	sent = len(dev.Requests())
	for name, args := range map[string][]string{
		"type":            {"create", "--device", dev.Target(), "nonsense"},
		"instance":        {"create", "--device", dev.Target(), "av:x"},
		"device":          {"create", "--device", dev.Target(), "device:5"},
		"no =":            {"create", "--device", dev.Target(), "av", "description"},
		"no type":         {"create", "--device", dev.Target(), "av", "description=Spare"},
		"bad value":       {"create", "--device", dev.Target(), "av", "present-value=real:warm"},
		"property":        {"create", "--device", dev.Target(), "av", "nonsense=real:1"},
		"special":         {"create", "--device", dev.Target(), "av", "all=real:1"},
		"twice":           {"create", "--device", dev.Target(), "av", "--name", "A", "object-name=string:B"},
		"no device":       {"create", "av"},
		"delete object":   {"delete", "--device", dev.Target(), "nonsense", "--yes-i-am-sure"},
		"delete device":   {"delete", "--device", dev.Target(), "device:1234", "--yes-i-am-sure"},
		"delete wildcard": {"delete", "--device", dev.Target(), "device", "--yes-i-am-sure"},
		"delete nowhere":  {"delete", "av:1", "--yes-i-am-sure"},
	} {
		r := run(t, args...)
		if r.code != 3 || r.stdout != "" || !strings.HasPrefix(r.stderr, "error: ") {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	on(t, dev, "create").want(t, 2)
	on(t, dev, "delete").want(t, 2)
	if len(dev.Requests()) != sent {
		t.Errorf("a rejected invocation sent: %v", dev.Requests()[sent:])
	}
}

// Neither is repeated when the answer does not come.
func TestCreateAndDeleteUnknownOutcome(t *testing.T) {
	dev := bactest.Start(t, bactest.Mute(bactest.CreateObject, bactest.DeleteObject))
	for _, args := range [][]string{{"create", "av:60"}, {"delete", "av:60", "--yes-i-am-sure"}} {
		args = append(append(args, "--format", "json"), fast...)
		if e := on(t, dev, args...).want(t, 5).failure(t); e.Kind != "timeout" || e.Code != "outcome_unknown" {
			t.Errorf("%v: %+v", args, e)
		}
	}
	if dev.Count(bactest.CreateObject) != 1 || dev.Count(bactest.DeleteObject) != 1 {
		t.Errorf("sent exactly once each: %v", dev.Requests())
	}
}
