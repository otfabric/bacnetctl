// SPDX-License-Identifier: MIT

package cmd

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
)

func TestDiscover(t *testing.T) {
	dev := bactest.Start(t)
	base := []string{"discover", "--target", dev.Target(), "--local-port", "0"}

	run(t, base...).want(t, 0).has(t, "1 device found", "Who-Is to "+dev.Target(), "INSTANCE", "1234", "no-segmentation", "1476", "999")
	r := run(t, append(base, "--format", "table")...).want(t, 0).has(t, "INSTANCE", "SEGMENTATION", "1234")
	if strings.Contains(r.stdout, "found") {
		t.Errorf("the table carries the summary line:\n%s", r.stdout)
	}

	var res types.DiscoverResult
	run(t, append(base, "--format", "json", "--names")...).want(t, 0).decode(t, &res)
	if res.Mode != "unicast" || res.Target != dev.Target() || res.Count != 1 || len(res.Devices) != 1 {
		t.Fatalf("result: %+v", res)
	}
	d := res.Devices[0]
	if d.Instance != 1234 || d.Address != dev.Target() || d.Network != nil || d.VendorID != 999 || d.MaxAPDU != 1476 ||
		d.Segmentation != "no-segmentation" || d.SegmentationID != 3 || d.Name != "AHU-1 Controller" || d.VendorName != "OT Fabric" || d.ModelName != "bacsim" {
		t.Errorf("device: %+v", d)
	}
	run(t, append(base, "--names")...).want(t, 0).has(t, "VENDOR NAME", "AHU-1 Controller", "bacsim")

	// A range that leaves the device out, and one that holds it.
	var none types.DiscoverResult
	run(t, append(base, "--low", "1", "--high", "100", "--wait", "100", "--format", "json")...).want(t, 0).decode(t, &none)
	if none.Count != 0 || none.Devices == nil {
		t.Errorf("out of range: %+v", none)
	}
	run(t, append(base, "--low", "1", "--high", "100", "--wait", "100")...).want(t, 0).has(t, "0 devices found")
	run(t, append(base, "--low", "1000", "--high", "2000")...).want(t, 0).has(t, "1 device found")

	// A device that does not answer is no error: nothing was found.
	silent := bactest.Start(t, bactest.Silent())
	run(t, "discover", "--target", silent.Target(), "--local-port", "0", "--wait", "100").want(t, 0).has(t, "0 devices found")

	for name, args := range map[string][]string{
		"low without high": {"--low", "5"},
		"low above high":   {"--low", "9", "--high", "5"},
		"instance":         {"--low", "x", "--high", "5"},
		"high":             {"--low", "1", "--high", "99999999"},
		"network":          {"--network", "0"},
		"network name":     {"--network", "everything"},
		"target":           {"--target", "tcp://10.0.0.1"},
		"bbmd":             {"--bbmd", "[::1]:47808"},
		"bbmd ttl":         {"--bbmd", "10.0.0.1", "--bbmd-ttl", "1"},
		"interface":        {"--interface", "no-such-interface0"},
		"format":           {"--format", "xml"},
	} {
		run(t, append([]string{"discover"}, args...)...).want(t, 3).silent(t).errHas(t, "error: ")
		_ = name
	}
}

func TestDiscoverThroughRouterAndBBMD(t *testing.T) {
	dev := bactest.Start(t, bactest.Router())

	// Asked for all networks, the router passes on the answer of the device behind it.
	var res types.DiscoverResult
	run(t, "discover", "--target", dev.Target(), "--local-port", "0", "--network", "all", "--wait", "1000", "--names", "--format", "json").want(t, 0).decode(t, &res)
	if res.Count != 2 || res.Devices[0].Instance != 1234 || res.Devices[1].Instance != bactest.RoutedInstance {
		t.Fatalf("devices: %+v", res.Devices)
	}
	routed := res.Devices[1]
	if routed.Network == nil || *routed.Network != 2001 || routed.MAC != "0a" || routed.Address != dev.Target() || routed.Name != "Zone Controller 10" {
		t.Errorf("routed device: %+v", routed)
	}
	// Asked for one network, only that.
	run(t, "discover", "--target", dev.Target(), "--local-port", "0", "--network", "2001", "--wait", "1000").want(t, 0).
		has(t, "1 device found", "2001010", "2001", "0a").lacks(t, "1234 ")

	// As a foreign device the Who-Is goes through the BBMD.
	run(t, "discover", "--bbmd", dev.Target(), "--local-port", "0", "--wait", "1000").want(t, 0).has(t, "2 devices found", "broadcast Who-Is")
	if dev.Count(bactest.RegisterForeign) == 0 {
		t.Error("no foreign device registration was sent")
	}
	// The device that is no BBMD refuses the registration.
	plain := bactest.Start(t)
	r := run(t, "discover", "--bbmd", plain.Target(), "--local-port", "0", "--timeout", "200", "--retries", "0", "--format", "json").want(t, 4)
	if e := r.failure(t); e.Kind != "transport" || e.Code != "foreign_device_registration_failed" {
		t.Errorf("error: %+v", e)
	}
}

