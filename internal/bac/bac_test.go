// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestParseObject(t *testing.T) {
	for in, want := range map[string]bacnet.ObjectIdentifier{
		"analog-input:1":        {Type: 0, Instance: 1},
		"ai:1":                  {Type: 0, Instance: 1},
		"AI:1":                  {Type: 0, Instance: 1},
		"0:1":                   {Type: 0, Instance: 1},
		"Analog_Input:1":        {Type: 0, Instance: 1},
		"analogInput:1":         {Type: 0, Instance: 1},
		" analog value : 7 ":    {Type: 2, Instance: 7},
		"ao:0":                  {Type: 1, Instance: 0},
		"av:2":                  {Type: 2, Instance: 2},
		"bi:3":                  {Type: 3, Instance: 3},
		"bo:4":                  {Type: 4, Instance: 4},
		"bv:5":                  {Type: 5, Instance: 5},
		"msi:6":                 {Type: 13, Instance: 6},
		"mso:7":                 {Type: 14, Instance: 7},
		"msv:8":                 {Type: 19, Instance: 8},
		"dev:1234":              {Type: 8, Instance: 1234},
		"device:4194302":        {Type: 8, Instance: 4194302},
		"8:1234":                {Type: 8, Instance: 1234},
		"trend-log:1":           {Type: 20, Instance: 1},
		"tl:1":                  {Type: 20, Instance: 1},
		"notification-class:5":  {Type: 15, Instance: 5},
		"network-port:1":        {Type: 56, Instance: 1},
		"300:9":                 {Type: 300, Instance: 9},
		"device":                bacnet.AnyDevice(),
		"dev":                   bacnet.AnyDevice(),
		"8":                     bacnet.AnyDevice(),
		"characterstring-value": {},
	} {
		got, err := ParseObject(in)
		if in == "characterstring-value" {
			if err == nil {
				t.Errorf("ParseObject(%q) = %v, want an error: only the device has a wildcard", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("ParseObject(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", ":", "ai", "ai:", ":1", "ai:x", "ai:-1", "ai:4194304", "nonsense:1", "1024:1", "ai:1:2", "99999:1"} {
		if got, err := ParseObject(in); err == nil {
			t.Errorf("ParseObject(%q) = %v, want an error", in, got)
		}
	}
	if ids, err := ParseObjects([]string{"ai:1", "bv:2"}); err != nil || len(ids) != 2 || ids[1].Type != 5 {
		t.Errorf("ParseObjects: %v, %v", ids, err)
	}
	if _, err := ParseObjects([]string{"ai:1", "x"}); err == nil {
		t.Error("ParseObjects accepted a bad object")
	}
	if got, err := ParseObjectTypes(" ai, binary-value ,19,"); err != nil || fmt.Sprint(got) != "[analog-input binary-value multi-state-value]" {
		t.Errorf("ParseObjectTypes: %v, %v", got, err)
	}
	if got, err := ParseObjectTypes(""); err != nil || got != nil {
		t.Errorf("no object types: %v, %v", got, err)
	}
	if _, err := ParseObjectTypes("ai,nonsense"); err == nil {
		t.Error("ParseObjectTypes accepted a bad type")
	}
	if _, err := ParseObjectType(""); err == nil {
		t.Error("an empty object type was accepted")
	}
	if ObjectName(bacnet.ObjectIdentifier{Type: 300, Instance: 9}) != "300:9" || ObjectTypeName(8) != "device" {
		t.Error("object names")
	}
}

func TestParseProperty(t *testing.T) {
	for in, want := range map[string]bacnet.PropertyIdentifier{
		"present-value": 85, "pv": 85, "PV": 85, "85": 85, "presentValue": 85, "Present_Value": 85, "present value": 85,
		"object-name": 77, "name": 77, "description": 28, "desc": 28, "units": 117, "status-flags": 111, "flags": 111,
		"oos": 81, "out-of-service": 81, "priority-array": 87, "object-list": 76, "property-list": 371, "all": 8,
		"required": 105, "optional": 80, "max-apdu-length-accepted": 62, "512": 512, "4194303": 4194303, "0": 0,
	} {
		if got, err := ParseProperty(in); err != nil || got != want {
			t.Errorf("ParseProperty(%q) = %v, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "nonsense", "-1", "4194304", "pv:1"} {
		if got, err := ParseProperty(in); err == nil {
			t.Errorf("ParseProperty(%q) = %v, want an error", in, got)
		}
	}
	refs, err := ParseProperties(" pv, units ,priority-array[8],state-text[0]")
	if err != nil || len(refs) != 4 || refs[0].ArrayIndex != nil || refs[2].Identifier != 87 || *refs[2].ArrayIndex != 8 || *refs[3].ArrayIndex != 0 {
		t.Errorf("ParseProperties: %+v, %v", refs, err)
	}
	for _, in := range []string{"", ",", "pv[", "pv[x]", "pv[]", "pv[4294967295]", "nonsense[1]", "pv,nonsense"} {
		if got, err := ParseProperties(in); err == nil {
			t.Errorf("ParseProperties(%q) = %v, want an error", in, got)
		}
	}
	if PropertyName(85) != "present-value" || PropertyName(9999) != "9999" {
		t.Error("property names")
	}
}

// The names of bacnetctl are those of the library wherever the library has one, and every
// name parses back to its number.
func TestNameTables(t *testing.T) {
	for id, name := range objectTypeNames {
		if lib := id.String(); !strings.HasPrefix(lib, "ObjectType(") && lib != name {
			t.Errorf("object type %d: %q here, %q in go-bacnet", id, name, lib)
		}
		if got, err := ParseObjectType(name); err != nil || got != id {
			t.Errorf("object type %q parses to %v, %v", name, got, err)
		}
	}
	for id, name := range propertyNames {
		if lib := id.String(); !strings.HasPrefix(lib, "PropertyIdentifier(") && lib != name {
			t.Errorf("property %d: %q here, %q in go-bacnet", id, name, lib)
		}
		if got, err := ParseProperty(name); err != nil || got != id {
			t.Errorf("property %q parses to %v, %v", name, got, err)
		}
	}
	// No name is given to two numbers.
	if len(objectTypeByName) != len(objectTypeNames) || len(propertyByName) != len(propertyNames) {
		t.Error("a name is used twice")
	}
	for alias, id := range objectTypeAliases {
		if _, clash := objectTypeByName[alias]; clash || objectTypeNames[id] == "" {
			t.Errorf("alias %q", alias)
		}
	}
	for alias, id := range propertyAliases {
		if _, clash := propertyByName[alias]; clash || propertyNames[id] == "" {
			t.Errorf("alias %q", alias)
		}
	}
	types, props := ObjectTypes(), Properties()
	if types[0].Name != "analog-input" || types[0].Alias != "ai" || types[8].Name != "device" || types[8].Alias != "dev" || props[85-3].Number > 90 {
		t.Errorf("tables: %+v %+v", types[0], types[8])
	}
	for i := 1; i < len(props); i++ {
		if props[i-1].Number >= props[i].Number {
			t.Fatalf("properties are not in numeric order at %d", props[i].Number)
		}
	}
	if n := ObjectTypeNames(); len(n) != len(objectTypeNames)+len(objectTypeAliases) || len(PropertyNames()) != len(propertyNames) {
		t.Error("names for completion")
	}

	// The unit table is indexed by the unit number.
	if len(unitNames) != 190 {
		t.Fatalf("%d units, want 190 (0-189)", len(unitNames))
	}
	for u, want := range map[uint32]string{0: "square-meters", 3: "amperes", 5: "volts", 19: "kilowatt-hours", 27: "hertz", 29: "percent-relative-humidity",
		47: "watts", 48: "kilowatts", 53: "pascals", 62: "degrees-celsius", 64: "degrees-fahrenheit", 73: "seconds", 82: "liters", 95: "no-units",
		96: "parts-per-million", 98: "percent", 104: "revolutions-per-minute", 118: "centimeters", 135: "cubic-meters-per-hour", 146: "megawatt-hours",
		159: "milliseconds", 189: "watts-per-meter-per-degree-kelvin"} {
		if UnitName(u) != want {
			t.Errorf("unit %d is %q, want %q", u, UnitName(u), want)
		}
	}
	if UnitName(9999) != "9999" || UnitSymbol(62) != "°C" || UnitSymbol(95) != "" || UnitSymbol(21) != "therms" {
		t.Error("unit names and symbols")
	}
	if len(servicesSupportedNames) != 49 || servicesSupportedNames[12] != "read-property" || servicesSupportedNames[35] != "read-range" || servicesSupportedNames[48] != "you-are" {
		t.Error("the services supported bits")
	}
}

func TestParseValues(t *testing.T) {
	vc := ValueContext{ObjectType: bacnet.ObjectTypeBinaryValue, Property: bacnet.PropertyPresentValue}
	for spec, want := range map[string]bacnet.ApplicationValue{
		"null":                      bacnet.NullValue(),
		"NULL":                      bacnet.NullValue(),
		"real:21.5":                 bacnet.RealValue(21.5),
		"real:-0.25":                bacnet.RealValue(-0.25),
		"float:1e3":                 bacnet.RealValue(1000),
		"double:21.5":               bacnet.DoubleValue(21.5),
		"unsigned:3":                bacnet.UnsignedValue(3),
		"unsigned:0x10":             bacnet.UnsignedValue(16),
		"uint:18446744073709551615": bacnet.UnsignedValue(math.MaxUint64),
		"signed:-1":                 bacnet.SignedValue(-1),
		"int:42":                    bacnet.SignedValue(42),
		"bool:true":                 bacnet.BoolValue(true),
		"bool:off":                  bacnet.BoolValue(false),
		"boolean:YES":               bacnet.BoolValue(true),
		"bool:0":                    bacnet.BoolValue(false),
		"enum:1":                    bacnet.EnumValue(1),
		"enum:active":               bacnet.EnumValue(1),
		"enumerated:Inactive":       bacnet.EnumValue(0),
		"string:Room 4":             {Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "Room 4"}},
		"string:":                   {Kind: bacnet.ValueCharacterString},
		"string:a:b":                {Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "a:b"}},
		"octets:0a1b2c":             {Kind: bacnet.ValueOctetString, OctetString: []byte{0x0a, 0x1b, 0x2c}},
		"hex:0A 1B":                 {Kind: bacnet.ValueOctetString, OctetString: []byte{0x0a, 0x1b}},
		"octets:":                   {Kind: bacnet.ValueOctetString, OctetString: []byte{}},
		"bits:1011":                 {Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0xB0}}},
		"bits:000000001":            {Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 7, Bytes: []byte{0, 0x80}}},
		"bits:":                     {Kind: bacnet.ValueBitString, BitString: bacnet.BitString{Bytes: []byte{}}},
		"date:2026-10-10":           bacnet.DateValue(bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}),
		"date:2024-02-29":           bacnet.DateValue(bacnet.Date{Year: 124, Month: 2, Day: 29, Weekday: 4}),
		"date:*-12-25":              bacnet.DateValue(bacnet.Date{Year: 0xFF, Month: 12, Day: 25, Weekday: 0xFF}),
		"date:2026-*-*":             bacnet.DateValue(bacnet.Date{Year: 126, Month: 0xFF, Day: 0xFF, Weekday: 0xFF}),
		"time:12:00:00":             bacnet.TimeValue(bacnet.Time{Hour: 12}),
		"time:23:59":                bacnet.TimeValue(bacnet.Time{Hour: 23, Minute: 59}),
		"time:07:08:09.50":          bacnet.TimeValue(bacnet.Time{Hour: 7, Minute: 8, Second: 9, Hundredths: 50}),
		"object:ai:1":               bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: 0, Instance: 1}),
		"oid:device:1234":           bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: 8, Instance: 1234}),
	} {
		typ, text, ok := SplitValueSpec(spec)
		if !ok {
			t.Errorf("%q has no type", spec)
			continue
		}
		got, err := ParseTypedValue(typ, text, vc)
		if err != nil || fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
			t.Errorf("%q = %+v, %v; want %+v", spec, got, err, want)
		}
		// What parses also encodes.
		if _, err := bacnet.AppendApplicationValue(nil, got); err != nil {
			t.Errorf("%q does not encode: %v", spec, err)
		}
	}
	for _, spec := range []string{"real:warm", "real:1e99", "double:x", "unsigned:-1", "unsigned:1.5", "signed:x", "bool:maybe", "enum:red", "enum:-1",
		"octets:xyz", "octets:abc", "bits:102", "date:2026-13-01", "date:2026-02-30", "date:1899-01-01", "date:26-1-1-1", "date:x-01-01", "date:2026-01-32",
		"time:24:00", "time:12", "time:12:60", "time:12:00:61", "time:12:00:00.5", "time:12:00:00.xx", "time:a:b", "time:1:2:3:4", "object:nothing", "object:ai:x"} {
		typ, text, ok := SplitValueSpec(spec)
		if !ok {
			t.Errorf("%q has no type", spec)
			continue
		}
		if got, err := ParseTypedValue(typ, text, ValueContext{}); err == nil {
			t.Errorf("%q = %+v, want an error", spec, got)
		}
	}
	if _, err := ParseTypedValue(TypeNull, "something", vc); err == nil {
		t.Error("null with a value was accepted")
	}
	if _, err := ParseTypedValue("matrix", "1", vc); err == nil {
		t.Error("an unknown type was accepted")
	}
	// What does not start with a type is all value.
	for _, spec := range []string{"21.5", "Room 4", "http://example.net", "auto:1", ":1", "nonsense:1", "12:30"} {
		if typ, text, ok := SplitValueSpec(spec); ok || text != spec || typ != "" {
			t.Errorf("%q was split into %q and %q", spec, typ, text)
		}
	}
	// The names of an enumeration follow the property; without names only numbers work.
	if v, ok := ParseEnum(bacnet.ObjectTypeAnalogInput, bacnet.PropertyUnits, "degrees-celsius"); !ok || v != 62 {
		t.Errorf("units by name: %v, %v", v, ok)
	}
	if v, ok := ParseEnum(bacnet.ObjectTypeAnalogInput, bacnet.PropertyEventState, "high_limit"); !ok || v != 3 {
		t.Errorf("event state by name: %v, %v", v, ok)
	}
	if _, ok := ParseEnum(bacnet.ObjectTypeAnalogInput, bacnet.PropertyPresentValue, "active"); ok {
		t.Error("an analog input took the name of a binary value")
	}
	if _, err := ParseTypedValue(TypeEnum, "sideways", vc); err == nil || !strings.Contains(err.Error(), "inactive, active") {
		t.Errorf("the error does not list the names: %v", err)
	}

	for kind, want := range map[bacnet.ValueKind]string{
		bacnet.ValueBoolean: TypeBool, bacnet.ValueUnsigned: TypeUnsigned, bacnet.ValueSigned: TypeSigned, bacnet.ValueReal: TypeReal,
		bacnet.ValueDouble: TypeDouble, bacnet.ValueOctetString: TypeOctets, bacnet.ValueCharacterString: TypeString, bacnet.ValueBitString: TypeBits,
		bacnet.ValueEnumerated: TypeEnum, bacnet.ValueDate: TypeDate, bacnet.ValueTime: TypeTime, bacnet.ValueObjectID: TypeObject,
	} {
		if got, ok := TypeOfKind(kind); !ok || got != want {
			t.Errorf("type of kind %d: %q, %v", kind, got, ok)
		}
		if _, known := ValueTypes()[want]; !known {
			t.Errorf("%q is not a documented type", want)
		}
	}
	for _, kind := range []bacnet.ValueKind{bacnet.ValueNull, bacnet.ValueConstructed, bacnet.ValueContext} {
		if _, ok := TypeOfKind(kind); ok {
			t.Errorf("kind %d says what to write", kind)
		}
	}
	if typ, ok := CanonicalType(" Float "); !ok || typ != TypeReal {
		t.Errorf("CanonicalType: %q, %v", typ, ok)
	}
}

