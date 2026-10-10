// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
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

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor OBJECT [OBJECT...]",
		Short: "Print the values of objects as they change (SubscribeCOV)",
		Long: `Subscribe to the changes of value of one or more objects and print every notification
the device sends: the present value and status flags of the object, as soon as the value
changes by more than the object's COV increment. A device sends the current value right
after it accepts a subscription, so the stream starts with what is there now.

Monitoring runs until interrupted (Ctrl-C), or until --duration seconds have passed or
--count notifications were printed. The subscriptions have a lifetime (--lifetime) and
are renewed while monitoring runs; when it ends they are cancelled on the device, so that
it stops sending.

  --property P     subscribes to one property (SubscribeCOVProperty), with --increment as
                   the change that triggers a notification
  --confirmed      asks for confirmed notifications, which bacnetctl acknowledges; the
                   default is unconfirmed, which costs the device less
  --poll MS        does not subscribe: reads present-value (or --property) every MS
                   milliseconds and prints it when it changed. For devices and objects
                   without COV support

Notifications are sent to the address the subscription came from: no broadcast is
involved, and the local port may be any (--local-port 0, the default). A firewall or NAT
between the device and this computer must let the device's datagrams back in.

Each notification goes to stdout as it arrives: one line with --format text, one JSON
object per line with --format json, one row per value with --format table. What was
subscribed, and a summary, go to stderr.

` + addressing,
		Example: `
  # Watch two points until Ctrl-C
  bacnetctl monitor --device 192.168.1.20 analog-input:1 binary-input:3

  # The next 10 changes of a temperature of at least 0.5, as JSON Lines
  bacnetctl monitor --device 192.168.1.20 ai:1 --property present-value --increment 0.5 --count 10 --format json

  # Record one hour into a file
  bacnetctl monitor --device 192.168.1.20 ai:1 ai:2 ai:3 --duration 3600 --format json > changes.jsonl

  # A device without COV: poll every 2 seconds
  bacnetctl monitor --device 192.168.1.20 ai:1 --poll 2000
`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.MonitorConfig{ConnConfig: connDefaults(), Lifetime: bac.DefaultCOVLifetime, OutputFormat: textFormat}
	wire(cmd, &cfg, propertyFlag)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return bac.CheckMonitor(&cfg, args) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				summary, err := bac.Monitor(ctx, &cfg, args, cmd.ErrOrStderr(), func(r *types.COVRecord) error { return out.Write(r) })
				if summary.Target != "" {
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

func newAlarmsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alarms",
		Short: "List the active alarms and events of a device (GetEventInformation)",
		Long: `Ask a device which of its objects are in an alarm state, or have a transition that
still waits for an acknowledgement, and list them: the object, its event state, the
transitions that are unacknowledged, and the time stamps of its last to-offnormal,
to-fault and to-normal transitions.

The list is read with GetEventInformation, in as many requests as the device needs. A
device that does not implement that service is asked with the older GetAlarmSummary,
which knows the state and the unacknowledged transitions only.

To acknowledge an entry, see "bacnetctl ack"; to see events as they happen, "bacnetctl
events".

` + addressing,
		Example: `
  # What is in alarm?
  bacnetctl alarms --device 192.168.1.20

  # As JSON
  bacnetctl alarms --device 192.168.1.20 --format json
`,
		Args: cobra.NoArgs,
	}
	cfg := config.AlarmsConfig{ConnConfig: connDefaults(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckAlarms(&cfg) },
			func(ctx context.Context) (any, error) { return bac.Alarms(ctx, &cfg, cmd.ErrOrStderr()) })
	}
	return cmd
}

func newAckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ack OBJECT",
		Short: "Acknowledge an alarm (AcknowledgeAlarm)",
		Long: `Acknowledge one transition of an object: tell the device that a person has seen it.
THIS CHANGES THE DEVICE: the transition is marked acknowledged, and the device tells the
recipients of its notification class.

An acknowledgement names the event state the object went to (offnormal, fault, normal,
high-limit, low-limit or life-safety-alarm) and carries the time stamp of that transition,
exactly as the device recorded it. bacnetctl reads both from the object: its event-state,
and its event-time-stamps. That is two extra requests, and it acknowledges the state the
object is in now. To acknowledge an earlier transition (the return to normal, say), give
--state; to skip the read of the time stamp, give it as --sequence or --event-time, as an
event notification showed it. --source is recorded by the device as who acknowledged.

The request is sent exactly once; when no answer arrives the outcome is unknown.

` + addressing,
		Example: `
  # Acknowledge the alarm of an analog input
  bacnetctl ack --device 192.168.1.20 analog-input:1 --source "J. Smith"

  # Acknowledge its return to normal
  bacnetctl ack --device 192.168.1.20 analog-input:1 --state normal --source "J. Smith"

  # With the state and time stamp an event notification carried
  bacnetctl ack --device 192.168.1.20 ai:1 --state high-limit --sequence 42 --source control-room
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.AckConfig{ConnConfig: connDefaults(), Source: "bacnetctl", OutputFormat: textFormat}
	wire(cmd, &cfg, enum("state", "offnormal", "fault", "normal", "high-limit", "low-limit", "life-safety-alarm"))
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckAck(&cfg, args[0]) },
			func(ctx context.Context) (any, error) { return bac.Ack(ctx, &cfg, args[0], cmd.ErrOrStderr()) })
	}
	return cmd
}

func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Listen for event notifications (alarms and events as they happen)",
		Long: `Listen for the event notifications devices send and print each one: the device and
object, the kind of event, the transition (normal -> high-limit), its priority and
message text, and the time stamp an acknowledgement needs.

A device sends its notifications to the recipients of its notification classes: a
broadcast, or the addresses listed there. bacnetctl only hears what is sent to this
computer or broadcast on its network, on UDP port 47808, which this command binds. Nothing
is sent, except the acknowledgement of a confirmed notification that is addressed to this
computer (and the rejection of any other confirmed request). To be a recipient, add this computer to the recipient-list of the notification
class (with the vendor's tool, or by writing the property).

Listening runs until interrupted (Ctrl-C), or until --duration seconds have passed or
--count notifications were printed. Each notification goes to stdout as it arrives: one
line with --format text, one JSON object per line with --format json.`,
		Example: `
  # Print event notifications until Ctrl-C
  bacnetctl events

  # One hour of events as JSON Lines
  bacnetctl events --duration 3600 --format json > events.jsonl

  # Events from another IP subnet, through the BBMD there
  bacnetctl events --bbmd 10.0.20.1
`,
		Args: cobra.NoArgs,
	}
	cfg := config.EventsConfig{SocketConfig: broadcastSocket(), OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunStream(cmd, cfg.OutputFormat,
			func() error { return bac.CheckEvents(&cfg) },
			func(ctx context.Context, out *format.StreamWriter) (int, error) {
				summary, err := bac.Events(ctx, &cfg, cmd.ErrOrStderr(), func(r *types.EventRecord) error { return out.Write(r) })
				if summary.Target != "" {
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
