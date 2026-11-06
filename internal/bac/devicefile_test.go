// SPDX-License-Identifier: MIT

package bac

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	bacnet "github.com/otfabric/go-bacnet"
)

// The device fixtures of bacnet-interop are device files. testdata holds copies of
// device-server-v1 and device-baseline-v2 (bacnet-interop 4a5b44c, MIT).
func TestDeviceFileFixtures(t *testing.T) {
	for file, want := range map[string]int{"device-server-v1.json": 8, "device-baseline-v2.json": 4} {
		df, err := LoadDeviceFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		dev, err := df.Build()
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if n := len(dev.ObjectIdentifiers()); n != want || df.DeviceInstance != 1234 || df.Port != 47808 {
			t.Errorf("%s: %d objects, %+v", file, n, df)
		}
	}
	df, _ := LoadDeviceFile(filepath.Join("testdata", "device-server-v1.json"))
	dev, _ := df.Build()
	// The trend log starts with its records, the files with their content, and analog
	// values can be created and deleted again.
	tl, ok := dev.Object(bacnet.ObjectIdentifier{Type: 20, Instance: 1})
	if !ok {
		t.Fatal("no trend-log:1")
	}
	if v, err := tl.ReadProperty(t.Context(), bacnet.PropertyReference{Identifier: bacnet.PropertyRecordCount}); err != nil || v.Unsigned != 10 {
		t.Errorf("record count: %+v %v", v, err)
	}
	created, err := dev.Create(bacnet.ObjectTypeAnalogValue, nil, "new")
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := created.(interface{ Deletable() bool }); !ok || !d.Deletable() {
		t.Error("a created object cannot be deleted")
	}
	if _, err := dev.Create(bacnet.ObjectTypeBinaryValue, nil, "bv"); err == nil {
		t.Error("a type that is not creatable was created")
	}
}

func TestDefaultDeviceFileRoundTrip(t *testing.T) {
	data, err := json.Marshal(DefaultDeviceFile())
	if err != nil {
		t.Fatal(err)
	}
	df, err := ParseDeviceFile(data)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := df.Build()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(dev.ObjectIdentifiers()); n != 6 || dev.Config().VendorID != DefaultVendorID {
		t.Errorf("%d objects, config %+v", n, dev.Config())
	}
	ai, _ := dev.Object(bacnet.ObjectIdentifier{Type: 0, Instance: 1})
	if v, _ := ai.ReadProperty(t.Context(), bacnet.PropertyReference{Identifier: bacnet.PropertyUnits}); v.Enumerated != 62 {
		t.Errorf("units of analog-input:1: %+v", v)
	}
}

