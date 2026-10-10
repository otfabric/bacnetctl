// SPDX-License-Identifier: MIT

package bac

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

var bvlcFunctionNames = map[bvlc.Function]string{
	bvlc.FunctionResult:                            "BVLC-Result",
	bvlc.FunctionWriteBroadcastDistributionTable:   "Write-Broadcast-Distribution-Table",
	bvlc.FunctionReadBroadcastDistributionTable:    "Read-Broadcast-Distribution-Table",
	bvlc.FunctionReadBroadcastDistributionTableAck: "Read-Broadcast-Distribution-Table-Ack",
	bvlc.FunctionForwardedNPDU:                     "Forwarded-NPDU",
	bvlc.FunctionRegisterForeignDevice:             "Register-Foreign-Device",
	bvlc.FunctionReadForeignDeviceTable:            "Read-Foreign-Device-Table",
	bvlc.FunctionReadForeignDeviceTableAck:         "Read-Foreign-Device-Table-Ack",
	bvlc.FunctionDeleteForeignDeviceTableEntry:     "Delete-Foreign-Device-Table-Entry",
	bvlc.FunctionDistributeBroadcastToNetwork:      "Distribute-Broadcast-To-Network",
	bvlc.FunctionOriginalUnicastNPDU:               "Original-Unicast-NPDU",
	bvlc.FunctionOriginalBroadcastNPDU:             "Original-Broadcast-NPDU",
}

var networkMessageNames = []string{
	"Who-Is-Router-To-Network", "I-Am-Router-To-Network", "I-Could-Be-Router-To-Network",
	"Reject-Message-To-Network", "Router-Busy-To-Network", "Router-Available-To-Network",
	"Initialize-Routing-Table", "Initialize-Routing-Table-Ack", "Establish-Connection-To-Network",
	"Disconnect-Connection-To-Network", "Challenge-Request", "Security-Payload", "Security-Response",
	"Request-Key-Update", "Update-Key-Set", "Update-Distribution-Key", "Request-Master-Key", "Set-Master-Key",
	"What-Is-Network-Number", "Network-Number-Is",
}

var confirmedServiceNames = map[uint8]string{
	0: "acknowledge-alarm", 1: "confirmed-cov-notification", 2: "confirmed-event-notification",
	3: "get-alarm-summary", 4: "get-enrollment-summary", 5: "subscribe-cov", 6: "atomic-read-file",
	7: "atomic-write-file", 8: "add-list-element", 9: "remove-list-element", 10: "create-object",
	11: "delete-object", 12: "read-property", 13: "read-property-conditional", 14: "read-property-multiple",
	15: "write-property", 16: "write-property-multiple", 17: "device-communication-control",
	18: "confirmed-private-transfer", 19: "confirmed-text-message", 20: "reinitialize-device", 21: "vt-open",
	22: "vt-close", 23: "vt-data", 24: "authenticate", 25: "request-key", 26: "read-range",
	27: "life-safety-operation", 28: "subscribe-cov-property", 29: "get-event-information",
	30: "subscribe-cov-property-multiple", 31: "confirmed-cov-notification-multiple",
	32: "confirmed-audit-notification", 33: "audit-log-query",
}

var unconfirmedServiceNames = []string{
	"i-am", "i-have", "unconfirmed-cov-notification", "unconfirmed-event-notification",
	"unconfirmed-private-transfer", "unconfirmed-text-message", "time-synchronization", "who-has", "who-is",
	"utc-time-synchronization", "write-group", "unconfirmed-cov-notification-multiple",
	"unconfirmed-audit-notification", "who-am-i", "you-are",
}

// ConfirmedServiceName names a confirmed service choice.
func ConfirmedServiceName(choice uint8) string {
	if n, ok := confirmedServiceNames[choice]; ok {
		return n
	}
	return fmt.Sprintf("service-%d", choice)
}

