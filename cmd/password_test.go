// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/types"
	"github.com/otfabric/go-bacnet/service"
)

const deprecation = "warning: --password is deprecated: on the command line the password is in the shell history and the process list; use --password-file or "

// writeFile writes content to a new file in dir and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The password of reinitialize and comm-control comes from --password-file, from stdin, or
// from BACNETCTL_PASSWORD_FILE; --password still works, with a warning. It is never printed.
func TestPasswordFile(t *testing.T) {
	const secret = "hunter2-do-not-print"
	dev := bactest.Start(t, bactest.Password(secret))
	dir := t.TempDir()
	reinit := []string{"reinitialize", "warmstart", "--yes-i-am-sure"}

	for name, content := range map[string]string{"lf": secret + "\n", "crlf": secret + "\r\n", "bare": secret} {
		path := writeFile(t, dir, name, content)
		on(t, dev, append([]string{"--debug"}, append(reinit, "--password-file", path)...)...).want(t, 0).
			has(t, "reinitialize-device warmstart acknowledged by the device").lacks(t, secret, "deprecated")
	}
	if state, _ := dev.Reinitialized(); state != service.ReinitializedWarmstart {
		t.Errorf("state: %v", state)
	}
	good := writeFile(t, dir, "good", secret+"\n")
	var res types.ManageResult
	on(t, dev, "comm-control", "disable", "--yes-i-am-sure", "--password-file", good, "--format", "json").want(t, 0).lacks(t, secret).decode(t, &res)
	if res.Status != "acknowledged" || res.State != "disable" {
		t.Errorf("result: %+v", res)
	}

	// The device refuses what the file holds when it is not the password.
	wrong := writeFile(t, dir, "wrong", "not-the-password\n")
	on(t, dev, append(reinit, "--password-file", wrong)...).want(t, 6).errHas(t, "password-failure").lacks(t, "not-the-password")
	// Only the last line break goes: the secret (20 octets) and the line break before it are
	// 21 octets, one more than BACnet allows.
	twoLines := writeFile(t, dir, "two-lines", secret+"\n\n")
	on(t, dev, append(reinit, "--password-file", twoLines)...).want(t, 3).errHas(t, "longer than the 20 characters").lacks(t, secret)

	// What cannot be a password is refused before anything is sent.
	sent := len(dev.Requests())
	missing := filepath.Join(dir, "missing")
	if e := on(t, dev, append(reinit, "--password-file", missing, "--format", "json")...).want(t, 3).failure(t); e.Code != "input_file_open_failed" {
		t.Errorf("a missing file: %+v", e)
	}
	for name, args := range map[string][]string{
		"longer than 20":   {"--password-file", writeFile(t, dir, "long", strings.Repeat("x", 21)+"\n")},
		"a large file":     {"--password-file", writeFile(t, dir, "large", strings.Repeat("x", maxPasswordFile+1))},
		"a directory":      {"--password-file", dir},
		"both flags":       {"--password-file", good, "--password", secret},
		"missing for comm": {"--password-file", missing},
	} {
		full := append(append([]string{}, reinit...), args...)
		if name == "missing for comm" {
			full = append([]string{"comm-control", "enable", "--yes-i-am-sure"}, args...)
		}
		if r := on(t, dev, full...); r.code != 3 || r.stdout != "" || strings.Contains(r.stderr, secret) {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
	// Without the confirmation nothing is read either: a usage error.
	on(t, dev, "reinitialize", "warmstart", "--password-file", missing).want(t, 2).errHas(t, "confirm with --yes-i-am-sure")
	if n := len(dev.Requests()); n != sent {
		t.Errorf("%d requests were sent for refused invocations", n-sent)
	}

	// From stdin.
	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetArgs([]string{"reinitialize", "coldstart", "--device", dev.Target(), "--yes-i-am-sure", "--password-file", "-"})
	root.SetIn(strings.NewReader(secret + "\n"))
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.ExecuteContext(context.Background()); err != nil || strings.Contains(out.String()+errOut.String(), secret) {
		t.Errorf("from stdin: %v\n%s%s", err, out.String(), errOut.String())
	}
	if state, _ := dev.Reinitialized(); state != service.ReinitializedColdstart {
		t.Errorf("state after the password from stdin: %v", state)
	}

	// The deprecated flag still works, says so on stderr, and leaves stdout to the result.
	res = types.ManageResult{}
	r := on(t, dev, append(reinit, "--password", secret, "--format", "json")...).want(t, 0).errHas(t, deprecation+"BACNETCTL_PASSWORD\n").lacks(t, secret)
	r.decode(t, &res)
	run(t, "reinitialize", "--help").want(t, 0).has(t, "--password-file string", "[env: BACNETCTL_PASSWORD_FILE]", "(deprecated: on the command line")

	// The variables: the file, and the file and the password together.
	t.Setenv("BACNETCTL_PASSWORD_FILE", good)
	on(t, dev, reinit...).want(t, 0).lacks(t, secret, "deprecated")
	t.Setenv("BACNETCTL_PASSWORD", "not-the-password")
	on(t, dev, reinit...).want(t, 3).errHas(t, "BACNETCTL_PASSWORD and BACNETCTL_PASSWORD_FILE are both set").lacks(t, "not-the-password")
	// A flag wins over the variables.
	on(t, dev, append(reinit, "--password-file", good)...).want(t, 0)
	t.Setenv("BACNETCTL_PASSWORD_FILE", "")
	on(t, dev, append(reinit, "--password-file", good)...).want(t, 0)
	on(t, dev, reinit...).want(t, 6)
	for _, args := range [][]string{{"reinitialize", "--help"}, {"comm-control", "--help"}} {
		run(t, args...).want(t, 0).lacks(t, "not-the-password", secret)
	}
}

// server run takes its password from --password-file or BACNETCTL_SERVER_PASSWORD_FILE too.
func TestServerPasswordFile(t *testing.T) {
	const secret = "s3cret-pw"
	dir := t.TempDir()
	path := writeFile(t, dir, "server-password", secret+"\n")
	clientFile := writeFile(t, dir, "client-password", secret+"\n")

	s := startServer(t, "--password-file", path)
	run(t, "reinitialize", "warmstart", "--device", s.addr, "--yes-i-am-sure").want(t, 6)
	run(t, "reinitialize", "warmstart", "--device", s.addr, "--yes-i-am-sure", "--password-file", clientFile).want(t, 0)
	s.stop(t).want(t, 0).errHas(t, "password required for device management").lacks(t, secret, "deprecated")

	t.Setenv("BACNETCTL_SERVER_PASSWORD_FILE", path)
	s = startServer(t)
	run(t, "comm-control", "enable", "--device", s.addr, "--yes-i-am-sure").want(t, 6)
	run(t, "comm-control", "enable", "--device", s.addr, "--yes-i-am-sure", "--password-file", clientFile).want(t, 0)
	s.stop(t).want(t, 0).lacks(t, secret)

	t.Setenv("BACNETCTL_SERVER_PASSWORD_FILE", "")
	s = startServer(t, "--password", secret)
	s.stop(t).want(t, 0).errHas(t, deprecation+"BACNETCTL_SERVER_PASSWORD\n").lacks(t, secret)

	for name, args := range map[string][]string{
		"missing":    {"--password-file", filepath.Join(dir, "missing")},
		"too long":   {"--password-file", writeFile(t, dir, "long", strings.Repeat("x", 21))},
		"both flags": {"--password-file", path, "--password", secret},
	} {
		if r := run(t, append([]string{"server", "run", "--port", "0", "--duration", "1"}, args...)...); r.code != 3 || r.stdout != "" || strings.Contains(r.stderr, secret) {
			t.Errorf("%s: exit code %d, stdout %q, stderr %q", name, r.code, r.stdout, r.stderr)
		}
	}
}
