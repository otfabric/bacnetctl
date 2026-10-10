// SPDX-License-Identifier: MIT

package cmd

import (
	"context"

	"github.com/otfabric/bacnetctl/internal/bac"
	"github.com/otfabric/bacnetctl/internal/cli"
	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/format"
	"github.com/otfabric/bacnetctl/internal/runner"
	"github.com/spf13/cobra"
)

func newTrendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trend OBJECT",
		Short: "Read the records of a trend log (ReadRange)",
		Long: `Read records from the log buffer of a trend log object and print each with its
position, the time the device logged it, and its value and status flags. A record can
also be a change of the log status, a failure to read the logged property, or a change
of the device's clock.

Records are read by position: --start is the first record (1 is the oldest in the
buffer), --count how many. A device returns what fits one answer, which can be fewer than
asked for: the result then says that more follow. --all reads on from --start to the end
of the buffer, in requests of --count records.

Time stamps are the device's own local time, as it logged them.

` + addressing,
		Example: `
  # The 20 oldest records of a trend log
  bacnetctl trend --device 192.168.1.20 trend-log:1

  # 100 records from position 500
  bacnetctl trend --device 192.168.1.20 trend-log:1 --start 500 --count 100

  # The whole buffer, as JSON
  bacnetctl trend --device 192.168.1.20 trend-log:1 --all --format json > trend.json
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.TrendConfig{ConnConfig: connDefaults(), Start: 1, Count: 20, OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckTrend(&cfg, args[0]) },
			func(ctx context.Context) (any, error) { return bac.Trend(ctx, &cfg, args[0], cmd.ErrOrStderr()) })
	}
	return cmd
}

func newFileCmd() *cobra.Command {
	return group("file", "Read and write file objects",
		`Transfer the content of file objects with AtomicReadFile and AtomicWriteFile: a
configuration, a log, a firmware image, whatever the device keeps in its file objects.
Only stream-access files are transferred.`,
		newFileGetCmd(), newFilePutCmd())
}

func newFileGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get OBJECT",
		Short: "Read a file object into a local file (AtomicReadFile)",
		Long: `Read the content of a stream-access file object, from the first octet to the end of
the file, and write it to --output. The result names the number of octets and the SHA-256
of the content.

The file is read in requests of --chunk octets: lower it for a device with a small APDU
(the answer must fit it), raise it up to 1400 for speed with a device that accepts large
APDUs. --max-size refuses a file that is larger, before anything is written.

With --output - the content goes to stdout and the result to stderr.

` + addressing,
		Example: `
  # Fetch file 1 into a local file
  bacnetctl file get --device 192.168.1.20 file:1 --output config.bin

  # Pipe a text file
  bacnetctl file get --device 192.168.1.20 file:2 --output - | less
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.FileGetConfig{ConnConfig: connDefaults(), MaxSize: 16 << 20, Chunk: 256, OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		collect := func(ctx context.Context) (any, error) {
			return bac.FileGet(ctx, &cfg, args[0], cmd.OutOrStdout(), cmd.ErrOrStderr())
		}
		if cfg.Output != bac.Stdout {
			return runner.RunFormattedChecked(cmd, cfg.OutputFormat, func() error { return bac.CheckFileGet(&cfg, args[0]) }, collect)
		}
		// The content takes stdout: the result goes to stderr.
		outFmt, err := runner.AttachOutputFormat(cmd, cfg.OutputFormat)
		if err != nil {
			return err
		}
		if err := bac.CheckFileGet(&cfg, args[0]); err != nil {
			return errs.WrapValidation(err)
		}
		res, err := collect(cmd.Context())
		if err != nil {
			return err
		}
		if err := format.Write(cmd.ErrOrStderr(), outFmt, res); err != nil {
			return errs.Output(errs.CodeOutputWriteFailed, err)
		}
		return nil
	}
	return cmd
}

func newFilePutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put OBJECT",
		Short: "Write a local file into a file object (AtomicWriteFile)",
		Long: `Write the content of --input to a stream-access file object, starting at --start (the
beginning of the file unless given). THIS CHANGES THE DEVICE: a file object can hold a
configuration or a program the device acts on.

The content is written in requests of --chunk octets. Every request is sent exactly once.
When one is refused or not acknowledged, the transfer stops and the error says how many
octets were acknowledged before: the file object then holds a part of the content.

` + addressing,
		Example: `
  # Send a configuration file
  bacnetctl file put --device 192.168.1.20 file:1 --input config.bin

  # Append at a position, in small requests
  bacnetctl file put --device 192.168.1.20 file:2 --input more.txt --start 4096 --chunk 128
`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: objectArgs,
	}
	cfg := config.FilePutConfig{ConnConfig: connDefaults(), Chunk: 256, OutputFormat: textFormat}
	wire(cmd, &cfg)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg.Debug = cli.Debug(cmd)
		return runner.RunFormattedChecked(cmd, cfg.OutputFormat,
			func() error { return bac.CheckFilePut(&cfg, args[0]) },
			func(ctx context.Context) (any, error) { return bac.FilePut(ctx, &cfg, args[0], cmd.ErrOrStderr()) })
	}
	return cmd
}
