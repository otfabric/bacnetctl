// SPDX-License-Identifier: MIT

package bac

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/bacnetctl/internal/errs"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/server/object"
)

// DefaultVendorID is the vendor identifier of a served device whose device file and flags
// give none: the value the examples and the interop server of go-bacnet use.
const DefaultVendorID = 999

// maxDeviceFile bounds what is read of a device file.
const maxDeviceFile = 16 << 20

// noUnits is BACnetEngineeringUnits no-units, the units of an analog object without any.
const noUnits = 95

// DeviceFile describes a device for `server run`. It is a superset of the device fixtures
// of bacnet-interop (fixtures/device, schema device.schema.json): such a file is served as
// it is, as far as it uses object types bacnetctl can serve.
type DeviceFile struct {
	// Fixture, Description, Notes and Alignment document a bacnet-interop fixture; they are
	// accepted and not served. The description of the device is that of its device entry.
	Fixture     string          `json:"fixture,omitempty"`
	Description string          `json:"description,omitempty"`
	Notes       []string        `json:"notes,omitempty"`
	Alignment   json.RawMessage `json:"alignment,omitempty"`

	DeviceInstance uint32 `json:"device_instance"`
	DeviceName     string `json:"device_name"`
	// Port is the UDP port to serve on, unless --port is given.
	Port            uint16           `json:"port,omitempty"`
	Objects         []DeviceObject   `json:"objects"`
	ObjectLifecycle *ObjectLifecycle `json:"object_lifecycle,omitempty"`
}

// DeviceObject is one object of a device file. Which fields apply depends on Type; a
// field that does not apply to the type is refused.
type DeviceObject struct {
	Type        string `json:"type"`
	Instance    uint32 `json:"instance"`
	ObjectName  string `json:"object_name"`
	Description string `json:"description,omitempty"`
	// Deletable lets a client delete the object (DeleteObject).
	Deletable bool `json:"deletable,omitempty"`

	// Device.
	Location                   string  `json:"location,omitempty"`
	VendorID                   *uint16 `json:"vendor_id,omitempty"`
	VendorName                 string  `json:"vendor_name,omitempty"`
	ModelName                  string  `json:"model_name,omitempty"`
	FirmwareRevision           string  `json:"firmware_revision,omitempty"`
	ApplicationSoftwareVersion string  `json:"application_software_version,omitempty"`
	SerialNumber               string  `json:"serial_number,omitempty"`

	// Points and value objects.
	PresentValue json.RawMessage `json:"present_value,omitempty"`
	Commandable  bool            `json:"commandable,omitempty"`
	// Units is a BACnetEngineeringUnits name (degrees-celsius) or number (62).
	Units        json.RawMessage `json:"units,omitempty"`
	COVIncrement float32         `json:"cov_increment,omitempty"`
	ActiveText   string          `json:"active_text,omitempty"`
	InactiveText string          `json:"inactive_text,omitempty"`
	// NumberOfStates defaults to the number of StateText entries.
	NumberOfStates     uint32              `json:"number_of_states,omitempty"`
	StateText          []string            `json:"state_text,omitempty"`
	IntrinsicReporting *IntrinsicReporting `json:"intrinsic_reporting,omitempty"`

	// Trend Log.
	LogBuffer  []LogEntry `json:"log_buffer,omitempty"`
	BufferSize uint32     `json:"buffer_size,omitempty"`
	// Enable defaults to true when the log has records, as bacnet-interop serves it.
	Enable *bool `json:"enable,omitempty"`

	// File.
	AccessMethod     string   `json:"access_method,omitempty"`
	StreamDataBase64 string   `json:"stream_data_base64,omitempty"`
	Records          []string `json:"records,omitempty"`
	ReadOnly         bool     `json:"read_only,omitempty"`
	FileType         string   `json:"file_type,omitempty"`
}

