// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
)

func TestObjects(t *testing.T) {
	dev := bactest.Start(t)

	r := on(t, dev, "objects").want(t, 0).has(t, "device:1234 at "+dev.Target()+": 10 objects", "OBJECT", "NAME",
		"analog-input:1       Supply Air Temp", "multi-state-value:1  Operating Mode", "file:1               config.txt")
	if strings.Contains(r.stdout, "VALUE") || r.stderr != "" {
		t.Errorf("without --values:\n%s\nstderr: %s", r.stdout, r.stderr)
	}
	on(t, dev, "objects", "--values").want(t, 0).has(t,
		"OBJECT               NAME                 VALUE         STATUS    DESCRIPTION",
		"analog-input:1       Supply Air Temp      21.5 °C       ok        AHU-1 supply air temperature",
		"analog-input:2       Return Air Humidity  71.25 %RH     in-alarm  AHU-1 return air humidity",
		"analog-output:1      Heating Valve        0 %           ok",
		"binary-input:1       Filter Alarm         inactive (0)  ok        Filterüberwachung AHU-1",
		"multi-state-value:1  Operating Mode       2             ok",
		"trend-log:1          Supply Air Temp Log  -             -         15 minute log of analog-input:1",
	)
	r = on(t, dev, "objects", "--values", "--type", "ai,binary-value", "--format", "table").want(t, 0).
		has(t, "analog-input:1", "analog-input:2", "binary-value:1").lacks(t, "analog-output", "device:1234")
	if strings.Contains(r.stdout, "objects\n") {
		t.Errorf("the table carries the summary line:\n%s", r.stdout)
	}
	on(t, dev, "objects", "--limit", "3").want(t, 0).has(t, "10 objects, 3 listed", "analog-input:2").lacks(t, "analog-output:1")
	on(t, dev, "objects", "--type", "schedule").want(t, 0).has(t, "10 objects, 0 listed")

	var res types.ObjectsResult
	on(t, dev, "objects", "--values", "--format", "json").want(t, 0).decode(t, &res)
	if res.Target != dev.Target() || res.Device != "device:1234" || res.Total != 10 || res.Count != 10 || res.Failed != 0 {
		t.Fatalf("result: %+v", res)
	}
	ai := res.Objects[1]
	if ai.Object != "analog-input:1" || ai.ObjectType != 0 || ai.Instance != 1 || ai.Name != "Supply Air Temp" || ai.PresentValue.Type != "real" ||
		ai.PresentValue.Value != 21.5 || ai.PresentValue.Unit != "°C" || ai.Units != "degrees-celsius" || *ai.UnitsID != 62 ||
		ai.StatusFlags == nil || len(ai.StatusFlags) != 0 || ai.Description != "AHU-1 supply air temperature" || ai.Error != nil {
		t.Errorf("analog input: %+v %+v", ai, ai.PresentValue)
	}
	if alarm := res.Objects[2]; len(alarm.StatusFlags) != 1 || alarm.StatusFlags[0] != "in-alarm" {
		t.Errorf("status flags: %+v", alarm.StatusFlags)
	}
	// An object without a present value has none: that is no failure.
	if tl := res.Objects[8]; tl.Object != "trend-log:1" || tl.PresentValue != nil || tl.StatusFlags != nil || tl.Units != "" {
		t.Errorf("trend log: %+v", tl)
	}
	if bv := res.Objects[6]; bv.PresentValue.Name != "active" || bv.PresentValue.Value != 1.0 {
		t.Errorf("binary value: %+v", bv.PresentValue)
	}
}

