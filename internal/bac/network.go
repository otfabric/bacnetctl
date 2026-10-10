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
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Names of the time services in output.
const (
	ServiceTimeSync    = "time-synchronization"
	ServiceUTCTimeSync = "utc-time-synchronization"
	StatusSent         = "sent"
)

// ParseSyncTime parses --time: now, or an RFC 3339 time.
func ParseSyncTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "now") {
		return time.Now(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--time %q: use now, or RFC 3339 such as 2026-01-31T12:00:00+01:00", s)
	}
	return t, nil
}

// CheckTimeSync validates the configuration of `time sync`.
func CheckTimeSync(cfg *config.TimeSyncConfig) error {
	if _, err := ParseSyncTime(cfg.Time); err != nil {
		return err
	}
	if cfg.Broadcast {
		if cfg.Device != "" {
			return fmt.Errorf("--broadcast and --device are two destinations: give one")
		}
		return CheckSocket(&cfg.SocketConfig)
	}
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	if cfg.Network != 0 {
		return fmt.Errorf("time synchronization of one device behind a router is not supported: send it to the device directly, or with --broadcast on its own network")
	}
	return nil
}

// TimeSync sends a TimeSynchronization or UTCTimeSynchronization request. The service is
// unconfirmed: a device does not answer it.
func TimeSync(ctx context.Context, cfg *config.TimeSyncConfig, logw io.Writer) (*types.TimeSyncResult, error) {
	t, _ := ParseSyncTime(cfg.Time)
	res := &types.TimeSyncResult{Target: ModeBroadcast, Service: ServiceTimeSync, Status: StatusSent}
	if cfg.UTC {
		t, res.Service = t.UTC(), ServiceUTCTimeSync
	}
	d, tm := DateTimeOf(t)
	res.Time = DateText(d) + " " + TimeText(tm)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	var target client.Target
	if !cfg.Broadcast {
		if target, err = s.Target(ctx, &cfg.ConnConfig); err != nil {
			return nil, err
		}
		res.Target = TargetText(target)
	}
	send := s.TimeSynchronization
	if cfg.UTC {
		send = s.UTCTimeSynchronization
	}
	if err := send(ctx, target.Endpoint, cfg.Broadcast, service.TimeSynchronization{Date: d, Time: tm}); err != nil {
		return nil, WrapError(err)
	}
	return res, nil
}

// CheckRouterList validates the configuration of `router list`.
func CheckRouterList(cfg *config.RouterListConfig) error {
	if err := CheckSocket(&cfg.SocketConfig); err != nil {
		return err
	}
	if cfg.Network == 0xFFFF {
		return fmt.Errorf("--network must be 1-65534, or 0 for all networks")
	}
	if cfg.Target != "" {
		if _, err := config.ParseHostPort(cfg.Target); err != nil {
			return fmt.Errorf("--target: %w", err)
		}
	}
	return nil
}

var routeStates = []string{"available", "busy", "rejected", "expired"}

// routes lists every route the client has learned.
func (s *Session) routes() []types.Router {
	out := []types.Router{}
	for _, r := range s.AllRoutes() {
		out = append(out, types.Router{Network: r.Network, Router: r.NextHop.String(), State: nameAt(routeStates, uint32(r.State))})
	}
	return out
}

// RouterList sends a Who-Is-Router-To-Network and lists the networks that routers
// announced with I-Am-Router-To-Network.
func RouterList(ctx context.Context, cfg *config.RouterListConfig, logw io.Writer) (*types.RoutersResult, error) {
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	var network *uint16
	if cfg.Network != 0 {
		network = &cfg.Network
	}
	res := &types.RoutersResult{Mode: ModeBroadcast}
	start := time.Now()
	var first func() bool
	if cfg.Target != "" {
		ep, err := ResolveEndpoint(ctx, cfg.Target)
		if err != nil {
			return nil, err
		}
		res.Mode, res.Target = ModeUnicast, ep.String()
		if err := s.WhoIsRouterToNetworkAt(ctx, ep, false, network); err != nil {
			return nil, WrapError(err)
		}
		// One router answers with one message that lists all its networks.
		first = func() bool { return len(s.routes()) > 0 }
	} else if err := s.WhoIsRouterToNetwork(ctx, network); err != nil {
		return nil, WrapError(err)
	}
	if err := collect(ctx, Wait(cfg.Wait), first); err != nil {
		return nil, WrapError(err)
	}
	res.DurationMS = ms(time.Since(start))
	res.Routers = s.routes()
	res.Count = len(res.Routers)
	return res, nil
}

// Tables of a BBMD.
const (
	TableBDT = "bdt"
	TableFDT = "fdt"
)

// CheckBBMDTable validates the configuration of `bbmd bdt` and `bbmd fdt`.
func CheckBBMDTable(cfg *config.BBMDTableConfig) error {
	if err := CheckSocket(&cfg.SocketConfig); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Device) == "" {
		return fmt.Errorf("no BBMD given: set --device host[:port] (or BACNETCTL_DEVICE)")
	}
	if _, err := config.ParseHostPort(cfg.Device); err != nil {
		return fmt.Errorf("--device: %w", err)
	}
	return nil
}

// BBMDTable reads the broadcast distribution table or the foreign device table of a BBMD.
func BBMDTable(ctx context.Context, cfg *config.BBMDTableConfig, table string, logw io.Writer) (*types.BBMDTableResult, error) {
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	ep, err := ResolveEndpoint(ctx, cfg.Device)
	if err != nil {
		return nil, err
	}
	res := &types.BBMDTableResult{BBMD: ep.String(), Table: table, Entries: []types.TableEntry{}}
	switch table {
	case TableBDT:
		entries, err := s.ReadBroadcastDistributionTable(ctx, ep)
		if err != nil {
			return nil, bbmdError(err)
		}
		for _, e := range entries {
			res.Entries = append(res.Entries, types.TableEntry{Address: e.Endpoint.String(), Mask: e.Mask.Addr().String()})
		}
	default:
		entries, err := s.ReadForeignDeviceTable(ctx, ep)
		if err != nil {
			return nil, bbmdError(err)
		}
		for _, e := range entries {
			ttl, remaining := e.TTL, e.Remaining
			res.Entries = append(res.Entries, types.TableEntry{Address: e.Address.String(), TTL: &ttl, Remaining: &remaining})
		}
	}
	res.Count = len(res.Entries)
	return res, nil
}

// bbmdError says what a refusal of a table read means: a device that is no BBMD answers
// it with a negative BVLC result.
func bbmdError(err error) error {
	wrapped := WrapError(err)
	if e, ok := errs.As(wrapped); ok && e.Code == errs.CodeBVLCResult {
		e.Message += " (the device is not a BBMD, or does not let its tables be read)"
	}
	return wrapped
}
