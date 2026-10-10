// SPDX-License-Identifier: MIT

package bactest

import (
	"fmt"
	"math"
	"sort"

	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Property identifiers the model uses and the library has no constant for.
const (
	propAckedTransitions  bacnet.PropertyIdentifier = 0
	propAPDUTimeout       bacnet.PropertyIdentifier = 11
	propAppSoftware       bacnet.PropertyIdentifier = 12
	propCOVIncrement      bacnet.PropertyIdentifier = 22
	propEventEnable       bacnet.PropertyIdentifier = 35
	propFileAccessMethod  bacnet.PropertyIdentifier = 41
	propFileSize          bacnet.PropertyIdentifier = 42
	propFirmwareRevision  bacnet.PropertyIdentifier = 44
	propHighLimit         bacnet.PropertyIdentifier = 45
	propLocalDate         bacnet.PropertyIdentifier = 56
	propLocalTime         bacnet.PropertyIdentifier = 57
	propLocation          bacnet.PropertyIdentifier = 58
	propNotifyType        bacnet.PropertyIdentifier = 72
	propAPDURetries       bacnet.PropertyIdentifier = 73
	propNumberOfStates    bacnet.PropertyIdentifier = 74
	propOutOfService      bacnet.PropertyIdentifier = 81
	propObjectTypes       bacnet.PropertyIdentifier = 96
	propServices          bacnet.PropertyIdentifier = 97
	propStateText         bacnet.PropertyIdentifier = 110
	propUTCOffset         bacnet.PropertyIdentifier = 119
	propEventTimeStamps   bacnet.PropertyIdentifier = 130
	propLogEnable         bacnet.PropertyIdentifier = 133
	propRecordCount       bacnet.PropertyIdentifier = 141
	propDatabaseRevision  bacnet.PropertyIdentifier = 155
	propAll               bacnet.PropertyIdentifier = 8
	propOptional          bacnet.PropertyIdentifier = 80
	propRequired          bacnet.PropertyIdentifier = 105
	unitsCelsius                                    = 62
	unitsPercent                                    = 98
	unitsRelativeHumidity                           = 29
)

// Error classes and codes the model answers with.
type bacErr struct{ class, code uint16 }

var (
	errUnknownObject   = bacErr{bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject}
	errUnknownProperty = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty}
	errNotAnArray      = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodePropertyIsNotAnArray}
	errInvalidIndex    = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex}
	errWriteDenied     = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied}
	errInvalidDataType = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType}
	errOutOfRange      = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodeValueOutOfRange}
	errNotCOV          = bacErr{bacnet.ErrorClassObject, bacnet.ErrorCodeOptionalFunctionalityNotSupported}
	errPassword        = bacErr{bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure}
	errInvalidStamp    = bacErr{bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidTimeStamp}
	errInvalidState    = bacErr{bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidEventState}
	errFileAccess      = bacErr{bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileStartPosition}
	errNotATrend       = bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodePropertyIsNotAList}
	errNotCreatable    = bacErr{bacnet.ErrorClassObject, bacnet.ErrorCodeDynamicCreationNotSupported}
	errObjectExists    = bacErr{bacnet.ErrorClassObject, bacnet.ErrorCodeObjectIdentifierAlreadyExists}
	errNotDeletable    = bacErr{bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted}
)

func (e bacErr) response() *bacnet.ErrorResponse {
	return &bacnet.ErrorResponse{Class: e.class, Code: e.code}
}

// prop is one property of an object in the model.
type prop struct {
	id       bacnet.PropertyIdentifier
	value    bacnet.ApplicationValue
	array    bool
	required bool
}

// object is one object of the simulated device.
type object struct {
	id    bacnet.ObjectIdentifier
	props []*prop
	// commandable objects keep one value per priority and a relinquish default.
	commandable bool
	priorities  [16]*bacnet.ApplicationValue
	// low and high bound what may be written to the present value of an analog object.
	low, high float32
	// stamps are the event time stamps of the three transitions of an alarmed object.
	stamps [3]service.TimeStamp
	acked  [3]bool
	state  uint32
	// records is the buffer of a trend log, content the content of a file.
	records []service.LogRecord
	content []byte
	// dynamic objects were made with CreateObject, and only they can be deleted.
	dynamic bool
}