func TestDescribe(t *testing.T) {
	dev := bactest.Start(t)

	on(t, dev, "describe", "analog-input:2").want(t, 0).has(t, "analog-input:2 at "+dev.Target()+" (14 properties)",
		"object-identifier:  analog-input:2", "object-type:        analog-input (0)", "present-value:      71.25 %RH",
		"status-flags:       {in-alarm}", "event-state:        high-limit (3)", "out-of-service:     false",
		"units:              percent-relative-humidity (29)", "event-enable:       {to-offnormal, to-fault, to-normal}",
		"acked-transitions:  {to-fault, to-normal}", "event-time-stamps:  [2026-10-10 09:30:00.00, sequence 0, 2026-10-09 18:05:00.00]")
	on(t, dev, "describe", "av:1", "--format", "table").want(t, 0).has(t, "PROPERTY", "ID", "TYPE", "VALUE",
		"present-value", "85", "real", "priority-array", "87", "list", "relinquish-default")
	// "device" alone is the device at the address.
	on(t, dev, "describe", "device").want(t, 0).has(t, "device:1234 at ", "vendor-name:", "OT Fabric",
		"segmentation-supported:", "no-segmentation (3)", "{acknowledge-alarm, subscribe-cov,", "object-list:", "analog-input:1, analog-input:2")

	var res types.DescribeResult
	on(t, dev, "describe", "msv:1", "--format", "json").want(t, 0).decode(t, &res)
	if res.Object != "multi-state-value:1" || res.ObjectType != 19 || res.Name != "Operating Mode" || res.Count != 10 || res.Failed != 0 {
		t.Fatalf("result: %+v", res)
	}
	var text *types.PropertyRow
	for i, p := range res.Properties {
		if p.Property == "state-text" {
			text = &res.Properties[i]
		}
	}
	if text == nil || text.PropertyID != 110 || text.Value.Type != "list" {
		t.Fatalf("state-text: %+v", text)
	}
	items, _ := text.Value.Value.([]any)
	if len(items) != 3 || items[1].(map[string]any)["value"] != "Auto" || items[1].(map[string]any)["type"] != "character-string" {
		t.Errorf("state-text items: %+v", text.Value.Value)
	}

	on(t, dev, "describe", "ai:9").want(t, 6).silent(t).errHas(t, "unknown-object")
	// The log buffer of a trend log cannot be read as a property: its row fails.
	on(t, dev, "describe", "trend-log:1").want(t, 7).has(t, "log-buffer:", "error: read-access-denied", "record-count:       7")
}

func TestDevice(t *testing.T) {
	dev := bactest.Start(t)

	r := on(t, dev, "device").want(t, 0).has(t,
		"Device:                device:1234", "Address:               "+dev.Target(), "Name:                  AHU-1 Controller",
		"Location:              Plant room 1", "Vendor:                OT Fabric", "Vendor ID:             999",
		"Model:                 bacsim", "Firmware:              1.4.2", "System status:         operational (0)",
		"Protocol revision:     22", "Max APDU:              1476", "Segmentation:          no-segmentation (3)",
		"APDU timeout:          3000 ms", "Local date:            2026-10-10", "Local time:            12:00:00.00",
		"UTC offset:            -60 min", "Objects:               10",
		"Services supported (19):", "read-property-multiple", "Object types supported (9):", "multi-state-value, trend-log")
	if strings.Contains(r.stdout, "Serial number") || strings.Contains(r.stdout, "Max segments") {
		t.Errorf("properties the device does not have are shown:\n%s", r.stdout)
	}
	on(t, dev, "device", "--format", "table").want(t, 0).has(t, "PROPERTY", "VALUE", "Device", "device:1234", "Services", "who-is")

	var res types.DeviceResult
	on(t, dev, "device", "--format", "json").want(t, 0).decode(t, &res)
	if res.Object != "device:1234" || res.Instance != 1234 || res.ObjectType != 8 || res.Name != "AHU-1 Controller" || *res.VendorID != 999 ||
		res.SystemStatus != "operational" || *res.SystemStatusID != 0 || *res.ProtocolRevision != 22 || *res.MaxAPDU != 1476 ||
		res.Segmentation != "no-segmentation" || *res.SegmentationID != 3 || *res.ObjectCount != 10 || *res.UTCOffsetMinutes != -60 ||
		res.SerialNumber != "" || res.MaxSegments != nil || len(res.ServicesSupported) != 19 || len(res.ObjectTypesSupported) != 9 {
		t.Errorf("result: %+v", res)
	}
	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"device"}, fast...)...).want(t, 5)
	on(t, silent, append([]string{"objects"}, fast...)...).want(t, 5)
	on(t, silent, append([]string{"describe", "ai:1"}, fast...)...).want(t, 5)
}

// Not every device knows itself as device:4194303. The commands that need the
// device instance then ask with a Who-Is and take it from the I-Am.
func TestDeviceWithoutWildcardInstance(t *testing.T) {
	dev := bactest.Start(t, bactest.NoWildcard())
	on(t, dev, "device").want(t, 0).has(t, "Device:                device:1234", "Name:                  AHU-1 Controller")
	on(t, dev, "read", "device", "object-name").want(t, 0).has(t, "AHU-1 Controller")
	on(t, dev, "objects").want(t, 0).has(t, "analog-input:1")
}

