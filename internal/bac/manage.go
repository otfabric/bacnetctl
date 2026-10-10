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
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Names of the device management requests in output.
const (
	RequestReinitialize = "reinitialize-device"
	RequestCommControl  = "device-communication-control"
)

// SureFlag is the flag that confirms a command that changes how a device runs.
const SureFlag = "yes-i-am-sure"

// maxPassword is the longest password BACnet carries.
const maxPassword = 20

var reinitStates = map[string]service.ReinitializedStateOfDevice{
	"coldstart": service.ReinitializedColdstart, "warmstart": service.ReinitializedWarmstart,
	"start-backup": service.ReinitializedStartBackup, "end-backup": service.ReinitializedEndBackup,
	"start-restore": service.ReinitializedStartRestore, "end-restore": service.ReinitializedEndRestore,
	"abort-restore": service.ReinitializedAbortRestore, "activate-changes": service.ReinitializedActivateChanges,
}

var commStates = map[string]service.EnableDisable{
	"enable": service.EnableDisableEnable, "disable": service.EnableDisableDisable,
	"disable-initiation": service.EnableDisableDisableInitiation,
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ReinitializeStates lists the states `reinitialize` takes.
func ReinitializeStates() []string { return keys(reinitStates) }

// CommControlStates lists the states `comm-control` takes.
func CommControlStates() []string { return keys(commStates) }

// Unconfirmed is the usage error of a device management command without its
// confirmation. Nothing is sent.
func Unconfirmed(what string) error {
	return errs.New(errs.KindUsage, errs.CodeConfirmationRequired, fmt.Sprintf(
		"%s changes how the device runs: confirm with --%s (nothing was sent)", what, SureFlag), nil)
}

func checkManage(cfg *config.ManageConfig, password string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	if len(password) > maxPassword {
		return fmt.Errorf("the password is longer than the %d characters BACnet allows", maxPassword)
	}
	return nil
}

func passwordOf(password string) *bacnet.CharacterString {
	if password == "" {
		return nil
	}
	return &bacnet.CharacterString{Value: password}
}

// manage opens a session that may send device management requests.
func manage(ctx context.Context, cfg *config.ManageConfig, logw io.Writer) (*Session, client.Target, error) {
	s, err := Open(ctx, &cfg.SocketConfig, logw, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	if err != nil {
		return nil, client.Target{}, err
	}
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		_ = s.Close()
		return nil, client.Target{}, err
	}
	return s, target, nil
}

// CheckReinitialize validates the configuration of `reinitialize`.
func CheckReinitialize(cfg *config.ManageConfig, state, password string) error {
	if !cfg.Sure {
		return Unconfirmed("reinitialize")
	}
	if _, ok := reinitStates[normalizeName(state)]; !ok {
		return fmt.Errorf("unknown state %q: use one of %s", state, strings.Join(ReinitializeStates(), ", "))
	}
	return checkManage(cfg, password)
}

// Reinitialize sends a ReinitializeDevice request: it restarts a device, or steps through
// a backup or restore. The request is sent once.
func Reinitialize(ctx context.Context, cfg *config.ManageConfig, state, password string, logw io.Writer) (*types.ManageResult, error) {
	name := normalizeName(state)
	s, target, err := manage(ctx, cfg, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	start := time.Now()
	err = s.ReinitializeDevice(ctx, target, service.ReinitializeDeviceRequest{State: reinitStates[name], Password: passwordOf(password)})
	if err != nil {
		return nil, WrapError(err)
	}
	return &types.ManageResult{Target: TargetText(target), Request: RequestReinitialize, State: name, Status: StatusAcknowledged, RTTMS: ms(time.Since(start))}, nil
}

// CheckCommControl validates the configuration of `comm-control`.
func CheckCommControl(cfg *config.CommControlConfig, state, password string) error {
	if !cfg.Sure {
		return Unconfirmed("comm-control")
	}
	st, ok := commStates[normalizeName(state)]
	if !ok {
		return fmt.Errorf("unknown state %q: use one of %s", state, strings.Join(CommControlStates(), ", "))
	}
	if st == service.EnableDisableEnable && cfg.Duration != 0 {
		return fmt.Errorf("--duration has no meaning with enable")
	}
	return checkManage(&cfg.ManageConfig, password)
}

// CommControl sends a DeviceCommunicationControl request: it silences a device, or lets
// it speak again. The request is sent once.
func CommControl(ctx context.Context, cfg *config.CommControlConfig, state, password string, logw io.Writer) (*types.ManageResult, error) {
	name := normalizeName(state)
	s, target, err := manage(ctx, &cfg.ManageConfig, logw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	req := service.DeviceCommunicationControlRequest{EnableDisable: commStates[name], Password: passwordOf(password)}
	res := &types.ManageResult{Target: TargetText(target), Request: RequestCommControl, State: name, Status: StatusAcknowledged}
	if cfg.Duration != 0 {
		d := cfg.Duration
		req.TimeDuration, res.DurationMinutes = &d, &d
	}
	start := time.Now()
	if err := s.DeviceCommunicationControl(ctx, target, req); err != nil {
		return nil, WrapError(err)
	}
	res.RTTMS = ms(time.Since(start))
	return res, nil
}

// NameTable builds the output of `types` and `properties`.
func NameTable(what string, rows []NamedNumber, filter string) *types.NameList {
	filter = strings.ToLower(strings.TrimSpace(filter))
	out := &types.NameList{What: what, Names: []types.Named{}}
	for _, r := range rows {
		if filter != "" && !strings.Contains(r.Name, filter) && !strings.Contains(r.Alias, filter) {
			continue
		}
		out.Names = append(out.Names, types.Named{ID: r.Number, Name: r.Name, Alias: r.Alias})
	}
	out.Count = len(out.Names)
	return out
}
