// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
)

// A time a BACnet date cannot hold is refused before anything is sent: exit 3,
// invalid_input (docs/exitcodes.md).
func TestTimeSyncYearOutOfRange(t *testing.T) {
	dev := bactest.Start(t)
	for _, tm := range []string{
		"1899-12-31T23:59:59Z",
		"2155-01-01T00:00:00Z",
		// Local 2154, but 2155 in UTC.
		"2154-12-31T23:30:00-01:00",
		// Local 1900, but 1899 in UTC.
		"1900-01-01T00:30:00+01:00",
	} {
		for _, utc := range []string{"--utc=false", "--utc"} {
			r := on(t, dev, "time", "sync", "--time", tm, utc, "--format", "json").want(t, 3)
			if e := r.failure(t); e.Kind != "invalid_input" || e.Code != "invalid_input" || !strings.Contains(e.Message, "1900-2154") {
				t.Errorf("%s %s: error %+v", tm, utc, e)
			}
			on(t, dev, "time", "sync", "--time", tm, utc).want(t, 3).silent(t).errHas(t, "--time", "1900-2154")
		}
	}
	// The ends of the range are accepted.
	on(t, dev, "time", "sync", "--time", "2154-12-31T23:59:59Z").want(t, 0).has(t, "2154-12-31 23:59:59.00")
	on(t, dev, "time", "sync", "--utc", "--time", "1900-01-01T00:30:00-01:00").want(t, 0).has(t, "1900-01-01 01:30:00.00")
	dev.AwaitRequest(t, bactest.UTCTimeSync, 1)
	if n := dev.Count(bactest.TimeSync) + dev.Count(bactest.UTCTimeSync); n != 2 {
		t.Errorf("the device received %d time synchronizations, want the 2 in range", n)
	}
}

func TestAckEventTimeOutOfRange(t *testing.T) {
	dev := bactest.Start(t)
	for _, tm := range []string{"1899-12-31T23:59:59Z", "2155-01-01T00:00:00Z", "2155-01-01T00:30:00+01:00"} {
		r := on(t, dev, "ack", "ai:2", "--state", "normal", "--event-time", tm, "--format", "json").want(t, 3)
		if e := r.failure(t); e.Kind != "invalid_input" || e.Code != "invalid_input" || !strings.Contains(e.Message, "--event-time") {
			t.Errorf("%s: error %+v", tm, e)
		}
		on(t, dev, "ack", "ai:2", "--state", "normal", "--event-time", tm).want(t, 3).silent(t).errHas(t, "1900-2154")
	}
	if n := dev.Count(bactest.AcknowledgeAlarm); n != 0 {
		t.Errorf("%d acknowledgements were sent", n)
	}
}

func TestFilePutStartOutOfRange(t *testing.T) {
	dev := bactest.Start(t)
	in := filepath.Join(t.TempDir(), "in.txt")
	if err := os.WriteFile(in, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := on(t, dev, "file", "put", "file:1", "--input", in, "--start", "2147483648", "--format", "json").want(t, 3)
	if e := r.failure(t); e.Code != "invalid_input" {
		t.Errorf("error %+v", e)
	}
	// One octet at 2^31-1 would end past the last addressable octet: refused before sending.
	r = on(t, dev, "file", "put", "file:1", "--input", in, "--start", "2147483647", "--format", "json").want(t, 3)
	if e := r.failure(t); e.Code != "invalid_input" || !strings.Contains(e.Message, "ends past octet 2147483647") {
		t.Errorf("error %+v", e)
	}
	on(t, dev, "file", "put", "file:1", "--input", in, "--start", "2147483646").want(t, 6).errHas(t, "invalid-file-start-position")
	if n := dev.Count(bactest.AtomicWriteFile); n != 1 {
		t.Errorf("%d writes were sent, want the one in range", n)
	}
}

// file get creates its output file readable by the owner only; a file that is there keeps
// its mode.
func TestFileGetOutputMode(t *testing.T) {
	dev := bactest.Start(t)
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.txt")
	on(t, dev, "file", "get", "file:1", "--output", fresh).want(t, 0)
	got, err := os.ReadFile(fresh)
	if err != nil || string(got) != string(dev.FileContent(1)) {
		t.Fatalf("downloaded %q, %v", got, err)
	}
	existing := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(existing, []byte("old content that is longer than nothing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o644); err != nil {
		t.Fatal(err)
	}
	on(t, dev, "file", "get", "file:1", "--output", existing).want(t, 0)
	if got, err := os.ReadFile(existing); err != nil || string(got) != string(dev.FileContent(1)) {
		t.Fatalf("overwritten with %q, %v", got, err)
	}
	if runtime.GOOS == "windows" {
		return // Windows has no Unix permission bits
	}
	if fi, err := os.Stat(fresh); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("new output file: %v, %v; want -rw-------", fi.Mode(), err)
	}
	if fi, err := os.Stat(existing); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("existing output file: %v, %v; want it to keep -rw-r--r--", fi.Mode(), err)
	}
}

