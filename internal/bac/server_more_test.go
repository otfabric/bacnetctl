// SPDX-License-Identifier: MIT

package bac

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
)

// A device file without a port serves on 47808.
func TestParseServerDefaultPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev.json")
	file := `{"device_instance": 1, "device_name": "d", "objects": [{"type": "device", "instance": 1, "object_name": "d"}]}`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	req, err := ParseServer(&config.ServerRunConfig{DeviceFile: path, MaxAPDU: DefaultServerMaxAPDU}, "")
	if err != nil || req.Port != config.DefaultPort {
		t.Fatalf("port %d, %v", req.Port, err)
	}
}

// --bbmd with an --interface that does not exist is refused before anything is bound.
func TestParseServerBBMDInterface(t *testing.T) {
	_, err := ParseServer(&config.ServerRunConfig{MaxAPDU: DefaultServerMaxAPDU, BBMD: true, Interface: "no-such-if0"}, "")
	if err == nil || !strings.Contains(err.Error(), "no such network interface") {
		t.Fatalf("interface: %v", err)
	}
}

// A record that cannot be printed while serving ends the server with that error.
func TestServeOutputFailsAfterListening(t *testing.T) {
	cfg := config.ServerRunConfig{MaxAPDU: DefaultServerMaxAPDU, Listen: "127.0.0.1", Port: "0"}
	listening := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := Serve(t.Context(), &cfg, "", nil, func(r *types.ServerRecord) error {
			if r.Event == types.ServerListening {
				listening <- r.Address
				return nil
			}
			return errors.New("pipe closed")
		})
		done <- err
	}()
	addr := <-listening
	s := &served{}
	s.target.Endpoint = mustEndpoint(t, addr)
	c := testClient(t)
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prio := uint8(8)
	_ = c.WriteProperty(t.Context(), s.target, av, pvRef, bacnet.RealValue(1), &prio)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "pipe closed") {
		t.Fatalf("serve: %v", err)
	}
}

func TestPlural(t *testing.T) {
	if plural(1, "peer", "peers") != "1 peer" || plural(2, "peer", "peers") != "2 peers" {
		t.Fatal(plural(1, "peer", "peers"))
	}
}

func TestDeviceFileMoreErrors(t *testing.T) {
	dev := `{"type": "device", "instance": 1, "object_name": "d"}`
	file := func(o string) string {
		return `{"device_instance": 1, "device_name": "d", "objects": [` + dev + `,` + o + `]}`
	}
	texts := `"` + strings.Repeat(`a","`, 65536) + `a"`
	for name, tc := range map[string]struct{ file, want string }{
		"value units":     {file(`{"type": "integer-value", "instance": 1, "object_name": "x", "units": "furlongs"}`), "units"},
		"too many states": {file(`{"type": "msv", "instance": 1, "object_name": "x", "number_of_states": 2, "state_text": [` + texts + `]}`), "at most 65535"},
	} {
		df, err := ParseDeviceFile([]byte(tc.file))
		if err == nil {
			_, err = df.Build()
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func mustEndpoint(t *testing.T, addr string) bip.Endpoint {
	t.Helper()
	ep, err := netip.ParseAddrPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return bip.NewEndpoint(ep)
}
