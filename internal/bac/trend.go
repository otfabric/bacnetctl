// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Kinds of a log record in output.
const (
	RecordValue      = "value"
	RecordStatus     = "status"
	RecordFailure    = "failure"
	RecordTimeChange = "time-change"
	// RecordValues is a record of a trend log multiple: one value per logged property.
	RecordValues = "values"
	// RecordEvent is a record of an event log: an event notification.
	RecordEvent = "event"
)

// maxTrendRecords bounds what `trend --all` reads.
const maxTrendRecords = 1000000

// CheckTrend validates the configuration of `trend`.
func CheckTrend(cfg *config.TrendConfig, object string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	if _, err := ParseObject(object); err != nil {
		return err
	}
	if cfg.Start == 0 {
		return fmt.Errorf("--start counts from 1, the oldest record in the buffer")
	}
	if cfg.Count == 0 || cfg.Count > 10000 {
		return fmt.Errorf("--count must be 1-10000")
	}
	return nil
}

// Trend reads records of the log buffer of a trend log, by position.
func Trend(ctx context.Context, cfg *config.TrendConfig, objectArg string, logw io.Writer) (*types.TrendResult, error) {
	object, _ := ParseObject(objectArg)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	count, err := i32(cfg.Count, "--count")
	if err != nil {
		return nil, errs.InvalidInput(errs.CodeInvalidInput, err.Error(), err)
	}
	res := &types.TrendResult{Target: TargetText(target), ObjectRef: objRef(object), Start: cfg.Start, Records: []types.LogRecord{}}
	req := service.ReadRangeRequest{
		Object: object, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By: service.ReadRangeByPosition, ReferenceIndex: cfg.Start, Count: count,
	}
	var (
		records []service.LogRecord
		multi   []service.LogMultipleRecord
		events  []service.EventLogRecord
	)
	items := 0
	if cfg.All {
		page, err := s.ReadRangeAll(ctx, target, req, client.ReadRangePageOptions{PageCount: count, MaxItems: maxTrendRecords, MaxPages: maxTrendRecords})
		if err != nil {
			return nil, WrapError(err)
		}
		records, multi, events, res.Partial = page.LogRecords, page.LogMultipleRecords, page.EventLogRecords, page.Partial
		for _, p := range page.Pages {
			items += int(p.ItemCount)
		}
	} else {
		ack, err := s.ReadRange(ctx, target, req)
		if err != nil {
			return nil, WrapError(err)
		}
		records, multi, events, items = ack.LogRecords, ack.LogMultipleRecords, ack.EventLogRecords, int(ack.ItemCount)
		// The device returns what fits its answer: fewer than asked for, with more to come.
		res.Partial = ack.MoreItems()
	}
	if len(records)+len(multi)+len(events) != items {
		return nil, errs.New(errs.KindProtocol, errs.CodeMalformedResponse,
			fmt.Sprintf("protocol error: the device returned %d items that are no log records (is %s a trend log, trend log multiple or event log?)", items, ObjectName(object)), nil)
	}
	for i, rec := range records {
		res.Records = append(res.Records, logRecord(cfg.Start+uint32(i), rec))
	}
	for i, rec := range multi {
		res.Records = append(res.Records, logMultipleRecord(cfg.Start+uint32(i), rec))
	}
	for i, rec := range events {
		res.Records = append(res.Records, eventLogRecord(cfg.Start+uint32(i), rec))
	}
	res.Count = len(res.Records)
	// How many records there are is worth a read, and not worth failing for.
	if v, err := s.ReadProperty(ctx, target, object, bacnet.PropertyReference{Identifier: propRecordCount}); err == nil {
		res.Total = uintOf(v)
	}
	return res, nil
}