func (o *object) get(id bacnet.PropertyIdentifier) *prop {
	for _, p := range o.props {
		if p.id == id {
			return p
		}
	}
	return nil
}

func (o *object) set(id bacnet.PropertyIdentifier, v bacnet.ApplicationValue) {
	if p := o.get(id); p != nil {
		p.value = v
		return
	}
	o.props = append(o.props, &prop{id: id, value: v})
}

func str(s string) bacnet.ApplicationValue {
	return bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: s}}
}

// latin1 is a character string in ISO 8859-1, the character set of many older devices.
func latin1(s string) bacnet.ApplicationValue {
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		raw = append(raw, byte(r))
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Encoding: 5, Value: string(raw)}}
}

func bits(set ...bool) bacnet.ApplicationValue {
	b := bacnet.BitString{Bytes: make([]byte, (len(set)+7)/8)}
	for i, on := range set {
		if on {
			b.Bytes[i/8] |= 0x80 >> (i % 8)
		}
	}
	b.UnusedBits = uint8(len(b.Bytes)*8 - len(set))
	return bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: b}
}

func bitSet(n int, on ...int) bacnet.ApplicationValue {
	set := make([]bool, n)
	for _, i := range on {
		set[i] = true
	}
	return bits(set...)
}

func list(values ...bacnet.ApplicationValue) bacnet.ApplicationValue {
	els := make([]bacnet.Element, 0, len(values))
	for _, v := range values {
		els = append(els, bacnet.Element{Value: v})
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

func stampElement(ts service.TimeStamp) bacnet.Element {
	if ts.Choice == service.TimeStampSequence {
		return bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(uint64(ts.Sequence))}
	}
	return bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		{Value: bacnet.DateValue(ts.DateTime.Date)}, {Value: bacnet.TimeValue(ts.DateTime.Time)},
	}}}
}

// model is the object model of one simulated device.
type model struct {
	// noWildcard makes the device not know itself as device:4194303.
	noWildcard bool
	instance   uint32
	objects    []*object
	password   string
}

func (m *model) deviceID() bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: m.instance}
}

func (m *model) find(id bacnet.ObjectIdentifier) *object {
	if id == bacnet.AnyDevice() && !m.noWildcard {
		id = m.deviceID()
	}
	for _, o := range m.objects {
		if o.id == id {
			return o
		}
	}
	return nil
}

func (m *model) add(typ bacnet.ObjectType, instance uint32, name string, props ...*prop) *object {
	id := bacnet.ObjectIdentifier{Type: typ, Instance: instance}
	o := &object{id: id, props: []*prop{
		{id: bacnet.PropertyObjectIdentifier, value: bacnet.ObjectIDValue(id), required: true},
		{id: bacnet.PropertyObjectName, value: str(name), required: true},
		{id: bacnet.PropertyObjectType, value: bacnet.EnumValue(uint32(typ)), required: true},
	}}
	o.props = append(o.props, props...)
	m.objects = append(m.objects, o)
	return o
}

func req(id bacnet.PropertyIdentifier, v bacnet.ApplicationValue) *prop {
	return &prop{id: id, value: v, required: true}
}

func opt(id bacnet.PropertyIdentifier, v bacnet.ApplicationValue) *prop {
	return &prop{id: id, value: v}
}

func point(pv bacnet.ApplicationValue, units uint32, description string, flags bacnet.ApplicationValue, state uint32) []*prop {
	props := []*prop{
		req(bacnet.PropertyPresentValue, pv),
		opt(bacnet.PropertyDescription, str(description)),
		req(bacnet.PropertyStatusFlags, flags),
		req(bacnet.PropertyEventState, bacnet.EnumValue(state)),
		req(propOutOfService, bacnet.BoolValue(false)),
	}
	if units != math.MaxUint32 {
		props = append(props, req(bacnet.PropertyUnits, bacnet.EnumValue(units)))
	}
	return props
}

const noUnits = math.MaxUint32

func dateTime(day, hour, minute uint8) service.DateTime {
	// 2026-10-10 is a Saturday: day of week 6.
	return service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: day, Weekday: uint8((int(day)-10+6-1)%7 + 1)},
		Time: bacnet.Time{Hour: hour, Minute: minute}}
}

