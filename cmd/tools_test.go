// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/types"
)

func TestDecode(t *testing.T) {
	// A Who-Is broadcast.
	run(t, "decode", "810b000c0120ffff00ff1008").want(t, 0).has(t,
		"BVLC     Original-Broadcast-NPDU", "NPDU     application message", "destination      global broadcast (network 65535)",
		"hop_count        255", "APDU     Unconfirmed-Request", "service  who-is (8)", "SERVICE  who-is", "range  all devices")
	// An I-Am, with separators and split over arguments.
	iam := []string{"81:0b:00:14", "0x01 0x00", "10-00-c4-02-00-04-d2-22-05-c4-91-03-21-63"}
	run(t, append([]string{"decode"}, iam...)...).want(t, 0).has(t, "SERVICE  i-am", "device        device:1234", "max_apdu      1476",
		"segmentation  no-segmentation", "vendor_id     99")
	run(t, append([]string{"decode", "--format", "table"}, iam...)...).want(t, 0).has(t, "LAYER", "FIELD", "VALUE", "bvlc", "service", "i-am", "device:1234")

	var res types.DecodeResult
	run(t, append([]string{"decode", "--format", "json"}, iam...)...).want(t, 0).decode(t, &res)
	if res.Length != 20 || len(res.Layers) != 4 || res.Error != "" || res.Layers[0].Layer != "bvlc" || res.Layers[3].Layer != "service" ||
		res.Layers[3].Summary != "i-am" || res.Layers[3].Fields[0].Name != "device" || res.Layers[3].Fields[0].Value != "device:1234" ||
		res.Layers[3].Fields[1].Value != 1476.0 {
		t.Errorf("result: %+v", res)
	}

	// From stdin.
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetArgs([]string{"decode", "-"})
	root.SetIn(strings.NewReader("81 0a 00 11 01 04 00 05 01 0c 0c 00 00 00 01 19 55\n"))
	root.SetOut(&out)
	if err := root.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), "SERVICE  read-property request") ||
		!strings.Contains(out.String(), "object    analog-input:1") || !strings.Contains(out.String(), "property  present-value (85)") {
		t.Errorf("decode from stdin: %v\n%s", err, out.String())
	}

	// A frame that stops making sense is decoded as far as it goes.
	res = types.DecodeResult{}
	run(t, "decode", "810a000b01040005010cff", "--format", "json").want(t, 0).decode(t, &res)
	if len(res.Layers) != 4 || len(res.Layers[3].Fields) != 0 || !strings.HasPrefix(res.Error, "service: ") {
		t.Errorf("truncated service: %+v", res)
	}
	run(t, "decode", "810a000b01040005010cff").want(t, 0).has(t, "APDU     Confirmed-Request", "error: service: ")
	run(t, "decode", "810a0006ff00").want(t, 0).has(t, "BVLC     Original-Unicast-NPDU", "error: NPDU: ")

	// What is no BACnet/IP at all.
	run(t, "decode", "0102030405").want(t, 3).silent(t).errHas(t, "not a BACnet/IP datagram")
	run(t, "decode", "810b00ff0120").want(t, 3).errHas(t, "BVLC")
	run(t, "decode", "zz").want(t, 3).errHas(t, "invalid hex")
	run(t, "decode", " ").want(t, 3).errHas(t, "no hex given")
	if e := run(t, "decode", "00", "--format", "json").want(t, 3).failure(t); e.Code != "input_decode_failed" {
		t.Errorf("error: %+v", e)
	}
	run(t, "decode", "81", "--format", "xml").want(t, 3)
}

func TestTypesAndProperties(t *testing.T) {
	run(t, "types").want(t, 0).has(t, "ID  NAME", "0   analog-input", "ai", "8   device", "56  network-port")
	run(t, "types", "--filter", "value", "--format", "table").want(t, 0).has(t, "analog-value", "multi-state-value").lacks(t, "analog-input")
	var list types.NameList
	run(t, "types", "--format", "json").want(t, 0).decode(t, &list)
	if list.What != "object-types" || list.Count < 60 || list.Names[0].ID != 0 || list.Names[0].Name != "analog-input" || list.Names[0].Alias != "ai" {
		t.Errorf("types: %d, %+v", list.Count, list.Names[0])
	}

	run(t, "properties").want(t, 0).has(t, "85   present-value", "pv", "371  property-list")
	run(t, "properties", "--filter", "limit").want(t, 0).has(t, "high-limit", "low-limit", "limit-enable").lacks(t, "present-value")
	list = types.NameList{}
	run(t, "properties", "--filter", "PV", "--format", "json").want(t, 0).decode(t, &list)
	if list.What != "properties" || list.Count != 1 || list.Names[0].ID != 85 {
		t.Errorf("properties filtered by alias: %+v", list)
	}
	run(t, "properties", "--filter", "no-such-thing", "--format", "json").want(t, 0).has(t, `"names": []`)
	run(t, "types", "--format", "xml").want(t, 3)
}
