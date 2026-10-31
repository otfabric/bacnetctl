// SPDX-License-Identifier: MIT

// Command bacsim runs the simulated BACnet/IP device of the tests on a UDP port, to try
// bacnetctl by hand without a device:
//
//	go run ./internal/bactest/bacsim -listen 127.0.0.1:47809
//	bacnetctl objects --device 127.0.0.1:47809 --values
//
// It is a development aid, not part of bacnetctl: it implements just enough of a device
// for the tests, and it answers to the address that asked instead of broadcasting.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/otfabric/bacnetctl/internal/bactest"
)

// exit ends the process; a test replaces it.
var exit = os.Exit

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stderr)
	stop()
	exit(code)
}

// run serves the simulated device until ctx ends and returns the exit code.
func run(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("bacsim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", "127.0.0.1:47809", "UDP address to listen on")
	router := fs.Bool("router", true, "also be a BACnet router to network 2001 (device at MAC 0A) and a BBMD")
	password := fs.String("password", "", "password the device asks for in device management requests")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	opts := []bactest.Option{bactest.Password(*password)}
	if *router {
		opts = append(opts, bactest.Router())
	}
	dev, err := bactest.Listen(*listen, opts...)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "bacsim:", err)
		return 1
	}
	_, _ = fmt.Fprintf(stderr, "bacsim: device %d listening on %s (Ctrl-C to stop)\n", bactest.DefaultInstance, dev.Target())
	dev.Run(ctx)
	return 0
}
