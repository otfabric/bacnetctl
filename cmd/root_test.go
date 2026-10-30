// SPDX-License-Identifier: MIT

package cmd

import (
	"runtime"
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestVersionHelpAndUsage(t *testing.T) {
	SetBuildMeta("1.2.3", "v1.2.3", "abc1234", "2026-01-31T12:00:00Z")
	t.Cleanup(func() { SetBuildMeta("dev", "", "", "") })
	run(t, "version").want(t, 0).has(t, "bacnetctl version: 1.2.3", "tag:         v1.2.3", "commit:      abc1234", "build date:  2026-01-31T12:00:00Z", "go version:", "platform:")
	SetBuildMeta(" ", "", "", "")
	r := run(t, "version").want(t, 0).has(t, "bacnetctl version: (unknown)")
	if strings.Contains(r.stdout, "commit:") {
		t.Errorf("empty build metadata is printed:\n%s", r.stdout)
	}

	run(t).want(t, 0).has(t, "Find devices and objects:", "discover", "read", "write", "monitor", "decode", "completion", "version")
	run(t, "file").want(t, 0).has(t, "get", "put")
	run(t, "time").want(t, 0).has(t, "sync")
	run(t, "router").want(t, 0).has(t, "list")
	run(t, "bbmd").want(t, 0).has(t, "bdt", "fdt")
	run(t, "write", "--help").want(t, 0).has(t, "THIS CHANGES THE DEVICE", "BACNETCTL_DEVICE", "--priority", "real:21.5")

	run(t, "nope").want(t, 2).errHas(t, `error: unknown command "nope"`)
	run(t, "file", "nope").want(t, 2)
	run(t, "read", "--nope").want(t, 2).errHas(t, "unknown flag")
	run(t, "read", "--local-port", "banana", "ai:1", "pv").want(t, 2)
	run(t, "read", "ai:1").want(t, 2)
	run(t, "write", "ai:1", "pv").want(t, 2)
	run(t, "describe").want(t, 2)
	run(t, "monitor").want(t, 2)
	run(t, "decode").want(t, 2)
	run(t, "version", "extra").want(t, 2)
	run(t, "discover", "extra").want(t, 2)
}

func TestCompletion(t *testing.T) {
	for shell, marker := range map[string]string{
		"bash":       "__start_bacnetctl",
		"zsh":        "#compdef bacnetctl",
		"fish":       "complete -c bacnetctl",
		"powershell": "Register-ArgumentCompleter",
	} {
		run(t, "completion", shell).want(t, 0).has(t, marker)
	}
	run(t, "completion").want(t, 2)
	run(t, "completion", "tcsh").want(t, 2)

	// Flags with a fixed set of values complete to those values, object arguments to the
	// object types and property arguments to the property names.
	for _, c := range []struct {
		args []string
		want []string
		not  []string
	}{
		{[]string{"read", "--format", ""}, []string{"json", "table", "text"}, nil},
		{[]string{"write", "--type", "o"}, []string{"object", "octets"}, []string{"real"}},
		{[]string{"ack", "--state", "h"}, []string{"high-limit"}, []string{"fault"}},
		{[]string{"objects", "--type", "analog"}, []string{"analog-input", "analog-output", "analog-value"}, []string{"binary-input"}},
		{[]string{"release", "--property", "present"}, []string{"present-value"}, nil},
		{[]string{"read", "analog-i"}, []string{"analog-input:"}, []string{"binary-input:"}},
		{[]string{"read", "ai:1", "pres"}, []string{"present-value"}, []string{"units"}},
		{[]string{"read", "ai:1", "units,pres"}, []string{"units,present-value"}, nil},
		{[]string{"read", "ai:1", "a"}, []string{"all", "apdu-timeout"}, nil},
		{[]string{"read", "ai:1", "pv", "b"}, []string{"binary-value:", "bv:"}, nil},
		{[]string{"describe", "dev"}, []string{"device:", "dev:"}, nil},
		{[]string{"monitor", "ai:1", "msv"}, []string{"msv:"}, nil},
		{[]string{"reinitialize", "w"}, []string{"warmstart"}, []string{"coldstart"}},
		{[]string{"comm-control", ""}, []string{"enable", "disable", "disable-initiation"}, nil},
		{[]string{"completion", ""}, []string{"bash", "zsh", "fish", "powershell"}, nil},
	} {
		r := run(t, append([]string{cobra.ShellCompNoDescRequestCmd}, c.args...)...).want(t, 0)
		for _, w := range c.want {
			if !strings.Contains("\n"+r.stdout, "\n"+w+"\n") {
				t.Errorf("completion of %v lacks %q:\n%s", c.args, w, r.stdout)
			}
		}
		for _, n := range c.not {
			if strings.Contains("\n"+r.stdout, "\n"+n+"\n") {
				t.Errorf("completion of %v offers %q:\n%s", c.args, n, r.stdout)
			}
		}
	}
	// An object that has its type is not completed further.
	r := run(t, cobra.ShellCompNoDescRequestCmd, "describe", "ai:").want(t, 0)
	if strings.Contains(r.stdout, "ai:") {
		t.Errorf("an instance number was completed:\n%s", r.stdout)
	}
}

// Every runnable command has a long description, every leaf command examples, and every
// flag bound to the configuration says which environment variable sets it.
func TestCommandsAreDocumented(t *testing.T) {
	leaves := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Name() == "help" || strings.HasPrefix(c.Name(), "__") {
			return
		}
		if c.Long == "" && c.Runnable() && c.Name() != "version" {
			t.Errorf("%s has no long description", c.CommandPath())
		}
		if c.Runnable() && !c.HasSubCommands() && c.Name() != "version" {
			leaves++
			if c.Example == "" {
				t.Errorf("%s has no examples", c.CommandPath())
			}
			if !strings.Contains(c.Example, c.CommandPath()) {
				t.Errorf("the examples of %s do not show the command", c.CommandPath())
			}
		}
		if c.Short == "" {
			t.Errorf("%s has no short description", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	root := NewRootCmd()
	walk(root)
	if leaves < 23 {
		t.Errorf("only %d leaf commands found", leaves)
	}

	r := run(t, "monitor", "--help").want(t, 0)
	seen := map[string]bool{}
	flags := r.stdout[strings.Index(r.stdout, "\nFlags:\n"):strings.Index(r.stdout, "\nGlobal Flags:")]
	for _, line := range strings.Split(flags, "\n") {
		i := strings.Index(line, "[env: ")
		if !strings.HasPrefix(strings.TrimSpace(line), "--") || strings.Contains(line, "--help") {
			continue
		}
		if i < 0 {
			t.Errorf("flag without an environment variable: %s", strings.TrimSpace(line))
			continue
		}
		name := line[i+6 : i+strings.Index(line[i:], "]")]
		if !strings.HasPrefix(name, "BACNETCTL_") || seen[name] {
			t.Errorf("environment variable %q is not BACNETCTL_* or is used twice", name)
		}
		seen[name] = true
	}
	if len(seen) < 17 {
		t.Errorf("only %d flags with an environment variable found in the help of monitor", len(seen))
	}

	// The environment is read for every command at startup: a variable must mean the same
	// kind of value wherever it is used, or setting it for one command breaks the others.
	kinds := map[string]string{}
	var check func(c *cobra.Command)
	check = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			i := strings.Index(f.Usage, "[env: ")
			if i < 0 {
				return
			}
			name := f.Usage[i+6 : i+strings.Index(f.Usage[i:], "]")]
			if kind, known := kinds[name]; known && kind != f.Value.Type() {
				t.Errorf("%s is a %s for one command and a %s for %s", name, kind, f.Value.Type(), c.CommandPath())
			}
			kinds[name] = f.Value.Type()
		})
		for _, sub := range c.Commands() {
			check(sub)
		}
	}
	check(root)
	if len(kinds) < 45 {
		t.Errorf("only %d environment variables found", len(kinds))
	}
}