// ucs4 is a character string in ISO 10646 UCS-4 (character set 3) of raw code points.
func ucs4(points ...uint32) bacnet.ApplicationValue {
	raw := make([]byte, 0, 4*len(points))
	for _, p := range points {
		raw = append(raw, byte(p>>24), byte(p>>16), byte(p>>8), byte(p))
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Encoding: 3, Value: string(raw)}}
}

// A device that sends values wider than their fields, and strings with code points that
// do not exist, gets neither truncated values nor a crash.
func TestHostileDeviceValues(t *testing.T) {
	// 2^32 + 999: its low 32 bits are the vendor identifier of the simulated device.
	wide := bacnet.UnsignedValue(1<<32 + 999)
	dev := bactest.Start(t,
		bactest.DeviceProperty(bacnet.PropertyVendorIdentifier, wide),
		bactest.DeviceProperty(bacnet.PropertyMaxAPDULength, bacnet.UnsignedValue(1<<40|1476)),
		bactest.DeviceProperty(bacnet.PropertyProtocolRevision, bacnet.UnsignedValue(1<<63|22)),
		bactest.DeviceProperty(bacnet.PropertyDescription, ucs4('A', 0x110000, 0xD800, 0xFFFFFFFF, 'Z')),
		bactest.DeviceProperty(bacnet.PropertyObjectType, bacnet.EnumValue(65536+8)),
	)

	r := on(t, dev, "device").want(t, 0).has(t, "Name:                  AHU-1 Controller", "A\uFFFD\uFFFD\uFFFDZ")
	for _, field := range []string{"Vendor ID:", "Max APDU:", "Protocol revision:"} {
		if strings.Contains(r.stdout, field) {
			t.Errorf("a value of more than 32 bits is shown as %q:\n%s", field, r.stdout)
		}
	}

	var res types.DeviceResult
	r = on(t, dev, "device", "--format", "json").want(t, 0)
	r.decode(t, &res)
	if res.VendorID != nil || res.MaxAPDU != nil || res.ProtocolRevision != nil {
		t.Errorf("wide values in the result: vendor %v, max apdu %v, revision %v", res.VendorID, res.MaxAPDU, res.ProtocolRevision)
	}
	if res.Description != "A\uFFFD\uFFFD\uFFFDZ" || res.Name != "AHU-1 Controller" || res.APDUTimeoutMS == nil || *res.APDUTimeoutMS != 3000 {
		t.Errorf("result: %+v", res)
	}
	for _, key := range []string{`"vendor_id"`, `"max_apdu"`, `"protocol_revision"`} {
		if strings.Contains(r.stdout, key) {
			t.Errorf("%s is in the JSON output:\n%s", key, r.stdout)
		}
	}

	// read shows the value the device sent, all of it.
	on(t, dev, "read", "device", "vendor-identifier").want(t, 0).has(t, "4294968295").lacks(t, ": 999")
	// An object type beyond 16 bits has no name: it is not the device type of its low bits.
	r = on(t, dev, "read", "device", "object-type", "--format", "json").want(t, 0).has(t, "65544")
	if strings.Contains(r.stdout, `"device"`) {
		t.Errorf("object type 65544 named device:\n%s", r.stdout)
	}
}

