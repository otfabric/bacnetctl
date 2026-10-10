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
	"os"
	"os/signal"
	"syscall"

	"github.com/otfabric/bacnetctl/internal/bactest"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:47809", "UDP address to listen on")
	router := flag.Bool("router", true, "also be a BACnet router to network 2001 (device at MAC 0A) and a BBMD")
	password := flag.String("password", "", "password the device asks for in device management requests")
	flag.Parse()

	opts := []bactest.Option{bactest.Password(*password)}
	if *router {
		opts = append(opts, bactest.Router())
	}
	dev, err := bactest.Listen(*listen, opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bacsim:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "bacsim: device %d listening on %s (Ctrl-C to stop)\n", bactest.DefaultInstance, dev.Target())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dev.Run(ctx)
}
