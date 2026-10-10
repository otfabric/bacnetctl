// SPDX-License-Identifier: MIT

package bactest

import (
	"net/netip"

	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// request is one request to one of the devices of the simulation.
type request struct {
	d    *Device
	m    *model
	from netip.AddrPort
	// source is the BACnet address the answer comes from, for the device behind the
	// router; client is the BACnet address of a client behind another router.
	source bacnet.Address
	client bacnet.Address
	// after is what follows the answer: the notifications a request causes.
	after []func()
	// errorPayload replaces the class and code of an Error for the services whose Error
	// carries more than that.
	errorPayload []byte
}

func (r *request) reply(raw []byte) {
	_ = r.d.send(r.from, r.source, r.client, raw)
}

func (r *request) unconfirmed(u *apdu.UnconfirmedRequest, limits bacnet.DecodeLimits) {
	d := r.d
	switch u.ServiceChoice {
	case apdu.ServiceWhoIs:
		d.record(WhoIs)
		w, err := service.DecodeWhoIs(u.Payload, limits)
		if err != nil || d.without[WhoIs] {
			return
		}
		if w.LowLimit != nil && w.HighLimit != nil && (r.m.instance < *w.LowLimit || r.m.instance > *w.HighLimit) {
			return
		}
		raw, err := service.EncodeIAmAPDU(service.IAm{Device: r.m.deviceID(), MaxAPDULength: 1476, Segmentation: 3, VendorID: 999})
		if err == nil {
			r.reply(raw)
		}
	case apdu.ServiceWhoHas:
		d.record(WhoHas)
		w, err := service.DecodeWhoHas(u.Payload, limits)
		if err != nil || d.without[WhoHas] {
			return
		}
		if w.LowLimit != nil && w.HighLimit != nil && (r.m.instance < *w.LowLimit || r.m.instance > *w.HighLimit) {
			return
		}
		for _, o := range r.m.objects {
			name := o.get(bacnet.PropertyObjectName).value.Character
			if (w.Object != nil && *w.Object == o.id) || (w.Name != nil && w.Name.Value == name.Value) {
				if raw, err := service.EncodeIHaveAPDU(service.IHave{Device: r.m.deviceID(), Object: o.id, Name: name}); err == nil {
					r.reply(raw)
				}
			}
		}
	case apdu.ServiceTimeSynchronization, apdu.ServiceUTCTimeSynchronization:
		name := TimeSync
		if u.ServiceChoice == apdu.ServiceUTCTimeSynchronization {
			name = UTCTimeSync
		}
		if t, err := service.DecodeTimeSynchronization(u.Payload, limits); err == nil {
			d.timeSync = &t
		}
		d.record(name)
	}
}

func (r *request) confirmed(c *apdu.ConfirmedRequest, limits bacnet.DecodeLimits) {
	d := r.d
	name, known := confirmedNames[c.ServiceChoice]
	if !known || d.without[name] {
		d.record("unknown")
		r.reply(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: c.InvokeID, Reason: bacnet.RejectReasonUnrecognizedService}))
		return
	}
	reply, e, reject := r.serve(c, limits)
	// Recorded once the request was carried out: a test that waits for it sees its effect.
	d.record(name)
	if d.mute[name] {
		return
	}
	switch {
	case reject:
		r.reply(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: c.InvokeID, Reason: bacnet.RejectReasonInvalidTag}))
	case e != nil:
		payload, _ := bacnet.AppendApplicationValue(nil, bacnet.EnumValue(uint32(e.class)))
		payload, _ = bacnet.AppendApplicationValue(payload, bacnet.EnumValue(uint32(e.code)))
		if r.errorPayload != nil {
			payload = r.errorPayload
		}
		r.reply(apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: c.InvokeID, ServiceChoice: c.ServiceChoice, Payload: payload}))
	case reply == nil:
		r.reply(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: c.InvokeID, ServiceChoice: c.ServiceChoice}))
	default:
		raw := apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: c.InvokeID, ServiceChoice: c.ServiceChoice, Payload: reply})
		if d.maxAPDU > 0 && len(raw) > d.maxAPDU {
			raw = apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: c.InvokeID, Reason: bacnet.AbortReasonSegmentationNotSupported})
		}
		r.reply(raw)
	}
	// What follows an acknowledgement: the notifications a change or a subscription causes.
	for _, f := range r.after {
		f()
	}
}