// IntrinsicReporting is out-of-range reporting of an analog object (clause 13.3.6), to
// the recipients of a Notification Class, as alarms, on every transition.
type IntrinsicReporting struct {
	NotificationClass uint32  `json:"notification_class"`
	HighLimit         float32 `json:"high_limit"`
	LowLimit          float32 `json:"low_limit"`
	Deadband          float32 `json:"deadband,omitempty"`
	TimeDelay         uint32  `json:"time_delay,omitempty"`
}

// LogEntry is one record a Trend Log starts with.
type LogEntry struct {
	// Timestamp is local time, 2006-01-02T15:04:05, or RFC 3339.
	Timestamp string  `json:"timestamp"`
	Real      float32 `json:"real"`
}

// ObjectLifecycle says which objects clients may create and delete.
type ObjectLifecycle struct {
	// CreatableTypes are the object types CreateObject creates. A created object can be
	// deleted again.
	CreatableTypes []string `json:"creatable_types,omitempty"`
	// PrecreatedDeletable are objects of the file a client may delete.
	PrecreatedDeletable []ObjectRefEntry `json:"precreated_deletable,omitempty"`
	// ProtectedObjects are objects no client may delete; they must not be deletable.
	ProtectedObjects []ObjectRefEntry `json:"protected_objects,omitempty"`
	// AllocatedInstanceRange is accepted for bacnet-interop fixtures and not applied:
	// go-bacnet gives a created object the lowest free instance.
	AllocatedInstanceRange json.RawMessage `json:"allocated_instance_range,omitempty"`
}

// ObjectRefEntry names an object of a device file.
type ObjectRefEntry struct {
	Type     string `json:"type"`
	Instance uint32 `json:"instance"`
}

// family is the kind of object a device file entry is built as.
type family int

const (
	famDevice family = iota
	famAnalog
	famBinary
	famMultiState
	famText
	famInteger
	famUnsigned
	famNotificationClass
	famTrendLog
	famFile
)

// servable are the object types a device file may hold, with their family.
var servable = map[bacnet.ObjectType]family{
	bacnet.ObjectTypeDevice:       famDevice,
	bacnet.ObjectTypeAnalogInput:  famAnalog,
	bacnet.ObjectTypeAnalogOutput: famAnalog,
	bacnet.ObjectTypeAnalogValue:  famAnalog,
	bacnet.ObjectTypeBinaryInput:  famBinary,
	bacnet.ObjectTypeBinaryOutput: famBinary,
	bacnet.ObjectTypeBinaryValue:  famBinary,
	13:                            famMultiState, // multi-state-input
	14:                            famMultiState, // multi-state-output
	19:                            famMultiState, // multi-state-value
	40:                            famText,       // characterstring-value
	45:                            famInteger,    // integer-value
	48:                            famUnsigned,   // positive-integer-value
	15:                            famNotificationClass,
	20:                            famTrendLog,
	10:                            famFile,
}

// creatable are the object types object_lifecycle.creatable_types may name.
var creatable = []bacnet.ObjectType{0, 1, 2, 3, 4, 5, 10, 40, 45, 48}

// common are the fields of every entry; fields lists those of each family.
var common = []string{"type", "instance", "object_name", "description", "deletable"}

var fields = map[family][]string{
	famDevice: {"location", "vendor_id", "vendor_name", "model_name", "firmware_revision",
		"application_software_version", "serial_number"},
	famAnalog:            {"present_value", "commandable", "units", "cov_increment", "intrinsic_reporting"},
	famBinary:            {"present_value", "commandable", "active_text", "inactive_text"},
	famMultiState:        {"present_value", "commandable", "number_of_states", "state_text"},
	famText:              {"present_value", "commandable"},
	famInteger:           {"present_value", "commandable", "units"},
	famUnsigned:          {"present_value", "commandable", "units"},
	famNotificationClass: {},
	famTrendLog:          {"log_buffer", "buffer_size", "enable"},
	famFile:              {"access_method", "stream_data_base64", "records", "read_only", "file_type"},
}

// isInput reports whether t is an input object: it has no priority array.
func isInput(t bacnet.ObjectType) bool { return t == 0 || t == 3 || t == 13 }

