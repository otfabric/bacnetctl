// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/otfabric/bacnetctl/internal/types"
	"github.com/spf13/cobra"
)

func newWriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write OBJECT PROPERTY VALUE [OBJECT PROPERTY VALUE]...",
		Short: "Write one or more properties (WriteProperty, WritePropertyMultiple)",
		Long: `Write a value to a property of an object. THIS CHANGES THE DEVICE: a present value
that is written may start a fan, open a valve or override a control loop.

VALUE says its type with a prefix:

  real:21.5         REAL                    double:21.5       Double
  unsigned:3        Unsigned Integer        signed:-1         Signed Integer
  bool:true         BOOLEAN                 enum:1            Enumerated
  string:Room 4     CharacterString         octets:0a1b2c     OctetString (hex)
  bits:1011         BitString               object:ai:1       Object identifier
  date:2026-10-10   Date                    time:12:00:00     Time
  null              NULL (relinquish)

Without a prefix (and with --type auto, the default) the type is taken from the device:
the property is read first, and VALUE is written with the type it holds now. That costs
one extra request, and it cannot work when the property holds NULL or a list. An
enumerated property takes the names bacnetctl knows for it, such as active and inactive
for a binary object. --type names the type for a VALUE without prefix and skips the read.
A negative number without prefix must follow "--": write ... -- -5.

--priority 1-16 is the command priority for a commandable property (the present value of
an output, usually). 8 is the priority of a manual operator; without it the device uses
the lowest, 16. A value written at a priority stays in force until that priority is
relinquished: see "bacnetctl release".

Everything is checked before anything is sent, and the request is sent exactly once. When
no acknowledgement arrives, the outcome is unknown: the device may have carried out the
write. bacnetctl says so (exit code 5, error code outcome_unknown) and does not repeat it.

Several OBJECT PROPERTY VALUE groups are written with one WritePropertyMultiple request;
--priority and --type then hold for all of them. The device carries the writes out in the
order given and stops at the first one it refuses: the result lists every write as
written, failed (with the error of the device) or not-written, and the exit code is 7
(partial success). What was written before the refusal stays written. A device without
WritePropertyMultiple rejects the request (exit code 6): write the properties one by one.

` + addressing,
		Example: `
  # Set an analog value
  bacnetctl write --device 192.168.1.20 analog-value:1 present-value real:21.5

  # The same, letting bacnetctl look up the type
  bacnetctl write --device 192.168.1.20 analog-value:1 present-value 21.5

  # Command a binary output on at operator priority
  bacnetctl write --device 192.168.1.20 binary-output:2 present-value active --priority 8

  # Take a point out of service, then give it a value
  bacnetctl write --device 192.168.1.20 ai:1 out-of-service bool:true
  bacnetctl write --device 192.168.1.20 ai:1 present-value real:19.0

  # Rename an object
  bacnetctl write --device 192.168.1.20 av:1 object-name "string:Setpoint Room 4"

  # A negative number without prefix
  bacnetctl write --device 192.168.1.20 av:7 present-value -- -5

  # Two setpoints and a name in one request
  bacnetctl write --device 192.168.1.20 av:1 pv real:21.5 av:2 pv real:55 av:2 object-name "string:Humidity SP"
`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) < 3 || len(args)%3 != 0 {
				return fmt.Errorf("write takes OBJECT PROPERTY VALUE, once or several times (got %d arguments)", len(args))
			}
			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			switch len(args) % 3 {
			case 0:
				return completeObject(toComplete)
			case 1:
				return completeProperty(toComplete)
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	cfg := config.WriteConfig{ConnConfig: connDefaults(), Type: bac.TypeAuto, OutputFormat: textFormat}
	wire(cmd, &cfg, described("type", bac.ValueTypes()))
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		if len(args) > 3 {
			return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
				func() error { return bac.CheckWrites(&cfg, args) },
				func(ctx context.Context) (any, error) { return bac.WriteMultiple(ctx, &cfg, args, cmd.ErrOrStderr()) },
				runner.WithSuccessExit(func(v any) int {
					if r, ok := v.(*types.WriteMultipleResult); ok && r.Failed > 0 {
						return errs.ExitPartial
					}
					return errs.ExitOK
				}))
		}
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckWrite(&cfg, args[0], args[1], args[2]) },
			func(ctx context.Context) (any, error) {
				return bac.Write(ctx, &cfg, args[0], args[1], args[2], cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create TYPE[:INSTANCE] [PROPERTY=VALUE]...",
		Short: "Create an object in a device (CreateObject)",
		Long: `Ask a device to create an object. THIS CHANGES THE DEVICE: the object becomes part of
its database, and stays there until it is deleted.

TYPE is an object type (analog-value, av, or its number). With TYPE:INSTANCE the object
gets that instance number; with TYPE alone the device chooses one, which the result
shows. Whether a type can be created at all is up to the device: most create only a few
(values, schedules, trend logs, notification classes, files) and many none.

PROPERTY=VALUE gives the object initial values. The object does not exist yet, so the
type of each value cannot be looked up: say it, as in description=string:Spare or
present-value=real:21.5 (see "bacnetctl write" for the prefixes). --name is short for
object-name=string:NAME. A device that refuses one initial value creates nothing.

The request is sent exactly once; when no answer arrives the outcome is unknown (exit
code 5, error code outcome_unknown): list the objects before you repeat it.

` + addressing,
		Example: `
  # An analog value, instance chosen by the device
  bacnetctl create --device 192.168.1.20 analog-value --name "Spare Setpoint"

  # A given instance, with initial values
  bacnetctl create --device 192.168.1.20 av:40 --name "Room 4 Offset" description=string:Operator present-value=real:0
`,
		Args: cobra.MinimumNArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return filter(bac.ObjectTypeNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
	}
	cfg := config.CreateConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckCreate(&cfg, args[0], args[1:]) },
			func(ctx context.Context) (any, error) {
				return bac.Create(ctx, &cfg, args[0], args[1:], cmd.ErrOrStderr())
			})
	}
	return cmd
}

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete OBJECT",
		Short: "Delete an object from a device (DeleteObject)",
		Long: `Ask a device to delete an object. THIS CHANGES THE DEVICE, AND CANNOT BE UNDONE: the
object is gone with its configuration and history, and whatever refers to it (a schedule,
a trend log, a program, the building management system) is left pointing at nothing.

A device deletes only what it allows to be deleted, usually the objects that were created
with CreateObject: it refuses the rest with object-deletion-not-permitted.

The command refuses to run without --yes-i-am-sure, and has sent nothing then. The request
is sent exactly once; when no answer arrives the outcome is unknown (exit code 5, error
code outcome_unknown): list the objects to see whether it is still there.

` + addressing,
		Example: `
  # Remove an object that was created by mistake
  bacnetctl delete --device 192.168.1.20 analog-value:40 --yes-i-am-sure
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.DeleteConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.Flags().BoolVar(&cfg.Sure, bac.SureFlag, false, "Confirm that this object may be deleted (required)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckDelete(&cfg, args[0]) },
			func(ctx context.Context) (any, error) { return bac.Delete(ctx, &cfg, args[0], cmd.ErrOrStderr()) })
	}
	return cmd
}

func newReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release OBJECT",
		Short: "Relinquish a command priority (write NULL at that priority)",
		Long: `Relinquish one priority of a commandable property: write NULL to it at --priority.
