// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"github.com/otfabric/bacnetctl/cmd"
)

// Injected at link time (see Makefile): -X main.version=... etc.
var (
	version   = "dev"
	tag       = ""
	commit    = ""
	buildDate = ""
)

func main() {
	cmd.SetBuildMeta(version, tag, commit, buildDate)
	os.Exit(cmd.Execute())
}