func TestEnvironment(t *testing.T) {
	dev := bactest.Start(t)

	// The environment sets what a flag would.
	t.Setenv("BACNETCTL_DEVICE", dev.Target())
	t.Setenv("BACNETCTL_OUTPUT_FORMAT", "json")
	if v := run(t, "read", "ai:1", "pv").want(t, 0).object(t)["rows"].([]any)[0].(map[string]any)["value"].(map[string]any)["value"]; v != 21.5 {
		t.Errorf("value read through the environment: %v", v)
	}
	// It is shown as the default of the flag.
	run(t, "read", "--help").want(t, 0).has(t, `[env: BACNETCTL_DEVICE] (default "`+dev.Target()+`")`)

	// A flag wins over the environment.
	run(t, "read", "ai:1", "pv", "--format", "text").want(t, 0).has(t, "21.5\n").lacks(t, "{")
	t.Setenv("BACNETCTL_DEVICE", bactest.Start(t, bactest.Silent()).Target())
	run(t, "read", "ai:1", "pv", "--device", dev.Target()).want(t, 0)
	// The short timeout is for the device that never answers: a live one on a busy
	// machine can miss it.
	t.Setenv("BACNETCTL_TIMEOUT", "100")
	t.Setenv("BACNETCTL_RETRIES", "0")
	run(t, "read", "ai:1", "pv").want(t, 5)

	// A malformed value is an error before anything runs, whatever the command.
	t.Setenv("BACNETCTL_LOCAL_PORT", "banana")
	// BACNETCTL_OUTPUT_FORMAT is still json: the error is the JSON object, on stdout.
	if e := run(t, "version").want(t, 3).failure(t); e.Kind != "invalid_input" || !strings.Contains(e.Message, `invalid environment: BACNETCTL_LOCAL_PORT="banana"`) {
		t.Errorf("error: %+v", e)
	}
	run(t, "version", "--format", "text").want(t, 3).silent(t).errHas(t, `error: invalid environment: BACNETCTL_LOCAL_PORT="banana"`)
	defer func() {
		if recover() == nil {
			t.Error("a malformed environment value was accepted")
		}
	}()
	NewRootCmd()
}