// UnconfirmedServiceName names an unconfirmed service choice.
func UnconfirmedServiceName(choice uint8) string {
	if n := nameAt(unconfirmedServiceNames, uint32(choice)); n != "" {
		return n
	}
	return fmt.Sprintf("service-%d", choice)
}

// layer collects the fields of one protocol layer.
type layer struct{ types.Layer }

func newLayer(name, summary string) *layer {
	return &layer{types.Layer{Layer: name, Summary: summary, Fields: []types.Field{}}}
}

func (l *layer) add(name string, value any) {
	l.Fields = append(l.Fields, types.Field{Name: name, Value: value})
}

func (l *layer) object(name string, id bacnet.ObjectIdentifier) { l.add(name, ObjectName(id)) }

func (l *layer) property(ref bacnet.PropertyReference) {
	l.add("property", fmt.Sprintf("%s (%d)", PropertyName(ref.Identifier), ref.Identifier))
	if ref.ArrayIndex != nil {
		l.add("array_index", *ref.ArrayIndex)
	}
}

func (l *layer) value(name string, obj bacnet.ObjectIdentifier, prop bacnet.PropertyIdentifier, v bacnet.ApplicationValue) {
	val := ValueOf(obj.Type, prop, v)
	l.add(name, fmt.Sprintf("%s (%s)", val.Text, val.Type))
}

func addressText(a bacnet.Address) string {
	switch a.Scope() {
	case bacnet.AddressGlobalBroadcast:
		return "global broadcast (network 65535)"
	case bacnet.AddressRemoteBroadcast:
		return fmt.Sprintf("broadcast on network %d", a.Network())
	case bacnet.AddressRemoteStation:
		return fmt.Sprintf("network %d, MAC %s", a.Network(), a.MAC())
	default:
		return ""
	}
}

