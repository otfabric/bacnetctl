// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// runningServer is `bacnetctl server run` in the background, stopped as by Ctrl-C.
type runningServer struct {
	addr           string
	stdout, stderr *syncBuffer
	cancel         context.CancelFunc
	code           chan int
}

var listeningAt = regexp.MustCompile(`(?:"address":"|  on )(127\.0\.0\.1:\d+)`)

// startServer runs `server run` on loopback, on a free port, with JSON records, and waits
// until it listens.
func startServer(t *testing.T, args ...string) *runningServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &runningServer{stdout: &syncBuffer{}, stderr: &syncBuffer{}, cancel: cancel, code: make(chan int, 1)}
	full := append([]string{"server", "run", "--listen", "127.0.0.1", "--port", "0", "--format", "json"}, args...)
	go func() { s.code <- Run(ctx, full, s.stdout, s.stderr) }()
	t.Cleanup(cancel)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m := listeningAt.FindStringSubmatch(s.stdout.String()); m != nil {
			s.addr = m[1]
			return s
		}
		select {
		case c := <-s.code:
			t.Fatalf("server run ended with %d before it listened\n%s\n%s", c, s.stdout, s.stderr)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("server run did not listen\n%s", s.stderr)
	return nil
}

// stop interrupts the server and returns its result.
func (s *runningServer) stop(t *testing.T) result {
	t.Helper()
	s.cancel()
	select {
	case c := <-s.code:
		return result{s.stdout.String(), s.stderr.String(), c}
	case <-time.After(10 * time.Second):
		t.Fatal("server run did not stop")
		return result{}
	}
}

// The client commands of bacnetctl against its own server: what they change is recorded,
// and Ctrl-C ends the server normally.
func TestServerRun(t *testing.T) {
	t.Setenv("BACNETCTL_SERVER_PASSWORD", "s3cret-pw")
	s := startServer(t, "--instance", "4321")
	run(t, "read", "--device", s.addr, "av:1", "pv").want(t, 0).has(t, "21")
	run(t, "write", "--device", s.addr, "av:1", "pv", "22.5", "--priority", "8").want(t, 0)
	run(t, "read", "--device", s.addr, "av:1", "pv").want(t, 0).has(t, "22.5")
	run(t, "device", "--device", s.addr, "--format", "json").want(t, 0).has(t, `"object": "device:4321"`, `"name": "bacnetctl-4321"`)
	// Device management needs the password.
	run(t, "reinitialize", "warmstart", "--device", s.addr, "--yes-i-am-sure").want(t, 6)
	run(t, "reinitialize", "warmstart", "--device", s.addr, "--yes-i-am-sure", "--password", "s3cret-pw").want(t, 0)

	r := s.stop(t).want(t, 0)
	lines := r.lines(t)
	if len(lines) != 3 || lines[0]["event"] != "listening" || lines[0]["object"] != "device:4321" || lines[0]["objects"] != 6.0 ||
		lines[1]["event"] != "write" || lines[1]["priority"] != 8.0 || lines[1]["status"] != "allowed" ||
		lines[1]["value"].(map[string]any)["value"] != 22.5 || lines[2]["event"] != "reinitialize" || lines[2]["state"] != "warmstart" {
		t.Errorf("records: %v", lines)
	}
	r.errHas(t, `serving device:4321 "bacnetctl-4321" from (built-in example device) on `+s.addr,
		"password required for device management", "device:4321 on "+s.addr+": served for ", "3 records")
	r.lacks(t, "s3cret-pw")
}

// server example prints a device file that server run serves; a bacnet-interop fixture
// is one too.
func TestServerExampleAndFixture(t *testing.T) {
	r := run(t, "server", "example").want(t, 0)
	var df map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &df); err != nil || df["device_instance"] != 1234.0 {
		t.Fatalf("example: %v\n%s", err, r.stdout)
	}
	path := filepath.Join(t.TempDir(), "device.json")
	if err := os.WriteFile(path, []byte(r.stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	s := startServer(t, "--config", path, "--read-only", "--name", "renamed")
	run(t, "write", "--device", s.addr, "av:1", "pv", "1").want(t, 6)
	out := s.stop(t).want(t, 0)
	if lines := out.lines(t); len(lines) != 2 || lines[0]["name"] != "renamed" || lines[1]["status"] != "refused" {
		t.Errorf("records: %v", lines)
	}

	s = startServer(t, "--config", filepath.Join("..", "internal", "bac", "testdata", "device-server-v1.json"), "--format", "text")
	run(t, "trend", "--device", s.addr, "tl:1").want(t, 0).has(t, "9")
	run(t, "file", "get", "--device", s.addr, "file:1", "--output", "-").want(t, 0).has(t, "hello-world-stream-file-data")
	s.stop(t).want(t, 0).has(t, "listening", `"InteropDevice"`, "8 objects")
}

func TestServerRunErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`{"device_instance": 1, "device_name": "d", "objects": [{"type": "toaster", "instance": 1, "object_name": "t"}]}`), 0o600)
	for name, tc := range map[string]struct {
		args       []string
		code       int
		kind, want string
	}{
		"argument":    {[]string{"extra"}, 2, "usage", "invalid_input"},
		"listen":      {[]string{"--listen", "::1"}, 3, "invalid_input", "invalid_input"},
		"missing":     {[]string{"--config", filepath.Join(dir, "none.json")}, 3, "invalid_input", "input_file_open_failed"},
		"bad file":    {[]string{"--config", bad}, 3, "invalid_input", "invalid_config"},
		"bdt":         {[]string{"--bdt", "10.0.0.1"}, 3, "invalid_input", "invalid_input"},
		"format":      {[]string{"--format", "yaml"}, 3, "invalid_input", "invalid_format"},
		"long secret": {[]string{"--password", strings.Repeat("p", 21)}, 3, "invalid_input", "invalid_input"},
	} {
		args := append([]string{"server", "run", "--format", "json", "--port", "0"}, tc.args...)
		if name == "format" {
			args = append([]string{"server", "run", "--port", "0"}, tc.args...)
		}
		r := run(t, args...).want(t, tc.code)
		if name == "format" {
			continue
		}
		if e := r.failure(t); e.Kind != tc.kind || e.Code != tc.want {
			t.Errorf("%s: %+v", name, e)
		}
		r.lacks(t, strings.Repeat("p", 21))
	}
	// The port of another program.
	s := startServer(t)
	port := s.addr[strings.LastIndex(s.addr, ":")+1:]
	if e := run(t, "server", "run", "--listen", "127.0.0.1", "--port", port, "--format", "json").want(t, 4).failure(t); e.Code != "transport_port_in_use" {
		t.Errorf("port in use: %+v", e)
	}
	// --duration ends it on its own; the password from the environment is not in --help.
	t.Setenv("BACNETCTL_SERVER_PASSWORD", "hidden-pw")
	run(t, "server", "run", "--listen", "127.0.0.1", "--port", "0", "--duration", "1", "--format", "table").want(t, 0).has(t, "EVENT", "listening")
	run(t, "server", "run", "--help").want(t, 0).has(t, "BACNETCTL_SERVER_PASSWORD").lacks(t, "hidden-pw")
	run(t, "server").want(t, 0).has(t, "run", "example")
}