// junkDevice answers every datagram with what reply makes of it, until the test ends.
func junkDevice(t *testing.T, reply func(req []byte) []byte) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = conn.Close()
		<-done
	})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			if out := reply(append([]byte(nil), buf[:n]...)); out != nil {
				_, _ = conn.WriteToUDPAddrPort(out, from)
			}
		}
	}()
	return conn.LocalAddr().String()
}

// invokeID returns the invoke ID and service of a confirmed request in a unicast frame
// without network addresses, as bacnetctl sends to --device.
func invokeID(req []byte) (id, service byte, ok bool) {
	if len(req) < 10 || req[0] != 0x81 || req[4] != 0x01 || req[5]&0x28 != 0 || req[6]>>4 != 0 {
		return 0, 0, false
	}
	return req[8], req[9], true
}

// frame is a BVLC Original-Unicast-NPDU without network addresses that carries apduBytes.
func frame(apduBytes ...byte) []byte {
	out := append([]byte{0x81, 0x0a, 0x00, 0x00, 0x01, 0x00}, apduBytes...)
	out[2], out[3] = byte(len(out)>>8), byte(len(out))
	return out
}

// A device that answers with junk, or not at all, ends the command with the documented
// exit code, within a bound.
func TestHostileDeviceAnswers(t *testing.T) {
	cases := []struct {
		name  string
		reply func([]byte) []byte
		code  int
		ecode string
	}{
		{"silence", func([]byte) []byte { return nil }, 5, "request_timeout"},
		{"not BACnet", func([]byte) []byte { return []byte("HTTP/1.1 400 Bad Request\r\n\r\n") }, 5, "request_timeout"},
		{"a BVLC header and nothing", func([]byte) []byte { return []byte{0x81, 0x0a, 0x00, 0x04} }, 5, "request_timeout"},
		{"a wrong BVLC length", func([]byte) []byte { return []byte{0x81, 0x0a, 0xff, 0xff, 0x01, 0x00, 0x30} }, 5, "request_timeout"},
		{"an NPDU of junk", func([]byte) []byte { return []byte{0x81, 0x0a, 0x00, 0x08, 0x07, 0xff, 0xff, 0xff} }, 5, "request_timeout"},
		{"a Complex-ACK of junk", func(req []byte) []byte {
			id, svc, ok := invokeID(req)
			if !ok {
				return nil
			}
			return frame(0x30, id, svc, 0x3e, 0xff, 0xff, 0x1f)
		}, 6, ""},
		{"a ReadProperty-ACK with a property identifier of five octets", func(req []byte) []byte {
			id, svc, ok := invokeID(req)
			if !ok || svc != 0x0c {
				return nil
			}
			// device:1234, property 2^32 + 79 (79 is object-type), value 8.
			return frame(0x30, id, svc, 0x0c, 0x02, 0x00, 0x04, 0xd2, 0x1d, 0x05, 0x01, 0x00, 0x00, 0x00, 0x4f, 0x3e, 0x91, 0x08, 0x3f)
		}, 6, ""},
		{"a ReadProperty-ACK cut short", func(req []byte) []byte {
			id, svc, ok := invokeID(req)
			if !ok || svc != 0x0c {
				return nil
			}
			return frame(0x30, id, svc, 0x0c, 0x02, 0x00, 0x04, 0xd2, 0x19, 0x4f, 0x3e, 0x25, 0x05, 0x01)
		}, 6, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr := junkDevice(t, c.reply)
			began := time.Now()
			r := run(t, append([]string{"read", "--device", addr, "device:1234", "object-type", "--format", "json"}, fast...)...).want(t, c.code)
			e := r.failure(t)
			if c.ecode != "" && e.Code != c.ecode {
				t.Errorf("error code %q, want %q: %+v", e.Code, c.ecode, e)
			}
			if c.code == 6 && e.Kind != "protocol" {
				t.Errorf("error kind %q, want protocol: %+v", e.Kind, e)
			}
			// --timeout 150 --retries 0: generous, for loaded runners under -race.
			if d := time.Since(began); d > 30*time.Second {
				t.Errorf("took %v", d)
			}
			run(t, append([]string{"device", "--device", addr}, fast...)...).want(t, c.code)
		})
	}
}
