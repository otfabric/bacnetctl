// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The built binary serves a device, its client commands read and write it, and SIGINT
// (SIGTERM too) ends the server with exit code 0 and a summary on stderr.
func TestServerRunServesAndStopsOnSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are POSIX")
	}
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			bin := os.Getenv("BACNETCTL_E2E_BIN")
			if bin == "" {
				t.Fatal("BACNETCTL_E2E_BIN is not set; run make e2e")
			}
			cmd := exec.Command(bin, "server", "run", "--listen", "127.0.0.1", "--port", "0", "--format", "json")
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			lines := make(chan map[string]any, 16)
			go func() {
				sc := bufio.NewScanner(stdout)
				for sc.Scan() {
					var m map[string]any
					if json.Unmarshal(sc.Bytes(), &m) == nil {
						lines <- m
					}
				}
				close(lines)
			}()
			next := func() map[string]any {
				t.Helper()
				select {
				case m, ok := <-lines:
					if !ok {
						t.Fatalf("stdout ended\n%s", stderr.String())
					}
					return m
				case <-time.After(30 * time.Second):
					t.Fatal("no record from the server")
				}
				return nil
			}
			listening := next()
			addr, _ := listening["address"].(string)
			if listening["event"] != "listening" || !strings.HasPrefix(addr, "127.0.0.1:") {
				t.Fatalf("first record: %v", listening)
			}

			if out := want(t, 0, "read", "--device", addr, "av:1", "pv"); strings.TrimSpace(out) != "21" {
				t.Errorf("read: %q", out)
			}
			want(t, 0, "write", "--device", addr, "bv:1", "pv", "active")
			if rec := next(); rec["event"] != "write" || rec["object"] != "binary-value:1" {
				t.Errorf("write record: %v", rec)
			}

			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				for range lines {
				}
				done <- cmd.Wait()
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("server run after %s: %v\n%s", sig, err, stderr.String())
				}
			case <-time.After(30 * time.Second):
				t.Fatalf("server run did not stop on %s", sig)
			}
			if !strings.Contains(stderr.String(), "device:1234 on "+addr+": served for ") {
				t.Errorf("no summary on stderr:\n%s", stderr.String())
			}
		})
	}
}

// A device file that cannot be served is invalid input: exit code 3.
func TestServerRunInvalidDeviceFile(t *testing.T) {
	path := t.TempDir() + "/device.json"
	if err := os.WriteFile(path, []byte(`{"device_instance": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runSplit(t, "server", "run", "--config", path, "--port", "0", "--format", "json")
	if code != 3 || !strings.Contains(stdout, `"code": "invalid_config"`) {
		t.Fatalf("exit code %d, stdout %s", code, stdout)
	}
}
