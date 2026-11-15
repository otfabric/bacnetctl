// SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/otfabric/bacnetctl/internal/bactest"
)

// --max-size holds for every value it takes, up to 2^32-1, on 32-bit platforms as well:
// CI runs this test on linux/386 too, where int has 32 bits. The file of the simulated
// device is 98 octets long.
func TestFileGetMaxSizeBoundaries(t *testing.T) {
	dev := bactest.Start(t)
	content := dev.FileContent(1)
	if len(content) != 98 {
		t.Fatalf("the simulated file has %d octets, want 98", len(content))
	}
	out := filepath.Join(t.TempDir(), "config.txt")
	for _, c := range []struct {
		maxSize string
		code    int
	}{
		{"0", 3},
		{"1", 3},
		{"97", 3},
		{"98", 0},
		{"99", 0},
		{"16777216", 0},
		{"2147483647", 0},
		{"2147483648", 0},
		{"4294967294", 0},
		{"4294967295", 0},
		{"4294967296", 2},
		{"-1", 2},
	} {
		_ = os.Remove(out)
		r := on(t, dev, "file", "get", "file:1", "--output", out, "--max-size", c.maxSize)
		if r.code != c.code {
			t.Errorf("--max-size %s: exit code %d, want %d\nstdout:\n%s\nstderr:\n%s", c.maxSize, r.code, c.code, r.stdout, r.stderr)
			continue
		}
		got, err := os.ReadFile(out)
		switch {
		case c.code == 0 && (err != nil || string(got) != string(content)):
			t.Errorf("--max-size %s: wrote %d octets, %v", c.maxSize, len(got), err)
		case c.code != 0 && err == nil:
			t.Errorf("--max-size %s: refused, and wrote %d octets", c.maxSize, len(got))
		}
		if n, _ := strconv.Atoi(c.maxSize); c.code == 3 && n > 0 {
			r.errHas(t, "the file is larger than --max-size ("+c.maxSize+" octets): nothing was written")
		}
	}

	// The environment takes the same values.
	t.Setenv("BACNETCTL_MAX_SIZE", "4294967295")
	on(t, dev, "file", "get", "file:1", "--output", "-").want(t, 0).has(t, string(content))
	t.Setenv("BACNETCTL_MAX_SIZE", "97")
	on(t, dev, "file", "get", "file:1", "--output", "-").want(t, 3).silent(t)
}
