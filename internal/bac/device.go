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

// deviceProperties are the properties of the Device object `device` shows.
var deviceProperties = []bacnet.PropertyIdentifier{
	bacnet.PropertyObjectName, bacnet.PropertyDescription, propLocation, bacnet.PropertyVendorName,
	bacnet.PropertyVendorIdentifier, bacnet.PropertyModelName, propSerialNumber, propFirmwareRevision,
	propApplicationSoftware, bacnet.PropertySystemStatus, bacnet.PropertyProtocolVersion, bacnet.PropertyProtocolRevision,
	bacnet.PropertyMaxAPDULength, bacnet.PropertySegmentation, propMaxSegmentsAccepted, propAPDUTimeout, propAPDURetries,
	propDatabaseRevision, propLocalDate, propLocalTime, propUTCOffset, propServicesSupported, propObjectTypesSupported,
}

// CheckDevice validates the configuration of `device`.
func CheckDevice(cfg *config.DeviceConfig) error { return CheckConn(&cfg.ConnConfig) }

// DeviceInfo reads what a device says about itself.
func DeviceInfo(ctx context.Context, cfg *config.DeviceConfig, logw io.Writer) (*types.DeviceResult, error) {
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
	spec := service.ReadAccessSpecification{Object: device}
	for _, p := range deviceProperties {
		spec.Properties = append(spec.Properties, bacnet.PropertyReference{Identifier: p})
	}
	// The number of objects is element 0 of object-list: the list itself can be long.
	zero := uint32(0)
	spec.Properties = append(spec.Properties, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &zero})
	results, err := s.ReadMany(ctx, target, []service.ReadAccessSpecification{spec})
	if err != nil {
		return nil, WrapError(err)
	}
	res := &types.DeviceResult{Target: TargetText(target), ObjectRef: objRef(device)}
	for _, p := range results[0].Properties {
		if p.Err == nil {
			setDeviceField(res, p)
		}
	}
	return res, nil
}

func uintOf(v bacnet.ApplicationValue) *uint32 {
	switch v.Kind {
	case bacnet.ValueUnsigned:
		n := uint32(v.Unsigned)
		return &n
	case bacnet.ValueEnumerated:
		n := v.Enumerated
		return &n
	default:
		return nil
	}
}

func setDeviceField(res *types.DeviceResult, p service.PropertyResult) {
	v := p.Value
	switch p.Property.Identifier {
	case bacnet.PropertyObjectName:
		res.Name = stringOf(v)
	case bacnet.PropertyDescription:
		res.Description = stringOf(v)
	case propLocation:
		res.Location = stringOf(v)
	case bacnet.PropertyVendorName:
		res.VendorName = stringOf(v)
	case bacnet.PropertyVendorIdentifier:
		res.VendorID = uintOf(v)
	case bacnet.PropertyModelName:
		res.ModelName = stringOf(v)
	case propSerialNumber:
		res.SerialNumber = stringOf(v)
	case propFirmwareRevision:
		res.FirmwareRevision = stringOf(v)
	case propApplicationSoftware:
		res.ApplicationVersion = stringOf(v)
	case bacnet.PropertySystemStatus:
		if res.SystemStatusID = uintOf(v); res.SystemStatusID != nil {
			res.SystemStatus = nameAt(systemStatusNames, *res.SystemStatusID)
		}
	case bacnet.PropertyProtocolVersion:
		res.ProtocolVersion = uintOf(v)
	case bacnet.PropertyProtocolRevision:
		res.ProtocolRevision = uintOf(v)
	case bacnet.PropertyMaxAPDULength:
		res.MaxAPDU = uintOf(v)
	case bacnet.PropertySegmentation:
		if res.SegmentationID = uintOf(v); res.SegmentationID != nil {
			res.Segmentation = nameAt(segmentationNames, *res.SegmentationID)
		}
	case propMaxSegmentsAccepted:
		res.MaxSegments = uintOf(v)
	case propAPDUTimeout:
		res.APDUTimeoutMS = uintOf(v)
	case propAPDURetries:
		res.APDURetries = uintOf(v)
	case propDatabaseRevision:
		res.DatabaseRevision = uintOf(v)
	case propLocalDate:
		if v.Kind == bacnet.ValueDate {
			res.LocalDate = DateText(v.Date)
		}
	case propLocalTime:
		if v.Kind == bacnet.ValueTime {
			res.LocalTime = TimeText(v.Time)
		}
	case propUTCOffset:
		if v.Kind == bacnet.ValueSigned {
			off := v.Signed
			res.UTCOffsetMinutes = &off
		}
	case propServicesSupported:
		if v.Kind == bacnet.ValueBitString {
			res.ServicesSupported = SetBitNames(v.BitString, servicesSupportedNames)
		}
	case propObjectTypesSupported:
		if v.Kind == bacnet.ValueBitString {
			res.ObjectTypesSupported = SetBitNames(v.BitString, bitNames(propObjectTypesSupported))
		}
	case bacnet.PropertyObjectList:
		res.ObjectCount = uintOf(v)
	default:
		// A property `device` does not show.
	}
}
