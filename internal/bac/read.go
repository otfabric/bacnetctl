// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Names of the services in output.
const (
	ServiceReadProperty         = "read-property"
	ServiceReadPropertyMultiple = "read-property-multiple"
)

// maxListItems bounds the lists bacnetctl reads element by element.
const maxListItems = 100000

func isSpecial(p bacnet.PropertyIdentifier) bool {
	return p == PropertyAll || p == PropertyRequired || p == PropertyOptional
}

func tooLarge(err error) bool {
	var abort *bacnet.AbortError
	if !errors.As(err, &abort) {
		return false
	}
	switch abort.Reason {
	case bacnet.AbortReasonBufferOverflow, bacnet.AbortReasonSegmentationNotSupported, bacnet.AbortReasonAPDUTooLong:
		return true
	}
	return false
}

// ReadMany reads the properties of specs and returns one result per spec, in order, with
// the error of a property that could not be read in its place.
//
// It uses ReadPropertyMultiple, in as many requests as the device needs. A device without
// that service, and an answer that does not fit the device's buffer even for one object,
// are read with one ReadProperty per property instead: the properties behind "all" are
// then taken from property-list.
func (s *Session) ReadMany(ctx context.Context, target client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
	// "device" without an instance is resolved once, up front: a device that does not
	// answer the wildcard instance would report unknown-object for every property.
	resolved := make([]service.ReadAccessSpecification, len(specs))
	copy(resolved, specs)
	for i := range resolved {
		if resolved[i].Object == bacnet.AnyDevice() {
			object, err := s.ResolveObject(ctx, target, resolved[i].Object)
			if err != nil {
				return nil, err
			}
			resolved[i].Object = object
		}
	}
	specs = resolved
	results, err := s.ReadPropertyMultipleBatched(ctx, target, specs, client.RPMBatchOptions{})
	if err == nil {
		return results, nil
	}
	if !NotImplemented(err) && !tooLarge(err) {
		return nil, err
	}
	rpm := !NotImplemented(err)
	out := make([]service.ReadAccessResult, len(specs))
	for i, spec := range specs {
		if rpm {
			one, err := s.ReadPropertyMultipleBatched(ctx, target, []service.ReadAccessSpecification{spec}, client.RPMBatchOptions{})
			if err == nil {
				out[i] = one[0]
				continue
			}
			if !tooLarge(err) {
				return nil, err
			}
		}
		res, err := s.readSingly(ctx, target, spec)
		if err != nil {
			return nil, err
		}
		out[i] = res
	}
	return out, nil
}

// readSingly reads the properties of one object with one ReadProperty each.
func (s *Session) readSingly(ctx context.Context, target client.Target, spec service.ReadAccessSpecification) (service.ReadAccessResult, error) {
	object, err := s.ResolveObject(ctx, target, spec.Object)
	if err != nil {
		return service.ReadAccessResult{}, err
	}
	res := service.ReadAccessResult{Object: object}
	for _, ref := range spec.Properties {
		refs := []bacnet.PropertyReference{ref}
		if isSpecial(ref.Identifier) {
			expanded, err := s.expand(ctx, target, spec.Object, ref.Identifier)
			if err != nil {
				// What cannot be asked for is the error of its row; a device that is
				// gone ends the command.
				if _, typed := errs.As(err); !typed && Fatal(err) {
					return res, err
				}
				res.Properties = append(res.Properties, service.PropertyResult{Property: ref, Err: err})
				continue
			}
			refs = expanded
		}
		for _, r := range refs {
			v, err := s.ReadProperty(ctx, target, spec.Object, r)
			if err != nil && Fatal(err) {
				return res, err
			}
			res.Properties = append(res.Properties, service.PropertyResult{Property: r, Value: v, Err: err})
		}
	}
	return res, nil
}

// expand lists the properties "all" stands for. Without ReadPropertyMultiple a device
// cannot say which of its properties are required and which optional.
func (s *Session) expand(ctx context.Context, target client.Target, object bacnet.ObjectIdentifier, special bacnet.PropertyIdentifier) ([]bacnet.PropertyReference, error) {
	if special != PropertyAll {
		return nil, errs.New(errs.KindProtocol, errs.CodeUnsupported, fmt.Sprintf(
			"%q needs ReadPropertyMultiple, which the device does not answer for this object; read \"all\" instead", PropertyName(special)), nil)
	}
	list, err := s.ReadPropertyList(ctx, target, object, 0)
	if err != nil {
		return nil, err
	}
	// property-list leaves out the three properties every object has, and itself.
	refs := []bacnet.PropertyReference{
		{Identifier: bacnet.PropertyObjectIdentifier}, {Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyObjectType},
	}
	for _, p := range list {
		switch p {
		case bacnet.PropertyObjectIdentifier, bacnet.PropertyObjectName, bacnet.PropertyObjectType:
		default:
			refs = append(refs, bacnet.PropertyReference{Identifier: p})
		}
	}
	return refs, nil
}

// ReadRequest is what `read` reads: the properties of one object.
type ReadRequest = service.ReadAccessSpecification

