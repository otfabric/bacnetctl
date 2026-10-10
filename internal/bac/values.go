// SPDX-License-Identifier: MIT

package bac

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Character sets of a BACnet character string (clause 20.2.9).
const (
	charsetUTF8     = 0
	charsetUCS4     = 3
	charsetUCS2     = 4
	charsetISO88591 = 5
)

// Text returns a character string as UTF-8, whatever the device sent. The conversion is
// that of go-bacnet (UTF-8, UCS-2, UCS-4, ISO 8859-1). What it refuses is still shown,
// as well as it can be: a string with octets that do not fit its character set is
// converted leniently, and the octets of the two legacy double-byte sets (IBM/Microsoft
// DBCS and JIS X 0208) are returned as they are, with what is no valid UTF-8 replaced by
// U+FFFD.
func Text(s bacnet.CharacterString) string {
	if text, err := s.Text(); err == nil {
		return text
	}
	raw := []byte(s.Value)
	switch s.Encoding {
	case charsetUCS2:
		u := make([]uint16, 0, len(raw)/2)
		for i := 0; i+1 < len(raw); i += 2 {
			u = append(u, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
		return string(utf16.Decode(u))
	case charsetUCS4:
		var b strings.Builder
		for i := 0; i+3 < len(raw); i += 4 {
			r := rune(uint32(raw[i])<<24 | uint32(raw[i+1])<<16 | uint32(raw[i+2])<<8 | uint32(raw[i+3]))
			if !utf8.ValidRune(r) {
				r = utf8.RuneError
			}
			b.WriteRune(r)
		}
		return b.String()
	case charsetISO88591:
		r := make([]rune, len(raw))
		for i, c := range raw {
			r[i] = rune(c)
		}
		return string(r)
	default:
		return strings.ToValidUTF8(s.Value, string(utf8.RuneError))
	}
}

// DateText writes a BACnet date as YYYY-MM-DD, with * for a field that is unspecified.
func DateText(d bacnet.Date) string {
	year, month, day := "*", "*", "*"
	if d.Year != 0xFF {
		year = fmt.Sprintf("%04d", 1900+int(d.Year))
	}
	switch {
	case d.Month == 13:
		month = "odd"
	case d.Month == 14:
		month = "even"
	case d.Month != 0xFF:
		month = fmt.Sprintf("%02d", d.Month)
	}
	switch {
	case d.Day == 32:
		day = "last"
	case d.Day != 0xFF:
		day = fmt.Sprintf("%02d", d.Day)
	}
	return year + "-" + month + "-" + day
}

// TimeText writes a BACnet time as HH:MM:SS.hh, with * for a field that is unspecified.
func TimeText(t bacnet.Time) string {
	f := func(v uint8) string {
		if v == 0xFF {
			return "*"
		}
		return fmt.Sprintf("%02d", v)
	}
	return f(t.Hour) + ":" + f(t.Minute) + ":" + f(t.Second) + "." + f(t.Hundredths)
}

// BitText writes a bit string as ones and zeros, the first bit first.
func BitText(b bacnet.BitString) string {
	var sb strings.Builder
	for _, set := range BitList(b) {
		if set {
			sb.WriteByte('1')
		} else {
			sb.WriteByte('0')
		}
	}
	return sb.String()
}

// floatValue renders a floating point number. JSON has no NaN and no infinities: those are
// written as strings.
func floatValue(typ string, f float64, bits int) types.Value {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		s := strconv.FormatFloat(f, 'g', -1, bits)
		return types.Value{Type: typ, Value: s, Text: s}
	}
	return types.Value{Type: typ, Value: jsonFloat{f, bits}, Text: types.FormatFloat(f, bits)}
}

// jsonFloat marshals a float32 without the noise of its conversion to float64: 21.5 and
// 0.1, not 0.10000000149011612.
type jsonFloat struct {
	f    float64
	bits int
}

// MarshalJSON implements json.Marshaler.
func (j jsonFloat) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(j.f, 'g', -1, j.bits)), nil
}

// Float64 returns the number.
func (j jsonFloat) Float64() float64 { return j.f }

