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

func newReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read OBJECT PROPERTY[,PROPERTY...] [OBJECT PROPERTY[,PROPERTY...]]...",
		Short: "Read properties of objects (ReadProperty, ReadPropertyMultiple)",
		Long: `Read one or more properties of one or more objects of a device.

OBJECT is type:instance: analog-input:1, the short form ai:1, or the numbers 0:1. "device"
alone is the Device object of the device that is addressed, whatever its instance.
PROPERTY is a name (present-value), a short form (pv, name, desc) or a number; several are
separated by commas. An element of an array is written name[index], or with --index for
all properties; index 0 is the number of elements. "bacnetctl types" and "bacnetctl
properties" list the names.

One property is read with ReadProperty, and a refusal of the device is the error of the
command (exit code 6). Several are read with ReadPropertyMultiple, in as many requests as
the device's APDU size needs; a property the device refuses is then reported in its row,
the others are still read, and the exit code is 7 (partial success). A device without
ReadPropertyMultiple is read with one ReadProperty per property.

The special properties all, required and optional ask the device for every property of
that kind.

` + addressing + `

With one value, --format text prints the value alone, ready for a script.`,
		Example: `
  # One value
  bacnetctl read --device 192.168.1.20 analog-input:1 present-value

  # Several properties of one object, as a table
  bacnetctl read --device 192.168.1.20 ai:1 present-value,units,status-flags,object-name --format table

  # Two objects in one request, as JSON
  bacnetctl read --device 192.168.1.20 ai:1 pv av:2 pv,description --format json

  # What is the device instance and name of the device at this address?
  bacnetctl read --device 192.168.1.20 device object-identifier,object-name

  # Priority 8 of the priority array of a commandable object
  bacnetctl read --device 192.168.1.20 analog-value:1 priority-array[8]

  # A device on MS/TP network 2001, station 10, behind the router 192.168.1.1
  bacnetctl read --device 192.168.1.1 --network 2001 --mac 0a analog-input:3 present-value
`,
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: pairArgs,
	}
	cfg := config.ReadConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckRead(&cfg, args) },
			func(ctx context.Context) (any, error) { return bac.Read(ctx, &cfg, args, cmd.ErrOrStderr()) },
			runner.WithSuccessExit(func(v any) int {
				if r, ok := v.(*types.ReadResult); ok && r.Failed > 0 {
					return errs.ExitPartial
				}
				return errs.ExitOK
			}))
	}
	return cmd
}

func newDescribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe OBJECT",
		Short: "Show every property of one object",
		Long: `Read all properties of one object and print each with its decoded value: enumerations
by name, bit strings as the names of the bits that are set, lists element by element.

The properties are asked for with the "all" property of ReadPropertyMultiple. A device
that does not implement that, or whose answer does not fit one APDU, is read property by
property from its property-list instead. A property the device refuses is shown with its
error, and the exit code is then 7 (partial success).

` + addressing,
		Example: `
  # Everything about one object
  bacnetctl describe --device 192.168.1.20 analog-input:1

  # The Device object of the device at this address
  bacnetctl describe --device 192.168.1.20 device

  # As JSON: every value with its type
  bacnetctl describe --device 192.168.1.20 binary-value:3 --format json
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.DescribeConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckDescribe(&cfg, args[0]) },
			func(ctx context.Context) (any, error) { return bac.Describe(ctx, &cfg, args[0], cmd.ErrOrStderr()) },
			runner.WithSuccessExit(func(v any) int {
				if r, ok := v.(*types.DescribeResult); ok && r.Failed > 0 {
					return errs.ExitPartial
				}
				return errs.ExitOK
			}))
	}
	return cmd
}

func newObjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "objects",
		Short: "List the objects of a device",
		Long: `Read the object list of a device and print every object with its name. With --values
the present value (with its unit), the status flags and the description are read too:
the point list of the device.

The object list is read as a whole, or element by element from a device that cannot send
it in one answer. The properties are read with ReadPropertyMultiple, as many objects per
request as the device's APDU size allows; an object without a present value, units or
description simply shows none. Only an object whose name cannot be read counts as a
failure (exit code 7).

--type limits the list to some object types, --limit to a number of objects.

` + addressing,
		Example: `
  # Which objects does the device have?
  bacnetctl objects --device 192.168.1.20

  # The point list: values, units, status and descriptions
  bacnetctl objects --device 192.168.1.20 --values

  # Only the analog and binary inputs, as a table
  bacnetctl objects --device 192.168.1.20 --values --type ai,bi --format table

  # As JSON, for a script
  bacnetctl objects --device 192.168.1.20 --values --format json
`,
		Args: cobra.NoArgs,
	}
	cfg := config.ObjectsConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg, enum("type", bac.ObjectTypeNames()...))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckObjects(&cfg) },
			func(ctx context.Context) (any, error) { return bac.Objects(ctx, &cfg, cmd.ErrOrStderr()) },
			runner.WithSuccessExit(func(v any) int {
				if r, ok := v.(*types.ObjectsResult); ok && r.Failed > 0 {
					return errs.ExitPartial
				}
				return errs.ExitOK
			}))
	}
	return cmd
}

func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Show what a device says about itself",
		Long: `Read the Device object of a device and print its identity and capabilities: device
instance, name, vendor, model, firmware and application software, system status, protocol
version and revision, the largest APDU it accepts, segmentation, its APDU timeout and
retries, its local date and time, the number of objects, and the services and object
types it supports, by name.

The device instance need not be known: it is learned from the address. A property the
device does not have is left out.

` + addressing,
		Example: `
  # Who is at this address?
  bacnetctl device --device 192.168.1.20

  # As JSON
  bacnetctl device --device 192.168.1.20 --format json

  # A device behind a router
  bacnetctl device --device 192.168.1.1 --network 2001 --mac 0a
`,
		Args: cobra.NoArgs,
	}
	cfg := config.DeviceConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckDevice(&cfg) },
			func(ctx context.Context) (any, error) { return bac.DeviceInfo(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}
