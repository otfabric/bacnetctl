// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/bactest"
	bacnet "github.com/otfabric/go-bacnet"
)

// syncBuffer is an output buffer that is safe for concurrent writers: streaming commands
// write while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// result is the outcome of one invocation of bacnetctl.
type result struct {
	stdout, stderr string
	code           int
}

// run executes bacnetctl with args, as the process would.
func run(t testing.TB, args ...string) result {
	t.Helper()
	return runCtx(t, context.Background(), args...)
}

func runCtx(t testing.TB, ctx context.Context, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var stdout, stderr syncBuffer
	code := Run(ctx, args, &stdout, &stderr)
	return result{stdout.String(), stderr.String(), code}
}

// start runs bacnetctl in the background and returns a function that waits for it.
func start(t testing.TB, args ...string) func() result {
	t.Helper()
	done := make(chan result, 1)
	go func() { done <- run(t, args...) }()
	return func() result {
		t.Helper()
		select {
		case r := <-done:
			return r
		case <-time.After(60 * time.Second):
			t.Fatalf("bacnetctl %s did not end", strings.Join(args, " "))
			return result{}
		}
	}
}

// want fails the test unless the exit code is code.
func (r result) want(t testing.TB, code int) result {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, code, r.stdout, r.stderr)
	}
	return r
}

// has fails the test unless stdout contains every one of subs.
func (r result) has(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.stdout, s) {
			t.Errorf("stdout does not contain %q:\n%s", s, r.stdout)
		}
	}
	return r
}

// lacks fails the test when stdout or stderr contains one of subs.
func (r result) lacks(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(r.stdout, s) || strings.Contains(r.stderr, s) {
			t.Errorf("output contains %q:\nstdout:\n%s\nstderr:\n%s", s, r.stdout, r.stderr)
		}
	}
	return r
}

// errHas fails the test unless stderr contains every one of subs.
func (r result) errHas(t testing.TB, subs ...string) result {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(r.stderr, s) {
			t.Errorf("stderr does not contain %q:\n%s", s, r.stderr)
		}
	}
	return r
}

// silent fails the test unless stdout is empty: nothing but the result goes there.
func (r result) silent(t testing.TB) result {
	t.Helper()
	if r.stdout != "" {
		t.Errorf("stdout is not empty:\n%s", r.stdout)
	}
	return r
}

// decode parses stdout as one JSON document into v. A field v does not have is an error:
// the shapes in docs/json-output.md are a contract.
func (r result) decode(t testing.TB, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("stdout is not the expected JSON: %v\n%s", err, r.stdout)
	}
}

// object parses stdout as one JSON object.
func (r result) object(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, r.stdout)
	}
	return m
}

// lines parses stdout as JSON Lines.
func (r result) lines(t testing.TB) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout line is not JSON: %v\n%s", err, line)
		}
		out = append(out, m)
	}
	return out
}

// jsonError is the error object of a failed command with --format json.
type jsonError struct {
	Kind, Code, Message string
	BACnet              *struct {
		PDU      string
		Class    string
		ClassID  *int `json:"class_id"`
		Code     string
		CodeID   *int `json:"code_id"`
		Reason   string
		ReasonID *int `json:"reason_id"`
	}
}

// failure returns the JSON error object on stdout.
func (r result) failure(t testing.TB) jsonError {
	t.Helper()
	var env struct{ Error jsonError }
	if err := json.Unmarshal([]byte(r.stdout), &env); err != nil || env.Error.Code == "" {
		t.Fatalf("stdout is not a JSON error object: %v\n%s", err, r.stdout)
	}
	if strings.Count(strings.TrimSpace(r.stdout), "\n{") != 0 {
		t.Errorf("stdout holds more than the error object:\n%s", r.stdout)
	}
	return env.Error
}

// fast are flags that make a command give up on a device that does not answer at once.
var fast = []string{"--timeout", "150", "--retries", "0"}

// on runs a device command against dev: `bacnetctl <command…> --device <dev> <rest…>`.
// words is how many leading arguments name the command.
func on(t testing.TB, dev *bactest.Device, args ...string) result {
	t.Helper()
	n := 1
	switch args[0] {
	case "file", "time", "bbmd":
		n = 2
	}
	full := append([]string{}, args[:n]...)
	full = append(full, "--device", dev.Target())
	return run(t, append(full, args[n:]...)...)
}

// freeUDPPort returns a UDP port that is free at the moment it is asked for.
func freeUDPPort(t testing.TB) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, port, _ := net.SplitHostPort(conn.LocalAddr().String())
	return port
}

func oid(typ bacnet.ObjectType, instance uint32) bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: typ, Instance: instance}
}

var (
	ai1 = oid(bacnet.ObjectTypeAnalogInput, 1)
	av1 = oid(bacnet.ObjectTypeAnalogValue, 1)
	bv1 = oid(bacnet.ObjectTypeBinaryValue, 1)
)
