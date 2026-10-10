// SPDX-License-Identifier: MIT

package bac

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	bacnet "github.com/otfabric/go-bacnet"
)

// Value types of a value specification ("real:21.5") and of --type.
const (
	TypeAuto     = "auto"
	TypeNull     = "null"
	TypeBool     = "bool"
	TypeUnsigned = "unsigned"
	TypeSigned   = "signed"
	TypeReal     = "real"
	TypeDouble   = "double"
	TypeString   = "string"
	TypeOctets   = "octets"
	TypeBits     = "bits"
	TypeEnum     = "enum"
	TypeDate     = "date"
	TypeTime     = "time"
	TypeObject   = "object"
)

// ValueTypes describes the value types, for help and completion.
func ValueTypes() map[string]string {
	return map[string]string{
		TypeAuto:     "the type the property holds now (read first)",
		TypeNull:     "NULL (relinquishes a commandable property)",
		TypeBool:     "BOOLEAN: true or false",
		TypeUnsigned: "Unsigned Integer",
		TypeSigned:   "Signed Integer",
		TypeReal:     "REAL (32-bit floating point)",
		TypeDouble:   "Double (64-bit floating point)",
		TypeString:   "CharacterString (UTF-8)",
		TypeOctets:   "OctetString, as hex",
		TypeBits:     "BitString, as ones and zeros",
		TypeEnum:     "Enumerated: a number, or a name the property knows (active, inactive)",
		TypeDate:     "Date: YYYY-MM-DD",
		TypeTime:     "Time: HH:MM[:SS[.hh]]",
		TypeObject:   "BACnetObjectIdentifier: type:instance",
	}
}

// typeAliases are other spellings of the value types.
var typeAliases = map[string]string{
	"boolean": TypeBool, "uint": TypeUnsigned, "int": TypeSigned, "float": TypeReal, "str": TypeString,
	"character-string": TypeString, "characterstring": TypeString, "octet-string": TypeOctets,
	"octetstring": TypeOctets, "hex": TypeOctets, "bit-string": TypeBits, "bitstring": TypeBits,
	"enumerated": TypeEnum, "object-identifier": TypeObject, "objectid": TypeObject, "oid": TypeObject,
}

// CanonicalType returns the value type a name stands for.
func CanonicalType(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if a, ok := typeAliases[name]; ok {
		return a, true
	}
	_, ok := ValueTypes()[name]
	return name, ok
}

// SplitValueSpec splits "type:text" into its type and text. ok is false when spec does not
// start with a value type: then the whole of it is the text.
func SplitValueSpec(spec string) (typ, text string, ok bool) {
	if strings.EqualFold(strings.TrimSpace(spec), TypeNull) {
		return TypeNull, "", true
	}
	prefix, rest, found := strings.Cut(spec, ":")
	if !found {
		return "", spec, false
	}
	typ, known := CanonicalType(prefix)
	if !known || typ == TypeAuto {
		return "", spec, false
	}
	return typ, rest, true
}

// ValueContext says which property a value is for: it resolves the names of enumerations.
type ValueContext struct {
	ObjectType bacnet.ObjectType
	Property   bacnet.PropertyIdentifier
}

// ParseTypedValue parses text as a value of the given type.
func ParseTypedValue(typ, text string, vc ValueContext) (bacnet.ApplicationValue, error) {
	bad := func(want string) (bacnet.ApplicationValue, error) {
		return bacnet.ApplicationValue{}, fmt.Errorf("value %q is not %s", text, want)
	}
	switch typ {
	case TypeNull:
		if t := strings.TrimSpace(text); t != "" && !strings.EqualFold(t, "null") {
			return bad("null")
		}
		return bacnet.NullValue(), nil
	case TypeBool:
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true", "1", "yes", "on":
			return bacnet.BoolValue(true), nil
		case "false", "0", "no", "off":
			return bacnet.BoolValue(false), nil
		}
		return bad("a boolean (true or false)")
	case TypeUnsigned:
		u, err := strconv.ParseUint(strings.TrimSpace(text), 0, 64)
		if err != nil {
			return bad("an unsigned integer")
		}
		return bacnet.UnsignedValue(u), nil
	case TypeSigned:
		i, err := strconv.ParseInt(strings.TrimSpace(text), 0, 64)
		if err != nil {
			return bad("a signed integer")
		}
		return bacnet.SignedValue(i), nil
	case TypeReal:
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 32)
		if err != nil {
			return bad("a number that fits a 32-bit REAL")
		}
		return bacnet.RealValue(float32(f)), nil
	case TypeDouble:
		f, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return bad("a number")
		}
		return bacnet.DoubleValue(f), nil
	case TypeString:
		return bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: text}}, nil
	case TypeOctets:
		raw, err := ParseHex(text)
		if err != nil {
			return bad("hex octets")
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: raw}, nil
	case TypeBits:
		b, err := ParseBits(text)
		if err != nil {
			return bad("a bit string of ones and zeros")
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: b}, nil
	case TypeEnum:
		e, ok := ParseEnum(vc.ObjectType, vc.Property, text)
		if !ok {
			if names := enumNames(vc.ObjectType, vc.Property); names != nil {
				return bad("a number or one of " + strings.Join(nonEmpty(names), ", "))
			}
			return bad("an enumerated value (a number)")
		}
		return bacnet.EnumValue(e), nil
	case TypeDate:
		d, err := ParseDate(text)
		if err != nil {
			return bad("a date (YYYY-MM-DD)")
		}
		return bacnet.DateValue(d), nil
	case TypeTime:
		t, err := ParseTime(text)
		if err != nil {
			return bad("a time (HH:MM[:SS[.hh]])")
		}
		return bacnet.TimeValue(t), nil
	case TypeObject:
		id, err := ParseObject(text)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.ObjectIDValue(id), nil
	default:
		return bacnet.ApplicationValue{}, fmt.Errorf("unknown value type %q", typ)
	}
}