// ValueOf turns a decoded BACnet value into its output model. objType and prop say where
// the value comes from: they name enumerated values and the bits of bit strings.
func ValueOf(objType bacnet.ObjectType, prop bacnet.PropertyIdentifier, v bacnet.ApplicationValue) types.Value {
	switch v.Kind {
	case bacnet.ValueNull:
		return types.Value{Type: types.TypeNull, Value: nil, Text: "null"}
	case bacnet.ValueBoolean:
		return types.Value{Type: types.TypeBoolean, Value: v.Boolean, Text: strconv.FormatBool(v.Boolean)}
	case bacnet.ValueUnsigned:
		return types.Value{Type: types.TypeUnsigned, Value: v.Unsigned, Text: strconv.FormatUint(v.Unsigned, 10)}
	case bacnet.ValueSigned:
		return types.Value{Type: types.TypeSigned, Value: v.Signed, Text: strconv.FormatInt(v.Signed, 10)}
	case bacnet.ValueReal:
		return floatValue(types.TypeReal, float64(v.Real), 32)
	case bacnet.ValueDouble:
		return floatValue(types.TypeDouble, v.Double, 64)
	case bacnet.ValueOctetString:
		s := hex.EncodeToString(v.OctetString)
		return types.Value{Type: types.TypeOctetString, Value: s, Text: s}
	case bacnet.ValueCharacterString:
		s := Text(v.Character)
		return types.Value{Type: types.TypeCharacterString, Value: s, Text: s}
	case bacnet.ValueBitString:
		bits := BitText(v.BitString)
		out := types.Value{Type: types.TypeBitString, Value: bits, Text: bits}
		if names := bitNames(prop); names != nil {
			out.Bits = SetBitNames(v.BitString, names)
			out.Text = "{" + strings.Join(out.Bits, ", ") + "}"
		}
		return out
	case bacnet.ValueEnumerated:
		out := types.Value{Type: types.TypeEnumerated, Value: v.Enumerated, Text: strconv.FormatUint(uint64(v.Enumerated), 10)}
		if name := EnumName(objType, prop, v.Enumerated); name != "" {
			out.Name = name
			out.Text = fmt.Sprintf("%s (%d)", name, v.Enumerated)
		}
		return out
	case bacnet.ValueDate:
		s := DateText(v.Date)
		return types.Value{Type: types.TypeDate, Value: s, Text: s}
	case bacnet.ValueTime:
		s := TimeText(v.Time)
		return types.Value{Type: types.TypeTime, Value: s, Text: s}
	case bacnet.ValueObjectID:
		s := ObjectName(v.ObjectID)
		t, inst := uint16(v.ObjectID.Type), v.ObjectID.Instance
		return types.Value{Type: types.TypeObjectID, Value: s, Text: s, ObjectType: &t, Instance: &inst}
	case bacnet.ValueConstructed:
		if prop == propEventTimeStamps {
			if stamps, err := timeStamps(v); err == nil {
				return stampList(stamps)
			}
		}
		return listValue(objType, prop, v.Elements)
	default:
		// A context-tagged primitive: its type is not on the wire, only its octets.
		s := hex.EncodeToString(v.OctetString)
		return types.Value{Type: types.TypeContext, Value: s, Text: "0x" + s}
	}
}

func listValue(objType bacnet.ObjectType, prop bacnet.PropertyIdentifier, elements []bacnet.Element) types.Value {
	items := make([]types.Value, 0, len(elements))
	texts := make([]string, 0, len(elements))
	for _, el := range elements {
		item := ValueOf(objType, prop, el.Value)
		if el.Context {
			tag := el.TagNumber
			item.Tag = &tag
			if el.Value.Kind != bacnet.ValueConstructed {
				// The content octets of a context-tagged primitive, whatever kind the
				// decoder assigned.
				raw := hex.EncodeToString(el.Value.OctetString)
				item = types.Value{Type: types.TypeContext, Value: raw, Text: "0x" + raw, Tag: &tag}
			}
			item.Text = fmt.Sprintf("[%d] %s", tag, item.Text)
		}
		items = append(items, item)
		texts = append(texts, item.Text)
	}
	return types.Value{Type: types.TypeList, Value: items, Text: "[" + strings.Join(texts, ", ") + "]"}
}

// KindName returns the type name of a value kind, as used in output and in value
// specifications.
func KindName(k bacnet.ValueKind) string {
	return ValueOf(0, 0, bacnet.ApplicationValue{Kind: k}).Type
}

// stampList renders a list of BACnetTimeStamp, as in event-time-stamps.
func stampList(stamps []service.TimeStamp) types.Value {
	items := make([]types.Value, 0, len(stamps))
	texts := make([]string, 0, len(stamps))
	for _, ts := range stamps {
		text := TimeStampText(ts)
		items = append(items, types.Value{Type: types.TypeTimeStamp, Value: text, Text: text})
		texts = append(texts, text)
	}
	return types.Value{Type: types.TypeList, Value: items, Text: "[" + strings.Join(texts, ", ") + "]"}
}

// AttachUnits gives the present value of every object in rows the unit of its units
// property, when both were read.
func AttachUnits(rows []types.PropertyRow) {
	units := map[string]uint32{}
	for _, r := range rows {
		if r.PropertyID == uint32(bacnet.PropertyUnits) && r.Value != nil && r.Value.Type == types.TypeEnumerated {
			if u, ok := r.Value.Value.(uint32); ok {
				units[r.Object] = u
			}
		}
	}
	for i, r := range rows {
		u, ok := units[r.Object]
		if ok && r.PropertyID == uint32(bacnet.PropertyPresentValue) && r.Index == nil && r.Value != nil {
			rows[i].Value.Unit = UnitSymbol(u)
		}
	}
}