// serve carries out a confirmed request. A nil reply without error is a SimpleACK.
func (r *request) serve(c *apdu.ConfirmedRequest, limits bacnet.DecodeLimits) (reply []byte, e *bacErr, reject bool) {
	d, m := r.d, r.m
	fail := func(err error) ([]byte, *bacErr, bool) { return nil, nil, err != nil }
	switch c.ServiceChoice {
	case apdu.ServiceReadProperty:
		req, err := service.DecodeReadProperty(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		v, e := m.read(req.Object, req.Property)
		if e != nil {
			return nil, e, false
		}
		reply, err = service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: m.find(req.Object).id, Property: req.Property, Value: v})
		return reply, nil, err != nil
	case apdu.ServiceReadPropertyMultiple:
		specs, err := decodeRPM(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		reply, err = service.EncodeReadPropertyMultipleACK(m.readMultiple(specs))
		return reply, nil, err != nil
	case apdu.ServiceWriteProperty:
		req, err := service.DecodeWriteProperty(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		changed, e := m.write(req)
		if e == nil && changed {
			object := m.find(req.Object).id
			r.after = append(r.after, func() { d.notify(m, object) })
		}
		return nil, e, false
	case apdu.ServiceWritePropertyMultiple:
		specs, err := service.DecodeWritePropertyMultiple(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		// In order, and no further than the first write the device refuses.
		for _, spec := range specs {
			for _, p := range spec.Properties {
				changed, e := m.write(service.WritePropertyRequest{Object: spec.Object, Property: p.Property, Value: p.Value, Priority: p.Priority})
				if e != nil {
					r.errorPayload, _ = service.EncodeWritePropertyMultipleError(service.WritePropertyMultipleError{
						Class: e.class, Code: e.code, FirstFailed: spec.Object, FirstProperty: p.Property,
					})
					return nil, e, false
				}
				if changed {
					object := m.find(spec.Object).id
					r.after = append(r.after, func() { d.notify(m, object) })
				}
			}
		}
		return nil, nil, false
	case apdu.ServiceCreateObject:
		req, err := service.DecodeCreateObject(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		id, failed, e := m.create(req)
		if e != nil {
			// CreateObject-Error: the error [0] and the first failed element number [1].
			r.errorPayload = []byte{0x0E}
			r.errorPayload, _ = bacnet.AppendApplicationValue(r.errorPayload, bacnet.EnumValue(uint32(e.class)))
			r.errorPayload, _ = bacnet.AppendApplicationValue(r.errorPayload, bacnet.EnumValue(uint32(e.code)))
			r.errorPayload = append(r.errorPayload, 0x0F)
			r.errorPayload, _ = bacnet.AppendContextUnsigned(r.errorPayload, 1, uint64(failed))
			return nil, e, false
		}
		reply, err = service.EncodeCreateObjectACK(service.CreateObjectACK{Object: id})
		return reply, nil, err != nil
	case apdu.ServiceDeleteObject:
		req, err := service.DecodeDeleteObject(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		return nil, m.delete(req.Object), false
	case apdu.ServiceSubscribeCOV:
		req, err := service.DecodeSubscribeCOV(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		return nil, r.subscribe(req, nil), false
	case apdu.ServiceSubscribeCOVProperty:
		req, err := service.DecodeSubscribeCOVProperty(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		return nil, r.subscribe(req.SubscribeCOVRequest, &req.Property), false
	case apdu.ServiceGetEventInformation:
		if _, err := service.DecodeGetEventInformation(c.Payload, limits); err != nil {
			return fail(err)
		}
		var ack service.GetEventInformationACK
		for _, o := range m.alarms() {
			m.refresh(o)
			ack.Summaries = append(ack.Summaries, service.EventSummary{
				Object: o.id, EventState: o.state, AcknowledgedTransitions: bits(o.acked[0], o.acked[1], o.acked[2]).BitString,
				EventTimeStamps: o.get(propEventTimeStamps).value, NotifyType: 0, EventEnable: bitSet(3, 0, 1, 2).BitString,
				EventPriorities: list(bacnet.UnsignedValue(100), bacnet.UnsignedValue(60), bacnet.UnsignedValue(200)),
			})
		}
		reply, err := service.EncodeGetEventInformationACK(ack)
		return reply, nil, err != nil
	case apdu.ServiceGetAlarmSummary:
		var ack service.GetAlarmSummaryACK
		for _, o := range m.alarms() {
			ack.Entries = append(ack.Entries, service.AlarmSummaryEntry{
				Object: o.id, AlarmState: o.state, AckedTransitions: bits(o.acked[0], o.acked[1], o.acked[2]).BitString,
			})
		}
		reply, err := service.EncodeGetAlarmSummaryACK(ack)
		return reply, nil, err != nil
	case apdu.ServiceAcknowledgeAlarm:
		req, err := service.DecodeAcknowledgeAlarm(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		return nil, m.acknowledge(req), false
	case apdu.ServiceReadRange:
		req, err := service.DecodeReadRange(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		return m.readRange(req)
	case apdu.ServiceAtomicReadFile:
		req, err := service.DecodeAtomicReadFile(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		o := m.find(req.File)
		if o == nil || o.id.Type != bacnet.ObjectTypeFile {
			return nil, &errUnknownObject, false
		}
		if req.Access != service.FileAccessStream || req.StartPosition < 0 || int(req.StartPosition) > len(o.content) {
			return nil, &errFileAccess, false
		}
		end := min(int(req.StartPosition)+int(req.Count), len(o.content))
		reply, err := service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: end == len(o.content), Access: service.FileAccessStream, StartPosition: req.StartPosition,
			Data: o.content[req.StartPosition:end],
		})
		return reply, nil, err != nil
	case apdu.ServiceAtomicWriteFile:
		req, err := service.DecodeAtomicWriteFile(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		o := m.find(req.File)
		if o == nil || o.id.Type != bacnet.ObjectTypeFile {
			return nil, &errUnknownObject, false
		}
		if req.Access != service.FileAccessStream || req.StartPosition < 0 || int(req.StartPosition) > len(o.content) {
			return nil, &errFileAccess, false
		}
		o.content = append(o.content[:req.StartPosition:req.StartPosition], req.Data...)
		reply, err := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream, StartPosition: req.StartPosition})
		return reply, nil, err != nil
	case apdu.ServiceReinitializeDevice:
		req, err := service.DecodeReinitializeDevice(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		if !m.passwordOK(req.Password) {
			return nil, &errPassword, false
		}
		d.reinit = &req.State
		return nil, nil, false
	default:
		req, err := service.DecodeDeviceCommunicationControl(c.Payload, limits)
		if err != nil {
			return fail(err)
		}
		if !m.passwordOK(req.Password) {
			return nil, &errPassword, false
		}
		d.commControl = &req.EnableDisable
		return nil, nil, false
	}
}

func (m *model) passwordOK(given *bacnet.CharacterString) bool {
	if m.password == "" {
		return true
	}
	return given != nil && given.Value == m.password
}

// subscribe adds, renews or cancels a COV subscription. A new subscription is followed by
// a notification with the current value.
func (r *request) subscribe(req service.SubscribeCOVRequest, property *bacnet.PropertyReference) *bacErr {
	d, m := r.d, r.m
	o := m.find(req.MonitoredObject)
	if o == nil {
		return &errUnknownObject
	}
	if o.get(bacnet.PropertyPresentValue) == nil {
		return &errNotCOV
	}
	if property != nil && o.get(property.Identifier) == nil {
		return &errUnknownProperty
	}
	for i, s := range d.subs {
		if s.peer == r.from && s.process == req.ProcessIdentifier && s.object == o.id && s.model == m {
			if req.Cancellation {
				d.subs = append(d.subs[:i], d.subs[i+1:]...)
			} else {
				s.lifetime, s.confirmed = req.Lifetime, req.IssueConfirmed
			}
			return nil
		}
	}
	if req.Cancellation {
		return nil
	}
	s := &subscription{
		peer: r.from, source: r.client, model: m, process: req.ProcessIdentifier, object: o.id, property: property,
		confirmed: req.IssueConfirmed, lifetime: req.Lifetime,
	}
	d.subs = append(d.subs, s)
	r.after = append(r.after, func() { d.sendCOV(s) })
	return nil
}

// readMultiple answers a ReadPropertyMultiple: every property with its value or its error.
func (m *model) readMultiple(specs []service.ReadAccessSpecification) []service.ReadAccessResult {
	out := make([]service.ReadAccessResult, 0, len(specs))
	for _, spec := range specs {
		res := service.ReadAccessResult{Object: spec.Object}
		o := m.find(spec.Object)
		if o != nil {
			res.Object = o.id
		}
		for _, ref := range spec.Properties {
			refs := []bacnet.PropertyReference{ref}
			if o != nil && (ref.Identifier == propAll || ref.Identifier == propRequired || ref.Identifier == propOptional) {
				refs = refs[:0]
				for _, id := range o.expand(ref.Identifier) {
					refs = append(refs, bacnet.PropertyReference{Identifier: id})
				}
			}
			for _, r := range refs {
				v, e := m.read(spec.Object, r)
				p := service.PropertyResult{Property: r, Value: v}
				if e != nil {
					p.Err = e.response()
				}
				res.Properties = append(res.Properties, p)
			}
		}
		out = append(out, res)
	}
	return out
}

// readRange answers a ReadRange of the log buffer of a trend log, by position.
func (m *model) readRange(req service.ReadRangeRequest) ([]byte, *bacErr, bool) {
	o := m.find(req.Object)
	if o == nil {
		return nil, &errUnknownObject, false
	}
	if o.id.Type != bacnet.ObjectTypeTrendLog || req.Property.Identifier != bacnet.PropertyLogBuffer {
		return nil, &errNotATrend, false
	}
	// The simulated device returns at most three records per answer: enough to page.
	const perAnswer = 3
	first, count := 1, len(o.records)
	if req.By == service.ReadRangeByPosition {
		first, count = int(req.ReferenceIndex), int(req.Count)
	}
	if req.By != service.ReadRangeAll && req.By != service.ReadRangeByPosition || count < 0 || first < 1 {
		return nil, &bacErr{bacnet.ErrorClassServices, bacnet.ErrorCodeInconsistentParameters}, false
	}
	ack := service.ReadRangeACK{Object: o.id, Property: req.Property}
	var records []service.LogRecord
	if first <= len(o.records) {
		end := min(first-1+min(count, perAnswer), len(o.records))
		records = o.records[first-1 : end]
		ack.ResultFlags = service.EncodeResultFlags(first == 1, end == len(o.records), end < len(o.records) && end-first+1 < count)
	} else {
		ack.ResultFlags = service.EncodeResultFlags(false, false, false)
	}
	ack.ItemCount = uint32(len(records))
	ack.LogRecords = records
	reply, err := service.EncodeReadRangeACK(ack)
	return reply, nil, err != nil
}

// decodeRPM decodes a ReadPropertyMultiple request.
func decodeRPM(payload []byte, limits bacnet.DecodeLimits) ([]service.ReadAccessSpecification, error) {
	elements, _, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	var specs []service.ReadAccessSpecification
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return nil, err
			}
			specs = append(specs, service.ReadAccessSpecification{Object: id})
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el) && len(specs) > 0:
			cur := &specs[len(specs)-1]
			for _, p := range el.Value.Elements {
				u, err := bacnet.ContextUnsigned(p)
				if err != nil {
					return nil, err
				}
				if p.TagNumber == 0 {
					cur.Properties = append(cur.Properties, bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(u)})
				} else if n := len(cur.Properties); n > 0 {
					idx := uint32(u)
					cur.Properties[n-1].ArrayIndex = &idx
				}
			}
		default:
			return nil, bacnet.ErrMalformed
		}
	}
	return specs, nil
}
