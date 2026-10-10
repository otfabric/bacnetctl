// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"os"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/spf13/cobra"
)

// passwordEnv is the environment variable of --password. The variable is read when the
// command runs, never when its flags are defined: the default of a flag is printed by
// --help.
const passwordEnv = "BACNETCTL_PASSWORD"

// manageFlags adds --yes-i-am-sure and --password to cmd.
func manageFlags(cmd *cobra.Command, sure *bool) {
	cmd.Flags().BoolVar(sure, bac.SureFlag, false, "Confirm that this device may be restarted or silenced (required)")
	cmd.Flags().String("password", "", "Password of the device, if it asks for one; it is sent in clear text and never printed [env: "+passwordEnv+"]")
}

// password returns the password of an invocation: the flag, or the environment variable.
func password(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("password"); f != nil && f.Changed {
		return f.Value.String()
	}
	return os.Getenv(passwordEnv)
}

const manageWarning = `The command refuses to run without --yes-i-am-sure, and has sent nothing then. The
request is sent exactly once: when no answer arrives the outcome is unknown (exit code 5,
error code outcome_unknown), and the device may well have carried it out.

A device can ask for a password (--password, or BACNETCTL_PASSWORD). BACnet sends it in
clear text. bacnetctl never prints it.`

func newReinitializeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reinitialize STATE",
		Short: "Restart a device (ReinitializeDevice)",
		Long: `Send a ReinitializeDevice request. THIS RESTARTS THE DEVICE: while it restarts it
controls nothing, its outputs go to what the hardware does without it, and it may come
back with other values than it had.

STATE is what the device is asked to do:

  coldstart          restart as after a power cycle
  warmstart          restart and keep what the device keeps across a warm start
  activate-changes   apply configuration that was written and waits for activation
  start-backup, end-backup, start-restore, end-restore, abort-restore
                     the steps of the backup and restore procedure

` + manageWarning + `

` + addressing,
		Example: `
  # Warm start a device
  bacnetctl reinitialize warmstart --device 192.168.1.20 --yes-i-am-sure

  # Cold start with the password from the environment
  BACNETCTL_PASSWORD=... bacnetctl reinitialize coldstart --device 192.168.1.20 --yes-i-am-sure
`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: bac.ReinitializeStates(),
	}
	cfg := config.ManageConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	manageFlags(cmd, &cfg.Sure)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		pw := password(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckReinitialize(&cfg, args[0], pw) },
			func(ctx context.Context) (any, error) {
				return bac.Reinitialize(ctx, &cfg, args[0], pw, cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newCommControlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comm-control STATE",
		Short: "Silence a device, or let it speak again (DeviceCommunicationControl)",
		Long: `Send a DeviceCommunicationControl request. THIS SILENCES THE DEVICE: a disabled device
answers nothing but this request and ReinitializeDevice, so it drops out of the building
management system, its alarms are not delivered, and its values are not read.

STATE is one of:

  disable              the device neither answers nor initiates
  disable-initiation   the device answers, but initiates nothing (no I-Am, no COV, no events)
  enable               the device communicates again

--duration is the number of minutes after which the device enables itself again; without
it the device stays silent until it is enabled or restarted.

` + manageWarning + `

` + addressing,
		Example: `
  # Silence a device for 10 minutes
  bacnetctl comm-control disable --duration 10 --device 192.168.1.20 --yes-i-am-sure

  # Let it speak again
  bacnetctl comm-control enable --device 192.168.1.20 --yes-i-am-sure
`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: bac.CommControlStates(),
	}
	cfg := config.CommControlConfig{ManageConfig: config.ManageConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}}
	wire(cmd, &cfg)
	manageFlags(cmd, &cfg.Sure)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		pw := password(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckCommControl(&cfg, args[0], pw) },
			func(ctx context.Context) (any, error) {
				return bac.CommControl(ctx, &cfg, args[0], pw, cmd.ErrOrStderr())
			})
	}
	return cmd
}