// ServableTypes lists the object types a device file may hold.
func ServableTypes() []string {
	out := make([]string, 0, len(servable))
	for t := range servable {
		out = append(out, ObjectTypeName(t))
	}
	sort.Strings(out)
	return out
}

// configError is a device file that cannot be served: invalid input, nothing was bound.
func configError(source string, err error) *errs.Error {
	return errs.InvalidInput(errs.CodeInvalidConfig, fmt.Sprintf("invalid device file %s: %v", source, err), err)
}

// LoadDeviceFile reads and parses the device file at path.
func LoadDeviceFile(path string) (*DeviceFile, error) {
	f, err := os.Open(path) //nolint:gosec // a path the operator gave
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("cannot read %s: %v", path, err), err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxDeviceFile+1))
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("cannot read %s: %v", path, err), err)
	}
	if len(data) > maxDeviceFile {
		return nil, configError(path, fmt.Errorf("larger than %d MiB", maxDeviceFile>>20))
	}
	df, err := ParseDeviceFile(data)
	if err != nil {
		return nil, configError(path, err)
	}
	return df, nil
}

// ParseDeviceFile parses a device file and checks its structure: unknown fields, and
// fields that do not apply to the type of an object, are refused.
func ParseDeviceFile(data []byte) (*DeviceFile, error) {
	var raw struct {
		Objects []map[string]json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, jsonError(err)
	}
	for i, o := range raw.Objects {
		if err := checkFields(o); err != nil {
			return nil, fmt.Errorf("objects[%d]: %w", i, err)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var df DeviceFile
	if err := dec.Decode(&df); err != nil {
		return nil, jsonError(err)
	}
	return &df, nil
}

func jsonError(err error) error {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return fmt.Errorf("not JSON: %v (at offset %d)", err, syn.Offset)
	}
	return errors.New(strings.TrimPrefix(err.Error(), "json: "))
}

// checkFields refuses the fields of an object entry that do not apply to its type.
func checkFields(o map[string]json.RawMessage) error {
	var typ string
	if err := json.Unmarshal(o["type"], &typ); err != nil || typ == "" {
		return errors.New("no type")
	}
	t, err := ParseObjectType(typ)
	if err != nil {
		return err
	}
	fam, ok := servable[t]
	if !ok {
		return fmt.Errorf("object type %s cannot be served (bacnetctl serves %s)", ObjectTypeName(t), strings.Join(ServableTypes(), ", "))
	}
	allowed := append(slices.Clone(common), fields[fam]...)
	if isInput(t) {
		allowed = slices.DeleteFunc(allowed, func(f string) bool { return f == "commandable" })
	}
	for k := range o {
		if !slices.Contains(allowed, k) {
			return fmt.Errorf("%s: field %q does not apply to %s", typ, k, ObjectTypeName(t))
		}
	}
	return nil
}

// Build makes the Device of the file. Nothing is served yet.
func (df *DeviceFile) Build() (*object.Device, error) { return df.build(nil) }

// build is Build with created called for every object a client creates.
func (df *DeviceFile) build(created func(bacnet.ObjectIdentifier)) (*object.Device, error) {
	if df.DeviceName == "" {
		return nil, errors.New("device_name is empty")
	}
	if df.DeviceInstance >= bacnet.MaxObjectInstance {
		return nil, fmt.Errorf("device_instance %d: use 0-%d", df.DeviceInstance, bacnet.MaxObjectInstance-1)
	}
	if len(df.Objects) == 0 {
		return nil, errors.New("no objects")
	}
	cfg := object.DeviceConfig{Instance: df.DeviceInstance, Name: df.DeviceName, VendorID: DefaultVendorID,
		VendorName: "otfabric", ModelName: "bacnetctl server", FirmwareRevision: "bacnetctl", ApplicationSoftwareVersion: "bacnetctl"}
	devices := 0
	for i, o := range df.Objects {
		t, err := ParseObjectType(o.Type)
		if err != nil || t != bacnet.ObjectTypeDevice {
			continue
		}
		devices++
		if o.Instance != df.DeviceInstance || o.ObjectName != df.DeviceName {
			return nil, fmt.Errorf("objects[%d]: the device entry is device:%d %q, the file says device_instance %d and device_name %q",
				i, o.Instance, o.ObjectName, df.DeviceInstance, df.DeviceName)
		}
		if o.Deletable {
			return nil, fmt.Errorf("objects[%d]: the Device object cannot be deletable", i)
		}
		cfg.Description, cfg.Location, cfg.SerialNumber = o.Description, o.Location, o.SerialNumber
		if o.VendorID != nil {
			cfg.VendorID = *o.VendorID
		}
		cfg.VendorName = orDefault(o.VendorName, cfg.VendorName)
		cfg.ModelName = orDefault(o.ModelName, cfg.ModelName)
		cfg.FirmwareRevision = orDefault(o.FirmwareRevision, cfg.FirmwareRevision)
		cfg.ApplicationSoftwareVersion = orDefault(o.ApplicationSoftwareVersion, cfg.ApplicationSoftwareVersion)
	}
	if devices > 1 {
		return nil, errors.New("more than one device entry")
	}
	dev, err := object.NewDevice(cfg)
	if err != nil {
		return nil, fmt.Errorf("device: %w", err)
	}
	for i, o := range df.Objects {
		t, err := ParseObjectType(o.Type)
		if err != nil {
			return nil, fmt.Errorf("objects[%d]: %w", i, err)
		}
		if t == bacnet.ObjectTypeDevice {
			continue
		}
		where := fmt.Sprintf("objects[%d] (%s:%d)", i, ObjectTypeName(t), o.Instance)
		obj, err := o.build(t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, objectError(err))
		}
		if o.Deletable {
			setDeletable(obj)
		}
		if err := dev.Add(obj); err != nil {
			return nil, fmt.Errorf("%s: %w", where, objectError(err))
		}
	}
	if err := df.applyLifecycle(dev, created); err != nil {
		return nil, fmt.Errorf("object_lifecycle: %w", err)
	}
	return dev, nil
}