func TestParseHex(t *testing.T) {
	for in, want := range map[string]string{
		"810b000c":            "810b000c",
		"81 0b 00 0c":         "810b000c",
		"81:0B:00:0C":         "810b000c",
		"81-0b-00-0c":         "810b000c",
		"0x81 0x0b,0x00,0X0c": "810b000c",
		"0x810b000c":          "810b000c",
		"81 0b\n00\t0c\r\n":   "810b000c",
		"":                    "",
	} {
		if got, err := ParseHex(in); err != nil || fmt.Sprintf("%x", got) != want {
			t.Errorf("ParseHex(%q) = %x, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"8", "81 0", "zz", "81 0b 0g", "0x"} {
		if in == "0x" {
			continue
		}
		if got, err := ParseHex(in); err == nil {
			t.Errorf("ParseHex(%q) = %x, want an error", in, got)
		}
	}
}

func TestValues(t *testing.T) {
	ai, bv := bacnet.ObjectTypeAnalogInput, bacnet.ObjectTypeBinaryValue
	pv := bacnet.PropertyPresentValue
	for _, c := range []struct {
		typ   bacnet.ObjectType
		prop  bacnet.PropertyIdentifier
		value bacnet.ApplicationValue
		text  string
		json  string
	}{
		{ai, pv, bacnet.NullValue(), "null", `{"type":"null","value":null}`},
		{ai, pv, bacnet.BoolValue(true), "true", `{"type":"boolean","value":true}`},
		{ai, pv, bacnet.UnsignedValue(42), "42", `{"type":"unsigned","value":42}`},
		{ai, pv, bacnet.SignedValue(-7), "-7", `{"type":"signed","value":-7}`},
		// A REAL prints as the number it is, not as its conversion to 64 bits.
		{ai, pv, bacnet.RealValue(21.5), "21.5", `{"type":"real","value":21.5}`},
		{ai, pv, bacnet.RealValue(0.1), "0.1", `{"type":"real","value":0.1}`},
		{ai, pv, bacnet.RealValue(-1234567), "-1234567", `{"type":"real","value":-1.234567e+06}`},
		{ai, pv, bacnet.RealValue(0), "0", `{"type":"real","value":0}`},
		{ai, pv, bacnet.RealValue(1e20), "1e+20", `{"type":"real","value":1e+20}`},
		{ai, pv, bacnet.RealValue(float32(math.NaN())), "NaN", `{"type":"real","value":"NaN"}`},
		{ai, pv, bacnet.RealValue(float32(math.Inf(1))), "+Inf", `{"type":"real","value":"+Inf"}`},
		{ai, pv, bacnet.DoubleValue(0.1), "0.1", `{"type":"double","value":0.1}`},
		{ai, pv, bacnet.DoubleValue(math.Inf(-1)), "-Inf", `{"type":"double","value":"-Inf"}`},
		{ai, pv, bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{0xde, 0xad}}, "dead", `{"type":"octet-string","value":"dead"}`},
		{ai, 77, bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "Zone 4"}}, "Zone 4", `{"type":"character-string","value":"Zone 4"}`},
		{ai, 111, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x90}}}, "{in-alarm, out-of-service}",
			`{"type":"bit-string","value":"1001","bits":["in-alarm","out-of-service"]}`},
		{ai, 111, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0}}}, "{}", `{"type":"bit-string","value":"0000","bits":[]}`},
		// A bit string bacnetctl has no names for is shown as bits.
		{ai, 9999, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xA0}}}, "101", `{"type":"bit-string","value":"101"}`},
		{ai, 111, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 3, Bytes: []byte{0x08}}}, "{bit-4}", `{"type":"bit-string","value":"00001","bits":["bit-4"]}`},
		{bv, pv, bacnet.EnumValue(1), "active (1)", `{"type":"enumerated","value":1,"name":"active"}`},
		{ai, pv, bacnet.EnumValue(1), "1", `{"type":"enumerated","value":1}`},
		{bv, pv, bacnet.EnumValue(7), "7", `{"type":"enumerated","value":7}`},
		{ai, 117, bacnet.EnumValue(62), "degrees-celsius (62)", `{"type":"enumerated","value":62,"name":"degrees-celsius"}`},
		{ai, 36, bacnet.EnumValue(4), "low-limit (4)", `{"type":"enumerated","value":4,"name":"low-limit"}`},
		{ai, 79, bacnet.EnumValue(0), "analog-input (0)", `{"type":"enumerated","value":0,"name":"analog-input"}`},
		{ai, 103, bacnet.EnumValue(12), "communication-failure (12)", `{"type":"enumerated","value":12,"name":"communication-failure"}`},
		{8, 112, bacnet.EnumValue(0), "operational (0)", `{"type":"enumerated","value":0,"name":"operational"}`},
		{8, 107, bacnet.EnumValue(3), "no-segmentation (3)", `{"type":"enumerated","value":3,"name":"no-segmentation"}`},
		{ai, 84, bacnet.EnumValue(1), "reverse (1)", `{"type":"enumerated","value":1,"name":"reverse"}`},
		{ai, 72, bacnet.EnumValue(1), "event (1)", `{"type":"enumerated","value":1,"name":"event"}`},
		{10, 41, bacnet.EnumValue(1), "stream-access (1)", `{"type":"enumerated","value":1,"name":"stream-access"}`},
		{9, 37, bacnet.EnumValue(5), "out-of-range (5)", `{"type":"enumerated","value":5,"name":"out-of-range"}`},
		{8, 196, bacnet.EnumValue(2), "warmstart (2)", `{"type":"enumerated","value":2,"name":"warmstart"}`},
		{20, 197, bacnet.EnumValue(1), "cov (1)", `{"type":"enumerated","value":1,"name":"cov"}`},
		{ai, pv, bacnet.DateValue(bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}), "2026-10-10", `{"type":"date","value":"2026-10-10"}`},
		{ai, pv, bacnet.DateValue(bacnet.Date{Year: 0xFF, Month: 0xFF, Day: 0xFF}), "*-*-*", `{"type":"date","value":"*-*-*"}`},
		{ai, pv, bacnet.DateValue(bacnet.Date{Year: 126, Month: 13, Day: 32}), "2026-odd-last", `{"type":"date","value":"2026-odd-last"}`},
		{ai, pv, bacnet.DateValue(bacnet.Date{Year: 126, Month: 14, Day: 1}), "2026-even-01", `{"type":"date","value":"2026-even-01"}`},
		{ai, pv, bacnet.TimeValue(bacnet.Time{Hour: 7, Minute: 8, Second: 9, Hundredths: 5}), "07:08:09.05", `{"type":"time","value":"07:08:09.05"}`},
		{ai, pv, bacnet.TimeValue(bacnet.Time{Hour: 0xFF, Minute: 0xFF, Second: 0xFF, Hundredths: 0xFF}), "*:*:*.*", `{"type":"time","value":"*:*:*.*"}`},
		{ai, 75, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: 0, Instance: 1}), "analog-input:1", `{"type":"object-identifier","value":"analog-input:1","object_type":0,"instance":1}`},
		{ai, 75, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: 300, Instance: 2}), "300:2", `{"type":"object-identifier","value":"300:2","object_type":300,"instance":2}`},
		{19, 110, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "Off"}}}, {Value: bacnet.UnsignedValue(2)},
		}}, "[Off, 2]", `{"type":"list","value":[{"type":"character-string","value":"Off"},{"type":"unsigned","value":2}]}`},
		{ai, pv, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}, "[]", `{"type":"list","value":[]}`},
		// Context tags keep their number; their content is octets without a type.
		{ai, 9999, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{0x05}}},
			{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(1.5)}}}},
		}}, "[[0] 0x05, [1] [1.5]]", `{"type":"list","value":[{"type":"context","value":"05","tag":0},{"type":"list","value":[{"type":"real","value":1.5}],"tag":1}]}`},
		{ai, pv, bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{0xAB}}, "0xab", `{"type":"context","value":"ab"}`},
		{bv, 87, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.NullValue()}, {Value: bacnet.EnumValue(1)}}},
			"[null, active (1)]", `{"type":"list","value":[{"type":"null","value":null},{"type":"enumerated","value":1,"name":"active"}]}`},
		{ai, 371, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.EnumValue(85)}, {Value: bacnet.EnumValue(9999)}}},
			"[present-value (85), 9999 (9999)]", `{"type":"list","value":[{"type":"enumerated","value":85,"name":"present-value"},{"type":"enumerated","value":9999,"name":"9999"}]}`},
		{ai, 130, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{42}}},
			{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{12, 30, 0, 0}}},
		}}, "[sequence 42, 12:30:00.00]", `{"type":"list","value":[{"type":"time-stamp","value":"sequence 42"},{"type":"time-stamp","value":"12:30:00.00"}]}`},
	} {
		v := ValueOf(c.typ, c.prop, c.value)
		if v.Text != c.text {
			t.Errorf("text of %+v: %q, want %q", c.value, v.Text, c.text)
		}
		raw, err := json.Marshal(v)
		if err != nil || string(raw) != c.json {
			t.Errorf("JSON of %q: %s, %v; want %s", c.text, raw, err, c.json)
		}
	}
	v := ValueOf(ai, pv, bacnet.RealValue(21.5))
	v.Unit = "°C"
	if raw, _ := json.Marshal(v); v.String() != "21.5 °C" || string(raw) != `{"type":"real","value":21.5,"unit":"°C"}` || types.TextOf(&v) != "21.5 °C" || types.TextOf(nil) != "-" {
		t.Errorf("a value with a unit: %q, %s", v.String(), raw)
	}
	if KindName(bacnet.ValueReal) != "real" || KindName(bacnet.ValueConstructed) != "list" || KindName(bacnet.ValueNull) != "null" {
		t.Error("kind names")
	}
	if f, ok := v.Value.(interface{ Float64() float64 }); !ok || f.Float64() != 21.5 {
		t.Error("a float value gives its number")
	}
}

