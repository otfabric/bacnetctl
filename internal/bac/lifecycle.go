// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Statuses of `create` and `delete` in output.
const (
	StatusCreated = "created"
	StatusDeleted = "deleted"
)

// CreateRequest is a parsed `create` invocation.
type CreateRequest struct {
	// Type is the object type; Instance is set when the invocation names one, and the
	// device chooses it otherwise.
	Type     bacnet.ObjectType
	Instance *uint32
	Initial  []service.PropertyValue
}

// ParseCreate parses the arguments and flags of `create`: TYPE or TYPE:INSTANCE, then
// PROPERTY=VALUE initial values.
func ParseCreate(cfg *config.CreateConfig, object string, initial []string) (CreateRequest, error) {
	var req CreateRequest
	if strings.Contains(object, ":") {
		id, err := ParseObject(object)
		if err != nil {
			return req, err
		}
		req.Type, req.Instance = id.Type, &id.Instance
	} else {
		typ, err := ParseObjectType(object)
		if err != nil {
			return req, err
		}
		req.Type = typ
	}
	if req.Type == bacnet.ObjectTypeDevice {
		return req, fmt.Errorf("a device cannot create another Device object")
	}
	if cfg.Name != "" {
		initial = append([]string{"object-name=string:" + cfg.Name}, initial...)
	}
	seen := map[bacnet.PropertyIdentifier]bool{}
	for _, arg := range initial {
		name, value, ok := strings.Cut(arg, "=")
		if !ok || name == "" {
			return req, fmt.Errorf("initial value %q: write PROPERTY=VALUE, as in description=string:Spare", arg)
		}
		ref, err := parsePropertyReference(name)
		if err != nil {
			return req, err
		}
		if isSpecial(ref.Identifier) {
			return req, fmt.Errorf("initial value %q: %s is not a property that holds a value", arg, name)
		}
		if seen[ref.Identifier] && ref.ArrayIndex == nil {
			return req, fmt.Errorf("initial value %q: %s is given twice (--name is the object-name)", arg, PropertyName(ref.Identifier))
		}
		seen[ref.Identifier] = true
		// The object does not exist yet, so nothing can be read to tell the type.
		typ, text, ok := SplitValueSpec(value)
		if !ok {
			return req, fmt.Errorf("initial value %q: say the type of the value, as in %s=string:%s (see \"bacnetctl write --help\" for the types)", arg, name, value)
		}
		v, err := ParseTypedValue(typ, text, ValueContext{ObjectType: req.Type, Property: ref.Identifier})
		if err != nil {
			return req, err
		}
		req.Initial = append(req.Initial, service.PropertyValue{Property: ref, Value: v})
	}
	return req, nil
}

// CheckCreate validates the configuration of `create`.
func CheckCreate(cfg *config.CreateConfig, object string, initial []string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseCreate(cfg, object, initial)
	return err
}

// Create makes an object in a device with CreateObject.
func Create(ctx context.Context, cfg *config.CreateConfig, object string, initial []string, logw io.Writer) (*types.ObjectChangeResult, error) {
	parsed, _ := ParseCreate(cfg, object, initial)
	req := service.CreateObjectRequest{InitialValues: parsed.Initial}
	if parsed.Instance != nil {
		req.ObjectIdentifier = &bacnet.ObjectIdentifier{Type: parsed.Type, Instance: *parsed.Instance}
	} else {
		req.ObjectType = &parsed.Type
	}
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
	ack, err := s.CreateObject(ctx, target, req)
	if err != nil {
		return nil, WrapError(err)
	}
	return &types.ObjectChangeResult{Target: TargetText(target), ObjectRef: objRef(ack.Object), Status: StatusCreated, RTTMS: ms(time.Since(start))}, nil
}

// CheckDelete validates the configuration of `delete`.
func CheckDelete(cfg *config.DeleteConfig, object string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	id, err := ParseObject(object)
	if err != nil {
		return err
	}
	if id.Type == bacnet.ObjectTypeDevice {
		return fmt.Errorf("the Device object of a device cannot be deleted")
	}
	if !cfg.Sure {
		return errs.New(errs.KindUsage, errs.CodeConfirmationRequired, fmt.Sprintf(
			"delete removes %s from the device, with everything that refers to it left dangling: confirm with --%s (nothing was sent)",
			ObjectName(id), SureFlag), nil)
	}
	return nil
}

// Delete removes an object from a device with DeleteObject.
func Delete(ctx context.Context, cfg *config.DeleteConfig, object string, logw io.Writer) (*types.ObjectChangeResult, error) {
	id, _ := ParseObject(object)
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
	if err := s.DeleteObject(ctx, target, service.DeleteObjectRequest{Object: id}); err != nil {
		return nil, WrapError(err)
	}
	return &types.ObjectChangeResult{Target: TargetText(target), ObjectRef: objRef(id), Status: StatusDeleted, RTTMS: ms(time.Since(start))}, nil
}