// objectError names a BACnet error of the object model by its code, as in
// duplicate-name; another error is returned as it is.
func objectError(err error) error {
	if e, ok := errors.AsType[*bacnet.ErrorResponse](err); ok {
		return fmt.Errorf("%s (%w)", bacnet.ErrorCodeName(e.Code), err)
	}
	return err
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func setDeletable(o object.Object) {
	if d, ok := o.(interface{ SetDeletable(bool) }); ok {
		d.SetDeletable(true)
	}
}

func (df *DeviceFile) applyLifecycle(dev *object.Device, created func(bacnet.ObjectIdentifier)) error {
	lc := df.ObjectLifecycle
	if lc == nil {
		return nil
	}
	for _, name := range lc.CreatableTypes {
		t, err := ParseObjectType(name)
		if err != nil {
			return err
		}
		if !slices.Contains(creatable, t) {
			return fmt.Errorf("creatable type %s: bacnetctl can create %s", ObjectTypeName(t), creatableNames())
		}
		dev.RegisterFactory(t, factory(t, created))
	}
	find := func(what string, r ObjectRefEntry) (object.Object, error) {
		t, err := ParseObjectType(r.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		o, ok := dev.Object(bacnet.ObjectIdentifier{Type: t, Instance: r.Instance})
		if !ok {
			return nil, fmt.Errorf("%s: %s:%d is not an object of the device", what, ObjectTypeName(t), r.Instance)
		}
		return o, nil
	}
	for _, r := range lc.PrecreatedDeletable {
		o, err := find("precreated_deletable", r)
		if err != nil {
			return err
		}
		if o.Identifier().Type == bacnet.ObjectTypeDevice {
			return errors.New("precreated_deletable: the Device object cannot be deleted")
		}
		setDeletable(o)
	}
	for _, r := range lc.ProtectedObjects {
		o, err := find("protected_objects", r)
		if err != nil {
			return err
		}
		if d, ok := o.(interface{ Deletable() bool }); ok && d.Deletable() {
			return fmt.Errorf("protected_objects: %s is also deletable", ObjectName(o.Identifier()))
		}
	}
	return nil
}

func creatableNames() string {
	names := make([]string, 0, len(creatable))
	for _, t := range creatable {
		names = append(names, ObjectTypeName(t))
	}
	return strings.Join(names, ", ")
}

// factory creates an object of type t for CreateObject: with the defaults of a device
// file entry that gives nothing but its name. It can be deleted again. created, when not
// nil, is told about every object made.
func factory(t bacnet.ObjectType, created func(bacnet.ObjectIdentifier)) object.Factory {
	return func(instance uint32, name string) (object.Object, error) {
		o := DeviceObject{Type: ObjectTypeName(t), Instance: instance, ObjectName: name}
		obj, err := o.build(t)
		if err != nil {
			return nil, err
		}
		setDeletable(obj)
		if created != nil {
			created(obj.Identifier())
		}
		return obj, nil
	}
}

// build makes the object of one entry of the device file.
func (o *DeviceObject) build(t bacnet.ObjectType) (object.Object, error) {
	if o.ObjectName == "" {
		return nil, errors.New("object_name is empty")
	}
	if o.Instance >= bacnet.MaxObjectInstance {
		return nil, fmt.Errorf("instance %d: use 0-%d", o.Instance, bacnet.MaxObjectInstance-1)
	}
	switch servable[t] {
	case famAnalog:
		return o.analog(t)
	case famBinary:
		return o.binary(t)
	case famMultiState:
		return o.multiState(t)
	case famText, famInteger, famUnsigned:
		return o.value(t)
	case famNotificationClass:
		return object.NewNotificationClass(object.NotificationClassConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description})
	case famTrendLog:
		return o.trendLog()
	case famFile:
		return o.file()
	default:
		return nil, fmt.Errorf("object type %s cannot be served", ObjectTypeName(t))
	}
}

func (o *DeviceObject) point() object.PointConfig {
	return object.PointConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description, Commandable: o.Commandable}
}