// newModel builds the demo device: a small air handling unit controller.
func newModel(instance uint32, name string) *model {
	m := &model{instance: instance}
	normal := bitSet(4)
	m.add(bacnet.ObjectTypeDevice, instance, name,
		req(bacnet.PropertySystemStatus, bacnet.EnumValue(0)),
		req(bacnet.PropertyVendorName, str("OT Fabric")),
		req(bacnet.PropertyVendorIdentifier, bacnet.UnsignedValue(999)),
		req(bacnet.PropertyModelName, str("bacsim")),
		req(propFirmwareRevision, str("1.4.2")),
		req(propAppSoftware, str("ahu-demo 2026.10")),
		opt(propLocation, str("Plant room 1")),
		opt(bacnet.PropertyDescription, str("Simulated air handling unit controller")),
		req(bacnet.PropertyProtocolVersion, bacnet.UnsignedValue(1)),
		req(bacnet.PropertyProtocolRevision, bacnet.UnsignedValue(22)),
		req(propServices, bitSet(49, 0, 5, 6, 7, 12, 14, 15, 17, 20, 26, 27, 28, 32, 33, 34, 35, 36, 38, 39)),
		req(propObjectTypes, bitSet(65, 0, 1, 2, 3, 5, 8, 10, 19, 20)),
		&prop{id: bacnet.PropertyObjectList, array: true, required: true},
		req(bacnet.PropertyMaxAPDULength, bacnet.UnsignedValue(1476)),
		req(bacnet.PropertySegmentation, bacnet.EnumValue(3)),
		req(propAPDUTimeout, bacnet.UnsignedValue(3000)),
		req(propAPDURetries, bacnet.UnsignedValue(3)),
		req(propDatabaseRevision, bacnet.UnsignedValue(7)),
		opt(propLocalDate, bacnet.DateValue(dateTime(10, 0, 0).Date)),
		opt(propLocalTime, bacnet.TimeValue(bacnet.Time{Hour: 12})),
		opt(propUTCOffset, bacnet.SignedValue(-60)),
	)
	ai1 := m.add(bacnet.ObjectTypeAnalogInput, 1, "Supply Air Temp",
		point(bacnet.RealValue(21.5), unitsCelsius, "AHU-1 supply air temperature", normal, 0)...)
	ai1.set(propCOVIncrement, bacnet.RealValue(0.5))
	ai1.low, ai1.high = -50, 150

	ai2 := m.add(bacnet.ObjectTypeAnalogInput, 2, "Return Air Humidity",
		point(bacnet.RealValue(71.25), unitsRelativeHumidity, "AHU-1 return air humidity", bitSet(4, 0), 3)...)
	ai2.low, ai2.high = 0, 100
	ai2.state = 3
	ai2.stamps = [3]service.TimeStamp{
		{Choice: service.TimeStampDateTime, DateTime: dateTime(10, 9, 30)},
		{Choice: service.TimeStampSequence, Sequence: 0},
		{Choice: service.TimeStampDateTime, DateTime: dateTime(9, 18, 5)},
	}
	ai2.acked = [3]bool{false, true, true}
	ai2.props = append(ai2.props,
		opt(propHighLimit, bacnet.RealValue(65)),
		opt(propNotifyType, bacnet.EnumValue(0)),
		opt(propEventEnable, bitSet(3, 0, 1, 2)),
		&prop{id: propAckedTransitions},
		&prop{id: propEventTimeStamps, array: true},
	)

	ao1 := m.add(bacnet.ObjectTypeAnalogOutput, 1, "Heating Valve",
		point(bacnet.RealValue(0), unitsPercent, "AHU-1 heating coil valve", normal, 0)...)
	ao1.makeCommandable(bacnet.RealValue(0), 0, 100)

	av1 := m.add(bacnet.ObjectTypeAnalogValue, 1, "Supply Air Setpoint",
		point(bacnet.RealValue(20), unitsCelsius, "AHU-1 supply air temperature setpoint", normal, 0)...)
	av1.makeCommandable(bacnet.RealValue(20), 10, 30)

	m.add(bacnet.ObjectTypeBinaryInput, 1, "Filter Alarm",
		point(bacnet.EnumValue(0), noUnits, "", normal, 0)...).set(bacnet.PropertyDescription, latin1("Filterüberwachung AHU-1"))
	m.add(bacnet.ObjectTypeBinaryValue, 1, "Fan Enable",
		point(bacnet.EnumValue(1), noUnits, "AHU-1 supply fan enable", normal, 0)...)
	m.add(bacnet.ObjectTypeMultiStateValue, 1, "Operating Mode",
		append(point(bacnet.UnsignedValue(2), noUnits, "AHU-1 operating mode", normal, 0),
			req(propNumberOfStates, bacnet.UnsignedValue(3)),
			&prop{id: propStateText, array: true, value: list(str("Off"), str("Auto"), str("Manual"))})...)

	tl := m.add(bacnet.ObjectTypeTrendLog, 1, "Supply Air Temp Log",
		opt(bacnet.PropertyDescription, str("15 minute log of analog-input:1")),
		req(propLogEnable, bacnet.BoolValue(true)),
		req(propRecordCount, bacnet.UnsignedValue(0)),
		req(bacnet.PropertyEventState, bacnet.EnumValue(0)),
		&prop{id: bacnet.PropertyLogBuffer, required: true},
	)
	flags := bitSet(4).BitString
	for i, v := range []float32{20.75, 21, 21.25, 21.5, 21.5, 21.75} {
		tl.records = append(tl.records, service.LogRecord{
			Timestamp: dateTime(10, 10+uint8(i/4), uint8(i%4)*15), DatumChoice: service.LogDatumReal, Datum: bacnet.RealValue(v), StatusFlags: &flags,
		})
	}
	tl.records = append(tl.records, service.LogRecord{
		Timestamp: dateTime(10, 11, 30), DatumChoice: service.LogDatumLogStatus, Datum: bitSet(3, 2),
	})
	tl.set(propRecordCount, bacnet.UnsignedValue(uint64(len(tl.records))))

	file := m.add(bacnet.ObjectTypeFile, 1, "config.txt",
		opt(bacnet.PropertyDescription, str("Controller configuration")),
		req(propFileSize, bacnet.UnsignedValue(0)),
		req(propFileAccessMethod, bacnet.EnumValue(1)),
	)
	file.content = []byte("# AHU-1 controller configuration\nsupply.setpoint = 20.0\nfan.minimum = 30\nfilter.alarm.delay = 600\n")
	return m
}