// Every type a device file serves, with the values it takes.
func TestDeviceFileObjectTypes(t *testing.T) {
	file := `{"device_instance": 7, "device_name": "d", "objects": [
		{"type": "device", "instance": 7, "object_name": "d", "vendor_id": 42, "location": "lab", "serial_number": "s1",
		 "firmware_revision": "1", "application_software_version": "2", "vendor_name": "v", "model_name": "m"},
		{"type": "ai", "instance": 1, "object_name": "ai", "present_value": 1.5, "units": 62, "intrinsic_reporting":
		 {"notification_class": 1, "high_limit": 10, "low_limit": 0, "deadband": 1}},
		{"type": "analog-output", "instance": 1, "object_name": "ao", "units": "percent"},
		{"type": "analog-value", "instance": 1, "object_name": "av", "commandable": true, "units": "62"},
		{"type": "binary-input", "instance": 1, "object_name": "bi", "present_value": true},
		{"type": "binary-output", "instance": 1, "object_name": "bo", "present_value": 1},
		{"type": "binary-value", "instance": 1, "object_name": "bv", "present_value": "active", "active_text": "on", "inactive_text": "off"},
		{"type": "binary-value", "instance": 2, "object_name": "bv2", "present_value": null},
		{"type": "multi-state-input", "instance": 1, "object_name": "msi", "number_of_states": 4},
		{"type": "multi-state-output", "instance": 1, "object_name": "mso", "state_text": ["a", "b"], "present_value": 2},
		{"type": "msv", "instance": 1, "object_name": "msv", "number_of_states": 2, "state_text": ["a", "b"], "commandable": true},
		{"type": "characterstring-value", "instance": 1, "object_name": "csv", "present_value": "hi", "deletable": true},
		{"type": "integer-value", "instance": 1, "object_name": "iv", "present_value": -5},
		{"type": "positive-integer-value", "instance": 1, "object_name": "piv", "present_value": 5, "units": "no-units"},
		{"type": "notification-class", "instance": 1, "object_name": "nc"},
		{"type": "trend-log", "instance": 1, "object_name": "tl", "buffer_size": 5, "log_buffer": [{"timestamp": "2026-01-01T00:00:00Z", "real": 1}]},
		{"type": "trend-log", "instance": 2, "object_name": "tl2", "enable": false},
		{"type": "file", "instance": 1, "object_name": "f", "stream_data_base64": "aGk=", "read_only": true, "file_type": "text"},
		{"type": "file", "instance": 2, "object_name": "f2", "access_method": "record", "records": ["a"]}
	], "object_lifecycle": {"creatable_types": ["av", "binary-value", "file", "integer-value"],
		"precreated_deletable": [{"type": "ai", "instance": 1}], "protected_objects": [{"type": "device", "instance": 7}],
		"allocated_instance_range": {"minimum": 100, "maximum": 199}}}`
	df, err := ParseDeviceFile([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	dev, err := df.Build()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(dev.ObjectIdentifiers()); n != 19 {
		t.Errorf("%d objects", n)
	}
	if c := dev.Config(); c.VendorID != 42 || c.Location != "lab" || c.SerialNumber != "s1" || c.ModelName != "m" {
		t.Errorf("device: %+v", c)
	}
	for _, typ := range []bacnet.ObjectType{2, 5, 10, 45} {
		if _, err := dev.Create(typ, nil, "new-"+ObjectTypeName(typ)); err != nil {
			t.Errorf("create %s: %v", ObjectTypeName(typ), err)
		}
	}
	pv := func(typ bacnet.ObjectType, inst uint32) bacnet.ApplicationValue {
		t.Helper()
		o, ok := dev.Object(bacnet.ObjectIdentifier{Type: typ, Instance: inst})
		if !ok {
			t.Fatalf("no %d:%d", typ, inst)
		}
		v, err := o.ReadProperty(t.Context(), bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if pv(0, 1).Real != 1.5 || pv(3, 1).Enumerated != 1 || pv(4, 1).Enumerated != 1 || pv(5, 1).Enumerated != 1 ||
		pv(5, 2).Enumerated != 0 || pv(14, 1).Unsigned != 2 || pv(45, 1).Signed != -5 || pv(48, 1).Unsigned != 5 {
		t.Error("present values")
	}
}

func TestDeviceFileErrors(t *testing.T) {
	dev := `{"type": "device", "instance": 1, "object_name": "d"}`
	file := func(objects ...string) string {
		return `{"device_instance": 1, "device_name": "d", "objects": [` + strings.Join(append([]string{dev}, objects...), ",") + `]}`
	}
	for name, tc := range map[string]struct{ file, want string }{
		"not json":            {`{`, "unexpected end of JSON input"},
		"syntax":              {`{"objects": [}`, "not JSON"},
		"two documents":       {file() + "{}", "not JSON"},
		"unknown field":       {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "colour": 1}`, `unknown field "colour"`},
		"unknown type":        {file(`{"type": "toaster", "instance": 1, "object_name": "x"}`), `unknown object type "toaster"`},
		"no type":             {file(`{"instance": 1, "object_name": "x"}`), "no type"},
		"unservable":          {file(`{"type": "life-safety-point", "instance": 1, "object_name": "x"}`), "cannot be served"},
		"field of other type": {file(`{"type": "bv", "instance": 1, "object_name": "x", "units": 62}`), `field "units" does not apply to binary-value`},
		"commandable input":   {file(`{"type": "ai", "instance": 1, "object_name": "x", "commandable": true}`), `field "commandable" does not apply`},
		"no name":             {`{"device_instance": 1, "device_name": "", "objects": [` + dev + `]}`, "device_name is empty"},
		"no objects":          {`{"device_instance": 1, "device_name": "d", "objects": []}`, "no objects"},
		"device instance":     {`{"device_instance": 4194303, "device_name": "d", "objects": [` + dev + `]}`, "device_instance 4194303"},
		"device mismatch":     {`{"device_instance": 2, "device_name": "d", "objects": [` + dev + `]}`, "the device entry is device:1"},
		"two devices":         {file(dev), "more than one device entry"},
		"deletable device":    {`{"device_instance": 1, "device_name": "d", "objects": [{"type": "device", "instance": 1, "object_name": "d", "deletable": true}]}`, "cannot be deletable"},
		"object name":         {file(`{"type": "av", "instance": 1, "object_name": ""}`), "object_name is empty"},
		"object instance":     {file(`{"type": "av", "instance": 4194303, "object_name": "x"}`), "instance 4194303"},
		"duplicate":           {file(`{"type": "av", "instance": 1, "object_name": "x"}`, `{"type": "av", "instance": 1, "object_name": "y"}`), "analog-value:1"},
		"duplicate name":      {file(`{"type": "av", "instance": 1, "object_name": "x"}`, `{"type": "av", "instance": 2, "object_name": "x"}`), "analog-value:2"},
		"analog value":        {file(`{"type": "av", "instance": 1, "object_name": "x", "present_value": "warm"}`), "takes a number"},
		"analog too large":    {file(`{"type": "av", "instance": 1, "object_name": "x", "present_value": 1e40}`), "takes a number"},
		"units":               {file(`{"type": "av", "instance": 1, "object_name": "x", "units": "furlongs"}`), "units"},
		"units type":          {file(`{"type": "av", "instance": 1, "object_name": "x", "units": true}`), "units"},
		"cov increment":       {file(`{"type": "av", "instance": 1, "object_name": "x", "cov_increment": -1}`), "cov_increment"},
		"limits":              {file(`{"type": "av", "instance": 1, "object_name": "x", "intrinsic_reporting": {"notification_class": 1, "high_limit": 0, "low_limit": 5}}`), "low_limit 5 is above"},
		"deadband":            {file(`{"type": "av", "instance": 1, "object_name": "x", "intrinsic_reporting": {"notification_class": 1, "high_limit": 5, "low_limit": 0, "deadband": -1}}`), "deadband"},
		"binary value":        {file(`{"type": "bv", "instance": 1, "object_name": "x", "present_value": "on"}`), `takes "active" or "inactive"`},
		"states":              {file(`{"type": "msv", "instance": 1, "object_name": "x"}`), "needs number_of_states"},
		"state texts":         {file(`{"type": "msv", "instance": 1, "object_name": "x", "number_of_states": 3, "state_text": ["a"]}`), "state_text has 1 entries"},
		"state":               {file(`{"type": "msv", "instance": 1, "object_name": "x", "number_of_states": 3, "present_value": 4}`), "takes a state 1-3"},
		"text value":          {file(`{"type": "characterstring-value", "instance": 1, "object_name": "x", "present_value": 1}`), "takes a string"},
		"integer value":       {file(`{"type": "integer-value", "instance": 1, "object_name": "x", "present_value": 1.5}`), "integer of 32 bits"},
		"unsigned value":      {file(`{"type": "positive-integer-value", "instance": 1, "object_name": "x", "present_value": -1}`), "unsigned integer"},
		"disabled log":        {file(`{"type": "trend-log", "instance": 1, "object_name": "x", "enable": false, "log_buffer": [{"timestamp": "2026-01-01T00:00:00", "real": 1}]}`), "disabled trend log"},
		"log time":            {file(`{"type": "trend-log", "instance": 1, "object_name": "x", "log_buffer": [{"timestamp": "yesterday", "real": 1}]}`), `timestamp "yesterday"`},
		"access method":       {file(`{"type": "file", "instance": 1, "object_name": "x", "access_method": "random"}`), "access_method"},
		"stream records":      {file(`{"type": "file", "instance": 1, "object_name": "x", "records": ["a"]}`), "records are for access_method record"},
		"record stream":       {file(`{"type": "file", "instance": 1, "object_name": "x", "access_method": "record", "stream_data_base64": "aGk="}`), "stream_data_base64 is for"},
		"base64":              {file(`{"type": "file", "instance": 1, "object_name": "x", "stream_data_base64": "!!"}`), "stream_data_base64"},
		"creatable":           {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "object_lifecycle": {"creatable_types": ["trend-log"]}}`, "bacnetctl can create"},
		"creatable name":      {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "object_lifecycle": {"creatable_types": ["toaster"]}}`, "unknown object type"},
		"deletable missing":   {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "object_lifecycle": {"precreated_deletable": [{"type": "av", "instance": 9}]}}`, "analog-value:9 is not an object"},
		"deletable type":      {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "object_lifecycle": {"precreated_deletable": [{"type": "toaster", "instance": 9}]}}`, "unknown object type"},
		"precreated device":   {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "object_lifecycle": {"precreated_deletable": [{"type": "device", "instance": 1}]}}`, "Device object cannot be deleted"},
		"protected deletable": {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `, {"type": "av", "instance": 1, "object_name": "x", "deletable": true}], "object_lifecycle": {"protected_objects": [{"type": "av", "instance": 1}]}}`, "is also deletable"},
		"protected missing":   {`{"device_instance": 1, "device_name": "d", "objects": [` + dev + `], "object_lifecycle": {"protected_objects": [{"type": "av", "instance": 1}]}}`, "is not an object"},
	} {
		df, err := ParseDeviceFile([]byte(tc.file))
		if err == nil {
			_, err = df.Build()
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

func TestLoadDeviceFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadDeviceFile(filepath.Join(dir, "missing.json")); code(err) != errs.CodeInputFileOpenFailed {
		t.Errorf("missing file: %v", err)
	}
	if _, err := LoadDeviceFile(dir); code(err) != errs.CodeInputFileOpenFailed {
		t.Errorf("a directory: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"objects": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeviceFile(bad); code(err) != errs.CodeInvalidConfig || !strings.Contains(err.Error(), "invalid device file "+bad) {
		t.Errorf("bad file: %v", err)
	}
	big := filepath.Join(dir, "big.json")
	if err := os.WriteFile(big, make([]byte, maxDeviceFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeviceFile(big); code(err) != errs.CodeInvalidConfig || !strings.Contains(err.Error(), "larger than 16 MiB") {
		t.Errorf("big file: %v", err)
	}
}

func code(err error) string {
	if e, ok := errs.As(err); ok {
		return e.Code
	}
	return ""
}

func TestParseServer(t *testing.T) {
	base := func() config.ServerRunConfig {
		return config.ServerRunConfig{MaxAPDU: DefaultServerMaxAPDU, MaxSegments: DefaultServerMaxSegments}
	}
	req, err := ParseServer(&config.ServerRunConfig{MaxAPDU: 1476}, "")
	if err != nil || req.Source != BuiltInDevice || req.Port != 47808 || !req.Listen.IsUnspecified() || req.BBMD {
		t.Fatalf("defaults: %+v %v", req, err)
	}

	// Overrides of the identity: the built-in device is renamed with its instance.
	cfg := base()
	cfg.Instance, cfg.VendorID, cfg.Port, cfg.Listen = "77", "5", "0", "127.0.0.1"
	req, err = ParseServer(&cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	dev, _ := req.File.Build()
	if c := dev.Config(); c.Instance != 77 || c.Name != "bacnetctl-77" || c.VendorID != 5 || req.Port != 0 || req.Listen.String() != "127.0.0.1" {
		t.Errorf("overrides: %+v, %+v", c, req)
	}
	cfg.Name = "other"
	req, _ = ParseServer(&cfg, "")
	if dev, _ := req.File.Build(); dev.Config().Name != "other" {
		t.Errorf("name: %+v", dev.Config())
	}
	// A file without a device entry gets one for the vendor identifier.
	dir := t.TempDir()
	path := filepath.Join(dir, "d.json")
	_ = os.WriteFile(path, []byte(`{"device_instance": 3, "device_name": "x", "port": 47900, "objects": [{"type": "av", "instance": 1, "object_name": "a"}]}`), 0o600)
	cfg = base()
	cfg.DeviceFile, cfg.VendorID, cfg.Instance = path, "8", "4"
	req, err = ParseServer(&cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if dev, _ := req.File.Build(); dev.Config().VendorID != 8 || dev.Config().Instance != 4 || dev.Config().Name != "x" || req.Port != 47900 {
		t.Errorf("file overrides: %+v %+v", dev.Config(), req)
	}

	for name, tc := range map[string]struct {
		mod  func(*config.ServerRunConfig)
		pw   string
		want string
	}{
		"listen":         {func(c *config.ServerRunConfig) { c.Listen = "::1" }, "", "--listen"},
		"port":           {func(c *config.ServerRunConfig) { c.Port = "70000" }, "", "--port"},
		"interface":      {func(c *config.ServerRunConfig) { c.Interface = "nope0" }, "", "no such network interface"},
		"apdu":           {func(c *config.ServerRunConfig) { c.MaxAPDU = 49 }, "", "--max-apdu 49"},
		"segments":       {func(c *config.ServerRunConfig) { c.MaxSegments = 1 }, "", "--max-segments 1"},
		"password":       {func(*config.ServerRunConfig) {}, strings.Repeat("x", 21), "at most 20 octets"},
		"instance":       {func(c *config.ServerRunConfig) { c.Instance = "4194303" }, "", "--instance"},
		"vendor":         {func(c *config.ServerRunConfig) { c.VendorID = "65536" }, "", "--vendor-id"},
		"file":           {func(c *config.ServerRunConfig) { c.DeviceFile = filepath.Join(dir, "none.json") }, "", "cannot read"},
		"name":           {func(c *config.ServerRunConfig) { c.DeviceFile = path; c.Name = "a" }, "", "duplicate-name"},
		"bdt alone":      {func(c *config.ServerRunConfig) { c.BDT = []string{"10.0.0.1"} }, "", "--bdt needs --bbmd"},
		"address alone":  {func(c *config.ServerRunConfig) { c.BBMDAddress = "10.0.0.1" }, "", "--bbmd-address needs --bbmd"},
		"foreign alone":  {func(c *config.ServerRunConfig) { c.NoForeign = true }, "", "--no-foreign-devices needs --bbmd"},
		"bbmd self":      {func(c *config.ServerRunConfig) { c.BBMD = true }, "", "needs the BBMD's own address"},
		"bbmd address":   {func(c *config.ServerRunConfig) { c.BBMD, c.BBMDAddress = true, "0.0.0.0" }, "", "--bbmd-address"},
		"bbmd broadcast": {func(c *config.ServerRunConfig) { c.BBMD, c.BBMDAddress = true, "255.255.255.255" }, "", "--bbmd-address"},
		"bbmd address 6": {func(c *config.ServerRunConfig) { c.BBMD, c.BBMDAddress = true, "[::1]:47808" }, "", "--bbmd-address"},
		"bdt":            {func(c *config.ServerRunConfig) { c.BBMD, c.Listen, c.BDT = true, "127.0.0.1", []string{"peer"} }, "", "--bdt"},
		"bdt mask":       {func(c *config.ServerRunConfig) { c.BBMD, c.Listen, c.BDT = true, "127.0.0.1", []string{"10.0.0.1/24"} }, "", "--bdt"},
	} {
		cfg := base()
		tc.mod(&cfg)
		if err := CheckServer(&cfg, tc.pw); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}

	// A BBMD's own address: given, the listen address, or that of the interface.
	cfg = base()
	cfg.BBMD, cfg.BBMDAddress, cfg.BDT = true, "10.0.0.5:47809", []string{"10.0.0.1", "", "10.0.1.1:47810/255.255.255.0"}
	req, err = ParseServer(&cfg, "")
	if err != nil || req.Self.String() != "10.0.0.5" || req.SelfPort != 47809 || len(req.BDT) != 2 ||
		req.BDT[0].Endpoint.String() != "10.0.0.1:47808" || req.BDT[1].Mask.Addr().String() != "255.255.255.0" {
		t.Errorf("bbmd: %+v %v", req, err)
	}
	cfg = base()
	cfg.BBMD, cfg.Interface = true, loopbackInterface(t)
	if req, err := ParseServer(&cfg, ""); err != nil || !req.Self.IsLoopback() {
		t.Errorf("interface address: %+v %v", req, err)
	}
}

// loopbackInterface is the name of the loopback interface.
func loopbackInterface(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"lo0", "lo"} {
		if _, err := interfaceAddr(name); err == nil {
			return name
		}
	}
	t.Skip("no loopback interface with an IPv4 address")
	return ""
}

func TestParseUnits(t *testing.T) {
	for raw, want := range map[string]uint32{``: 95, `null`: 95, `62`: 62, `"degrees-celsius"`: 62, `"Degrees Celsius"`: 62, `"300"`: 300} {
		if got, err := parseUnits(json.RawMessage(raw), 95); err != nil || got != want {
			t.Errorf("%s: %d %v", raw, got, err)
		}
	}
	if _, err := parseUnits(json.RawMessage(`-1`), 95); err == nil {
		t.Error("negative units")
	}
}

func TestReinitStateName(t *testing.T) {
	if reinitStateName(1) != "warmstart" || reinitStateName(200) != "200" {
		t.Error("state names")
	}
}