THIS CHANGES THE DEVICE: the object takes the value of the next priority that holds one,
or its relinquish default when none does.

A commandable property (the present value of an analog, binary or multi-state output, and
of many values) keeps one value per priority, 1 (highest) to 16 (lowest), and takes the
one with the highest priority. A value written with "bacnetctl write --priority 8" stays
in force until priority 8 is relinquished with this command: writing another value does
not hand control back to the automation.

The request is sent exactly once; see "bacnetctl write" about an unknown outcome.

` + addressing,
		Example: `
  # Give a point back to the automation after an operator override
  bacnetctl release --device 192.168.1.20 analog-output:1 --priority 8

  # Look at what is commanded at which priority
  bacnetctl read --device 192.168.1.20 analog-output:1 priority-array
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.ReleaseConfig{ConnConfig: connDefaults(), Property: "present-value", OutputFormat: textFormat}
	wire(cmd, &cfg, propertyFlag)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckRelease(&cfg, args[0]) },
			func(ctx context.Context) (any, error) { return bac.Release(ctx, &cfg, args[0], cmd.ErrOrStderr()) })
	}
	return cmd
}

func newTimeCmd() *cobra.Command {
	sync := &cobra.Command{
		Use:   "sync",
		Short: "Set the clock of a device, or of every device on the network",
		Long: `Send a TimeSynchronization request with the current time of this computer, or with
--time. THIS CHANGES THE DEVICE: schedules, trend logs and the time stamps of alarms
follow its clock.

TimeSynchronization carries a date and time without a time zone: the local time the
device should show. bacnetctl sends the wall clock of this computer, or of the offset
given in --time. --utc sends UTCTimeSynchronization instead: the time in UTC, which the
device converts with its own UTC offset.

The request goes to --device, or with --broadcast to every device on the local network
(through the BBMD with --bbmd). The service is unconfirmed: a device does not answer it,
and one that does not implement it ignores it. Read local-date and local-time of the
Device object to see what the device made of it ("bacnetctl device").`,
		Example: `
  # Set the clock of one device to the time of this computer
  bacnetctl time sync --device 192.168.1.20

  # Every device on the local network, in UTC
  bacnetctl time sync --broadcast --utc

  # A given time
  bacnetctl time sync --device 192.168.1.20 --time 2026-01-31T12:00:00+01:00
`,
		Args: cobra.NoArgs,
	}
	cfg := config.TimeSyncConfig{ConnConfig: connDefaults(), Time: "now", OutputFormat: textFormat}
	wire(sync, &cfg)
	sync.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckTimeSync(&cfg) },
			func(ctx context.Context) (any, error) { return bac.TimeSync(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return group("time", "Time synchronization",
		`Commands about the clock of devices. "bacnetctl time sync" sets it; the local date and
time a device has are part of "bacnetctl device".`, sync)
}