func (o *object) makeCommandable(relinquish bacnet.ApplicationValue, low, high float32) {
	o.commandable = true
	o.low, o.high = low, high
	o.props = append(o.props,
		&prop{id: bacnet.PropertyPriorityArray, array: true, required: true},
		req(bacnet.PropertyRelinquishDefault, relinquish),
	)
}

// refresh computes the properties that follow from the state of the object.
func (m *model) refresh(o *object) {
	if p := o.get(bacnet.PropertyObjectList); p != nil {
		values := make([]bacnet.ApplicationValue, 0, len(m.objects))
		for _, x := range m.objects {
			values = append(values, bacnet.ObjectIDValue(x.id))
		}
		p.value = list(values...)
	}
	if p := o.get(bacnet.PropertyPriorityArray); p != nil {
		values := make([]bacnet.ApplicationValue, 16)
		for i, v := range o.priorities {
			if v != nil {
				values[i] = *v
			}
		}
		p.value = list(values...)
		pv := o.get(bacnet.PropertyRelinquishDefault).value
		for _, v := range o.priorities {
			if v != nil {
				pv = *v
				break
			}
		}
		o.set(bacnet.PropertyPresentValue, pv)
	}
	if p := o.get(propAckedTransitions); p != nil {
		p.value = bits(o.acked[0], o.acked[1], o.acked[2])
	}
	if p := o.get(propEventTimeStamps); p != nil {
		p.value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			stampElement(o.stamps[0]), stampElement(o.stamps[1]), stampElement(o.stamps[2]),
		}}
	}
	if p := o.get(propFileSize); p != nil {
		p.value = bacnet.UnsignedValue(uint64(len(o.content)))
	}
}

// propertyList is the value of property-list: every property but the three every object
// has, and property-list itself.
func (o *object) propertyList() bacnet.ApplicationValue {
	var values []bacnet.ApplicationValue
	for _, p := range o.props {
		switch p.id {
		case bacnet.PropertyObjectIdentifier, bacnet.PropertyObjectName, bacnet.PropertyObjectType:
		default:
			values = append(values, bacnet.EnumValue(uint32(p.id)))
		}
	}
	return list(values...)
}

