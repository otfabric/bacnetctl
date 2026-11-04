// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startSim starts the simulated device named by BACNETCTL_E2E_BACSIM (built by make e2e)
// on a free loopback port and returns its address. It is stopped when the test ends.
func startSim(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("BACNETCTL_E2E_BACSIM")
	if bin == "" {
		t.Fatal("BACNETCTL_E2E_BACSIM is not set; run make e2e")
	}
	cmd := exec.Command(bin, "-listen", "127.0.0.1:0", "-router=false")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	exited := make(chan error, 1)
	t.Cleanup(func() {
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})
	addr := make(chan string, 1)
	listening := regexp.MustCompile(`listening on (127\.0\.0\.1:\d+)`)
	go func() {
		sc := bufio.NewScanner(stderr)
		sent := false
		for sc.Scan() {
			if m := listening.FindStringSubmatch(sc.Text()); m != nil && !sent {
				addr <- m[1]
				sent = true
			}
		}
		exited <- cmd.Wait()
	}()
	select {
	case a := <-addr:
		return a
	case err := <-exited:
		exited <- err
		t.Fatalf("bacsim ended before it listened: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("bacsim did not say where it listens")
	}
	return ""
}

// runSplit executes the bacnetctl binary with stdout and stderr apart.
func runSplit(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	bin := os.Getenv("BACNETCTL_E2E_BIN")
	if bin == "" {
		t.Fatal("BACNETCTL_E2E_BIN is not set; run make e2e")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), errOut.String(), 0
	case errors.As(err, &exit) && ctx.Err() == nil:
		return out.String(), errOut.String(), exit.ExitCode()
	default:
		t.Fatalf("run %s %s: %v\n%s", bin, strings.Join(args, " "), err, errOut.String())
		return "", "", -1
	}
}

// want runs bacnetctl, fails unless it exits with code, and returns stdout.
func want(t *testing.T, code int, args ...string) string {
	t.Helper()
	stdout, stderr, got := runSplit(t, args...)
	if got != code {
		t.Fatalf("bacnetctl %s: exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), got, code, stdout, stderr)
	}
	return stdout
}

// decodeJSON parses stdout as exactly one JSON document.
func decodeJSON(t *testing.T, stdout string, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one JSON document:\n%s", stdout)
	}
}

// The built binary against the built simulated device, over real UDP on the loopback
// interface.
func TestAgainstSimulatedDevice(t *testing.T) {
	sim := startSim(t)

	t.Run("discover", func(t *testing.T) {
		var res struct {
			Count   int
			Devices []struct {
				Instance uint32
				Address  string
				VendorID *uint32 `json:"vendor_id"`
			}
		}
		decodeJSON(t, want(t, 0, "discover", "--target", sim, "--local-port", "0", "--format", "json"), &res)
		if res.Count != 1 || len(res.Devices) != 1 || res.Devices[0].Instance != 1234 || res.Devices[0].Address != sim {
			t.Errorf("discover: %+v", res)
		}
	})

	t.Run("read", func(t *testing.T) {
		if out := want(t, 0, "read", "--device", sim, "ai:1", "present-value"); !strings.Contains(out, "21.5") {
			t.Errorf("read:\n%s", out)
		}
		var res struct {
			Rows []struct {
				Object, Property string
				Value            struct {
					Type  string
					Value float64
				}
			}
		}
		decodeJSON(t, want(t, 0, "read", "--device", sim, "ai:1", "present-value", "--format", "json"), &res)
		if len(res.Rows) != 1 || res.Rows[0].Object != "analog-input:1" || res.Rows[0].Property != "present-value" || res.Rows[0].Value.Value != 21.5 {
			t.Errorf("read: %+v", res)
		}
	})

	t.Run("device", func(t *testing.T) {
		if out := want(t, 0, "device", "--device", sim); !strings.Contains(out, "AHU-1 Controller") || !strings.Contains(out, "device:1234") {
			t.Errorf("device:\n%s", out)
		}
		var res struct {
			Object   string
			Name     string
			VendorID *uint32 `json:"vendor_id"`
		}
		decodeJSON(t, want(t, 0, "device", "--device", sim, "--format", "json"), &res)
		if res.Object != "device:1234" || res.Name != "AHU-1 Controller" || res.VendorID == nil || *res.VendorID != 999 {
			t.Errorf("device: %+v", res)
		}
	})

	t.Run("file get", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "config.txt")
		var res struct {
			Direction string
			Path      string
			Length    int
			SHA256    string `json:"sha256"`
		}
		decodeJSON(t, want(t, 0, "file", "get", "--device", sim, "file:1", "--output", out, "--format", "json"), &res)
		data, err := os.ReadFile(out)
		if err != nil || len(data) == 0 || len(data) != res.Length || res.Direction != "download" || res.Path != out {
			t.Fatalf("file get: %+v, %d octets, %v", res, len(data), err)
		}
		if runtime.GOOS != "windows" {
			if fi, err := os.Stat(out); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("output file mode %v, %v; want -rw-------", fi.Mode(), err)
			}
		}
	})

	t.Run("time sync", func(t *testing.T) {
		want(t, 0, "time", "sync", "--device", sim, "--time", "2026-01-31T12:00:00+01:00")
		for _, tm := range []string{"2155-01-01T00:00:00Z", "2154-12-31T23:30:00-01:00", "1899-12-31T23:59:59Z"} {
			stdout, stderr, code := runSplit(t, "time", "sync", "--device", sim, "--time", tm)
			if code != 3 || stdout != "" || !strings.Contains(stderr, "1900-2154") {
				t.Errorf("--time %s: exit code %d, stdout %q, stderr %q", tm, code, stdout, stderr)
			}
			var env struct {
				Error struct{ Kind, Code, Message string }
			}
			stdout, _, code = runSplit(t, "time", "sync", "--device", sim, "--time", tm, "--format", "json")
			decodeJSON(t, stdout, &env)
			if code != 3 || env.Error.Kind != "invalid_input" || env.Error.Code != "invalid_input" {
				t.Errorf("--time %s --format json: exit code %d, %+v", tm, code, env)
			}
		}
	})

	// Every command with --format json writes one JSON document to stdout, and nothing else.
	t.Run("json", func(t *testing.T) {
		for _, args := range [][]string{
			{"objects", "--device", sim},
			{"describe", "--device", sim, "ai:1"},
			{"alarms", "--device", sim},
			{"trend", "--device", sim, "trend-log:1"},
			{"version"},
		} {
			stdout := want(t, 0, append(args, "--format", "json")...)
			var v map[string]any
			decodeJSON(t, stdout, &v)
			if len(v) == 0 {
				t.Errorf("%s: empty JSON object", strings.Join(args, " "))
			}
		}
	})
}
