// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Modes of the commands that ask a network.
const (
	ModeBroadcast = "broadcast"
	ModeUnicast   = "unicast"
)

// DefaultWait is how long answers are collected when --wait is 0.
const DefaultWait = 3 * time.Second

// Wait returns the collection window of a --wait value in milliseconds.
func Wait(ms uint32) time.Duration {
	if ms == 0 {
		return DefaultWait
	}
	return time.Duration(ms) * time.Millisecond
}

// collectPoll is how often the registries of the client are looked at while answers to a
// unicast request are awaited.
const collectPoll = 10 * time.Millisecond

// collect waits for window, or until done reports that nothing more can come.
func collect(ctx context.Context, window time.Duration, done func() bool) error {
	end := time.NewTimer(window)
	defer end.Stop()
	tick := time.NewTicker(collectPoll)
	defer tick.Stop()
	for {
		if done != nil && done() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-end.C:
			return nil
		case <-tick.C:
		}
	}
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

func objRef(id bacnet.ObjectIdentifier) types.ObjectRef {
	return types.ObjectRef{Object: ObjectName(id), ObjectType: uint16(id.Type), Instance: id.Instance}
}

func propRef(p bacnet.PropertyReference) types.PropertyRef {
	ref := types.PropertyRef{Property: PropertyName(p.Identifier), PropertyID: uint32(p.Identifier)}
	if p.ArrayIndex != nil && *p.ArrayIndex != bacnet.ArrayAll {
		idx := *p.ArrayIndex
		ref.Index = &idx
	}
	return ref
}

// CheckDiscover validates the configuration of `discover`.
func CheckDiscover(cfg *config.DiscoverConfig) error {
	if err := CheckSocket(&cfg.SocketConfig); err != nil {
		return err
	}
	if _, _, err := InstanceRange(cfg.Low, cfg.High); err != nil {
		return err
	}
	if _, err := ParseBroadcastNetwork(cfg.Network); err != nil {
		return err
	}
	if cfg.Target != "" {
		if _, err := config.ParseHostPort(cfg.Target); err != nil {
			return fmt.Errorf("--target: %w", err)
		}
	}
	return nil
}

// Discover sends a Who-Is and lists the devices that answered with an I-Am.
func Discover(ctx context.Context, cfg *config.DiscoverConfig, logw io.Writer) (*types.DiscoverResult, error) {
	low, high, _ := InstanceRange(cfg.Low, cfg.High)
	network, _ := ParseBroadcastNetwork(cfg.Network)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	res := &types.DiscoverResult{Mode: ModeBroadcast, Names: cfg.Names, Devices: []types.Device{}}
	opts := client.DiscoveryOptions{LowLimit: low, HighLimit: high, Address: network, Window: Wait(cfg.Wait)}
	start := time.Now()
	var found []client.DeviceObservation
	if cfg.Target != "" {
		ep, err := ResolveEndpoint(ctx, cfg.Target)
		if err != nil {
			return nil, err
		}
		res.Mode, res.Target = ModeUnicast, ep.String()
		if err := s.SendWhoIs(ctx, ep, false, opts); err != nil {
			return nil, WrapError(err)
		}
		// One device answers a Who-Is sent to its own address: stop at its I-Am. A router
		// asked for a network behind it passes on as many answers as there are devices.
		var first func() bool
		if network.Scope() != bacnet.AddressRemoteBroadcast && network.Scope() != bacnet.AddressGlobalBroadcast {
			first = func() bool { return len(s.Devices()) > 0 }
		}
		if err := collect(ctx, opts.Window, first); err != nil {
			return nil, WrapError(err)
		}
		found = s.Devices()
	} else {
		if found, err = s.Discover(ctx, opts); err != nil {
			return nil, WrapError(err)
		}
	}
	res.DurationMS = ms(time.Since(start))

	sort.Slice(found, func(i, j int) bool {
		if found[i].Instance != found[j].Instance {
			return found[i].Instance < found[j].Instance
		}
		return found[i].Origin.String() < found[j].Origin.String()
	})
	for _, obs := range found {
		dev := deviceOf(obs)
		if cfg.Names {
			if err := s.readNames(ctx, obs, &dev); err != nil {
				if Fatal(err) && ctx.Err() != nil {
					return nil, WrapError(err)
				}
				dev.Error = EmbeddedErrorInfo(err)
			}
		}
		res.Devices = append(res.Devices, dev)
	}
	res.Count = len(res.Devices)
	return res, nil
}

func deviceOf(obs client.DeviceObservation) types.Device {
	seg := obs.Capabilities.Segmentation.Value
	dev := types.Device{
		Instance:       obs.Instance,
		Address:        obs.Origin.String(),
		VendorID:       obs.Capabilities.VendorID.Value,
		MaxAPDU:        obs.Capabilities.MaxAPDULengthAccepted.Value,
		Segmentation:   nameAt(segmentationNames, uint32(seg)),
		SegmentationID: seg,
	}
	if !obs.Origin.IsValid() {
		dev.Address = obs.ImmediatePeer.String()
	}
	if dev.Segmentation == "" {
		dev.Segmentation = fmt.Sprint(seg)
	}
	if obs.Address.Scope() == bacnet.AddressRemoteStation {
		n := obs.Address.Network()
		dev.Network = &n
		dev.MAC = obs.Address.MAC().String()
	}
	return dev
}

