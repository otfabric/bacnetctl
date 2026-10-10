// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"io"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// CheckObjects validates the configuration of `objects`.
func CheckObjects(cfg *config.ObjectsConfig) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseObjectTypes(cfg.Types)
	return err
}

// Objects lists the objects of a device with their names and, with --values, their
// present value, units, status flags and description.
func Objects(ctx context.Context, cfg *config.ObjectsConfig, logw io.Writer) (*types.ObjectsResult, error) {
	only, _ := ParseObjectTypes(cfg.Types)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return nil, err
	}
	device, err := s.DeviceID(ctx, target)
	if err != nil {
		return nil, err
	}
	list, err := s.ReadObjectList(ctx, target, device, maxListItems)
	if err != nil {
		return nil, WrapError(err)
	}
	res := &types.ObjectsResult{Target: TargetText(target), Device: ObjectName(device), Total: len(list), Values: cfg.Values, Objects: []types.ObjectRow{}}

	wanted := map[bacnet.ObjectType]bool{}
	for _, t := range only {
		wanted[t] = true
	}
	var specs []service.ReadAccessSpecification
	for _, id := range list {
		if len(wanted) > 0 && !wanted[id.Type] {
			continue
		}
		if cfg.Limit > 0 && uint32(len(specs)) >= cfg.Limit {
			break
		}
		props := []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}}
		if cfg.Values {
			props = append(props,
				bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				bacnet.PropertyReference{Identifier: bacnet.PropertyUnits},
				bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags},
				bacnet.PropertyReference{Identifier: bacnet.PropertyDescription})
		}
		specs = append(specs, service.ReadAccessSpecification{Object: id, Properties: props})
	}
	if len(specs) == 0 {
		return res, nil
	}
	results, err := s.ReadMany(ctx, target, specs)
	if err != nil {
		return nil, WrapError(err)
	}
	for _, r := range results {
		row := objectRow(r)
		if row.Error != nil {
			res.Failed++
		}
		res.Objects = append(res.Objects, row)
	}
	res.Count = len(res.Objects)
	return res, nil
}

// objectRow builds the row of one object. Only a name that cannot be read makes the row
// a failure: many objects have no present value, units or description.
func objectRow(r service.ReadAccessResult) types.ObjectRow {
	row := types.ObjectRow{ObjectRef: objRef(r.Object)}
	for _, p := range r.Properties {
		if p.Err != nil {
			if p.Property.Identifier == bacnet.PropertyObjectName {
				row.Error = PropertyErrorInfo(p.Err)
			}
			continue
		}
		switch p.Property.Identifier {
		case bacnet.PropertyObjectName:
			row.Name = stringOf(p.Value)
		case bacnet.PropertyDescription:
			row.Description = stringOf(p.Value)
		case bacnet.PropertyPresentValue:
			v := ValueOf(r.Object.Type, bacnet.PropertyPresentValue, p.Value)
			row.PresentValue = &v
		case bacnet.PropertyUnits:
			if p.Value.Kind == bacnet.ValueEnumerated {
				u := p.Value.Enumerated
				row.UnitsID = &u
				row.Units = UnitName(u)
			}
		case bacnet.PropertyStatusFlags:
			if p.Value.Kind == bacnet.ValueBitString {
				row.StatusFlags = SetBitNames(p.Value.BitString, statusFlagNames)
			}
		default:
		}
	}
	if row.PresentValue != nil && row.UnitsID != nil {
		row.PresentValue.Unit = UnitSymbol(*row.UnitsID)
	}
	return row
}