// ParseIndex parses --index: empty for none.
func ParseIndex(s string) (*uint32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || uint32(n) == bacnet.ArrayAll {
		return nil, fmt.Errorf("--index %q: an array index is a number, 0 for the number of elements", s)
	}
	idx := uint32(n)
	return &idx, nil
}

// ParseReadArgs parses the arguments of `read`: OBJECT PROPERTY[,PROPERTY…] pairs.
func ParseReadArgs(args []string, index string) ([]ReadRequest, error) {
	if len(args) == 0 || len(args)%2 != 0 {
		return nil, fmt.Errorf("give OBJECT PROPERTY pairs, for example: analog-input:1 present-value")
	}
	idx, err := ParseIndex(index)
	if err != nil {
		return nil, err
	}
	var out []ReadRequest
	for i := 0; i < len(args); i += 2 {
		obj, err := ParseObject(args[i])
		if err != nil {
			return nil, err
		}
		props, err := ParseProperties(args[i+1])
		if err != nil {
			return nil, err
		}
		for j := range props {
			if idx == nil {
				continue
			}
			if props[j].ArrayIndex != nil || isSpecial(props[j].Identifier) {
				return nil, fmt.Errorf("--index cannot be combined with %s", args[i+1])
			}
			props[j].ArrayIndex = idx
		}
		out = append(out, ReadRequest{Object: obj, Properties: props})
	}
	return out, nil
}

// CheckRead validates the configuration of `read`.
func CheckRead(cfg *config.ReadConfig, args []string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseReadArgs(args, cfg.Index)
	return err
}

func propertyRow(object bacnet.ObjectIdentifier, p service.PropertyResult) types.PropertyRow {
	row := types.PropertyRow{ObjectRef: objRef(object), PropertyRef: propRef(p.Property)}
	if p.Err != nil {
		row.Error = PropertyErrorInfo(p.Err)
		return row
	}
	v := ValueOf(object.Type, p.Property.Identifier, p.Value)
	row.Value = &v
	return row
}

// Read reads properties: one with ReadProperty, more with ReadPropertyMultiple.
func Read(ctx context.Context, cfg *config.ReadConfig, args []string, logw io.Writer) (*types.ReadResult, error) {
	reqs, _ := ParseReadArgs(args, cfg.Index)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	res := &types.ReadResult{Target: TargetText(target), Service: ServiceReadPropertyMultiple, Rows: []types.PropertyRow{}}

	if len(reqs) == 1 && len(reqs[0].Properties) == 1 && !isSpecial(reqs[0].Properties[0].Identifier) {
		// One property: the service every device has. A refusal is the result of the command.
		res.Service = ServiceReadProperty
		ref := reqs[0].Properties[0]
		// "device" without an instance is resolved first: not every device answers
		// the wildcard instance, and the row names the real object either way.
		object, err := s.ResolveObject(ctx, target, reqs[0].Object)
		if err != nil {
			return nil, err
		}
		v, err := s.ReadProperty(ctx, target, object, ref)
		if err != nil {
			return nil, WrapError(err)
		}
		res.Rows = append(res.Rows, propertyRow(object, service.PropertyResult{Property: ref, Value: v}))
		res.Count = 1
		return res, nil
	}

	results, err := s.ReadMany(ctx, target, reqs)
	if err != nil {
		return nil, WrapError(err)
	}
	for _, r := range results {
		for _, p := range r.Properties {
			row := propertyRow(r.Object, p)
			if row.Error != nil {
				res.Failed++
			}
			res.Rows = append(res.Rows, row)
		}
	}
	AttachUnits(res.Rows)
	res.Count = len(res.Rows)
	return res, nil
}

// CheckDescribe validates the configuration of `describe`.
func CheckDescribe(cfg *config.DescribeConfig, object string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseObject(object)
	return err
}

// Describe reads every property of one object.
func Describe(ctx context.Context, cfg *config.DescribeConfig, objectArg string, logw io.Writer) (*types.DescribeResult, error) {
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
	results, err := s.ReadMany(ctx, target, []service.ReadAccessSpecification{
		{Object: object, Properties: []bacnet.PropertyReference{{Identifier: PropertyAll}}},
	})
	if err != nil {
		return nil, WrapError(err)
	}
	r := results[0]
	// An object the device does not have answers "all" with one error for "all" itself.
	if len(r.Properties) == 1 && r.Properties[0].Err != nil && r.Properties[0].Property.Identifier == PropertyAll {
		return nil, WrapError(r.Properties[0].Err)
	}
	res := &types.DescribeResult{Target: TargetText(target), ObjectRef: objRef(r.Object), Properties: []types.PropertyRow{}}
	for _, p := range r.Properties {
		row := propertyRow(r.Object, p)
		if row.Error != nil {
			res.Failed++
		}
		if p.Property.Identifier == bacnet.PropertyObjectName && p.Err == nil {
			res.Name = stringOf(p.Value)
		}
		res.Properties = append(res.Properties, row)
	}
	AttachUnits(res.Properties)
	res.Count = len(res.Properties)
	return res, nil
}
