// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"
	"net"
	"testing"

	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// targetAndNeighbour serves an address for --target. Every datagram bacnetctl sends there
// is first answered by another socket, a neighbour on another port, with neighbour; then the
// target answers with own, unless own is nil. It returns the address of the target.
func targetAndNeighbour(t *testing.T, neighbour, own []byte) string {
	t.Helper()
	listen := func() *net.UDPConn {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	target, other := listen(), listen()
	done := make(chan struct{})
	t.Cleanup(func() {
		_ = target.Close()
		_ = other.Close()
		<-done
	})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		for {
			_, from, err := target.ReadFromUDPAddrPort(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			_, _ = other.WriteToUDPAddrPort(neighbour, from)
			if own != nil {
				_, _ = target.WriteToUDPAddrPort(own, from)
			}
		}
	}()
	return target.LocalAddr().String()
}

func iAm(t *testing.T, instance uint32) []byte {
	t.Helper()
	raw, err := service.EncodeIAmAPDU(service.IAm{Device: oid(bacnet.ObjectTypeDevice, instance), MaxAPDULength: 1476, Segmentation: 3, VendorID: 999})
	if err != nil {
		t.Fatal(err)
	}
	return frame(raw...)
}

func iHave(t *testing.T, instance uint32, object bacnet.ObjectIdentifier, name string) []byte {
	t.Helper()
	raw, err := service.EncodeIHaveAPDU(service.IHave{Device: oid(bacnet.ObjectTypeDevice, instance), Object: object, Name: bacnet.CharacterString{Value: name}})
	if err != nil {
		t.Fatal(err)
	}
	return frame(raw...)
}

func iAmRouter(t *testing.T, network uint16) []byte {
	t.Helper()
	raw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, NetworkMessage: true, NetMsgType: npdu.NetMsgIAmRouterToNetwork,
		NetMsgData: []byte{byte(network >> 8), byte(network)}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// With --target, discover, whohas and router list list only the answers that came from the
// target: is the device there? A neighbour's answer that reaches the socket first is not
// the target's. A target that does not answer is the documented "nothing found" (exit 0,
// an empty result).
func TestTargetListsOnlyItsOwnAnswers(t *testing.T) {
	t.Run("discover", func(t *testing.T) {
		silent := targetAndNeighbour(t, iAm(t, 777), nil)
		var res types.DiscoverResult
		run(t, "discover", "--target", silent, "--local-port", "0", "--wait", "1000", "--format", "json").want(t, 0).decode(t, &res)
		if res.Mode != "unicast" || res.Target != silent || res.Count != 0 || len(res.Devices) != 0 {
			t.Errorf("a target that does not answer: %+v", res)
		}
		run(t, "discover", "--target", silent, "--local-port", "0", "--wait", "1000").want(t, 0).has(t, "0 devices found").lacks(t, "777")

		live := targetAndNeighbour(t, iAm(t, 777), iAm(t, 4321))
		res = types.DiscoverResult{}
		run(t, "discover", "--target", live, "--local-port", "0", "--wait", "1000", "--format", "json").want(t, 0).decode(t, &res)
		if res.Count != 1 || len(res.Devices) != 1 || res.Devices[0].Instance != 4321 || res.Devices[0].Address != live {
			t.Errorf("a target that answers after a neighbour: %+v", res)
		}
	})

	t.Run("whohas", func(t *testing.T) {
		silent := targetAndNeighbour(t, iHave(t, 777, ai1, "Room Temp"), nil)
		var res types.WhoHasResult
		run(t, "whohas", "--name", "Room Temp", "--target", silent, "--local-port", "0", "--wait", "1000", "--format", "json").want(t, 0).decode(t, &res)
		if res.Mode != "unicast" || res.Count != 0 || len(res.Objects) != 0 {
			t.Errorf("a target that does not answer: %+v", res)
		}

		live := targetAndNeighbour(t, iHave(t, 777, ai1, "Room Temp"), iHave(t, 4321, av1, "Room Temp"))
		res = types.WhoHasResult{}
		run(t, "whohas", "--name", "Room Temp", "--target", live, "--local-port", "0", "--wait", "1000", "--format", "json").want(t, 0).decode(t, &res)
		if res.Count != 1 || len(res.Objects) != 1 || res.Objects[0].Device != 4321 || res.Objects[0].Object != "analog-value:1" || res.Objects[0].Address != live {
			t.Errorf("a target that answers after a neighbour: %+v", res)
		}
	})

	t.Run("router list", func(t *testing.T) {
		silent := targetAndNeighbour(t, iAmRouter(t, 3003), nil)
		var res types.RoutersResult
		run(t, "router", "list", "--target", silent, "--local-port", "0", "--wait", "1000", "--format", "json").want(t, 0).decode(t, &res)
		if res.Mode != "unicast" || res.Count != 0 || len(res.Routers) != 0 {
			t.Errorf("a target that does not answer: %+v", res)
		}

		live := targetAndNeighbour(t, iAmRouter(t, 3003), iAmRouter(t, 2001))
		res = types.RoutersResult{}
		run(t, "router", "list", "--target", live, "--local-port", "0", "--wait", "1000", "--format", "json").want(t, 0).decode(t, &res)
		if res.Count != 1 || len(res.Routers) != 1 || res.Routers[0].Network != 2001 || res.Routers[0].Router != live {
			t.Errorf("a router that answers after a neighbour: %+v", res)
		}
	})
}
