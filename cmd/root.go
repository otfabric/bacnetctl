// SPDX-License-Identifier: MIT

// Package cmd is the command tree of bacnetctl.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/format"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/otfabric/bacnetctl/internal/types"
	"github.com/spf13/cobra"
)

// buildMeta holds link-time metadata set from main via SetBuildMeta.
var buildMeta struct {
	version, tag, commit, buildDate string
}

// SetBuildMeta is called from main before Execute; values come from ldflags -X main.*.
func SetBuildMeta(version, tag, commit, buildDate string) {
	buildMeta.version = version
	buildMeta.tag = tag
	buildMeta.commit = commit
	buildMeta.buildDate = buildDate
}

// Command groups of the help of the root command.
const (
	groupFind    = "find"
	groupRead    = "read"
	groupWrite   = "write"
	groupWatch   = "watch"
	groupNetwork = "network"
	groupManage  = "manage"
	groupTools   = "tools"
)

// NewRootCmd builds the command tree. Every call returns a fresh tree, with the flags of
// every command at the defaults the environment gives them.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "bacnetctl",
		Short: "BACnet/IP command-line client",
		Long: `bacnetctl is a BACnet/IP client for the command line: it finds the devices of a
building automation network, reads and writes their properties, lists their objects,
watches values change, reads alarms, trend logs and files, and decodes captured frames.

A device is addressed with --device host[:port] (UDP port 47808 unless given). A device
behind a BACnet router (on MS/TP, for example) is addressed with the router as --device
plus --network and --mac. Objects are written type:instance, as in analog-input:1 or
ai:1; properties by name, as in present-value.

Results go to stdout as text, a table or JSON (--format); progress and diagnostics go to
stderr. Exit codes are stable: see docs/exitcodes.md.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().Bool("debug", false, "Log the diagnostics of the BACnet client to stderr (dropped and unexpected frames, routes), and show the causes of an error")
	root.AddGroup(
		&cobra.Group{ID: groupFind, Title: "Find devices and objects:"},
		&cobra.Group{ID: groupRead, Title: "Read:"},
		&cobra.Group{ID: groupWrite, Title: "Write:"},
		&cobra.Group{ID: groupWatch, Title: "Watch values, alarms and events:"},
		&cobra.Group{ID: groupNetwork, Title: "Routers and BBMDs:"},
		&cobra.Group{ID: groupManage, Title: "Change how a device runs:"},
		&cobra.Group{ID: groupTools, Title: "Tools:"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add(groupFind, newDiscoverCmd(), newWhoHasCmd())
	add(groupRead, newReadCmd(), newObjectsCmd(), newDescribeCmd(), newDeviceCmd(), newTrendCmd(), newFileCmd())
	add(groupWrite, newWriteCmd(), newReleaseCmd(), newCreateCmd(), newDeleteCmd(), newTimeCmd())
	add(groupWatch, newMonitorCmd(), newAlarmsCmd(), newAckCmd(), newEventsCmd())
	add(groupNetwork, newRouterCmd(), newBBMDCmd())
	add(groupManage, newReinitializeCmd(), newCommControlCmd())
	add(groupTools, newDecodeCmd(), newTypesCmd(), newPropertiesCmd(), newCompletionCmd(), newVersionCmd())
	root.SetHelpCommandGroupID(groupTools)
	return root
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of bacnetctl",
		Args:  cobra.NoArgs,
	}
	cfg := config.VersionConfig{OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat, func() error { return nil },
			func(context.Context) (any, error) {
				ver := strings.TrimSpace(buildMeta.version)
				if ver == "" {
					ver = "(unknown)"
				}
				return &types.VersionResult{
					Version: ver, Tag: strings.TrimSpace(buildMeta.tag), Commit: strings.TrimSpace(buildMeta.commit),
					BuildDate: strings.TrimSpace(buildMeta.buildDate), GoVersion: runtime.Version(),
					OS: runtime.GOOS, Arch: runtime.GOARCH,
				}, nil
			})
	}
	return cmd
}

// Execute runs the root command and returns the process exit code (main should call os.Exit).
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

// Run executes bacnetctl with args, writing to stdout and stderr, and returns the exit
// code. It is Execute without the process: what the tests drive.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root, err := buildRoot()
	if err != nil {
		return runner.RenderFatal(usageInvocation(args), err, stdout, stderr, false)
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	inv := &runner.Invocation{}
	ctx = runner.WithInvocation(ctx, inv)
	err = root.ExecuteContext(ctx)
	debug, _ := root.PersistentFlags().GetBool("debug")
	if err != nil {
		if !inv.HasOutputFormat {
			// Cobra refused the invocation before the command saw its flags.
			inv = usageInvocation(args)
		}
		return runner.RenderFatal(inv, usageError(err), stdout, stderr, debug)
	}
	if inv.SuccessExitSet {
		return inv.SuccessExitCode
	}
	return errs.ExitOK
}

// usageInvocation says in which format to report an error that came before any command
// ran (an unknown flag, a missing argument, a malformed environment variable): as JSON
// when the invocation asks for JSON, with --format or BACNETCTL_FORMAT, so that a
// script that reads JSON from stdout gets its error object in every case.
func usageInvocation(args []string) *runner.Invocation {
	inv := &runner.Invocation{}
	asked := config.Getenv("BACNETCTL_FORMAT")
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if v, ok := strings.CutPrefix(arg, "--format="); ok {
			asked = v
		} else if arg == "--format" && i+1 < len(args) {
			asked = args[i+1]
		}
	}
	if strings.EqualFold(strings.TrimSpace(asked), string(format.FormatJSON)) {
		inv.SetOutputFormat(format.FormatJSON)
	}
	return inv
}

// buildRoot is NewRootCmd with a malformed environment variable reported as invalid input
// instead of a panic: the configuration of every command is read when the tree is built.
func buildRoot() (root *cobra.Command, err error) {
	defer func() {
		if r := recover(); r != nil {
			msg := strings.TrimPrefix(fmt.Sprint(r), "bacnetctl: ")
			err = errs.InvalidInput(errs.CodeInvalidInput, msg, nil)
		}
	}()
	return NewRootCmd(), nil
}

// usageError classifies what Cobra itself rejects (an unknown command or flag, a missing
// required flag, a value a flag cannot hold) as a usage error: exit code 2.
func usageError(err error) error {
	if _, ok := errs.As(err); ok {
		return err
	}
	return errs.New(errs.KindUsage, errs.CodeInvalidInput, err.Error(), err)
}
