// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/spf13/cobra"
)

// maxFrameInput bounds what `decode -` reads from stdin: a BACnet/IP datagram is at most
// 1497 octets, which is 2994 hex digits.
const maxFrameInput = 64 << 10

func newDecodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decode HEX...",
		Short: "Decode a captured BACnet/IP datagram (offline)",
		Long: `Decode one BACnet/IP datagram, given as hex, layer by layer: the BVLC header, the NPDU
with its routing information, the APDU, and the service it carries with its parameters.
Nothing is sent: this is for frames copied from a packet capture or a log.

HEX is the UDP payload, starting with 81. Spaces, colons, dashes and 0x prefixes are
ignored, and the hex may be split over several arguments. "-" reads it from stdin.

Services bacnetctl has a decoder for are shown field by field (Who-Is, I-Am, Who-Has,
I-Have, ReadProperty, ReadPropertyMultiple, WriteProperty, WritePropertyMultiple,
SubscribeCOV, COV and event notifications, ReadRange, the file services, time
synchronization, device management, alarm acknowledgement); of the others, and of a
segment of a segmented message, the payload is shown as hex. A password in a device
management request is not shown. Decoding goes as far as it can and reports what stopped
it; a frame that is no BACnet/IP at all is an error (exit code 3).`,
		Example: `
  # A Who-Is broadcast
  bacnetctl decode 810b000c0120ffff00ff1008

  # An I-Am, as copied from Wireshark, as JSON
  bacnetctl decode "81 0b 00 14 01 00 10 00 c4 02 00 04 d2 22 05 c4 91 03 21 63" --format json

  # From a file
  bacnetctl decode - < frame.hex
`,
		Args: cobra.MinimumNArgs(1),
	}
	cfg := config.DecodeConfig{OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		var raw []byte
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error {
				text := strings.Join(args, " ")
				if len(args) == 1 && args[0] == "-" {
					in, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxFrameInput))
					if err != nil {
						return errs.InvalidInput(errs.CodeInputDecodeFailed, fmt.Sprintf("cannot read stdin: %v", err), err)
					}
					text = string(in)
				}
				var err error
				if raw, err = bac.ParseHex(text); err != nil {
					return err
				}
				if len(raw) == 0 {
					return fmt.Errorf("no hex given")
				}
				if res := bac.Decode(raw); len(res.Layers) == 0 {
					return errs.InvalidInput(errs.CodeInputDecodeFailed, "not a BACnet/IP datagram: "+res.Error, nil)
				}
				return nil
			},
			func(context.Context) (any, error) { return bac.Decode(raw), nil })
	}
	return cmd
}

func newTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "types",
		Short: "List the object types bacnetctl knows by name",
		Long: `List the standard BACnet object types with their numbers and the short forms bacnetctl
accepts (ai for analog-input, and so on).

An object is written type:instance. The type can be any of these names, a short form, or
the number: analog-input:1, ai:1 and 0:1 are the same object. An object type without a
name here (a vendor's proprietary type, 128 and above) is written by its number.`,
		Example: `
  # All object types
  bacnetctl types

  # The ones with "value" in their name, as a table
  bacnetctl types --filter value --format table
`,
		Args: cobra.NoArgs,
	}
	cfg := config.ListConfig{OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat, func() error { return nil },
			func(context.Context) (any, error) {
				return bac.NameTable("object-types", bac.ObjectTypes(), cfg.Filter), nil
			})
	}
	return cmd
}

func newPropertiesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "properties",
		Short: "List the property identifiers bacnetctl knows by name",
		Long: `List the standard BACnet property identifiers with their numbers and the short forms
bacnetctl accepts (pv for present-value, and so on).

A property is written by one of these names or by its number; presentValue and
Present_Value are understood too. A property without a name here (a vendor's proprietary
property, 512 and above) is written by its number. The properties all, required and
optional are special: they ask "bacnetctl read" for every property of that kind.`,
		Example: `
  # All properties
  bacnetctl properties

  # Everything about limits
  bacnetctl properties --filter limit
`,
		Args: cobra.NoArgs,
	}
	cfg := config.ListConfig{OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat, func() error { return nil },
			func(context.Context) (any, error) {
				return bac.NameTable("properties", bac.Properties(), cfg.Filter), nil
			})
	}
	return cmd
}