// Decode decodes one BACnet/IP datagram layer by layer: BVLC, NPDU, APDU and the service
// it carries. Decoding goes as far as it can: what stops it is reported in the result.
func Decode(raw []byte) *types.DecodeResult {
	res := &types.DecodeResult{Length: len(raw), Layers: []types.Layer{}}
	fail := func(layer string, err error) *types.DecodeResult {
		res.Error = fmt.Sprintf("%s: %v", layer, err)
		return res
	}
	limits := bacnet.DefaultDecodeLimits()
	msg, err := bvlc.Parse(raw, limits)
	if err != nil {
		return fail("BVLC", err)
	}
	name, ok := bvlcFunctionNames[msg.Function]
	if !ok {
		name = fmt.Sprintf("function 0x%02X", uint8(msg.Function))
	}
	bl := newLayer("bvlc", name)
	bl.add("function", fmt.Sprintf("0x%02X", uint8(msg.Function)))
	bl.add("length", len(raw))
	carriesNPDU := false
	switch msg.Function {
	case bvlc.FunctionResult:
		bl.add("result_code", fmt.Sprintf("0x%04X", msg.ResultCode))
	case bvlc.FunctionRegisterForeignDevice:
		bl.add("ttl_seconds", msg.TTL)
	case bvlc.FunctionForwardedNPDU:
		if ap, ok := msg.OriginAddrPort(); ok {
			bl.add("origin", ap.String())
		}
		carriesNPDU = true
	case bvlc.FunctionOriginalUnicastNPDU, bvlc.FunctionOriginalBroadcastNPDU, bvlc.FunctionDistributeBroadcastToNetwork:
		carriesNPDU = true
	case bvlc.FunctionReadBroadcastDistributionTableAck, bvlc.FunctionWriteBroadcastDistributionTable:
		if entries, err := bvlc.DecodeBDTEntries(msg.Payload, limits); err == nil {
			for _, e := range entries {
				bl.add("entry", fmt.Sprintf("%s mask %s", e.Endpoint, e.Mask.Addr()))
			}
		}
	case bvlc.FunctionReadForeignDeviceTableAck:
		if entries, err := bvlc.DecodeFDTEntries(msg.Payload, limits); err == nil {
			for _, e := range entries {
				bl.add("entry", fmt.Sprintf("%s ttl %d s, %d s remaining", e.Address, e.TTL, e.Remaining))
			}
		}
	default:
		if len(msg.Payload) > 0 {
			bl.add("payload", hex.EncodeToString(msg.Payload))
		}
	}
	res.Layers = append(res.Layers, bl.Layer)
	if !carriesNPDU {
		return res
	}

	n, _, err := npdu.Parse(msg.Payload, limits)
	if err != nil {
		return fail("NPDU", err)
	}
	summary := "application message"
	if n.NetworkMessage {
		summary = "network layer message"
	}
	nl := newLayer("npdu", summary)
	nl.add("version", n.Version)
	nl.add("control", fmt.Sprintf("0x%02X", n.Control))
	nl.add("priority", []string{"normal", "urgent", "critical-equipment", "life-safety"}[n.Priority&3])
	nl.add("expecting_reply", n.ExpectingReply)
	if s := addressText(n.Destination); s != "" {
		nl.add("destination", s)
		nl.add("hop_count", n.HopCount)
	}
	if s := addressText(n.Source); s != "" {
		nl.add("source", s)
	}
	if n.NetworkMessage {
		msgName := nameAt(networkMessageNames, uint32(n.NetMsgType))
		if msgName == "" {
			msgName = fmt.Sprintf("message type 0x%02X", n.NetMsgType)
		}
		nl.Summary = msgName
		nl.add("message_type", fmt.Sprintf("0x%02X", n.NetMsgType))
		if nets, err := npdu.DecodeNetworkList(n.NetMsgData); err == nil && len(nets) > 0 {
			nl.add("networks", strings.Trim(fmt.Sprint(nets), "[]"))
		} else if len(n.NetMsgData) > 0 {
			nl.add("data", hex.EncodeToString(n.NetMsgData))
		}
		res.Layers = append(res.Layers, nl.Layer)
		return res
	}
	res.Layers = append(res.Layers, nl.Layer)
	if len(n.APDU) == 0 {
		return res
	}

	pdu, err := apdu.Parse(n.APDU, limits)
	if err != nil {
		return fail("APDU", err)
	}
	al, sl, err := decodeAPDU(pdu, limits)
	res.Layers = append(res.Layers, al.Layer)
	if sl != nil {
		res.Layers = append(res.Layers, sl.Layer)
	}
	if err != nil {
		return fail("service", err)
	}
	return res
}

