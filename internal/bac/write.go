// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Statuses of a write in output.
const (
	StatusWritten      = "written"
	StatusRelinquished = "relinquished"
	StatusAcknowledged = "acknowledged"
)

// WriteRequest is a parsed `write` invocation.
type WriteRequest struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
	Priority *uint8
	// Type is the value type, or TypeAuto when the current value of the property decides.
	Type string
	Text string
}

func parsePriority(p uint8) (*uint8, error) {
	if p == 0 {
		return nil, nil
	}
	if p > 16 {
		return nil, fmt.Errorf("--priority %d: command priorities are 1 (highest) to 16 (lowest)", p)
	}
	return &p, nil
}

// ParseWrite parses the arguments and flags of `write`.
func ParseWrite(cfg *config.WriteConfig, object, property, value string) (WriteRequest, error) {
	var req WriteRequest
	var err error
	if req.Object, err = ParseObject(object); err != nil {
		return req, err
	}
	refs, err := ParseProperties(property)
	if err != nil {
		return req, err
	}
	if len(refs) != 1 || isSpecial(refs[0].Identifier) {
		return req, fmt.Errorf("write takes exactly one property, not %q", property)
	}
	req.Property = refs[0]
	idx, err := ParseIndex(cfg.Index)
	if err != nil {
		return req, err
	}
	if idx != nil {
		if req.Property.ArrayIndex != nil {
			return req, fmt.Errorf("--index cannot be combined with %s", property)
		}
		req.Property.ArrayIndex = idx
	}
	if req.Priority, err = parsePriority(cfg.Priority); err != nil {
		return req, err
	}
	typ, ok := CanonicalType(cfg.Type)
	if cfg.Type == "" {
		typ, ok = TypeAuto, true
	}
	if !ok {
		return req, fmt.Errorf("--type %q: use auto, null, bool, unsigned, signed, real, double, string, octets, bits, enum, date, time or object", cfg.Type)
	}
	req.Type, req.Text = typ, value
	if typ == TypeAuto {
		// A value that says its own type needs no look at the device.
		if t, text, ok := SplitValueSpec(value); ok {
			req.Type, req.Text = t, text
		}
	}
	if req.Type != TypeAuto {
		// Everything that can be wrong with the value is found before anything is sent.
		if _, err := ParseTypedValue(req.Type, req.Text, ValueContext{ObjectType: req.Object.Type, Property: req.Property.Identifier}); err != nil {
			return req, err
		}
	}
	return req, nil
}

// CheckWrite validates the configuration of `write`.
func CheckWrite(cfg *config.WriteConfig, object, property, value string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseWrite(cfg, object, property, value)
	return err
}

// Write writes one property. A value without a type is written with the type the
// property holds now, which is read first.
func Write(ctx context.Context, cfg *config.WriteConfig, object, property, value string, logw io.Writer) (*types.WriteResult, error) {
	req, _ := ParseWrite(cfg, object, property, value)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	if req.Object, err = s.ResolveObject(ctx, target, req.Object); err != nil {
		return nil, err
	}
	vc := ValueContext{ObjectType: req.Object.Type, Property: req.Property.Identifier}
	if err := s.resolveType(ctx, target, &req); err != nil {
		return nil, err
	}
	v, err := ParseTypedValue(req.Type, req.Text, vc)
	if err != nil {
		return nil, errs.WrapValidation(err)
	}
	start := time.Now()
	if err := s.WriteProperty(ctx, target, req.Object, req.Property, v, req.Priority); err != nil {
		return nil, WrapError(err)
	}
	res := &types.WriteResult{
		Target: TargetText(target), ObjectRef: objRef(req.Object), PropertyRef: propRef(req.Property),
		Value: ValueOf(req.Object.Type, req.Property.Identifier, v), Priority: req.Priority, Status: StatusWritten, RTTMS: ms(time.Since(start)),
	}
	if v.Kind == bacnet.ValueNull && req.Priority != nil {
		res.Status = StatusRelinquished
	}
	return res, nil
}

// CheckRelease validates the configuration of `release`.
func CheckRelease(cfg *config.ReleaseConfig, object string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	if _, err := ParseObject(object); err != nil {
		return err
	}
	if cfg.Priority == 0 || cfg.Priority > 16 {
		return fmt.Errorf("--priority is required: the command priority to relinquish, 1 to 16")
	}
	_, err := ParseProperty(cfg.Property)
	return err
}

// Release relinquishes one command priority: it writes NULL at that priority.
func Release(ctx context.Context, cfg *config.ReleaseConfig, objectArg string, logw io.Writer) (*types.WriteResult, error) {
	object, _ := ParseObject(objectArg)
	prop, _ := ParseProperty(cfg.Property)
	ref := bacnet.PropertyReference{Identifier: prop}
	priority := cfg.Priority
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	if err := s.WriteProperty(ctx, target, object, ref, bacnet.NullValue(), &priority); err != nil {
		return nil, WrapError(err)
	}
	return &types.WriteResult{
		Target: TargetText(target), ObjectRef: objRef(object), PropertyRef: propRef(ref),
		Value: ValueOf(object.Type, prop, bacnet.NullValue()), Priority: &priority, Status: StatusRelinquished, RTTMS: ms(time.Since(start)),
	}, nil
}