// A device behind a BACnet router is reached through the router.
func TestRoutedDevice(t *testing.T) {
	dev := bactest.Start(t, bactest.Router())
	routed := []string{"--network", "2001", "--mac", "0a"}

	on(t, dev, append([]string{"read", "device", "object-name"}, routed...)...).want(t, 0).has(t, "Zone Controller 10\n")
	on(t, dev, "read", "device", "object-name").want(t, 0).has(t, "AHU-1 Controller\n")
	on(t, dev, append([]string{"device"}, routed...)...).want(t, 0).has(t, "device:2001010", dev.Target()+" net 2001 mac 0a")
	on(t, dev, append([]string{"write", "av:1", "pv", "real:25"}, routed...)...).want(t, 0)
	on(t, dev, append([]string{"read", "av:1", "pv"}, routed...)...).want(t, 0).has(t, "25\n")
	// The device at the router's own address is another one.
	on(t, dev, "read", "av:1", "pv").want(t, 0).has(t, "20\n")
	on(t, dev, append([]string{"objects", "--type", "av"}, routed...)...).want(t, 0).has(t, "device:2001010 at", "analog-value:1")
	// No such station behind the router: nobody answers.
	on(t, dev, append([]string{"read", "device", "object-name", "--network", "2001", "--mac", "0b"}, fast...)...).want(t, 5)

	var res types.RoutersResult
	run(t, "router", "list", "--target", dev.Target(), "--local-port", "0", "--format", "json").want(t, 0).decode(t, &res)
	if res.Mode != "unicast" || res.Count != 1 || res.Routers[0].Network != 2001 || res.Routers[0].Router != dev.Target() || res.Routers[0].State != "available" {
		t.Errorf("routes: %+v", res)
	}
	run(t, "router", "list", "--target", dev.Target(), "--local-port", "0").want(t, 0).has(t, "1 route found", "NETWORK", "2001", dev.Target(), "available")
	run(t, "router", "list", "--target", dev.Target(), "--local-port", "0", "--network", "2001", "--format", "table").want(t, 0).has(t, "ROUTER", "2001")
	// It does not route to network 7, and a plain device routes nowhere.
	run(t, "router", "list", "--target", dev.Target(), "--local-port", "0", "--network", "7", "--wait", "100").want(t, 0).has(t, "0 routes found")
	plain := bactest.Start(t)
	run(t, "router", "list", "--target", plain.Target(), "--local-port", "0", "--wait", "100").want(t, 0).has(t, "0 routes found")
	run(t, "router", "list", "--network", "65535").want(t, 3)
	run(t, "router", "list", "--target", "a b").want(t, 3)
	run(t, "router", "list", "--interface", "no-such-interface0").want(t, 3)
}

func TestBBMDTables(t *testing.T) {
	dev := bactest.Start(t, bactest.Router())

	on(t, dev, "bbmd", "bdt").want(t, 0).has(t, "broadcast distribution table of "+dev.Target()+": 2 entries",
		"ADDRESS", "MASK", "255.255.255.255", "10.0.20.1:47808  255.255.255.0")
	on(t, dev, "bbmd", "fdt").want(t, 0).has(t, "foreign device table of "+dev.Target()+": 0 entries")
	// A foreign device that registers shows up in the table.
	run(t, "discover", "--bbmd", dev.Target(), "--bbmd-ttl", "120", "--local-port", "0", "--wait", "100").want(t, 0)
	var res types.BBMDTableResult
	on(t, dev, "bbmd", "fdt", "--format", "json").want(t, 0).decode(t, &res)
	if res.BBMD != dev.Target() || res.Table != "fdt" || res.Count != 1 || *res.Entries[0].TTL != 120 || *res.Entries[0].Remaining != 150 ||
		!strings.HasPrefix(res.Entries[0].Address, "127.0.0.1:") {
		t.Errorf("foreign device table: %+v", res)
	}
	on(t, dev, "bbmd", "fdt", "--format", "table").want(t, 0).has(t, "TTL", "REMAINING", "120 s", "150 s")
	res = types.BBMDTableResult{}
	on(t, dev, "bbmd", "bdt", "--format", "json").want(t, 0).decode(t, &res)
	if res.Table != "bdt" || res.Count != 2 || res.Entries[1].Address != "10.0.20.1:47808" || res.Entries[1].Mask != "255.255.255.0" || res.Entries[1].TTL != nil {
		t.Errorf("broadcast distribution table: %+v", res)
	}

	// A device that is no BBMD refuses, and one that is not there does not answer.
	plain := bactest.Start(t)
	on(t, plain, "bbmd", "bdt").want(t, 6).errHas(t, "BVLC result 0x0020", "not a BBMD")
	if e := on(t, plain, "bbmd", "fdt", "--format", "json").want(t, 6).failure(t); e.Kind != "protocol" || e.Code != "bvlc_result" {
		t.Errorf("error: %+v", e)
	}
	silent := bactest.Start(t, bactest.Silent())
	on(t, silent, append([]string{"bbmd", "bdt"}, fast...)...).want(t, 5)
	run(t, "bbmd", "bdt").want(t, 3).errHas(t, "no BBMD given")
	run(t, "bbmd", "fdt", "--device", "a/b").want(t, 3)
	run(t, "bbmd", "fdt", "--device", "10.0.0.1", "--bbmd", "x y").want(t, 3)
}

