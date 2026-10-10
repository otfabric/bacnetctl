// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Names of the alarm services in output.
const (
	ServiceGetEventInformation = "get-event-information"
	ServiceGetAlarmSummary     = "get-alarm-summary"
)

// propEventTimeStamps is event-time-stamps: the times of the last three transitions.
const propEventTimeStamps bacnet.PropertyIdentifier = 130

// maxEventPages bounds the GetEventInformation requests of one `alarms`.
const maxEventPages = 1000

// TimeStampText writes a BACnetTimeStamp: a time, "sequence N", or a date and time.
func TimeStampText(ts service.TimeStamp) string {
	switch ts.Choice {
	case service.TimeStampTime:
		return TimeText(ts.Time)
	case service.TimeStampSequence:
		return "sequence " + strconv.FormatUint(uint64(ts.Sequence), 10)
	default:
		return DateText(ts.DateTime.Date) + " " + TimeText(ts.DateTime.Time)
	}
}

// timeStamps decodes a list of BACnetTimeStamp, as in event-time-stamps.
func timeStamps(v bacnet.ApplicationValue) ([]service.TimeStamp, error) {
	out := make([]service.TimeStamp, 0, len(v.Elements))
	for _, el := range v.Elements {
		ts, err := service.DecodeTimeStamp(el)
		if err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, nil
}

// unacknowledged names the transitions whose bit in acked-transitions is clear.
func unacknowledged(acked bacnet.BitString) []string {
	out := []string{}
	for i, set := range BitList(acked) {
		if !set && i < len(eventTransitionNames) {
			out = append(out, eventTransitionNames[i])
		}
	}
	return out
}

func eventStateName(state uint32) string {
	if n := nameAt(eventStateNames, state); n != "" {
		return n
	}
	return strconv.FormatUint(uint64(state), 10)
}

func namedOr(names []string, v uint32) string {
	if n := nameAt(names, v); n != "" {
		return n
	}
	return strconv.FormatUint(uint64(v), 10)
}

// CheckAlarms validates the configuration of `alarms`.
func CheckAlarms(cfg *config.AlarmsConfig) error { return CheckConn(&cfg.ConnConfig) }

// Alarms lists the objects of a device that are in an alarm state or have transitions that
// wait for an acknowledgement: with GetEventInformation, and with GetAlarmSummary for a
// device that does not implement that.
func Alarms(ctx context.Context, cfg *config.AlarmsConfig, logw io.Writer) (*types.AlarmsResult, error) {
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	res := &types.AlarmsResult{Target: TargetText(target), Service: ServiceGetEventInformation, Alarms: []types.Alarm{}}
	var req service.GetEventInformationRequest
	for page := 0; page < maxEventPages; page++ {
		ack, err := s.GetEventInformation(ctx, target, req)
		if err != nil {
			if page == 0 && NotImplemented(err) {
				return s.alarmSummary(ctx, target, res)
			}
			return nil, WrapError(err)
		}
		for _, sum := range ack.Summaries {
			notify := sum.NotifyType
			a := types.Alarm{
				ObjectRef: objRef(sum.Object), EventState: eventStateName(sum.EventState), EventStateID: sum.EventState,
				Unacknowledged: unacknowledged(sum.AcknowledgedTransitions),
				NotifyType:     namedOr(notifyTypeNames, notify), NotifyTypeID: &notify,
				EventEnable: SetBitNames(sum.EventEnable, eventTransitionNames),
			}
			if stamps, err := timeStamps(sum.EventTimeStamps); err == nil {
				for _, ts := range stamps {
					a.TimeStamps = append(a.TimeStamps, TimeStampText(ts))
				}
			}
			for _, el := range sum.EventPriorities.Elements {
				if p, err := bacnet.AsUnsigned(el.Value); err == nil {
					a.Priorities = append(a.Priorities, p)
				}
			}
			res.Alarms = append(res.Alarms, a)
		}
		if !ack.MoreEvents || len(ack.Summaries) == 0 {
			break
		}
		last := ack.Summaries[len(ack.Summaries)-1].Object
		req.LastReceived = &last
	}
	res.Count = len(res.Alarms)
	return res, nil
}

// alarmSummary fills res with GetAlarmSummary, the service older devices have.
func (s *Session) alarmSummary(ctx context.Context, target client.Target, res *types.AlarmsResult) (*types.AlarmsResult, error) {
	ack, err := s.GetAlarmSummary(ctx, target)
	if err != nil {
		return nil, WrapError(err)
	}
	res.Service = ServiceGetAlarmSummary
	for _, e := range ack.Entries {
		res.Alarms = append(res.Alarms, types.Alarm{
			ObjectRef: objRef(e.Object), EventState: eventStateName(e.AlarmState), EventStateID: e.AlarmState,
			Unacknowledged: unacknowledged(e.AckedTransitions),
		})
	}
	res.Count = len(res.Alarms)
	return res, nil
}

// AckRequest is a parsed `ack` invocation.
type AckRequest struct {
	Object bacnet.ObjectIdentifier
	// State is the event state that is acknowledged, when it was given; otherwise it is
	// the state the object is in.
	State *uint32
	// Stamp is the time stamp of the event, when it was given; otherwise it is read from
	// event-time-stamps of the object.
	Stamp *service.TimeStamp
}

// transitionIndex is the place of a state in event-time-stamps and acked-transitions:
// to-offnormal, to-fault, to-normal.
func transitionIndex(state uint32) int {
	switch state {
	case 0:
		return 2
	case 1:
		return 1
	default:
		return 0
	}
}

// ParseAck parses the arguments and flags of `ack`.
func ParseAck(cfg *config.AckConfig, object string) (AckRequest, error) {
	var req AckRequest
	var err error
	if req.Object, err = ParseObject(object); err != nil {
		return req, err
	}
	if state := normalizeName(cfg.State); state != "" {
		for i, n := range eventStateNames {
			if n == state {
				v := uint32(i)
				req.State = &v
			}
		}
		if req.State == nil {
			return req, fmt.Errorf("--state %q: use one of %s", cfg.State, strings.Join(eventStateNames, ", "))
		}
	}
	if cfg.Sequence != "" && cfg.Time != "" {
		return req, fmt.Errorf("--sequence and --event-time are two forms of the same time stamp: give one")
	}
	if cfg.Sequence != "" {
		n, err := strconv.ParseUint(strings.TrimSpace(cfg.Sequence), 10, 32)
		if err != nil {
			return req, fmt.Errorf("--sequence %q: a sequence number is an unsigned number", cfg.Sequence)
		}
		req.Stamp = &service.TimeStamp{Choice: service.TimeStampSequence, Sequence: uint32(n)}
	}
	if cfg.Time != "" {
		t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(cfg.Time))
		if err != nil {
			return req, fmt.Errorf("--event-time %q: use RFC 3339, for example 2026-01-31T12:00:00.25+01:00", cfg.Time)
		}
		d, tm := DateTimeOf(t)
		req.Stamp = &service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{Date: d, Time: tm}}
	}
	if strings.TrimSpace(cfg.Source) == "" {
		return req, fmt.Errorf("--source must not be empty: it is recorded as who acknowledged")
	}
	return req, nil
}

