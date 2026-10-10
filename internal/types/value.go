// SPDX-License-Identifier: MIT

// Package types holds the output models of bacnetctl: what a command prints as text, as a
// table or as JSON.
package types

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Type names of a Value.
const (
	TypeNull            = "null"
	TypeBoolean         = "boolean"
	TypeUnsigned        = "unsigned"
	TypeSigned          = "signed"
	TypeReal            = "real"
	TypeDouble          = "double"
	TypeOctetString     = "octet-string"
	TypeCharacterString = "character-string"
	TypeBitString       = "bit-string"
	TypeEnumerated      = "enumerated"
	TypeDate            = "date"
	TypeTime            = "time"
	TypeObjectID        = "object-identifier"
	TypeList            = "list"
	TypeContext         = "context"
	TypeTimeStamp       = "time-stamp"
)

// Value is one BACnet value in output.
//
// In JSON, value holds a native type: null, a boolean, a number (unsigned, signed, real,
// double, enumerated), a string (character strings; octet strings as hex; bit strings as
// "1011"; dates as 2026-10-10; times as 12:00:00.00; object identifiers as
// analog-input:1; time stamps as "2026-10-10 12:00:00.00" or "sequence 42") or an array of
// values (lists and constructed values).
type Value struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
	// Name is the name of an enumerated value, where bacnetctl knows it.
	Name string `json:"name,omitempty"`
	// Bits are the names of the bits that are set in a bit string, where known.
	Bits []string `json:"bits,omitempty"`
	// Unit is the symbol of the engineering unit, when the value was read with its units.
	Unit string `json:"unit,omitempty"`
	// ObjectType and Instance are the numbers of an object identifier.
	ObjectType *uint16 `json:"object_type,omitempty"`
	Instance   *uint32 `json:"instance,omitempty"`
	// Tag is the context tag number of an element of a constructed value.
	Tag *uint8 `json:"tag,omitempty"`
	// Text is how the value reads for a person.
	Text string `json:"-"`
}

// MarshalJSON implements json.Marshaler. The bits of a bit string bacnetctl has names for
// are always there, also when none is set: an empty list says "no flag", an absent one
// "no names".
func (v Value) MarshalJSON() ([]byte, error) {
	type plain Value
	out := struct {
		plain
		Bits *[]string `json:"bits,omitempty"`
	}{plain: plain(v)}
	if v.Bits != nil {
		out.Bits = &v.Bits
	}
	return json.Marshal(out)
}

// String returns the value as text, with its unit when it has one.
func (v Value) String() string {
	if v.Unit != "" {
		return v.Text + " " + v.Unit
	}
	return v.Text
}

// TextOf returns the text of v, or a dash when there is no value.
func TextOf(v *Value) string {
	if v == nil {
		return "-"
	}
	return v.String()
}

// FormatFloat writes a number the way a person would: the shortest text that reads back
// as the same value, without exponent for ordinary magnitudes.
func FormatFloat(f float64, bits int) string {
	s := strconv.FormatFloat(f, 'g', -1, bits)
	if strings.ContainsAny(s, "e") {
		if alt := strconv.FormatFloat(f, 'f', -1, bits); len(alt) <= 16 {
			return alt
		}
	}
	return s
}

// ErrorText returns "error: message" for a row that failed.
func ErrorText(e *ErrorInfo) string {
	if e == nil {
		return ""
	}
	return "error: " + e.Message
}