// A device without ReadPropertyMultiple is read one property at a time.
func TestWithoutReadPropertyMultiple(t *testing.T) {
	dev := bactest.Start(t, bactest.Without(bactest.ReadPropertyMulti))

	on(t, dev, "read", "ai:1", "pv,units,high-limit").want(t, 7).has(t, "present-value  21.5 °C", "degrees-celsius (62)", "high-limit     error: unknown-property")
	if dev.Count(bactest.ReadProperty) != 3 {
		t.Errorf("requests: %v", dev.Requests())
	}
	on(t, dev, "describe", "ai:1").want(t, 0).has(t, "(10 properties)", "object-identifier:  analog-input:1", "cov-increment:      0.5", "present-value:      21.5 °C")
	on(t, dev, "objects", "--values", "--type", "ai,tl").want(t, 0).has(t, "Supply Air Temp      21.5 °C", "trend-log:1     Supply Air Temp Log  -")
	on(t, dev, "device").want(t, 0).has(t, "Name:                  AHU-1 Controller", "Objects:               10")
	// Which properties are required is something only ReadPropertyMultiple tells.
	on(t, dev, "read", "ai:1", "required").want(t, 7).has(t, "error:", "needs ReadPropertyMultiple")
	on(t, dev, "read", "ai:9", "all").want(t, 7).has(t, "all", "error: unknown-object")
	on(t, dev, "describe", "ai:9").want(t, 6).errHas(t, "unknown-object")
	run(t, "discover", "--target", dev.Target(), "--local-port", "0", "--names").want(t, 0).has(t, "AHU-1 Controller", "bacsim")
}

// A device with a small buffer and no segmentation aborts an answer that does not fit:
// the request is split until it does.
func TestSmallAPDU(t *testing.T) {
	dev := bactest.Start(t, bactest.MaxAPDU(60))

	on(t, dev, "objects", "--values").want(t, 0).has(t, "10 objects",
		"analog-input:1       Supply Air Temp      21.5 °C       ok        AHU-1 supply air temperature", "file:1               config.txt")
	on(t, dev, "describe", "ai:1").want(t, 0).has(t, "(10 properties)", "present-value:      21.5 °C", "description:        AHU-1 supply air temperature")
	on(t, dev, "device").want(t, 0).has(t, "Name:                  AHU-1 Controller", "Objects:               10", "Services supported (19)")
	on(t, dev, "read", "ai:1", "pv,units,description,object-name", "av:1", "pv,units,description,object-name").want(t, 0).has(t, "21.5 °C", "20 °C", "Supply Air Setpoint")
	// One property that does not fit by itself is the error of its row.
	on(t, dev, "describe", "device").want(t, 7).has(t, "object-list:", "error: the device aborted the request: segmentation-not-supported", "vendor-name:")
	e := on(t, dev, "read", "device", "object-list", "--format", "json").want(t, 6).failure(t)
	if e.Kind != "device_error" || e.Code != "abort_segmentation_not_supported" || e.BACnet.PDU != "abort" || e.BACnet.Reason != "segmentation-not-supported" || *e.BACnet.ReasonID != 4 {
		t.Errorf("abort: %+v %+v", e, e.BACnet)
	}
}