func TestWhoHas(t *testing.T) {
	dev := bactest.Start(t)
	base := []string{"whohas", "--target", dev.Target(), "--local-port", "0"}

	run(t, append(base, "--name", "Fan Enable")...).want(t, 0).has(t, `1 answer for name "Fan Enable"`, "binary-value:1", "1234", dev.Target())
	run(t, append(base, "--object", "ai:1", "--format", "table")...).want(t, 0).has(t, "DEVICE", "analog-input:1", "Supply Air Temp")

	var res types.WhoHasResult
	run(t, append(base, "--object", "analog-value:1", "--format", "json")...).want(t, 0).decode(t, &res)
	if res.Count != 1 || res.Query != "object analog-value:1" || res.Mode != "unicast" {
		t.Fatalf("result: %+v", res)
	}
	if o := res.Objects[0]; o.Object != "analog-value:1" || o.ObjectType != 2 || o.Instance != 1 || o.Name != "Supply Air Setpoint" || o.Device != 1234 {
		t.Errorf("object: %+v", o)
	}
	run(t, append(base, "--name", "No Such Point", "--wait", "100")...).want(t, 0).has(t, "0 answers")
	run(t, append(base, "--name", "Fan Enable", "--low", "1", "--high", "2", "--wait", "100")...).want(t, 0).has(t, "0 answers")

	run(t, "whohas").want(t, 3).errHas(t, "exactly one of --name and --object")
	run(t, "whohas", "--name", "x", "--object", "ai:1").want(t, 3)
	run(t, "whohas", "--object", "nonsense").want(t, 3)
	run(t, "whohas", "--name", "x", "--low", "5").want(t, 3)
	run(t, "whohas", "--name", "x", "--network", "x").want(t, 3)
	run(t, "whohas", "--name", "x", "--target", "a b").want(t, 3)
}

