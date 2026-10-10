// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/format"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/spf13/cobra"
)

// textFormat is the default of --format.
const textFormat = string(format.FormatText)

// unicastSocket are the socket defaults of a command that talks to one device: a free
// local port, since every answer comes back to the address that asked.
func unicastSocket() config.SocketConfig {
	return config.SocketConfig{LocalPort: 0, BBMDTTL: 60, Timeout: 3000, Retries: 3}
}

// broadcastSocket are the socket defaults of a command that needs broadcasts: devices
// answer a Who-Is, and send their notifications, to UDP port 47808.
func broadcastSocket() config.SocketConfig {
	s := unicastSocket()
	s.LocalPort = config.DefaultPort
	return s
}

func connDefaults() config.ConnConfig {
	return config.ConnConfig{SocketConfig: unicastSocket()}
}

// enum registers shell completion for a flag with a fixed set of values.
func enum(flag string, values ...string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletion(cmd, flag, values) }
}

// described registers shell completion for a flag whose values carry a description.
func described(flag string, values map[string]string) func(*cobra.Command) {
	return func(cmd *cobra.Command) { _ = cli.RegisterEnumFlagCompletionWithDescriptions(cmd, flag, values) }
}

// propertyFlag completes --property with the property names.
func propertyFlag(cmd *cobra.Command) {
	_ = cli.RegisterEnumFlagCompletion(cmd, "property", bac.PropertyNames())
}

// wire loads cfg from the environment, binds its flags to cmd and registers completions.
func wire(cmd *cobra.Command, cfg any, hooks ...func(*cobra.Command)) {
	runner.RegisterCommandCfg(cmd, cfg, hooks...)
}

func filter(values []string, prefix string) []string {
	prefix = strings.ToLower(prefix)
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.HasPrefix(v, prefix) {
			out = append(out, v)
		}
	}
	return out
}

// completeObject completes an OBJECT argument: the object types, each ready for its
// instance number.
func completeObject(toComplete string) ([]string, cobra.ShellCompDirective) {
	if strings.Contains(toComplete, ":") {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := filter(bac.ObjectTypeNames(), toComplete)
	for i := range names {
		names[i] += ":"
	}
	return names, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// completeProperty completes a PROPERTY argument, also after a comma.
func completeProperty(toComplete string) ([]string, cobra.ShellCompDirective) {
	head, last := "", toComplete
	if i := strings.LastIndex(toComplete, ","); i >= 0 {
		head, last = toComplete[:i+1], toComplete[i+1:]
	}
	names := filter(append(bac.PropertyNames(), "all", "required", "optional"), last)
	for i := range names {
		names[i] = head + names[i]
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// objectArgs completes arguments that are all objects.
func objectArgs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeObject(toComplete)
}

// pairArgs completes OBJECT PROPERTY pairs.
func pairArgs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args)%2 == 0 {
		return completeObject(toComplete)
	}
	return completeProperty(toComplete)
}

// group returns a command that only holds subcommands and prints its help when run.
func group(use, short, long string, subs ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(subs...)
	return cmd
}

// addressing is the paragraph about --device every device command shares.
const addressing = `The device is --device host[:port] (UDP port 47808 unless given). For a device behind a
BACnet router give the router as --device, and the device as --network and --mac.`