func (o *DeviceObject) analog(t bacnet.ObjectType) (object.Object, error) {
	cfg := o.point()
	cfg.COVIncrement = o.COVIncrement
	units, err := parseUnits(o.Units, noUnits)
	if err != nil {
		return nil, err
	}
	cfg.Units = units
	if len(o.PresentValue) > 0 && string(o.PresentValue) != "null" {
		var f float64
		if err := json.Unmarshal(o.PresentValue, &f); err != nil || f > math.MaxFloat32 || f < -math.MaxFloat32 {
			return nil, fmt.Errorf("present_value %s: an analog object takes a number", o.PresentValue)
		}
		cfg.Value = float32(f)
	}
	if o.COVIncrement < 0 {
		return nil, fmt.Errorf("cov_increment %g: must not be negative", o.COVIncrement)
	}
	if ir := o.IntrinsicReporting; ir != nil {
		if ir.LowLimit > ir.HighLimit {
			return nil, fmt.Errorf("intrinsic_reporting: low_limit %g is above high_limit %g", ir.LowLimit, ir.HighLimit)
		}
		if ir.Deadband < 0 {
			return nil, fmt.Errorf("intrinsic_reporting: deadband %g must not be negative", ir.Deadband)
		}
		cfg.Events = &object.EventConfig{
			NotificationClass: ir.NotificationClass,
			EventEnable:       object.EnableToOffNormal | object.EnableToFault | object.EnableToNormal,
			NotifyType:        object.NotifyAlarm,
			TimeDelay:         ir.TimeDelay,
			HighLimit:         ir.HighLimit, LowLimit: ir.LowLimit, Deadband: ir.Deadband,
			LimitEnable: object.LimitEnableHigh | object.LimitEnableLow,
		}
	}
	switch t {
	case bacnet.ObjectTypeAnalogInput:
		return object.NewAnalogInput(cfg)
	case bacnet.ObjectTypeAnalogOutput:
		return object.NewAnalogOutput(cfg)
	default:
		return object.NewAnalogValue(cfg)
	}
}