// Statuses of one write among several.
const (
	StatusFailed     = "failed"
	StatusNotWritten = "not-written"
)

// ParseWrites parses the OBJECT PROPERTY VALUE triples of `write`.
func ParseWrites(cfg *config.WriteConfig, args []string) ([]WriteRequest, error) {
	if len(args) == 0 || len(args)%3 != 0 {
		return nil, fmt.Errorf("write takes OBJECT PROPERTY VALUE, once or several times: %d arguments do not divide into threes", len(args))
	}
	if len(args) > 3 && cfg.Index != "" {
		return nil, fmt.Errorf("--index belongs to one write: with several, write the index with the property, as in priority-array[8]")
	}
	reqs := make([]WriteRequest, 0, len(args)/3)
	for i := 0; i < len(args); i += 3 {
		req, err := ParseWrite(cfg, args[i], args[i+1], args[i+2])
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, req)
	}
	return reqs, nil
}

// CheckWrites validates the configuration of `write`.
func CheckWrites(cfg *config.WriteConfig, args []string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseWrites(cfg, args)
	return err
}

// resolveType settles the type of a value that does not say it: the type the property
// holds now, which is read.
func (s *Session) resolveType(ctx context.Context, target client.Target, req *WriteRequest) error {
	if req.Type != TypeAuto {
		return nil
	}
	current, err := s.ReadProperty(ctx, target, req.Object, req.Property)
	if err != nil {
		return WrapError(err)
	}
	typ, ok := TypeOfKind(current.Kind)
	if !ok {
		msg := fmt.Sprintf("cannot tell the type of %q from the current value of %s %s (%s): say it, as in real:%s or --type real",
			req.Text, ObjectName(req.Object), PropertyName(req.Property.Identifier), KindName(current.Kind), req.Text)
		return errs.InvalidInput(errs.CodeInvalidInput, msg, nil)
	}
	req.Type = typ
	return nil
}

// WriteMultiple writes several properties with one WritePropertyMultiple request. The
// device carries the writes out in order and stops at the first it refuses: that one is
// reported as failed, and those after it as not written.
func WriteMultiple(ctx context.Context, cfg *config.WriteConfig, args []string, logw io.Writer) (*types.WriteMultipleResult, error) {
	reqs, _ := ParseWrites(cfg, args)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	res := &types.WriteMultipleResult{Target: TargetText(target), Service: "write-property-multiple", Count: len(reqs)}
	specs := make([]service.WriteAccessSpecification, 0, len(reqs))
	for i := range reqs {
		req := &reqs[i]
		if req.Object, err = s.ResolveObject(ctx, target, req.Object); err != nil {
			return nil, err
		}
		if err := s.resolveType(ctx, target, req); err != nil {
			return nil, err
		}
		v, err := ParseTypedValue(req.Type, req.Text, ValueContext{ObjectType: req.Object.Type, Property: req.Property.Identifier})
		if err != nil {
			return nil, errs.WrapValidation(err)
		}
		specs = append(specs, service.WriteAccessSpecification{Object: req.Object, Properties: []service.WritePropertyValue{
			{Property: req.Property, Value: v, Priority: req.Priority},
		}})
		row := types.WriteRow{
			ObjectRef: objRef(req.Object), PropertyRef: propRef(req.Property),
			Value: ValueOf(req.Object.Type, req.Property.Identifier, v), Priority: req.Priority, Status: StatusWritten,
		}
		if v.Kind == bacnet.ValueNull && req.Priority != nil {
			row.Status = StatusRelinquished
		}
		res.Writes = append(res.Writes, row)
	}
	start := time.Now()
	err = s.WritePropertyMultiple(ctx, target, specs)
	res.RTTMS = ms(time.Since(start))
	if err == nil {
		return res, nil
	}
	var refused *service.WritePropertyMultipleError
	if !errors.As(err, &refused) {
		return nil, WrapError(err)
	}
	failed := -1
	for i, req := range reqs {
		if req.Object != refused.FirstFailed || req.Property.Identifier != refused.FirstProperty.Identifier {
			continue
		}
		// Two writes to elements of one array differ in the index only.
		if a, b := req.Property.ArrayIndex, refused.FirstProperty.ArrayIndex; a != nil && b != nil && *a != *b {
			continue
		}
		failed = i
		break
	}
	if failed < 0 {
		// The device names a write that was not asked for: nothing can be said about any.
		return nil, WrapError(err)
	}
	res.Failed, res.NotWritten = 1, len(reqs)-failed-1
	res.Writes[failed].Status = StatusFailed
	res.Writes[failed].Error = PropertyErrorInfo(&bacnet.ErrorResponse{Class: refused.Class, Code: refused.Code})
	for i := failed + 1; i < len(res.Writes); i++ {
		res.Writes[i].Status = StatusNotWritten
	}
	return res, nil
}