// readNames reads what a device calls itself.
func (s *Session) readNames(ctx context.Context, obs client.DeviceObservation, dev *types.Device) error {
	props := []bacnet.PropertyReference{
		{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyVendorName}, {Identifier: bacnet.PropertyModelName},
	}
	results, err := s.ReadMany(ctx, obs.Target(), []service.ReadAccessSpecification{{Object: obs.DeviceID(), Properties: props}})
	if err != nil {
		return err
	}
	for _, p := range results[0].Properties {
		if p.Err != nil {
			continue
		}
		text := stringOf(p.Value)
		switch p.Property.Identifier {
		case bacnet.PropertyObjectName:
			dev.Name = text
		case bacnet.PropertyVendorName:
			dev.VendorName = text
		case bacnet.PropertyModelName:
			dev.ModelName = text
		default:
		}
	}
	return nil
}

// stringOf returns the text of a character string value, and "" for any other value.
func stringOf(v bacnet.ApplicationValue) string {
	if v.Kind != bacnet.ValueCharacterString {
		return ""
	}
	return strings.TrimSpace(Text(v.Character))
}

// CheckWhoHas validates the configuration of `whohas`.
func CheckWhoHas(cfg *config.WhoHasConfig) error {
	if err := CheckSocket(&cfg.SocketConfig); err != nil {
		return err
	}
	if (cfg.Name == "") == (cfg.Object == "") {
		return fmt.Errorf("give exactly one of --name and --object")
	}
	if cfg.Object != "" {
		if _, err := ParseObject(cfg.Object); err != nil {
			return err
		}
	}
	if _, _, err := InstanceRange(cfg.Low, cfg.High); err != nil {
		return err
	}
	if _, err := ParseBroadcastNetwork(cfg.Network); err != nil {
		return err
	}
	if cfg.Target != "" {
		if _, err := config.ParseHostPort(cfg.Target); err != nil {
			return fmt.Errorf("--target: %w", err)
		}
	}
	return nil
}

// WhoHas sends a Who-Has and lists the devices that answered with an I-Have.
func WhoHas(ctx context.Context, cfg *config.WhoHasConfig, logw io.Writer) (*types.WhoHasResult, error) {
	low, high, _ := InstanceRange(cfg.Low, cfg.High)
	network, _ := ParseBroadcastNetwork(cfg.Network)
	opts := client.WhoHasOptions{LowLimit: low, HighLimit: high, Address: network, Window: Wait(cfg.Wait)}
	res := &types.WhoHasResult{Mode: ModeBroadcast, Objects: []types.FoundObject{}}
	if cfg.Object != "" {
		id, _ := ParseObject(cfg.Object)
		opts.Object = &id
		res.Query = "object " + ObjectName(id)
	} else {
		opts.Name = &bacnet.CharacterString{Value: cfg.Name}
		res.Query = fmt.Sprintf("name %q", cfg.Name)
	}
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()

	start := time.Now()
	var found []client.ObjectObservation
	if cfg.Target != "" {
		ep, err := ResolveEndpoint(ctx, cfg.Target)
		if err != nil {
			return nil, err
		}
		res.Mode, res.Target = ModeUnicast, ep.String()
		if err := s.SendWhoHas(ctx, ep, false, opts); err != nil {
			return nil, WrapError(err)
		}
		var first func() bool
		if network.Scope() != bacnet.AddressRemoteBroadcast && network.Scope() != bacnet.AddressGlobalBroadcast {
			first = func() bool { return len(s.Objects()) > 0 }
		}
		if err := collect(ctx, opts.Window, first); err != nil {
			return nil, WrapError(err)
		}
		found = s.Objects()
	} else {
		if found, err = s.DiscoverObjects(ctx, opts); err != nil {
			return nil, WrapError(err)
		}
	}
	res.DurationMS = ms(time.Since(start))
	sort.Slice(found, func(i, j int) bool {
		if found[i].DeviceInstance != found[j].DeviceInstance {
			return found[i].DeviceInstance < found[j].DeviceInstance
		}
		return ObjectName(found[i].Object) < ObjectName(found[j].Object)
	})
	for _, o := range found {
		row := types.FoundObject{ObjectRef: objRef(o.Object), Name: Text(o.Name), Device: o.DeviceInstance, Address: o.Origin.String()}
		if o.Address.Scope() == bacnet.AddressRemoteStation {
			n := o.Address.Network()
			row.Network = &n
			row.MAC = o.Address.MAC().String()
		}
		res.Objects = append(res.Objects, row)
	}
	res.Count = len(res.Objects)
	return res, nil
}