// An invocation Cobra refuses before a command runs is still answered in the format that
// was asked for: a script that reads JSON gets its error object in every case.
func TestUsageErrorsAsJSON(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag":      {"read", "--nope", "--format", "json"},
		"format with =":     {"read", "--format=json", "--nope"},
		"missing arguments": {"read", "--format", "json"},
		"unknown command":   {"nope", "--format", "json"},
		"bad flag value":    {"read", "--format", "json", "--local-port", "banana", "ai:1", "pv"},
		"write in twos":     {"write", "--format", "json", "av:1", "pv"},
	} {
		r := run(t, args...).want(t, 2)
		if e := r.failure(t); e.Kind != "usage" || e.Code != "invalid_input" || e.Message == "" || r.stderr != "" {
			t.Errorf("%s: %+v, stderr %q", name, e, r.stderr)
		}
	}
	// Without it, and with another format, the error is one line on stderr.
	run(t, "read", "--nope").want(t, 2).silent(t).errHas(t, "error: unknown flag")
	run(t, "read", "--nope", "--format", "table").want(t, 2).silent(t).errHas(t, "error: unknown flag")
	// What follows "--" is not a flag.
	run(t, "read", "--nope", "--", "--format", "json").want(t, 2).silent(t)
	// The environment asks for JSON as the flag does, and the flag wins.
	t.Setenv("BACNETCTL_OUTPUT_FORMAT", "json")
	if e := run(t, "nope").want(t, 2).failure(t); e.Kind != "usage" {
		t.Errorf("environment: %+v", e)
	}
	run(t, "nope", "--format", "text").want(t, 2).silent(t).errHas(t, "error: unknown command")
}

func TestVersionFormats(t *testing.T) {
	SetBuildMeta("1.2.3", "v1.2.3", "abc1234", "2026-01-31T12:00:00Z")
	t.Cleanup(func() { SetBuildMeta("dev", "", "", "") })
	var v types.VersionResult
	run(t, "version", "--format", "json").want(t, 0).decode(t, &v)
	if v.Version != "1.2.3" || v.Tag != "v1.2.3" || v.Commit != "abc1234" || v.BuildDate != "2026-01-31T12:00:00Z" ||
		v.GoVersion != runtime.Version() || v.OS != runtime.GOOS || v.Arch != runtime.GOARCH {
		t.Errorf("version: %+v", v)
	}
	run(t, "version", "--format", "table").want(t, 0).has(t, "FIELD", "version", "1.2.3", "platform", runtime.GOOS+"/"+runtime.GOARCH)
	run(t, "version", "--format", "yaml").want(t, 3)
	SetBuildMeta("dev", "", "", "")
	if m := run(t, "version", "--format", "json").want(t, 0).object(t); m["tag"] != nil || m["commit"] != nil || m["build_date"] != nil {
		t.Errorf("empty build metadata is printed: %v", m)
	}
}