func (o *DeviceObject) binary(t bacnet.ObjectType) (object.Object, error) {
	cfg := o.point()
	cfg.ActiveText, cfg.InactiveText = o.ActiveText, o.InactiveText
	if len(o.PresentValue) > 0 && string(o.PresentValue) != "null" {
		var v any
		_ = json.Unmarshal(o.PresentValue, &v)
		switch v {
		case "active", true, 1.0:
			cfg.Active = true
		case "inactive", false, 0.0:
		default:
			return nil, fmt.Errorf("present_value %s: a binary object takes \"active\" or \"inactive\"", o.PresentValue)
		}
	}
	switch t {
	case bacnet.ObjectTypeBinaryInput:
		return object.NewBinaryInput(cfg)
	case bacnet.ObjectTypeBinaryOutput:
		return object.NewBinaryOutput(cfg)
	default:
		return object.NewBinaryValue(cfg)
	}
}

func (o *DeviceObject) multiState(t bacnet.ObjectType) (object.Object, error) {
	cfg := o.point()
	cfg.StateText = o.StateText
	texts := len(o.StateText)
	if texts > math.MaxUint16 {
		return nil, fmt.Errorf("state_text has %d entries: at most %d", texts, math.MaxUint16)
	}
	cfg.NumberOfStates = o.NumberOfStates
	if cfg.NumberOfStates == 0 {
		cfg.NumberOfStates = uint32(texts)
	}
	if cfg.NumberOfStates == 0 {
		return nil, errors.New("a multi-state object needs number_of_states or state_text")
	}
	if texts > 0 && uint32(texts) != cfg.NumberOfStates {
		return nil, fmt.Errorf("state_text has %d entries, number_of_states is %d", texts, cfg.NumberOfStates)
	}
	cfg.State = 1
	if len(o.PresentValue) > 0 && string(o.PresentValue) != "null" {
		var n uint32
		if err := json.Unmarshal(o.PresentValue, &n); err != nil || n < 1 || n > cfg.NumberOfStates {
			return nil, fmt.Errorf("present_value %s: a multi-state object takes a state 1-%d", o.PresentValue, cfg.NumberOfStates)
		}
		cfg.State = n
	}
	switch t {
	case 13:
		return object.NewMultiStateInput(cfg)
	case 14:
		return object.NewMultiStateOutput(cfg)
	default:
		return object.NewMultiStateValue(cfg)
	}
}

func (o *DeviceObject) value(t bacnet.ObjectType) (object.Object, error) {
	cfg := object.ValueConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description, Commandable: o.Commandable}
	units, err := parseUnits(o.Units, noUnits)
	if err != nil {
		return nil, err
	}
	cfg.Units = units
	set := len(o.PresentValue) > 0 && string(o.PresentValue) != "null"
	switch servable[t] {
	case famText:
		if set && json.Unmarshal(o.PresentValue, &cfg.Text) != nil {
			return nil, fmt.Errorf("present_value %s: a characterstring-value takes a string", o.PresentValue)
		}
		return object.NewCharacterStringValue(cfg)
	case famInteger:
		if set && json.Unmarshal(o.PresentValue, &cfg.Integer) != nil {
			return nil, fmt.Errorf("present_value %s: an integer-value takes an integer of 32 bits", o.PresentValue)
		}
		return object.NewIntegerValue(cfg)
	default:
		if set && json.Unmarshal(o.PresentValue, &cfg.Unsigned) != nil {
			return nil, fmt.Errorf("present_value %s: a positive-integer-value takes an unsigned integer of 32 bits", o.PresentValue)
		}
		return object.NewPositiveIntegerValue(cfg)
	}
}

func (o *DeviceObject) trendLog() (object.Object, error) {
	enable := len(o.LogBuffer) > 0
	if o.Enable != nil {
		enable = *o.Enable
	}
	if !enable && len(o.LogBuffer) > 0 {
		return nil, errors.New("a disabled trend log cannot start with log_buffer records")
	}
	tl, err := object.NewTrendLog(object.TrendLogConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description,
		BufferSize: o.BufferSize, Enable: enable})
	if err != nil {
		return nil, err
	}
	for i, r := range o.LogBuffer {
		at, err := parseLogTime(r.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("log_buffer[%d]: %w", i, err)
		}
		if err := tl.Log(at, bacnet.RealValue(r.Real), nil); err != nil {
			return nil, fmt.Errorf("log_buffer[%d]: %w", i, err)
		}
	}
	return tl, nil
}