func TestCharacterSets(t *testing.T) {
	for _, c := range []struct {
		encoding uint8
		raw      string
		want     string
	}{
		{0, "Zone 4 — Küche", "Zone 4 — Küche"},
		{0, "bad \xff byte", "bad � byte"},
		{4, "\x00Z\x00\xfc\x20\xac", "Zü€"},
		{4, "\xd8\x3d\xde\x00", "😀"},
		{4, "\x00Z\x00", "Z"},
		{3, "\x00\x00\x00Z\x00\x01\xf6\x00", "Z😀"},
		{3, "\x00\x11\x00\x00", "�"},
		{5, "K\xfcche \xb0C", "Küche °C"},
		{1, "\x03\xa4abc", "\x03�abc"},
		{2, "abc", "abc"},
	} {
		if got := Text(bacnet.CharacterString{Encoding: c.encoding, Value: c.raw}); got != c.want {
			t.Errorf("character set %d of %q: %q, want %q", c.encoding, c.raw, got, c.want)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	timeout := fmt.Errorf("read: %w", bacnet.ErrTimeout)
	for _, c := range []struct {
		err  error
		kind errs.Kind
		code string
		exit int
		text string
	}{
		{&bacnet.ErrorResponse{Class: 1, Code: 31}, errs.KindDevice, "unknown_object", 6, "the device refused the request: unknown-object (error class object)"},
		{fmt.Errorf("wrapped: %w", &bacnet.ErrorResponse{Class: 2, Code: 40}), errs.KindDevice, "write_access_denied", 6, "write-access-denied (error class property)"},
		{&bacnet.ErrorResponse{Class: 200, Code: 900}, errs.KindDevice, "code_900", 6, "code-900 (error class class-200)"},
		{&bacnet.RejectError{Reason: 9}, errs.KindDevice, "reject_unrecognized_service", 6, "the device rejected the request: unrecognized-service (it does not implement this service)"},
		{&bacnet.RejectError{Reason: 4}, errs.KindDevice, "reject_invalid_tag", 6, "the device rejected the request: invalid-tag"},
		{&bacnet.AbortError{Reason: 4}, errs.KindDevice, "abort_segmentation_not_supported", 6, "the device aborted the request: segmentation-not-supported"},
		{&service.WritePropertyMultipleError{Class: 2, Code: 37, FirstFailed: bacnet.ObjectIdentifier{Type: 2, Instance: 1}, FirstProperty: bacnet.PropertyReference{Identifier: 85}},
			errs.KindDevice, "value_out_of_range", 6, "value-out-of-range (error class property) (first failed write: analog-value:1 present-value)"},
		{&bacnet.OutcomeUnknownError{Operation: "WriteProperty", Cause: bacnet.ErrTimeout}, errs.KindTimeout, "outcome_unknown", 5, "no acknowledgement arrived in time. The device may or may not have carried it out"},
		{&bacnet.OutcomeUnknownError{Operation: "WriteProperty", Cause: context.Canceled}, errs.KindTimeout, "outcome_unknown", 5, "it was interrupted before the acknowledgement arrived"},
		{&bacnet.OutcomeUnknownError{Operation: "ReinitializeDevice", Cause: bacnet.ErrClosed}, errs.KindTimeout, "outcome_unknown", 5, "the ReinitializeDevice request was sent, but the socket was closed"},
		{timeout, errs.KindTimeout, "request_timeout", 5, "timeout: no answer from the device"},
		{context.DeadlineExceeded, errs.KindTimeout, "request_timeout", 5, "timeout"},
		{context.Canceled, errs.KindInterrupted, "context_canceled", 130, "interrupted"},
		{bacnet.ErrClosed, errs.KindTransport, "connection_lost", 4, "socket was closed"},
		{fmt.Errorf("%w: port", bacnet.ErrInvalidConfig), errs.KindInvalidInput, "invalid_config", 3, "invalid input"},
		{&bacnet.APDUTooLargeError{EncodedSize: 600, RemoteMax: 480}, errs.KindProtocol, "apdu_too_large", 6, "(600 octets) is larger than the device accepts (480)"},
		{client.ErrNoRoute, errs.KindProtocol, "no_route", 6, "no BACnet router"},
		{&client.BVLCOperationError{Operation: "Read-BDT", Code: 0x20}, errs.KindProtocol, "bvlc_result", 6, "the BBMD refused Read-BDT: BVLC result 0x0020"},
		{&client.BVLCResultError{Code: 0x30}, errs.KindProtocol, "bvlc_result", 6, "BVLC result 0x0030"},
		{fmt.Errorf("%w: tag", bacnet.ErrMalformed), errs.KindProtocol, "malformed_response", 6, "protocol error"},
		{bacnet.ErrTrailingData, errs.KindProtocol, "malformed_response", 6, "protocol error"},
		{bacnet.ErrLimitExceeded, errs.KindProtocol, "malformed_response", 6, "protocol error"},
		{bacnet.ErrProtocolViolation, errs.KindProtocol, "protocol_violation", 6, "protocol error"},
		{bacnet.ErrUnsupported, errs.KindUnsupported, "unsupported", 8, "protocol error"},
		{bacnet.ErrDeviceManagementDisabled, errs.KindInternal, "internal_unexpected", 10, "internal error"},
		{&net.OpError{Op: "write", Err: syscall.ENETUNREACH}, errs.KindTransport, "transport_send_failed", 4, "network error"},
		{errors.New("something else"), errs.KindInternal, "internal_unexpected", 10, "internal error"},
	} {
		e, ok := errs.As(WrapError(c.err))
		if !ok || e.Kind != c.kind || e.Code != c.code || errs.ExitCodeForKind(e.Kind) != c.exit || !strings.Contains(e.Message, c.text) || !errors.Is(e, c.err) {
			t.Errorf("WrapError(%v) = %+v", c.err, e)
		}
	}
	if WrapError(nil) != nil {
		t.Error("nil is no error")
	}
	typed := errs.InvalidInput(errs.CodeInvalidInput, "bad", nil)
	if WrapError(typed) != error(typed) {
		t.Error("a typed error is returned as it is")
	}

	// What the device said travels with the error.
	e, _ := errs.As(WrapError(&bacnet.ErrorResponse{Class: 2, Code: 32}))
	if r := e.Remote; r == nil || r.PDU != "error" || r.Class != "property" || *r.ClassID != 2 || r.Code != "unknown-property" || *r.CodeID != 32 || r.Reason != "" {
		t.Errorf("remote error: %+v", e.Remote)
	}
	e, _ = errs.As(WrapError(&bacnet.AbortError{Reason: 1}))
	if r := e.Remote; r == nil || r.PDU != "abort" || r.Reason != "buffer-overflow" || *r.ReasonID != 1 || r.ClassID != nil {
		t.Errorf("remote abort: %+v", e.Remote)
	}
	// In a row the message is the name of the error.
	info := PropertyErrorInfo(&bacnet.ErrorResponse{Class: 2, Code: 32})
	if info.Message != "unknown-property" || info.Code != "unknown_property" || info.Kind != "device_error" || info.BACnet.Class != "property" {
		t.Errorf("property error: %+v", info)
	}
	if info := PropertyErrorInfo(&bacnet.RejectError{Reason: 9}); !strings.Contains(info.Message, "rejected") || info.BACnet.PDU != "reject" {
		t.Errorf("rejected property: %+v", info)
	}
	if EmbeddedErrorInfo(nil) != nil || PropertyErrorInfo(nil) != nil {
		t.Error("no error, no error object")
	}
	if info := EmbeddedErrorInfo(timeout); info.Kind != "timeout" || info.BACnet != nil {
		t.Errorf("embedded timeout: %+v", info)
	}

	// Which errors end a command that works through a list, and which mean "try another way".
	for err, fatal := range map[error]bool{
		nil: false, timeout: true, context.Canceled: true, bacnet.ErrClosed: true,
		&bacnet.ErrorResponse{Class: 1, Code: 31}: false, &bacnet.RejectError{Reason: 9}: false, &bacnet.AbortError{Reason: 4}: false,
	} {
		if Fatal(err) != fatal {
			t.Errorf("Fatal(%v) = %v", err, !fatal)
		}
	}
	for err, want := range map[error]bool{
		&bacnet.RejectError{Reason: 9}:            true,
		&bacnet.RejectError{Reason: 4}:            false,
		&bacnet.ErrorResponse{Class: 5, Code: 67}: true,
		&bacnet.ErrorResponse{Class: 5, Code: 45}: true,
		&bacnet.ErrorResponse{Class: 5, Code: 29}: true,
		&bacnet.ErrorResponse{Class: 1, Code: 31}: false,
		&bacnet.AbortError{Reason: 4}:             false,
		timeout:                                   false,
	} {
		if NotImplemented(err) != want {
			t.Errorf("NotImplemented(%v) = %v", err, !want)
		}
	}
	for err, want := range map[error]bool{
		&bacnet.AbortError{Reason: 1}: true, &bacnet.AbortError{Reason: 4}: true, &bacnet.AbortError{Reason: 11}: true,
		&bacnet.AbortError{Reason: 9}: false, timeout: false,
	} {
		if tooLarge(err) != want {
			t.Errorf("tooLarge(%v) = %v", err, !want)
		}
	}

	// A socket that cannot be opened.
	for err, code := range map[error]string{
		&net.OpError{Op: "listen", Err: syscall.EADDRINUSE}:          "transport_port_in_use",
		errors.New("bind: Only one usage... address already in use"): "transport_port_in_use",
		syscall.Errno(10048):                                  "transport_port_in_use",
		&net.OpError{Op: "listen", Err: syscall.EACCES}:       "transport_bind_failed",
		fmt.Errorf("%w: port 70000", bacnet.ErrInvalidConfig): "invalid_config",
	} {
		if e, ok := errs.As(bindError(47808, err)); !ok || e.Code != code {
			t.Errorf("bindError(%v) = %+v", err, e)
		}
	}
}

func TestAddressing(t *testing.T) {
	ctx := context.Background()
	ep, err := ResolveEndpoint(ctx, "192.168.1.20")
	if err != nil || ep.String() != "192.168.1.20:47808" {
		t.Fatalf("endpoint: %v, %v", ep, err)
	}
	// A directly reachable device has its address and port as its MAC.
	direct := EndpointTarget(ep)
	if direct.Address.Scope() != bacnet.AddressLocalStation || direct.Address.MAC().String() != "c0a80114bac0" || TargetText(direct) != "192.168.1.20:47808" {
		t.Errorf("direct target: %v %s", direct.Address, direct.Address.MAC())
	}
	for _, bad := range []string{"", "[::1]:47808", "::1", "a b"} {
		if _, err := ResolveEndpoint(ctx, bad); err == nil {
			t.Errorf("ResolveEndpoint(%q) succeeded", bad)
		} else if e, _ := errs.As(err); e.Kind != errs.KindInvalidInput || e.Code != errs.CodeInvalidAddress {
			t.Errorf("ResolveEndpoint(%q): %+v", bad, e)
		}
	}

	s := &Session{Timeout: 2 * time.Second, Retries: 3}
	if s.Budget() != 8*time.Second {
		t.Errorf("budget: %v", s.Budget())
	}
	routed, err := s.Target(ctx, &config.ConnConfig{Device: "192.168.1.1:47809", Network: 2001, MAC: "0a"})
	if err != nil || routed.Endpoint.String() != "192.168.1.1:47809" || routed.Address.Scope() != bacnet.AddressRemoteStation ||
		routed.Address.Network() != 2001 || routed.Address.MAC().String() != "0a" || TargetText(routed) != "192.168.1.1:47809 net 2001 mac 0a" {
		t.Errorf("routed target: %+v, %v", routed, err)
	}
	if _, err := s.Target(ctx, &config.ConnConfig{Device: "192.168.1.1", Network: 5}); err == nil {
		t.Error("a network without a MAC was accepted")
	}
	if _, err := s.Target(ctx, &config.ConnConfig{Device: "x y"}); err == nil {
		t.Error("a bad device address was accepted")
	}

	if Timeout(0) != DefaultTimeout || Timeout(250) != 250*time.Millisecond || Wait(0) != DefaultWait || Wait(100) != 100*time.Millisecond {
		t.Error("timeouts")
	}
	for in, want := range map[string]bacnet.AddressScope{
		"": bacnet.AddressLocalStation, "all": bacnet.AddressGlobalBroadcast, "ALL": bacnet.AddressGlobalBroadcast, "65535": bacnet.AddressGlobalBroadcast,
		"0xFFFF": bacnet.AddressGlobalBroadcast, "global": bacnet.AddressGlobalBroadcast, "2001": bacnet.AddressRemoteBroadcast, "1": bacnet.AddressRemoteBroadcast,
	} {
		if got, err := ParseBroadcastNetwork(in); err != nil || got.Scope() != want {
			t.Errorf("ParseBroadcastNetwork(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"0", "-1", "65536", "x"} {
		if _, err := ParseBroadcastNetwork(in); err == nil {
			t.Errorf("ParseBroadcastNetwork(%q) succeeded", in)
		}
	}
	if lo, hi, err := InstanceRange("", ""); err != nil || lo != nil || hi != nil {
		t.Error("no range")
	}
	if lo, hi, err := InstanceRange("0", "4194303"); err != nil || *lo != 0 || *hi != 4194303 {
		t.Error("the full range")
	}
	if idx, err := ParseIndex(" 3 "); err != nil || *idx != 3 {
		t.Error("ParseIndex")
	}
	if _, err := ParseIndex("4294967295"); err == nil {
		t.Error("the index that means all was accepted")
	}
	if _, err := ParseSyncTime("2026-01-31T12:00:00Z"); err != nil {
		t.Error(err)
	}
	if now, err := ParseSyncTime("NOW"); err != nil || time.Since(now) > time.Minute {
		t.Error("ParseSyncTime(now)")
	}
	// The BACnet day of week counts from Monday.
	if d, tm := DateTimeOf(time.Date(2026, 10, 11, 23, 59, 58, 990_000_000, time.UTC)); d != (bacnet.Date{Year: 126, Month: 10, Day: 11, Weekday: 7}) ||
		tm != (bacnet.Time{Hour: 23, Minute: 59, Second: 58, Hundredths: 99}) {
		t.Errorf("DateTimeOf: %+v %+v", d, tm)
	}
}

func TestDecodeFrames(t *testing.T) {
	names := func(res *types.DecodeResult) string {
		var parts []string
		for _, l := range res.Layers {
			parts = append(parts, l.Layer+"="+l.Summary)
			for _, f := range l.Fields {
				parts = append(parts, fmt.Sprintf("%s:%v", f.Name, f.Value))
			}
		}
		if res.Error != "" {
			parts = append(parts, "error="+res.Error)
		}
		return strings.Join(parts, " | ")
	}
	for _, c := range []struct {
		hex  string
		want []string
	}{
		// BVLC functions without an NPDU.
		{"810000060000", []string{"bvlc=BVLC-Result", "result_code:0x0000"}},
		{"81050006003c", []string{"bvlc=Register-Foreign-Device", "ttl_seconds:60"}},
		{"81020004", []string{"bvlc=Read-Broadcast-Distribution-Table"}},
		{"8103000e c0a80101bac0 ffffff00", []string{"Read-Broadcast-Distribution-Table-Ack", "entry:192.168.1.1:47808 mask 255.255.255.0"}},
		{"8107000e c0a80105bac0 003c 0050", []string{"Read-Foreign-Device-Table-Ack", "entry:192.168.1.5:47808 ttl 60 s, 80 s remaining"}},
		{"8108000a c0a80105bac0", []string{"Delete-Foreign-Device-Table-Entry", "payload:c0a80105bac0"}},
		// A forwarded Who-Is with a range.
		{"81040016 c0a80105bac0 0120ffff00ff 1008 0900 1963", []string{"bvlc=Forwarded-NPDU", "origin:192.168.1.5:47808", "service=who-is", "low_limit:0", "high_limit:99"}},
		// Network layer messages.
		{"810b0007 018000", []string{"npdu=Who-Is-Router-To-Network", "message_type:0x00"}},
		{"810b000b 018001 07d1 07d2", []string{"npdu=I-Am-Router-To-Network", "networks:2001 2002"}},
		{"810a000a 018003 0107d1", []string{"npdu=Reject-Message-To-Network", "data:0107d1"}},
		{"810a0007 018063", []string{"npdu=message type 0x63"}},
		// A routed read with source and destination.
		{"810a0019 012c 07d1010a 0005020102 ff 0003 010c 0c00000001 1955", []string{"destination:network 2001, MAC 0a", "source:network 5, MAC 0102", "hop_count:255",
			"apdu=Confirmed-Request", "invoke_id:1", "service:read-property (12)", "object:analog-input:1", "property:present-value (85)"}},
		{"810b000c 0120 07d100 ff 1008", []string{"destination:broadcast on network 2001"}},
		// Requests and their answers.
		{"810a0013 0104 0275 030c 0c00000001 1957 2908", []string{"segmented_response_accepted:true", "max_apdu_accepted:1476", "array_index:8"}},
		{"810a0017 0100 3003 0c 0c00000001 1955 3e 4441ac0000 3f", []string{"apdu=Complex-ACK", "service=read-property ack", "value:21.5 (real)"}},
		{"810a0017 0104 0005 040e 0c00000001 1e 0955 0975 1908 1f", []string{"service=read-property-multiple request", "analog-input:1:present-value, units[8]"}},
		{"810a0023 0100 3004 0e 0c00000001 1e 2955 4e 4441ac0000 4f 292d 5e 910291 20 5f 1f",
			[]string{"read-property-multiple ack", "analog-input:1 present-value:21.5", "analog-input:1 high-limit:error: unknown-property"}},
		{"810a0019 0104 0005 050f 0c00800001 1955 3e 4441b40000 3f 4908", []string{"service=write-property request", "object:analog-value:1", "value:22.5 (real)", "priority:8"}},
		{"810a0009 0100 20050f", []string{"apdu=Simple-ACK", "service:write-property (15)"}},
		{"810a000d 0100 50050f 9101 911f", []string{"apdu=Error", "error_class:object (1)", "error_code:unknown-object (31)"}},
		{"810a000a 0100 50050f ff", []string{"apdu=Error", "payload:ff"}},
		{"810a0009 0100 600509", []string{"apdu=Reject", "reason:unrecognized-service (9)"}},
		{"810a0009 0100 710504", []string{"apdu=Abort", "reason:segmentation-not-supported (4)", "from_server:true"}},
		{"810a000a 0100 40050301", []string{"apdu=Segment-ACK", "sequence_number:3", "actual_window_size:1"}},
		// COV.
		{"810a0014 0104 0005 0605 0901 1c00000001 2901 3978", []string{"service=subscribe-cov request", "process_id:1", "confirmed_notifications:true", "lifetime_seconds:120"}},
		{"810a0010 0104 0005 0705 0901 1c00000001", []string{"cancellation:true"}},
		{"810a001d 0104 0005 081c 0901 1c00000001 2900 3978 4e 0955 4f 5c 3f000000", []string{"service=subscribe-cov-property request", "property:present-value (85)", "cov_increment:0.5"}},
		{"810a0025 0100 1002 0901 1c020004d2 2c00000001 3978 4e 0955 2e 4441ac0000 2f 096f 2e 820400 2f 4f",
			[]string{"service=unconfirmed-cov-notification", "device:device:1234", "time_remaining_seconds:120", "present-value:21.5 (real)", "status-flags:{} (bit-string)"}},
		// Discovery.
		{"810b0019 0100 1007 3d 0b 00 46616e20456e61626c65", []string{"service=who-has", "name:Fan Enable"}},
		{"810b0011 0100 1007 2c01400001", []string{"service=who-has", "object:binary-value:1"}},
		{"810a001e 0100 1001 c4020004d2 c401400001 75 0b 00 46616e20456e61626c65", []string{"service=i-have", "device:device:1234", "object:binary-value:1", "name:Fan Enable"}},
		// Time, device management, files, ranges.
		{"810b0012 0100 1006 a47e0a0a06 b40c1e0f00", []string{"service=time-synchronization", "date:2026-10-10", "time:12:30:15.00"}},
		{"810a0014 0104 0005 0914 0901 1d 07 00 736563726574", []string{"service=reinitialize-device request", "state:warmstart", "password:present, 6 characters (not shown)"}},
		{"810a000e 0104 0005 0a11 090a 1901", []string{"service=device-communication-control request", "state:disable", "duration_minutes:10", "password:none"}},
		{"810a0013 0104 0005 0b06 c402800001 0e 3100 2114 0f", []string{"service=atomic-read-file request", "file:file:1", "start:0", "count:20"}},
		{"810a0012 0100 300b06 11 0e 3100 6503 616263 0f", []string{"service=atomic-read-file ack", "end_of_file:true", "octets:3"}},
		{"810a0016 0104 0005 0c07 c402800001 0e 3100 6503 616263 0f", []string{"service=atomic-write-file request", "octets:3"}},
		{"810a0009 0100 300c07 0900", []string{"service=atomic-write-file ack", "start:0"}},
		{"810a0018 0104 0005 0d1a 0c05000001 1983 3e 2101 3105 3f", []string{"service=read-range request", "object:trend-log:1", "property:log-buffer (131)", "range:by-position", "reference_index:1", "count:5"}},
		{"810a0015 0100 300d1a 0c05000001 1983 3a05c0 4900 5e 5f", []string{"service=read-range ack", "item_count:0", "first_item:true", "last_item:true"}},
		{"810a000b 0104 0005 0e1d", []string{"service=get-event-information request"}},
		{"810a000f 0104 0005 0e1d 0c00000002", []string{"last_received:analog-input:2"}},
		// What has no decoder is shown as octets.
		{"810a000c 0104 0005 0f0a 0c0102", []string{"service=create-object request", "payload:0c0102"}},
		{"810a000a 0100 1004 0c01", []string{"service=unconfirmed-private-transfer", "payload:0c01"}},
		{"810a000b 0100 300f0a c40000", []string{"service=create-object ack", "payload:c40000"}},
		{"810a000a 0100 1063 01", []string{"service=service-99"}},
		{"810a0009 0100 200163", []string{"service:service-99 (99)"}},
		// A segment of a segmented answer is not decoded further.
		{"810a000e 0100 3c 01 00 02 0c 0c0000", []string{"apdu=Complex-ACK", "segment:sequence 0, window 2, more follows true", "payload:0c0000"}},
	} {
		raw, err := ParseHex(c.hex)
		if err != nil {
			t.Fatalf("%s: %v", c.hex, err)
		}
		// The length field of the BVLC header is filled in here.
		raw[2], raw[3] = byte(len(raw)>>8), byte(len(raw))
		got := names(Decode(raw))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s\n  lacks %q in\n  %s", c.hex, w, got)
			}
		}
		if strings.Contains(got, "error=") {
			t.Errorf("%s\n  did not decode: %s", c.hex, got)
		}
	}
	if ConfirmedServiceName(12) != "read-property" || ConfirmedServiceName(200) != "service-200" || UnconfirmedServiceName(8) != "who-is" {
		t.Error("service names")
	}
	if _, err := DecodeReadPropertyMultiple([]byte{0x0c, 0, 0, 0, 1, 0x1e, 0x29, 0x55, 0x1f}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Error("a property reference with a result tag was accepted")
	}
	if _, err := DecodeReadPropertyMultiple(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Error("an empty request was accepted")
	}
}

// Frames built with the encoders of the library decode to what went in.
func TestDecodeEncodedServices(t *testing.T) {
	frame := func(apduBytes []byte) []byte {
		t.Helper()
		raw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
		if err != nil {
			t.Fatal(err)
		}
		out, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	confirmed := func(choice uint8, payload []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return frame(apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{InvokeID: 7, MaxAPDU: uint8(apdu.MaxAPDU480), ServiceChoice: choice, Payload: payload}))
	}
	text := func(raw []byte) string {
		t.Helper()
		out, err := Decode(raw).MarshalTextOutput()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	device := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	ai2 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 2}
	from, ack := uint32(0), true
	note := service.EventNotification{
		ProcessIdentifier: 1, InitiatingDevice: device, EventObject: ai2,
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 42},
		NotificationClass: 5, Priority: 100, EventType: 5, MessageText: &bacnet.CharacterString{Value: "Humidity high"},
		NotifyType: 0, AckRequired: &ack, FromState: &from, ToState: 3,
	}
	payload, err := service.EncodeEventNotification(note)
	for _, raw := range [][]byte{
		confirmed(apdu.ServiceConfirmedEventNotification, payload, err),
		frame(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: payload})),
	} {
		got := text(raw)
		for _, want := range []string{"event-notification", "device              device:1234", "object              analog-input:2", "event_time          sequence 42",
			"notification_class  5", "priority            100", "event_type          out-of-range", "notify_type         alarm", "from_state          normal",
			"to_state            high-limit", "message             Humidity high"} {
			if !strings.Contains(got, want) {
				t.Errorf("event notification lacks %q:\n%s", want, got)
			}
		}
	}

	priority := uint8(8)
	payload, err = service.EncodeWritePropertyMultiple([]service.WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1},
		Properties: []service.WritePropertyValue{
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.EnumValue(1), Priority: &priority},
		},
	}})
	if got := text(confirmed(apdu.ServiceWritePropertyMultiple, payload, err)); !strings.Contains(got, "SERVICE  write-property-multiple request") ||
		!strings.Contains(got, "binary-value:1 present-value  active (1)") || !strings.Contains(got, "max_apdu_accepted            480") {
		t.Errorf("WritePropertyMultiple:\n%s", got)
	}

	d, tm := DateTimeOf(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	payload, err = service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 1, EventObject: ai2, EventStateAcked: 3, TimeStamp: note.TimeStamp,
		AcknowledgmentSource: bacnet.CharacterString{Value: "J. Smith"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{Date: d, Time: tm}},
	})
	if got := text(confirmed(apdu.ServiceAcknowledgeAlarm, payload, err)); !strings.Contains(got, "state_acknowledged  high-limit") ||
		!strings.Contains(got, "time_stamp          sequence 42") || !strings.Contains(got, "source              J. Smith") {
		t.Errorf("AcknowledgeAlarm:\n%s", got)
	}

	payload, err = service.EncodeGetEventInformationACK(service.GetEventInformationACK{Summaries: []service.EventSummary{{
		Object: ai2, EventState: 3, AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x60}},
		EventTimeStamps: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}, EventEnable: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}},
		EventPriorities: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}, MoreEvents: true})
	if err != nil {
		t.Fatal(err)
	}
	got := text(frame(apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: 7, ServiceChoice: apdu.ServiceGetEventInformation, Payload: payload})))
	if !strings.Contains(got, "SERVICE  get-event-information ack") || !strings.Contains(got, "analog-input:2  high-limit") || !strings.Contains(got, "more_events     true") {
		t.Errorf("GetEventInformation ack:\n%s", got)
	}

	// The rows of a decoded frame, and a decoder that gives up.
	res := Decode(frame([]byte{0x10, 0x08, 0xFF}))
	if rows := res.TableRows(); res.Error == "" || rows[len(rows)-1][0] != "error" || len(res.TableHeaders()) != 3 {
		t.Errorf("a Who-Is with a bad tag: %+v", res)
	}
	if out, _ := res.MarshalTextOutput(); !strings.Contains(out, "error: service: ") {
		t.Errorf("text of a failed decode:\n%s", out)
	}
}