func logRecord(position uint32, rec service.LogRecord) types.LogRecord {
	out := types.LogRecord{
		Position: position, Timestamp: DateText(rec.Timestamp.Date) + " " + TimeText(rec.Timestamp.Time), Kind: RecordValue,
	}
	if rec.StatusFlags != nil {
		out.StatusFlags = SetBitNames(*rec.StatusFlags, statusFlagNames)
	}
	v := ValueOf(0, 0, rec.Datum)
	switch rec.DatumChoice {
	case service.LogDatumLogStatus:
		out.Kind = RecordStatus
		if rec.Datum.Kind == bacnet.ValueBitString {
			v.Bits = SetBitNames(rec.Datum.BitString, logStatusNames)
			v.Text = "{" + strings.Join(v.Bits, ", ") + "}"
		}
	case service.LogDatumFailure:
		out.Kind = RecordFailure
		if class, code, err := bacnet.DecodeBACnetError(rec.Datum.Elements); err == nil {
			v = types.Value{Type: types.TypeEnumerated, Value: code, Name: bacnet.ErrorCodeName(code),
				Text: fmt.Sprintf("%s (error class %s)", bacnet.ErrorCodeName(code), bacnet.ErrorClassName(class))}
		}
	case service.LogDatumTimeChange:
		out.Kind = RecordTimeChange
		v.Unit = "s"
	default:
		// A logged value.
	}
	out.Value = &v
	return out
}

// logMultipleRecord converts a record of a trend log multiple: a log status,
// a time change, or one value per logged property (each a value or a failure).
func logMultipleRecord(position uint32, rec service.LogMultipleRecord) types.LogRecord {
	out := types.LogRecord{Position: position, Timestamp: DateText(rec.Timestamp.Date) + " " + TimeText(rec.Timestamp.Time)}
	switch rec.Choice {
	case service.LogDataLogStatus:
		out.Kind = RecordStatus
		bits := SetBitNames(rec.LogStatus, logStatusNames)
		out.Value = &types.Value{Type: types.TypeBitString, Value: bits, Bits: bits, Text: "{" + strings.Join(bits, ", ") + "}"}
	case service.LogDataTimeChange:
		out.Kind = RecordTimeChange
		v := ValueOf(0, 0, bacnet.RealValue(rec.TimeChange))
		v.Unit = "s"
		out.Value = &v
	default:
		out.Kind = RecordValues
		out.Values = make([]types.Value, 0, len(rec.Values))
		for _, d := range rec.Values {
			out.Values = append(out.Values, logDatum(d.Choice, d.Value))
		}
	}
	return out
}

// eventLogRecord converts a record of an event log: a log status, a time
// change, or the event notification the log received.
func eventLogRecord(position uint32, rec service.EventLogRecord) types.LogRecord {
	out := types.LogRecord{Position: position, Timestamp: DateText(rec.Timestamp.Date) + " " + TimeText(rec.Timestamp.Time)}
	switch {
	case rec.Choice == service.LogDataLogStatus:
		out.Kind = RecordStatus
		bits := SetBitNames(rec.LogStatus, logStatusNames)
		out.Value = &types.Value{Type: types.TypeBitString, Value: bits, Bits: bits, Text: "{" + strings.Join(bits, ", ") + "}"}
	case rec.Choice == service.LogDataTimeChange:
		out.Kind = RecordTimeChange
		v := ValueOf(0, 0, bacnet.RealValue(rec.TimeChange))
		v.Unit = "s"
		out.Value = &v
	case rec.Notification != nil:
		n := rec.Notification
		out.Kind = RecordEvent
		ev := &types.LogEvent{
			EventObject: ObjectName(n.EventObject), EventType: namedOr(eventTypeNames, n.EventType),
			ToState: eventStateName(n.ToState), NotificationClass: n.NotificationClass, Priority: n.Priority,
		}
		if n.FromState != nil {
			ev.FromState = eventStateName(*n.FromState)
		}
		if n.MessageText != nil {
			ev.Message = n.MessageText.Value
		}
		out.Event = ev
	}
	return out
}

// logDatum converts one logged value of a trend log multiple.
func logDatum(choice service.LogDatumChoice, v bacnet.ApplicationValue) types.Value {
	if choice == service.LogDatumFailure {
		if class, code, err := bacnet.DecodeBACnetError(v.Elements); err == nil {
			return types.Value{Type: types.TypeEnumerated, Value: code, Name: bacnet.ErrorCodeName(code),
				Text: fmt.Sprintf("%s (error class %s)", bacnet.ErrorCodeName(code), bacnet.ErrorClassName(class))}
		}
	}
	return ValueOf(0, 0, v)
}