func decodeAPDU(pdu apdu.PDU, limits bacnet.DecodeLimits) (al, sl *layer, err error) {
	switch pdu.Type {
	case apdu.TypeConfirmedRequest:
		r := pdu.ConfirmedRequest
		al = newLayer("apdu", "Confirmed-Request")
		al.add("invoke_id", r.InvokeID)
		al.add("service", fmt.Sprintf("%s (%d)", ConfirmedServiceName(r.ServiceChoice), r.ServiceChoice))
		al.add("segmented_response_accepted", r.SegmentedResponseAccepted)
		if size, ok := apdu.DecodeMaxAPDUSize(apdu.MaxAPDUCode(r.MaxAPDU)); ok {
			al.add("max_apdu_accepted", size)
		}
		if r.SegmentedMessage {
			al.add("segment", fmt.Sprintf("sequence %d, window %d, more follows %v", r.SequenceNumber, r.ProposedWindowSize, r.MoreFollows))
			al.add("payload", hex.EncodeToString(r.Payload))
			return al, nil, nil
		}
		sl = newLayer("service", ConfirmedServiceName(r.ServiceChoice)+" request")
		return al, sl, decodeConfirmedRequest(sl, r.ServiceChoice, r.Payload, limits)
	case apdu.TypeUnconfirmedRequest:
		r := pdu.UnconfirmedRequest
		al = newLayer("apdu", "Unconfirmed-Request")
		al.add("service", fmt.Sprintf("%s (%d)", UnconfirmedServiceName(r.ServiceChoice), r.ServiceChoice))
		sl = newLayer("service", UnconfirmedServiceName(r.ServiceChoice))
		return al, sl, decodeUnconfirmed(sl, r.ServiceChoice, r.Payload, limits)
	case apdu.TypeSimpleACK:
		al = newLayer("apdu", "Simple-ACK")
		al.add("invoke_id", pdu.SimpleACK.InvokeID)
		al.add("service", fmt.Sprintf("%s (%d)", ConfirmedServiceName(pdu.SimpleACK.ServiceChoice), pdu.SimpleACK.ServiceChoice))
		return al, nil, nil
	case apdu.TypeComplexACK:
		r := pdu.ComplexACK
		al = newLayer("apdu", "Complex-ACK")
		al.add("invoke_id", r.InvokeID)
		al.add("service", fmt.Sprintf("%s (%d)", ConfirmedServiceName(r.ServiceChoice), r.ServiceChoice))
		if r.SegmentedMessage {
			al.add("segment", fmt.Sprintf("sequence %d, window %d, more follows %v", r.SequenceNumber, r.ProposedWindowSize, r.MoreFollows))
			al.add("payload", hex.EncodeToString(r.Payload))
			return al, nil, nil
		}
		sl = newLayer("service", ConfirmedServiceName(r.ServiceChoice)+" ack")
		return al, sl, decodeComplexACK(sl, r.ServiceChoice, r.Payload, limits)
	case apdu.TypeSegmentACK:
		r := pdu.SegmentACK
		al = newLayer("apdu", "Segment-ACK")
		al.add("invoke_id", r.InvokeID)
		al.add("sequence_number", r.SequenceNumber)
		al.add("actual_window_size", r.ActualWindowSize)
		al.add("negative", r.NegativeACK)
		al.add("from_server", r.Server)
		return al, nil, nil
	case apdu.TypeError:
		r := pdu.Error
		al = newLayer("apdu", "Error")
		al.add("invoke_id", r.InvokeID)
		al.add("service", fmt.Sprintf("%s (%d)", ConfirmedServiceName(r.ServiceChoice), r.ServiceChoice))
		if class, code, err := apdu.DecodeErrorClassCode(r.Payload, limits); err == nil {
			al.add("error_class", fmt.Sprintf("%s (%d)", bacnet.ErrorClassName(class), class))
			al.add("error_code", fmt.Sprintf("%s (%d)", bacnet.ErrorCodeName(code), code))
		} else {
			// An error with parameters of its own: shown as it is.
			al.add("payload", hex.EncodeToString(r.Payload))
		}
		return al, nil, nil
	case apdu.TypeReject:
		al = newLayer("apdu", "Reject")
		al.add("invoke_id", pdu.Reject.InvokeID)
		al.add("reason", fmt.Sprintf("%s (%d)", bacnet.RejectReasonName(pdu.Reject.Reason), pdu.Reject.Reason))
		return al, nil, nil
	default:
		al = newLayer("apdu", "Abort")
		al.add("invoke_id", pdu.Abort.InvokeID)
		al.add("reason", fmt.Sprintf("%s (%d)", bacnet.AbortReasonName(pdu.Abort.Reason), pdu.Abort.Reason))
		al.add("from_server", pdu.Abort.Server)
		return al, nil, nil
	}
}

func hidden(s *bacnet.CharacterString) string {
	if s == nil {
		return "none"
	}
	return fmt.Sprintf("present, %d characters (not shown)", len(s.Value))
}

func raw(l *layer, payload []byte) {
	if len(payload) > 0 {
		l.add("payload", hex.EncodeToString(payload))
	}
}