// read returns the value of a property, or of one element of an array property.
func (m *model) read(id bacnet.ObjectIdentifier, ref bacnet.PropertyReference) (bacnet.ApplicationValue, *bacErr) {
	o := m.find(id)
	if o == nil {
		return bacnet.ApplicationValue{}, &errUnknownObject
	}
	m.refresh(o)
	var value bacnet.ApplicationValue
	array := false
	switch p := o.get(ref.Identifier); {
	case ref.Identifier == bacnet.PropertyPropertyList:
		value, array = o.propertyList(), true
	case p == nil, ref.Identifier == bacnet.PropertyLogBuffer:
		// The log buffer is only readable with ReadRange.
		if p != nil {
			return bacnet.ApplicationValue{}, &bacErr{bacnet.ErrorClassProperty, bacnet.ErrorCodeReadAccessDenied}
		}
		return bacnet.ApplicationValue{}, &errUnknownProperty
	default:
		value, array = p.value, p.array
	}
	if ref.ArrayIndex == nil || *ref.ArrayIndex == bacnet.ArrayAll {
		return value, nil
	}
	if !array {
		return bacnet.ApplicationValue{}, &errNotAnArray
	}
	idx := int(*ref.ArrayIndex)
	switch {
	case idx == 0:
		return bacnet.UnsignedValue(uint64(len(value.Elements))), nil
	case idx > len(value.Elements):
		return bacnet.ApplicationValue{}, &errInvalidIndex
	}
	el := value.Elements[idx-1]
	if el.Context {
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{el}}, nil
	}
	return el.Value, nil
}

// expand lists the properties a special property identifier of ReadPropertyMultiple
// stands for.
func (o *object) expand(special bacnet.PropertyIdentifier) []bacnet.PropertyIdentifier {
	var out []bacnet.PropertyIdentifier
	for _, p := range o.props {
		if special == propAll || (special == propRequired) == p.required {
			out = append(out, p.id)
		}
	}
	return out
}

// write applies a WriteProperty and reports whether the present value changed.
func (m *model) write(r service.WritePropertyRequest) (changed bool, e *bacErr) {
	o := m.find(r.Object)
	if o == nil {
		return false, &errUnknownObject
	}
	p := o.get(r.Property.Identifier)
	if p == nil {
		return false, &errUnknownProperty
	}
	if r.Property.ArrayIndex != nil {
		return false, &errWriteDenied
	}
	switch r.Property.Identifier {
	case bacnet.PropertyPresentValue:
		return m.writePresentValue(o, r)
	case bacnet.PropertyObjectName, bacnet.PropertyDescription:
		if r.Value.Kind != bacnet.ValueCharacterString {
			return false, &errInvalidDataType
		}
	case propOutOfService:
		if r.Value.Kind != bacnet.ValueBoolean {
			return false, &errInvalidDataType
		}
		// Out of service shows in the status flags.
		o.set(bacnet.PropertyStatusFlags, bits(o.state != 0, false, false, r.Value.Boolean))
	default:
		return false, &errWriteDenied
	}
	p.value = r.Value.Clone()
	return r.Property.Identifier == propOutOfService, nil
}

func (m *model) writePresentValue(o *object, r service.WritePropertyRequest) (bool, *bacErr) {
	before := o.get(bacnet.PropertyPresentValue).value
	switch o.id.Type {
	case bacnet.ObjectTypeAnalogInput:
		if !o.get(propOutOfService).value.Boolean {
			return false, &errWriteDenied
		}
		fallthrough
	case bacnet.ObjectTypeAnalogOutput, bacnet.ObjectTypeAnalogValue:
		relinquish := r.Value.Kind == bacnet.ValueNull && o.commandable
		if !relinquish {
			if r.Value.Kind != bacnet.ValueReal {
				return false, &errInvalidDataType
			}
			if r.Value.Real < o.low || r.Value.Real > o.high {
				return false, &errOutOfRange
			}
		}
		if !o.commandable {
			o.set(bacnet.PropertyPresentValue, r.Value)
			break
		}
		priority := 16
		if r.Priority != nil {
			priority = int(*r.Priority)
		}
		if priority == 6 {
			// Priority 6 is reserved for the minimum on/off algorithm.
			return false, &errWriteDenied
		}
		if relinquish {
			o.priorities[priority-1] = nil
		} else {
			v := r.Value
			o.priorities[priority-1] = &v
		}
		m.refresh(o)
	case bacnet.ObjectTypeBinaryValue:
		if r.Value.Kind != bacnet.ValueEnumerated {
			return false, &errInvalidDataType
		}
		if r.Value.Enumerated > 1 {
			return false, &errOutOfRange
		}
		o.set(bacnet.PropertyPresentValue, r.Value)
	case bacnet.ObjectTypeMultiStateValue:
		if r.Value.Kind != bacnet.ValueUnsigned {
			return false, &errInvalidDataType
		}
		if r.Value.Unsigned < 1 || r.Value.Unsigned > 3 {
			return false, &errOutOfRange
		}
		o.set(bacnet.PropertyPresentValue, r.Value)
	default:
		return false, &errWriteDenied
	}
	after := o.get(bacnet.PropertyPresentValue).value
	return before.Kind != after.Kind || before.Real != after.Real || before.Enumerated != after.Enumerated || before.Unsigned != after.Unsigned, nil
}