func TestSmallHelpers(t *testing.T) {
	if eventStateName(1) != "fault" || eventStateName(77) != "77" || namedOr(notifyTypeNames, 9) != "9" {
		t.Error("names of states")
	}
	if transitionIndex(0) != 2 || transitionIndex(1) != 1 || transitionIndex(3) != 0 || transitionIndex(2) != 0 {
		t.Error("the place of a transition in event-time-stamps")
	}
	if stringOf(bacnet.RealValue(1)) != "" || stringOf(bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: " x "}}) != "x" {
		t.Error("stringOf")
	}
	if uintOf(bacnet.RealValue(1)) != nil || *uintOf(bacnet.UnsignedValue(5)) != 5 || *uintOf(bacnet.EnumValue(6)) != 6 {
		t.Error("uintOf")
	}
	if got := unacknowledged(bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x40}}); fmt.Sprint(got) != "[to-offnormal to-normal]" {
		t.Errorf("unacknowledged: %v", got)
	}
	// Log records that are no plain values.
	stamp := service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10}, Time: bacnet.Time{Hour: 9}}
	failure := logRecord(4, service.LogRecord{Timestamp: stamp, DatumChoice: service.LogDatumFailure, Datum: bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.EnumValue(2)}, {Value: bacnet.EnumValue(32)}}}})
	if failure.Kind != RecordFailure || failure.Value.Name != "unknown-property" || failure.Position != 4 || failure.Timestamp != "2026-10-10 09:00:00.00" {
		t.Errorf("failure record: %+v %+v", failure, failure.Value)
	}
	change := logRecord(5, service.LogRecord{Timestamp: stamp, DatumChoice: service.LogDatumTimeChange, Datum: bacnet.RealValue(-3.5)})
	if change.Kind != RecordTimeChange || change.Value.String() != "-3.5 s" || change.StatusFlags != nil {
		t.Errorf("time change record: %+v", change)
	}
	if got := NameTable("x", []NamedNumber{{Number: 1, Name: "alpha", Alias: "a"}, {Number: 2, Name: "beta"}}, " ALPHA "); got.Count != 1 || got.Names[0].ID != 1 {
		t.Errorf("NameTable: %+v", got)
	}
	if fmt.Sprint(ReinitializeStates()) != "[abort-restore activate-changes coldstart end-backup end-restore start-backup start-restore warmstart]" ||
		fmt.Sprint(CommControlStates()) != "[disable disable-initiation enable]" {
		t.Error("the states of the device management commands")
	}
	if stateName(reinitStates, 1) != "warmstart" || stateName(commStates, 9) != "9" || hidden(nil) != "none" {
		t.Error("names in decoded device management requests")
	}
}