func decodeConfirmedRequest(l *layer, choice uint8, payload []byte, limits bacnet.DecodeLimits) error {
	switch choice {
	case apdu.ServiceReadProperty:
		r, err := service.DecodeReadProperty(payload, limits)
		if err != nil {
			return err
		}
		l.object("object", r.Object)
		l.property(r.Property)
	case apdu.ServiceReadPropertyMultiple:
		specs, err := DecodeReadPropertyMultiple(payload, limits)
		if err != nil {
			return err
		}
		for _, s := range specs {
			names := make([]string, 0, len(s.Properties))
			for _, p := range s.Properties {
				names = append(names, propRef(p).Label())
			}
			l.add(ObjectName(s.Object), strings.Join(names, ", "))
		}
	case apdu.ServiceWriteProperty:
		r, err := service.DecodeWriteProperty(payload, limits)
		if err != nil {
			return err
		}
		l.object("object", r.Object)
		l.property(r.Property)
		l.value("value", r.Object, r.Property.Identifier, r.Value)
		if r.Priority != nil {
			l.add("priority", *r.Priority)
		}
	case apdu.ServiceWritePropertyMultiple:
		specs, err := service.DecodeWritePropertyMultiple(payload, limits)
		if err != nil {
			return err
		}
		for _, s := range specs {
			for _, p := range s.Properties {
				val := ValueOf(s.Object.Type, p.Property.Identifier, p.Value)
				l.add(ObjectName(s.Object)+" "+propRef(p.Property).Label(), val.Text)
			}
		}
	case apdu.ServiceSubscribeCOV:
		r, err := service.DecodeSubscribeCOV(payload, limits)
		if err != nil {
			return err
		}
		subscription(l, r)
	case apdu.ServiceSubscribeCOVProperty:
		r, err := service.DecodeSubscribeCOVProperty(payload, limits)
		if err != nil {
			return err
		}
		subscription(l, r.SubscribeCOVRequest)
		l.property(r.Property)
		if r.COVIncrement != nil {
			l.add("cov_increment", types.FormatFloat(float64(*r.COVIncrement), 32))
		}
	case apdu.ServiceConfirmedCOVNotification:
		return covNotification(l, payload, limits)
	case apdu.ServiceConfirmedEventNotification:
		return eventNotification(l, payload, limits)
	case apdu.ServiceReadRange:
		r, err := service.DecodeReadRange(payload, limits)
		if err != nil {
			return err
		}
		l.object("object", r.Object)
		l.property(r.Property)
		l.add("range", []string{"all", "by-position", "by-sequence-number", "by-time"}[r.By&3])
		if r.By == service.ReadRangeByPosition || r.By == service.ReadRangeBySequenceNumber {
			l.add("reference_index", r.ReferenceIndex)
		}
		l.add("count", r.Count)
	case apdu.ServiceAtomicReadFile:
		r, err := service.DecodeAtomicReadFile(payload, limits)
		if err != nil {
			return err
		}
		l.object("file", r.File)
		l.add("start", r.StartPosition)
		l.add("count", r.Count)
	case apdu.ServiceAtomicWriteFile:
		r, err := service.DecodeAtomicWriteFile(payload, limits)
		if err != nil {
			return err
		}
		l.object("file", r.File)
		l.add("start", r.StartPosition)
		l.add("octets", len(r.Data))
	case apdu.ServiceReinitializeDevice:
		r, err := service.DecodeReinitializeDevice(payload, limits)
		if err != nil {
			return err
		}
		l.add("state", stateName(reinitStates, uint8(r.State)))
		l.add("password", hidden(r.Password))
	case apdu.ServiceDeviceCommunicationControl:
		r, err := service.DecodeDeviceCommunicationControl(payload, limits)
		if err != nil {
			return err
		}
		l.add("state", stateName(commStates, uint8(r.EnableDisable)))
		if r.TimeDuration != nil {
			l.add("duration_minutes", *r.TimeDuration)
		}
		l.add("password", hidden(r.Password))
	case apdu.ServiceAcknowledgeAlarm:
		r, err := service.DecodeAcknowledgeAlarm(payload, limits)
		if err != nil {
			return err
		}
		l.object("object", r.EventObject)
		l.add("state_acknowledged", eventStateName(r.EventStateAcked))
		l.add("time_stamp", TimeStampText(r.TimeStamp))
		l.add("source", Text(r.AcknowledgmentSource))
	case apdu.ServiceGetEventInformation:
		r, err := service.DecodeGetEventInformation(payload, limits)
		if err != nil {
			return err
		}
		if r.LastReceived != nil {
			l.object("last_received", *r.LastReceived)
		}
	default:
		raw(l, payload)
	}
	return nil
}

