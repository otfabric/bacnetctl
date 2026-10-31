// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRunServesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out safeBuffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, []string{"-listen", "127.0.0.1:0", "-router=false"}, &out) }()
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(out.String(), "listening on 127.0.0.1:"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not listening: %q", out.String())
		}
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}
}

func TestRunWithRouter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := run(ctx, []string{"-listen", "127.0.0.1:0"}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestMainPassesTheExitCode(t *testing.T) {
	oldArgs, oldExit, oldStderr := os.Args, exit, os.Stderr
	defer func() { os.Args, exit, os.Stderr = oldArgs, oldExit, oldStderr }()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devnull.Close() }()
	code := -1
	exit = func(c int) { code = c }
	os.Args, os.Stderr = []string{"bacsim", "-no-such-flag"}, devnull
	main()
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestRunRefusesBadInvocations(t *testing.T) {
	if code := run(context.Background(), []string{"-no-such-flag"}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("bad flag: exit %d", code)
	}
	var out bytes.Buffer
	if code := run(context.Background(), []string{"-listen", "not an address"}, &out); code != 1 || !strings.Contains(out.String(), "bacsim:") {
		t.Fatalf("bad address: exit %d, %q", code, out.String())
	}
}