func TestRead(t *testing.T) {
	dev := bactest.Start(t)

	// One value alone, bare.
	if r := on(t, dev, "read", "analog-input:1", "present-value").want(t, 0); r.stdout != "21.5\n" || r.stderr != "" {
		t.Errorf("one value: %q, stderr %q", r.stdout, r.stderr)
	}
	if dev.Count(bactest.ReadProperty) != 1 || dev.Count(bactest.ReadPropertyMulti) != 0 {
		t.Errorf("one property is read with ReadProperty: %v", dev.Requests())
	}
	on(t, dev, "read", "ai:1", "object-name").want(t, 0).has(t, "Supply Air Temp\n")
	on(t, dev, "read", "0:1", "85").want(t, 0).has(t, "21.5\n")
	on(t, dev, "read", "bv:1", "pv").want(t, 0).has(t, "active (1)\n")
	on(t, dev, "read", "ai:2", "status-flags").want(t, 0).has(t, "{in-alarm}\n")
	on(t, dev, "read", "bi:1", "description").want(t, 0).has(t, "Filterüberwachung AHU-1\n")
	on(t, dev, "read", "device", "object-identifier").want(t, 0).has(t, "device:1234\n")
	on(t, dev, "read", "msv:1", "state-text").want(t, 0).has(t, "[Off, Auto, Manual]\n")
	on(t, dev, "read", "msv:1", "state-text[2]").want(t, 0).has(t, "Auto\n")
	on(t, dev, "read", "msv:1", "state-text", "--index", "0").want(t, 0).has(t, "3\n")
	on(t, dev, "read", "ai:1", "pv", "--format", "table").want(t, 0).has(t, "OBJECT", "PROPERTY", "VALUE", "analog-input:1", "present-value", "21.5")

	// Several properties, and several objects, in one request.
	before := dev.Count(bactest.ReadPropertyMulti)
	r := on(t, dev, "read", "ai:1", "pv,units,status-flags,object-name", "bv:1", "pv").want(t, 0).
		has(t, "analog-input:1  present-value  21.5 °C", "degrees-celsius (62)", "{}", "Supply Air Temp", "binary-value:1  present-value  active (1)")
	if dev.Count(bactest.ReadPropertyMulti) != before+1 || r.stderr != "" {
		t.Errorf("several properties are one ReadPropertyMultiple: %v; stderr %q", dev.Requests(), r.stderr)
	}

	var res types.ReadResult
	on(t, dev, "read", "ai:1", "pv,units,status-flags", "device", "object-name", "--format", "json").want(t, 0).decode(t, &res)
	if res.Target != dev.Target() || res.Service != "read-property-multiple" || res.Count != 4 || res.Failed != 0 {
		t.Fatalf("result: %+v", res)
	}
	pv := res.Rows[0]
	if pv.Object != "analog-input:1" || pv.ObjectType != 0 || pv.Instance != 1 || pv.Property != "present-value" || pv.PropertyID != 85 ||
		pv.Value.Type != "real" || pv.Value.Value != 21.5 || pv.Value.Unit != "°C" {
		t.Errorf("present value: %+v %+v", pv, pv.Value)
	}
	if u := res.Rows[1].Value; u.Type != "enumerated" || u.Value != 62.0 || u.Name != "degrees-celsius" {
		t.Errorf("units: %+v", u)
	}
	if f := res.Rows[2].Value; f.Type != "bit-string" || f.Value != "0000" || f.Bits == nil || len(f.Bits) != 0 {
		t.Errorf("status flags: %+v", f)
	}
	// The wildcard device comes back as the device it is.
	if n := res.Rows[3]; n.Object != "device:1234" || n.Instance != 1234 || n.Value.Value != "AHU-1 Controller" {
		t.Errorf("device name: %+v", n)
	}

	// A property the device does not have fails its row, not the others: exit code 7.
	res = types.ReadResult{}
	on(t, dev, "read", "ai:1", "pv,units[3],high-limit", "ai:9", "pv", "--format", "json").want(t, 7).decode(t, &res)
	if res.Count != 4 || res.Failed != 3 || res.Rows[0].Error != nil {
		t.Fatalf("partial: %+v", res)
	}
	for i, want := range []string{"property_is_not_an_array", "unknown_property", "unknown_object"} {
		e := res.Rows[i+1].Error
		if e == nil || e.Kind != "device_error" || e.Code != want || e.BACnet == nil || e.BACnet.PDU != "error" || res.Rows[i+1].Value != nil {
			t.Errorf("row %d: %+v", i+1, e)
		}
	}
	if e := res.Rows[2].Error; e.BACnet.Class != "property" || *e.BACnet.ClassID != 2 || e.BACnet.Code != "unknown-property" || *e.BACnet.CodeID != 32 || e.Message != "unknown-property" {
		t.Errorf("error detail: %+v %+v", e, e.BACnet)
	}
	if i := res.Rows[1].Index; i == nil || *i != 3 {
		t.Errorf("array index: %v", i)
	}
	on(t, dev, "read", "ai:1", "pv,high-limit").want(t, 7).has(t, "analog-input:1  high-limit     error: unknown-property")

	// The special properties.
	res = types.ReadResult{}
	on(t, dev, "read", "ai:1", "all", "--format", "json").want(t, 0).decode(t, &res)
	if res.Count != 10 || res.Rows[0].Property != "object-identifier" || res.Rows[0].Value.Value != "analog-input:1" || *res.Rows[0].Value.ObjectType != 0 {
		t.Errorf("all: %d rows, %+v", res.Count, res.Rows[0])
	}
	on(t, dev, "read", "ai:1", "required").want(t, 0).has(t, "present-value", "units").lacks(t, "description")
	on(t, dev, "read", "ai:1", "optional").want(t, 0).has(t, "description", "cov-increment").lacks(t, "present-value")
}