func stateName[T ~uint8](m map[string]T, v uint8) string {
	for name, s := range m {
		if uint8(s) == v {
			return name
		}
	}
	return fmt.Sprint(v)
}

func subscription(l *layer, r service.SubscribeCOVRequest) {
	l.add("process_id", r.ProcessIdentifier)
	l.object("object", r.MonitoredObject)
	if r.Cancellation {
		l.add("cancellation", true)
		return
	}
	l.add("confirmed_notifications", r.IssueConfirmed)
	l.add("lifetime_seconds", r.Lifetime)
}

func covNotification(l *layer, payload []byte, limits bacnet.DecodeLimits) error {
	n, err := service.DecodeCOVNotification(payload, limits)
	if err != nil {
		return err
	}
	l.add("process_id", n.ProcessIdentifier)
	l.object("device", n.InitiatingDevice)
	l.object("object", n.MonitoredObject)
	l.add("time_remaining_seconds", n.TimeRemaining)
	for _, v := range n.Values {
		l.value(propRef(v.Property).Label(), n.MonitoredObject, v.Property.Identifier, v.Value)
	}
	return nil
}

func eventNotification(l *layer, payload []byte, limits bacnet.DecodeLimits) error {
	n, err := service.DecodeEventNotification(payload, limits)
	if err != nil {
		return err
	}
	rec := eventRecord(n, "", false, false)
	l.add("process_id", n.ProcessIdentifier)
	l.add("device", rec.Device)
	l.add("object", rec.Object)
	l.add("event_time", rec.EventTime)
	l.add("notification_class", rec.NotificationClass)
	l.add("priority", rec.Priority)
	l.add("event_type", rec.EventType)
	l.add("notify_type", rec.NotifyType)
	if rec.FromState != "" {
		l.add("from_state", rec.FromState)
	}
	l.add("to_state", rec.ToState)
	if rec.Message != "" {
		l.add("message", rec.Message)
	}
	return nil
}

func decodeUnconfirmed(l *layer, choice uint8, payload []byte, limits bacnet.DecodeLimits) error {
	switch choice {
	case apdu.ServiceWhoIs:
		r, err := service.DecodeWhoIs(payload, limits)
		if err != nil {
			return err
		}
		if r.LowLimit != nil && r.HighLimit != nil {
			l.add("low_limit", *r.LowLimit)
			l.add("high_limit", *r.HighLimit)
		} else {
			l.add("range", "all devices")
		}
	case apdu.ServiceIAm:
		r, err := service.DecodeIAm(payload, limits)
		if err != nil {
			return err
		}
		l.object("device", r.Device)
		l.add("max_apdu", r.MaxAPDULength)
		l.add("segmentation", namedOr(segmentationNames, uint32(r.Segmentation)))
		l.add("vendor_id", r.VendorID)
	case apdu.ServiceWhoHas:
		r, err := service.DecodeWhoHas(payload, limits)
		if err != nil {
			return err
		}
		if r.LowLimit != nil && r.HighLimit != nil {
			l.add("low_limit", *r.LowLimit)
			l.add("high_limit", *r.HighLimit)
		}
		if r.Object != nil {
			l.object("object", *r.Object)
		}
		if r.Name != nil {
			l.add("name", Text(*r.Name))
		}
	case apdu.ServiceIHave:
		r, err := service.DecodeIHave(payload, limits)
		if err != nil {
			return err
		}
		l.object("device", r.Device)
		l.object("object", r.Object)
		l.add("name", Text(r.Name))
	case apdu.ServiceUnconfirmedCOV:
		return covNotification(l, payload, limits)
	case apdu.ServiceUnconfirmedEventNotification:
		return eventNotification(l, payload, limits)
	case apdu.ServiceTimeSynchronization, apdu.ServiceUTCTimeSynchronization:
		r, err := service.DecodeTimeSynchronization(payload, limits)
		if err != nil {
			return err
		}
		l.add("date", DateText(r.Date))
		l.add("time", TimeText(r.Time))
	default:
		raw(l, payload)
	}
	return nil
}

