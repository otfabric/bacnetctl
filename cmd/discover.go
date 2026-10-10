// SPDX-License-Identifier: MIT

package cmd

import (
	"context"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/otfabric/bacnetctl/internal/types"
	"github.com/spf13/cobra"
)

func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Find devices with Who-Is",
		Long: `Send a Who-Is and list every device that answers with an I-Am: its device instance,
address, vendor identifier, the largest APDU it accepts and whether it segments.

By default the Who-Is is a global broadcast: it reaches the local IP subnet and, through
BACnet routers, the networks behind them. IP routers do not pass broadcasts on. To reach
devices on another IP subnet, either register with a BBMD there (--bbmd) or send the
Who-Is to one address (--target). --target also works where broadcasts are filtered, and
through a Docker port mapping.

Devices answer by broadcast to UDP port 47808, so this command binds that port. Only one
program on a host can hold it: if another BACnet program is running, discover reports
that the port is in use. With --target and a device that answers to the address that asked
(most do), --local-port 0 works too.

--low and --high limit the question to a range of device instances. --network asks one
BACnet network behind a router. --wait is how long answers are collected; with --target
(and no --network) the command returns as soon as the device has answered. --names reads
object-name, vendor-name and model-name of every device found, one request per device.`,
		Example: `
  # Who is on this network?
  bacnetctl discover

  # Only device instances 1000-1999, wait 5 seconds, with the names of the devices
  bacnetctl discover --low 1000 --high 1999 --wait 5000 --names

  # Ask one address (another subnet, a container, a network that filters broadcasts)
  bacnetctl discover --target 10.0.20.15

  # Devices on another IP subnet, through the BBMD there
  bacnetctl discover --bbmd 10.0.20.1

  # The devices of BACnet network 2001 behind a router, as JSON
  bacnetctl discover --network 2001 --format json

  # A multi-homed host: broadcast on the subnet of eth1
  bacnetctl discover --interface eth1
`,
		Args: cobra.NoArgs,
	}
	cfg := config.DiscoverConfig{SocketConfig: broadcastSocket(), Wait: 3000, OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckDiscover(&cfg) },
			func(ctx context.Context) (any, error) { return bac.Discover(ctx, &cfg, cmd.ErrOrStderr()) },
			runner.WithSuccessExit(func(v any) int {
				if r, ok := v.(*types.DiscoverResult); ok {
					for _, d := range r.Devices {
						if d.Error != nil {
							return errs.ExitPartial
						}
					}
				}
				return errs.ExitOK
			}))
	}
	return cmd
}

func newWhoHasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whohas",
		Short: "Find the devices that have an object, by name or identifier (Who-Has)",
		Long: `Send a Who-Has and list every device that answers with an I-Have.

Ask for an object by its name (--name, an exact match) or by its identifier (--object
type:instance). The answer names the device, the object identifier and the object name:
this is how to find which device holds "AHU-1 Supply Temp" without reading every device.

Like discover, this is a broadcast unless --target is given, the answers come by broadcast
to UDP port 47808, and --bbmd reaches another IP subnet. Not every device implements
Who-Has: a device that stays silent may still have the object.`,
		Example: `
  # Which device has an object with this name?
  bacnetctl whohas --name "AHU-1 Supply Temp"

  # Which devices have an analog-input 1?
  bacnetctl whohas --object analog-input:1

  # Ask one device
  bacnetctl whohas --name "Room Temperature" --target 192.168.1.20
`,
		Args: cobra.NoArgs,
	}
	cfg := config.WhoHasConfig{SocketConfig: broadcastSocket(), Wait: 3000, OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckWhoHas(&cfg) },
			func(ctx context.Context) (any, error) { return bac.WhoHas(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