func TestReadErrors(t *testing.T) {
	dev := bactest.Start(t)

	// What the device refuses: exit code 6, with the error of the device.
	on(t, dev, "read", "ai:9", "pv").want(t, 6).silent(t).errHas(t, "error: the device refused the request: unknown-object (error class object)\n")
	e := on(t, dev, "read", "ai:9", "pv", "--format", "json").want(t, 6).failure(t)
	if e.Kind != "device_error" || e.Code != "unknown_object" || e.BACnet == nil || e.BACnet.PDU != "error" || e.BACnet.Class != "object" ||
		*e.BACnet.ClassID != 1 || e.BACnet.Code != "unknown-object" || *e.BACnet.CodeID != 31 {
		t.Errorf("error object: %+v %+v", e, e.BACnet)
	}
	on(t, dev, "read", "ai:1", "high-limit").want(t, 6).errHas(t, "unknown-property")
	on(t, dev, "read", "ai:1", "pv[2]").want(t, 6).errHas(t, "property-is-not-an-array")
	on(t, dev, "read", "msv:1", "state-text[9]").want(t, 6).errHas(t, "invalid-array-index")
	// With --debug the causes follow.
	on(t, dev, "--debug", "read", "ai:9", "pv").want(t, 6).errHas(t, "debug: BACnet/IP socket bound to", "cause: bacnet: error invoke=")

	// A device that does not answer: exit code 5.
	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"read", "ai:1", "pv"}, fast...)...).want(t, 5).silent(t).errHas(t, "error: timeout: no answer from the device\n")
	if e := on(t, silent, append([]string{"read", "ai:1", "pv,units", "--format", "json"}, fast...)...).want(t, 5).failure(t); e.Kind != "timeout" || e.Code != "request_timeout" || e.BACnet != nil {
		t.Errorf("timeout: %+v", e)
	}

	// What is wrong with the invocation: exit code 3, and nothing was sent.
	before := len(dev.Requests())
	for name, args := range map[string][]string{
		"no device":       {"read", "ai:1", "pv"},
		"odd arguments":   {"read", "--device", dev.Target(), "ai:1", "pv", "av:1"},
		"object":          {"read", "--device", dev.Target(), "nonsense:1", "pv"},
		"object form":     {"read", "--device", dev.Target(), "ai", "pv"},
		"instance":        {"read", "--device", dev.Target(), "ai:99999999", "pv"},
		"property":        {"read", "--device", dev.Target(), "ai:1", "nonsense"},
		"empty property":  {"read", "--device", dev.Target(), "ai:1", ","},
		"index":           {"read", "--device", dev.Target(), "ai:1", "pv", "--index", "x"},
		"index twice":     {"read", "--device", dev.Target(), "ai:1", "pv[1]", "--index", "2"},
		"index of all":    {"read", "--device", dev.Target(), "ai:1", "all", "--index", "2"},
		"bracket":         {"read", "--device", dev.Target(), "ai:1", "pv[1"},
		"address":         {"read", "--device", "http://x", "ai:1", "pv"},
		"ipv6":            {"read", "--device", "[fd00::1]:47808", "ai:1", "pv"},
		"port":            {"read", "--device", "10.0.0.1:0", "ai:1", "pv"},
		"mac alone":       {"read", "--device", dev.Target(), "--mac", "0a", "ai:1", "pv"},
		"network alone":   {"read", "--device", dev.Target(), "--network", "5", "ai:1", "pv"},
		"mac":             {"read", "--device", dev.Target(), "--network", "5", "--mac", "zz", "ai:1", "pv"},
		"long mac":        {"read", "--device", dev.Target(), "--network", "5", "--mac", "0102030405060708", "ai:1", "pv"},
		"network":         {"read", "--device", dev.Target(), "--network", "65535", "--mac", "0a", "ai:1", "pv"},
		"format":          {"read", "--device", dev.Target(), "ai:1", "pv", "--format", "yaml"},
		"describe object": {"describe", "--device", dev.Target(), "nonsense"},
		"objects type":    {"objects", "--device", dev.Target(), "--type", "nonsense"},
		"device":          {"device"},
	} {
		r := run(t, args...)
		if r.code != 3 || r.stdout != "" || !strings.HasPrefix(r.stderr, "error: ") {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	if e := run(t, "read", "--device", "x y", "ai:1", "pv", "--format", "json").want(t, 3).failure(t); e.Kind != "invalid_input" || e.Code != "invalid_input" {
		t.Errorf("invalid input: %+v", e)
	}
	if len(dev.Requests()) != before {
		t.Errorf("an invalid invocation sent requests: %v", dev.Requests()[before:])
	}
	// A host name that does not resolve is a transport error.
	if e := run(t, "read", "--device", "no-such-host.invalid", "ai:1", "pv", "--format", "json").want(t, 4).failure(t); e.Code != "transport_resolve_failed" {
		t.Errorf("unresolved host: %+v", e)
	}
	// A host name that does resolve works.
	run(t, "read", "--device", "localhost:"+strings.Split(dev.Target(), ":")[1], "ai:1", "pv").want(t, 0).has(t, "21.5")
}

func TestPortInUse(t *testing.T) {
	// Another program holds the port bacnetctl is told to bind.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	port := strconv.Itoa(conn.LocalAddr().(*net.UDPAddr).Port)

	run(t, "discover", "--local-port", port).want(t, 4).silent(t).
		errHas(t, "error: UDP port "+port+" is in use: another BACnet program", "--local-port 0")
	e := run(t, "events", "--local-port", port, "--format", "json").want(t, 4).failure(t)
	if e.Kind != "transport" || e.Code != "transport_port_in_use" {
		t.Errorf("error: %+v", e)
	}
	dev := bactest.Start(t)
	on(t, dev, "read", "ai:1", "pv", "--local-port", port).want(t, 4)
	// With a free port the same read works.
	on(t, dev, "read", "ai:1", "pv", "--local-port", "0").want(t, 0)
}