func decodeComplexACK(l *layer, choice uint8, payload []byte, limits bacnet.DecodeLimits) error {
	switch choice {
	case apdu.ServiceReadProperty:
		r, err := service.DecodeReadPropertyACK(payload, limits)
		if err != nil {
			return err
		}
		l.object("object", r.Object)
		l.property(r.Property)
		l.value("value", r.Object, r.Property.Identifier, r.Value)
	case apdu.ServiceReadPropertyMultiple:
		results, err := service.DecodeReadPropertyMultipleACK(payload, limits)
		if err != nil {
			return err
		}
		for _, r := range results {
			for _, p := range r.Properties {
				row := propertyRow(r.Object, p)
				text := types.TextOf(row.Value)
				if row.Error != nil {
					text = types.ErrorText(row.Error)
				}
				l.add(row.Object+" "+row.Label(), text)
			}
		}
	case apdu.ServiceReadRange:
		r, err := service.DecodeReadRangeACK(payload, limits)
		if err != nil {
			return err
		}
		l.object("object", r.Object)
		l.property(r.Property)
		l.add("item_count", r.ItemCount)
		l.add("first_item", r.FirstItem())
		l.add("last_item", r.LastItem())
		l.add("more_items", r.MoreItems())
	case apdu.ServiceAtomicReadFile:
		r, err := service.DecodeAtomicReadFileACK(payload, limits)
		if err != nil {
			return err
		}
		l.add("end_of_file", r.EndOfFile)
		l.add("start", r.StartPosition)
		l.add("octets", len(r.Data))
	case apdu.ServiceAtomicWriteFile:
		r, err := service.DecodeAtomicWriteFileACK(payload, limits)
		if err != nil {
			return err
		}
		l.add("start", r.StartPosition)
	case apdu.ServiceGetEventInformation:
		r, err := service.DecodeGetEventInformationACK(payload, limits)
		if err != nil {
			return err
		}
		for _, s := range r.Summaries {
			l.add(ObjectName(s.Object), eventStateName(s.EventState))
		}
		l.add("more_events", r.MoreEvents)
	default:
		raw(l, payload)
	}
	return nil
}

// DecodeReadPropertyMultiple decodes a ReadPropertyMultiple request: a list of objects,
// each with the properties to read.
func DecodeReadPropertyMultiple(payload []byte, limits bacnet.DecodeLimits) ([]service.ReadAccessSpecification, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	if n != len(payload) {
		return nil, fmt.Errorf("%w: ReadPropertyMultiple trailing data", bacnet.ErrTrailingData)
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
				switch {
				case p.TagNumber == 0:
					cur.Properties = append(cur.Properties, bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(u)})
				case p.TagNumber == 1 && len(cur.Properties) > 0:
					idx := uint32(u)
					cur.Properties[len(cur.Properties)-1].ArrayIndex = &idx
				default:
					return nil, fmt.Errorf("%w: unexpected property reference tag %d", bacnet.ErrMalformed, p.TagNumber)
				}
			}
		default:
			return nil, fmt.Errorf("%w: unexpected ReadPropertyMultiple tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: empty ReadPropertyMultiple", bacnet.ErrMalformed)
	}
	return specs, nil
}