// CheckAck validates the configuration of `ack`.
func CheckAck(cfg *config.AckConfig, object string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseAck(cfg, object)
	return err
}

// Ack acknowledges one transition of an object. The state that is acknowledged and the
// time stamp of the event are read from the object unless they were given.
func Ack(ctx context.Context, cfg *config.AckConfig, object string, logw io.Writer) (*types.AckResult, error) {
	req, _ := ParseAck(cfg, object)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	if req.State == nil {
		v, err := s.ReadProperty(ctx, target, req.Object, bacnet.PropertyReference{Identifier: bacnet.PropertyEventState})
		if err != nil {
			return nil, WrapError(err)
		}
		state, err := bacnet.AsEnumerated(v)
		if err != nil {
			return nil, errs.New(errs.KindProtocol, errs.CodeProtocolViolation, "protocol error: event-state of the object is not an enumeration; give --state", err)
		}
		req.State = &state
	}
	if req.Stamp == nil {
		v, err := s.ReadProperty(ctx, target, req.Object, bacnet.PropertyReference{Identifier: propEventTimeStamps})
		if err != nil {
			return nil, WrapError(err)
		}
		stamps, err := timeStamps(v)
		idx := transitionIndex(*req.State)
		if err != nil || len(stamps) <= idx {
			return nil, errs.New(errs.KindProtocol, errs.CodeProtocolViolation,
				"protocol error: event-time-stamps of the object does not hold three time stamps; give --sequence or --event-time", err)
		}
		req.Stamp = &stamps[idx]
	}
	d, tm := DateTimeOf(time.Now())
	ack := service.AcknowledgeAlarmRequest{
		ProcessIdentifier: cfg.ProcessID, EventObject: req.Object, EventStateAcked: *req.State, TimeStamp: *req.Stamp,
		AcknowledgmentSource: bacnet.CharacterString{Value: cfg.Source},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{Date: d, Time: tm}},
	}
	start := time.Now()
	if err := s.AcknowledgeAlarm(ctx, target, ack); err != nil {
		return nil, WrapError(err)
	}
	return &types.AckResult{
		Target: TargetText(target), ObjectRef: objRef(req.Object), State: eventStateName(*req.State), StateID: *req.State,
		TimeStamp: TimeStampText(*req.Stamp), Source: cfg.Source, Status: StatusAcknowledged, RTTMS: ms(time.Since(start)),
	}, nil
}