// alarms lists the objects that are in an alarm state or have an unacknowledged transition.
func (m *model) alarms() []*object {
	var out []*object
	for _, o := range m.objects {
		if o.get(propEventTimeStamps) != nil && (o.state != 0 || !o.acked[0] || !o.acked[1] || !o.acked[2]) {
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].id.Instance < out[j].id.Instance })
	return out
}

// acknowledge applies an AcknowledgeAlarm.
func (m *model) acknowledge(r service.AcknowledgeAlarmRequest) *bacErr {
	o := m.find(r.EventObject)
	if o == nil {
		return &errUnknownObject
	}
	if o.get(propEventTimeStamps) == nil {
		return &bacErr{bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured}
	}
	idx := 0
	switch r.EventStateAcked {
	case 0:
		idx = 2
	case 1:
		idx = 1
	}
	if idx == 0 && r.EventStateAcked != o.state {
		return &errInvalidState
	}
	if r.TimeStamp != o.stamps[idx] {
		return &errInvalidStamp
	}
	o.acked[idx] = true
	return nil
}

// create carries out CreateObject. The device makes analog and binary values; an initial
// value it refuses leaves no object behind, and failed is the number of that value.
func (m *model) create(r service.CreateObjectRequest) (id bacnet.ObjectIdentifier, failed uint32, e *bacErr) {
	var typ bacnet.ObjectType
	instance, named := uint32(0), false
	switch {
	case r.ObjectIdentifier != nil:
		typ, instance, named = r.ObjectIdentifier.Type, r.ObjectIdentifier.Instance, true
	case r.ObjectType != nil:
		typ = *r.ObjectType
	}
	if typ != bacnet.ObjectTypeAnalogValue && typ != bacnet.ObjectTypeBinaryValue {
		return id, 0, &errNotCreatable
	}
	if !named {
		for _, o := range m.objects {
			if o.id.Type == typ && o.id.Instance >= instance {
				instance = o.id.Instance + 1
			}
		}
	}
	id = bacnet.ObjectIdentifier{Type: typ, Instance: instance}
	if m.find(id) != nil {
		return id, 0, &errObjectExists
	}
	pv, units := bacnet.RealValue(0), uint32(unitsPercent)
	if typ == bacnet.ObjectTypeBinaryValue {
		pv, units = bacnet.EnumValue(0), noUnits
	}
	o := m.add(typ, instance, fmt.Sprintf("%s-%d", map[bool]string{true: "AV", false: "BV"}[typ == bacnet.ObjectTypeAnalogValue], instance),
		point(pv, units, "", bits(false, false, false, false), 0)...)
	o.dynamic = true
	o.low, o.high = -1e9, 1e9
	for i, init := range r.InitialValues {
		if _, e := m.write(service.WritePropertyRequest{Object: id, Property: init.Property, Value: init.Value, Priority: init.Priority}); e != nil {
			m.remove(id)
			return id, uint32(i + 1), e
		}
	}
	return id, 0, nil
}

func (m *model) remove(id bacnet.ObjectIdentifier) {
	for i, o := range m.objects {
		if o.id == id {
			m.objects = append(m.objects[:i], m.objects[i+1:]...)
			return
		}
	}
}

// delete carries out DeleteObject: only what CreateObject made can go.
func (m *model) delete(id bacnet.ObjectIdentifier) *bacErr {
	o := m.find(id)
	switch {
	case o == nil:
		return &errUnknownObject
	case !o.dynamic:
		return &errNotDeletable
	}
	m.remove(o.id)
	return nil
}