func parseLogTime(s string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("timestamp %q: use 2006-01-02T15:04:05 (local time) or RFC 3339", s)
}

func (o *DeviceObject) file() (object.Object, error) {
	cfg := object.FileConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description,
		FileType: o.FileType, ReadOnly: o.ReadOnly}
	switch o.AccessMethod {
	case "", "stream":
		if len(o.Records) > 0 {
			return nil, errors.New("records are for access_method record")
		}
		data, err := base64.StdEncoding.DecodeString(o.StreamDataBase64)
		if err != nil {
			return nil, fmt.Errorf("stream_data_base64: %w", err)
		}
		cfg.Data = data
	case "record":
		if o.StreamDataBase64 != "" {
			return nil, errors.New("stream_data_base64 is for access_method stream")
		}
		cfg.Record = true
		for _, r := range o.Records {
			cfg.Records = append(cfg.Records, []byte(r))
		}
	default:
		return nil, fmt.Errorf("access_method %q: use stream or record", o.AccessMethod)
	}
	return object.NewFile(cfg)
}

// unitByName are the engineering units by name.
var unitByName = func() map[string]uint32 {
	m := make(map[string]uint32, len(unitNames))
	var u uint32
	for _, n := range unitNames {
		m[n] = u
		u++
	}
	return m
}()

// parseUnits parses the units of an entry: a name or a number, def when absent.
func parseUnits(raw json.RawMessage, def uint32) (uint32, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return def, nil
	}
	var n uint32
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if u, ok := unitByName[normalizeName(s)]; ok {
			return u, nil
		}
		if n, err := strconv.ParseUint(s, 10, 32); err == nil {
			return uint32(n), nil
		}
	}
	return 0, fmt.Errorf("units %s: use a BACnetEngineeringUnits name, such as degrees-celsius, or its number", raw)
}

// DefaultDeviceFile is the device `server run` serves without --config, and what
// `server example` prints.
func DefaultDeviceFile() *DeviceFile {
	vendor := uint16(DefaultVendorID)
	return &DeviceFile{
		DeviceInstance: 1234,
		DeviceName:     "bacnetctl-1234",
		Port:           47808,
		Objects: []DeviceObject{
			{Type: "device", Instance: 1234, ObjectName: "bacnetctl-1234", Description: "Simulated device of bacnetctl server",
				VendorID: &vendor, VendorName: "otfabric", ModelName: "bacnetctl server"},
			{Type: "analog-input", Instance: 1, ObjectName: "zone-temperature", Description: "Zone temperature",
				PresentValue: json.RawMessage(`21.5`), Units: json.RawMessage(`"degrees-celsius"`), COVIncrement: 0.1},
			{Type: "analog-value", Instance: 1, ObjectName: "zone-setpoint", Description: "Zone temperature setpoint",
				PresentValue: json.RawMessage(`21`), Units: json.RawMessage(`"degrees-celsius"`), Commandable: true},
			{Type: "binary-value", Instance: 1, ObjectName: "occupied", Description: "Occupancy mode",
				PresentValue: json.RawMessage(`"inactive"`), ActiveText: "occupied", InactiveText: "unoccupied", Commandable: true},
			{Type: "multi-state-value", Instance: 1, ObjectName: "fan-mode", Description: "Fan mode",
				PresentValue: json.RawMessage(`1`), StateText: []string{"auto", "low", "high"}, Commandable: true},
			{Type: "characterstring-value", Instance: 1, ObjectName: "message", Description: "Operator message",
				PresentValue: json.RawMessage(`"hello"`)},
		},
		ObjectLifecycle: &ObjectLifecycle{CreatableTypes: []string{"analog-value", "binary-value"}},
	}
}