// CheckEvents validates the configuration of `events`.
func CheckEvents(cfg *config.EventsConfig) error { return CheckSocket(&cfg.SocketConfig) }

// Events listens for event notifications and emits each one, until ctx ends, --duration
// has passed or --count notifications were emitted. Confirmed notifications are
// acknowledged by the client.
func Events(ctx context.Context, cfg *config.EventsConfig, logw io.Writer, emit func(*types.EventRecord) error) (summary types.MonitorSummary, err error) {
	summary.What = "event notification"
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return summary, err
	}
	defer func() { _ = s.Close() }()
	stream := s.OpenEventStream(covBuffer)
	defer stream.Close()
	summary.Target = "UDP port " + strconv.Itoa(int(s.LocalEndpoint().Addr.Port()))
	if logw != nil {
		_, _ = fmt.Fprintf(logw, "listening for event notifications on %s\n", summary.Target)
	}
	start := time.Now()
	defer func() { summary.Duration = ms(time.Since(start)) }()
	stop, cancel := stopTimer(cfg.Duration)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return summary, nil
		case <-stop:
			return summary, nil
		case ev, ok := <-stream.Events():
			if !ok || ev.Closed {
				return summary, nil
			}
			if err := emit(eventRecord(ev.Delivery.Notification, ev.Delivery.Origin.String(), ev.Delivery.Confirmed, ev.Gap)); err != nil {
				return summary, errs.Output(errs.CodeOutputWriteFailed, err)
			}
			summary.Received++
			if cfg.Count > 0 && uint32(summary.Received) >= cfg.Count {
				return summary, nil
			}
		}
	}
}

func eventRecord(n service.EventNotification, from string, confirmed, gap bool) *types.EventRecord {
	rec := &types.EventRecord{
		Timestamp: Now(), From: from, Confirmed: confirmed, Device: ObjectName(n.InitiatingDevice), ObjectRef: objRef(n.EventObject),
		EventType: namedOr(eventTypeNames, n.EventType), EventTypeID: n.EventType,
		NotifyType: namedOr(notifyTypeNames, n.NotifyType), NotifyTypeID: n.NotifyType,
		ToState: eventStateName(n.ToState), ToStateID: n.ToState, Priority: n.Priority, NotificationClass: n.NotificationClass,
		AckRequired: n.AckRequired, EventTime: TimeStampText(n.TimeStamp), Gap: gap,
	}
	if n.FromState != nil {
		rec.FromState = eventStateName(*n.FromState)
	}
	if n.MessageText != nil {
		rec.Message = Text(*n.MessageText)
	}
	return rec
}