func nonEmpty(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// TypeOfKind returns the value type that writes a value of the kind a property holds.
func TypeOfKind(k bacnet.ValueKind) (string, bool) {
	switch k {
	case bacnet.ValueBoolean:
		return TypeBool, true
	case bacnet.ValueUnsigned:
		return TypeUnsigned, true
	case bacnet.ValueSigned:
		return TypeSigned, true
	case bacnet.ValueReal:
		return TypeReal, true
	case bacnet.ValueDouble:
		return TypeDouble, true
	case bacnet.ValueOctetString:
		return TypeOctets, true
	case bacnet.ValueCharacterString:
		return TypeString, true
	case bacnet.ValueBitString:
		return TypeBits, true
	case bacnet.ValueEnumerated:
		return TypeEnum, true
	case bacnet.ValueDate:
		return TypeDate, true
	case bacnet.ValueTime:
		return TypeTime, true
	case bacnet.ValueObjectID:
		return TypeObject, true
	default:
		// Null, lists and constructed values say nothing about what to write.
		return "", false
	}
}

// ParseHex parses hex octets. Spaces, colons, dashes and commas between octets and 0x
// prefixes are ignored.
func ParseHex(s string) ([]byte, error) {
	var b strings.Builder
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ':' || r == '-' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	}) {
		field = strings.TrimPrefix(strings.TrimPrefix(field, "0x"), "0X")
		b.WriteString(field)
	}
	raw, err := hex.DecodeString(b.String())
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	return raw, nil
}

// ParseBits parses a bit string written as ones and zeros, the first bit first.
func ParseBits(s string) (bacnet.BitString, error) {
	s = strings.TrimSpace(s)
	out := bacnet.BitString{Bytes: make([]byte, (len(s)+7)/8)}
	for i, c := range s {
		switch c {
		case '1':
			out.Bytes[i/8] |= 0x80 >> (i % 8)
		case '0':
		default:
			return bacnet.BitString{}, fmt.Errorf("invalid bit %q", c)
		}
	}
	out.UnusedBits = uint8(len(out.Bytes)*8 - len(s))
	return out, nil
}

// ParseDate parses YYYY-MM-DD. A field written as * is unspecified.
func ParseDate(s string) (bacnet.Date, error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 3 {
		return bacnet.Date{}, fmt.Errorf("invalid date %q", s)
	}
	d := bacnet.Date{Year: 0xFF, Month: 0xFF, Day: 0xFF, Weekday: 0xFF}
	if parts[0] != "*" {
		y, err := strconv.Atoi(parts[0])
		if err != nil || y < 1900 || y > 2154 {
			return bacnet.Date{}, fmt.Errorf("invalid year in %q (1900-2154)", s)
		}
		d.Year = uint8(y - 1900)
	}
	if parts[1] != "*" {
		m, err := strconv.Atoi(parts[1])
		if err != nil || m < 1 || m > 12 {
			return bacnet.Date{}, fmt.Errorf("invalid month in %q", s)
		}
		d.Month = uint8(m)
	}
	if parts[2] != "*" {
		day, err := strconv.Atoi(parts[2])
		if err != nil || day < 1 || day > 31 {
			return bacnet.Date{}, fmt.Errorf("invalid day in %q", s)
		}
		d.Day = uint8(day)
	}
	if d.Year != 0xFF && d.Month != 0xFF && d.Day != 0xFF {
		t := time.Date(1900+int(d.Year), time.Month(d.Month), int(d.Day), 0, 0, 0, 0, time.UTC)
		if int(t.Month()) != int(d.Month) {
			return bacnet.Date{}, fmt.Errorf("%q is not a day of the calendar", s)
		}
		d.Weekday = Weekday(t)
	}
	return d, nil
}

// Weekday returns the BACnet day of week of t: 1 is Monday, 7 is Sunday.
func Weekday(t time.Time) uint8 {
	if wd := t.Weekday(); wd != time.Sunday {
		return uint8(wd)
	}
	return 7
}

// ParseTime parses HH:MM, HH:MM:SS or HH:MM:SS.hh.
func ParseTime(s string) (bacnet.Time, error) {
	s = strings.TrimSpace(s)
	hundredths := 0
	if main, frac, ok := strings.Cut(s, "."); ok {
		h, err := strconv.Atoi(frac)
		if err != nil || len(frac) != 2 || h < 0 {
			return bacnet.Time{}, fmt.Errorf("invalid hundredths in %q", s)
		}
		hundredths, s = h, main
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return bacnet.Time{}, fmt.Errorf("invalid time %q", s)
	}
	n := [3]int{}
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return bacnet.Time{}, fmt.Errorf("invalid time %q", s)
		}
		n[i] = v
	}
	if n[0] > 23 || n[1] > 59 || n[2] > 59 {
		return bacnet.Time{}, fmt.Errorf("invalid time %q", s)
	}
	return bacnet.Time{Hour: uint8(n[0]), Minute: uint8(n[1]), Second: uint8(n[2]), Hundredths: uint8(hundredths)}, nil
}

// DateTimeOf returns the BACnet date and time of t.
func DateTimeOf(t time.Time) (bacnet.Date, bacnet.Time) {
	return bacnet.Date{Year: uint8(t.Year() - 1900), Month: uint8(t.Month()), Day: uint8(t.Day()), Weekday: Weekday(t)},
		bacnet.Time{Hour: uint8(t.Hour()), Minute: uint8(t.Minute()), Second: uint8(t.Second()), Hundredths: uint8(t.Nanosecond() / 10_000_000)}
}
