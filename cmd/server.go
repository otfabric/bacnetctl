// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/format"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/otfabric/bacnetctl/internal/types"
	"github.com/spf13/cobra"
)

// serverPasswordEnv and serverPasswordFileEnv are the environment variables of the
// --password and --password-file of `server run`. They are not BACNETCTL_PASSWORD and
// BACNETCTL_PASSWORD_FILE, which give the password the client commands send: setting
// those for a client must not lock the simulated device. They are read when the command
// runs.
const (
	serverPasswordEnv     = "BACNETCTL_SERVER_PASSWORD"
	serverPasswordFileEnv = "BACNETCTL_SERVER_PASSWORD_FILE"
)

func newServerCmd() *cobra.Command {
	return group("server", "Serve a simulated BACnet/IP device",
		`Serve a simulated BACnet/IP device on this computer, with go-bacnet's server: for
developing and testing BACnet clients (bacnetctl itself, a supervisor, a gateway) without
hardware. What the server implements, and how well each part is verified, is in the
server support table of go-bacnet (docs/SERVER_SUPPORT.md).`,
		newServerRunCmd(), newServerExampleCmd())
}

func newServerRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Serve a device described by a device file, or the built-in example device",
		Long: `Serve one BACnet device on a BACnet/IP address until interrupted (Ctrl-C or SIGTERM),
or until --duration seconds have passed. The device answers Who-Is and Who-Has, reads and
writes of its properties, COV subscriptions, alarms, trend logs, files, object creation and
deletion, time synchronization, and DeviceCommunicationControl and ReinitializeDevice
(with a password, only when they carry it). It sends an I-Am when it starts.

The device and its objects come from a device file (--config, JSON): see "bacnetctl
server example" for one, and the README for the fields. The device fixtures of
bacnet-interop are device files. Without --config the built-in example device is served.
--instance, --name and --vendor-id override the identity the file gives.

  --listen, --port   the address to bind: every address and UDP port 47808 by default.
                     --port 0 takes a free port; the listening record says which
  --read-only        refuses every write and delete (write-access-denied,
                     object-deletion-not-permitted)
  --bbmd             also acts as a BBMD on the same address: forwards broadcasts to the
                     peers of --bdt and accepts foreign devices (unless
                     --no-foreign-devices). The BBMD's own address is --listen,
                     --bbmd-address, or the IPv4 address of --interface

Values only change when clients write them: the device does not simulate a process.

stdout gets one record when the device listens and one for every request that changes it
(a write, an object created or deleted, a ReinitializeDevice, a time
synchronization): one line with --format
text, one JSON object per line with --format json, one row with --format table. The start
line and a summary go to stderr. Ctrl-C ends the command normally: exit code 0. A device
file that cannot be served is invalid input (exit code 3), and nothing is bound; a port
that is in use is exit code 4.

A device password is never printed. BACnet sends it in clear text.`,
		Example: `
  # Serve the built-in example device on every address, UDP port 47808
  bacnetctl server run

  # Serve a device file on loopback, on a free port, with the records as JSON Lines
  bacnetctl server run --config device.json --listen 127.0.0.1 --port 0 --format json

  # Serve a bacnet-interop fixture as device 4321
  bacnetctl server run --config fixtures/device/device-server-v1.json --instance 4321

  # Also be the BBMD of this subnet, with a peer BBMD on another subnet
  bacnetctl server run --listen 192.168.1.10 --bbmd --bdt 10.0.20.1

  # Require a password for ReinitializeDevice and DeviceCommunicationControl
  bacnetctl server run --config device.json --password-file device-password.txt
`,
		Args: cobra.NoArgs,
	}
	cfg := config.ServerRunConfig{MaxAPDU: bac.DefaultServerMaxAPDU, MaxSegments: bac.DefaultServerMaxSegments, OutputFormat: textFormat}
	wire(cmd, &cfg)
	passwordFlags(cmd, "Password that DeviceCommunicationControl and ReinitializeDevice must carry (at most 20 octets)", serverPasswordEnv, serverPasswordFileEnv)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		var pw string
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() (err error) {
				if pw, err = password(cmd, serverPasswordEnv, serverPasswordFileEnv); err != nil {
					return err
				}
				return bac.CheckServer(&cfg, pw)
			},
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				summary, err := bac.Serve(ctx, &cfg, pw, cmd.ErrOrStderr(), func(r *types.ServerRecord) error { return out.Write(r) })
				if summary.Address != "" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), summary)
				}
				if err != nil {
					return 0, err
				}
				return errs.ExitOK, nil
			})
	}
	return cmd
}

func newServerExampleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "example",
		Short: "Print the built-in example device as a device file",
		Long: `Print the device file of the device "bacnetctl server run" serves without --config: a
starting point for a device file of your own. It is JSON, whatever --format says
elsewhere: the output is a file, not a result.

The object types a device file can hold are analog, binary and multi-state inputs,
outputs and values, characterstring-value, integer-value, positive-integer-value,
notification-class, trend-log and file.`,
		Example: `
  # Write the example to a file, change it, serve it
  bacnetctl server example > device.json
  bacnetctl server run --config device.json
`,
		Args: cobra.NoArgs,
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		data, err := json.MarshalIndent(bac.DefaultDeviceFile(), "", "  ")
		if err != nil {
			return errs.Internal(err)
		}
		if _, err := cmd.OutOrStdout().Write(append(data, '\n')); err != nil {
			return errs.Output(errs.CodeOutputWriteFailed, err)
		}
		return nil
	}
	return cmd
}
