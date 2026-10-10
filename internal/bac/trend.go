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
	res := &types.TrendResult{Target: TargetText(target), ObjectRef: objRef(object), Start: cfg.Start, Records: []types.LogRecord{}}
	req := service.ReadRangeRequest{
		Object: object, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By: service.ReadRangeByPosition, ReferenceIndex: cfg.Start, Count: int32(cfg.Count),
	}
	var records []service.LogRecord
	items := 0
	if cfg.All {
		page, err := s.ReadRangeAll(ctx, target, req, client.ReadRangePageOptions{PageCount: int32(cfg.Count), MaxItems: maxTrendRecords, MaxPages: maxTrendRecords})
		if err != nil {
			return nil, WrapError(err)
		}
		records, res.Partial = page.LogRecords, page.Partial
		for _, p := range page.Pages {
			items += int(p.ItemCount)
		}
	} else {
		ack, err := s.ReadRange(ctx, target, req)
		if err != nil {
			return nil, WrapError(err)
		}
		records, items = ack.LogRecords, int(ack.ItemCount)
		// The device returns what fits its answer: fewer than asked for, with more to come.
		res.Partial = ack.MoreItems()
	}
	if len(records) != items {
		return nil, errs.New(errs.KindProtocol, errs.CodeMalformedResponse,
			fmt.Sprintf("protocol error: the device returned %d items that are no trend log records (is %s a trend log?)", items, ObjectName(object)), nil)
	}
	for i, rec := range records {
		res.Records = append(res.Records, logRecord(cfg.Start+uint32(i), rec))
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
