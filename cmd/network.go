// SPDX-License-Identifier: MIT

package cmd

import (
	"context"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/spf13/cobra"
)

func newRouterCmd() *cobra.Command {
	list := &cobra.Command{
		Use:   "list",
		Short: "Find the BACnet routers and the networks behind them",
		Long: `Send a Who-Is-Router-To-Network and list every network a router announces with
I-Am-Router-To-Network, with the address of that router.

A BACnet router connects BACnet networks: BACnet/IP to MS/TP, or one BACnet/IP network to
another. The devices behind it have a network number and a MAC address, and are reached
through the router: use the router's address as --device, with --network and --mac, in
the other commands. "bacnetctl discover" shows both for every device it finds.

The question is a local broadcast, and the answers come by broadcast to UDP port 47808,
which this command binds. --target asks one router instead; --network asks only for the
way to one network.`,
		Example: `
  # Which networks are there, and through which routers?
  bacnetctl router list

  # Ask one router
  bacnetctl router list --target 192.168.1.1

  # Who routes to network 2001?
  bacnetctl router list --network 2001 --format json
`,
		Args: cobra.NoArgs,
	}
	cfg := config.RouterListConfig{SocketConfig: broadcastSocket(), Wait: 3000, OutputFormat: textFormat}
	wire(list, &cfg)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckRouterList(&cfg) },
			func(ctx context.Context) (any, error) { return bac.RouterList(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return group("router", "BACnet routers",
		`Commands about BACnet routers, the devices that connect BACnet networks.`, list)
}

func newBBMDCmd() *cobra.Command {
	table := func(use, short, long, example, which string) *cobra.Command {
		cmd := &cobra.Command{Use: use, Short: short, Long: long, Example: example, Args: cobra.NoArgs}
		cfg := config.BBMDTableConfig{SocketConfig: unicastSocket(), OutputFormat: textFormat}
		wire(cmd, &cfg)
		cmd.RunE = func(cmd *cobra.Command, _ []string) error {
			cfg.Debug = cli.Debug(cmd)
			return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
				func() error { return bac.CheckBBMDTable(&cfg) },
				func(ctx context.Context) (any, error) { return bac.BBMDTable(ctx, &cfg, which, cmd.ErrOrStderr()) })
		}
		return cmd
	}
	bdt := table("bdt", "Read the broadcast distribution table of a BBMD",
		`Read the broadcast distribution table (BDT) of the BBMD at --device: the other BBMDs
it forwards broadcasts to, each with its broadcast distribution mask.

A BBMD (BACnet/IP broadcast management device) carries broadcasts from one IP subnet to
the BBMDs of the others, since IP routers do not. The table shows which subnets belong
together. A device that is no BBMD answers with a negative result.`,
		`
  # Which BBMDs does this one forward to?
  bacnetctl bbmd bdt --device 192.168.1.1
`, bac.TableBDT)
	fdt := table("fdt", "Read the foreign device table of a BBMD",
		`Read the foreign device table (FDT) of the BBMD at --device: the devices on other
subnets that registered with it to take part in its broadcasts, each with the lifetime of
its registration and the time that remains.

bacnetctl itself registers as a foreign device with --bbmd, and then shows up here.`,
		`
  # Who is registered as a foreign device?
  bacnetctl bbmd fdt --device 192.168.1.1 --format table
`, bac.TableFDT)
	return group("bbmd", "Tables of a BBMD (BACnet/IP broadcast management device)",
		`Read the tables of a BBMD, the device that carries BACnet broadcasts between IP subnets.
To send broadcasts through a BBMD, use --bbmd with discover, whohas, events or time sync.`, bdt, fdt)
}
