// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/bacnetctl/internal/bactest"
	"github.com/otfabric/bacnetctl/internal/config"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// A notification lost in the queue of a subscription marks the next event that gets
// through, whatever it is. When that is a state change, which prints nothing, the mark
// goes to the next notification: "gap" is the only sign of the loss that a script sees.
func TestForwardCOVKeepsGap(t *testing.T) {
	note := func(n uint32) *service.COVNotification { return &service.COVNotification{ProcessIdentifier: n} }
	failed := errors.New("no answer")
	in := make(chan client.SubscriptionEvent, 16)
	for _, ev := range []client.SubscriptionEvent{
		{State: client.SubscriptionActive},
		{Notification: note(1)},
		{State: client.SubscriptionRenewing, Gap: true},
		{State: client.SubscriptionActive},
		{Notification: note(2)},
		{Notification: note(3)},
		{State: client.SubscriptionDegraded, Err: failed, Gap: true},
		{Notification: note(4)},
		{Notification: note(5), Gap: true},
		{Notification: note(6)},
	} {
		in <- ev
	}
	close(in)

	out := make(chan client.SubscriptionEvent, 8)
	var warnings []error
	forwardCOV(in, out, make(chan struct{}), func(err error) { warnings = append(warnings, err) })
	close(out)

	want := map[uint32]bool{1: false, 2: true, 3: false, 4: true, 5: true, 6: false}
	got := 0
	for ev := range out {
		got++
		if n := ev.Notification.ProcessIdentifier; ev.Gap != want[n] {
			t.Errorf("notification %d: gap %v, want %v", n, ev.Gap, want[n])
		}
	}
	if got != len(want) {
		t.Errorf("%d notifications, want %d", got, len(want))
	}
	if len(warnings) != 1 || !errors.Is(warnings[0], failed) {
		t.Errorf("warnings: %v", warnings)
	}
}

// A consumer that has stopped is not waited for.
func TestForwardCOVStopsWithDone(t *testing.T) {
	in := make(chan client.SubscriptionEvent, 1)
	in <- client.SubscriptionEvent{Notification: &service.COVNotification{}}
	done := make(chan struct{})
	close(done)
	forwardCOV(in, make(chan client.SubscriptionEvent), done, func(error) {})
}

// The event TestForwardCOVKeepsGap starts from is what the client really does: with the
// notifications dropped and the queue empty again, the next event is a renewal with Gap.
func TestSubscriptionGapLandsOnStateEvent(t *testing.T) {
	dev := bactest.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, &config.SocketConfig{Timeout: 3000, Retries: 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	cfg := config.ConnConfig{Device: dev.Target()}
	target, err := s.Target(ctx, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	object := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	sub, err := s.SubscribeCOV(ctx, target, object, client.COVOptions{Lifetime: 2, BufferSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()

	// The queue holds one event, so these are dropped. The answer to a read comes after
	// them on the same socket: when it is here, they have been handled.
	dev.SetPresentValue(object, bacnet.RealValue(30))
	dev.SetPresentValue(object, bacnet.RealValue(31))
	if _, err := s.ReadProperty(ctx, target, object, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}); err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		select {
		case <-sub.Events():
		default:
			drained = true
		}
	}
	select {
	case ev := <-sub.Events():
		if ev.Notification != nil || !ev.Gap {
			t.Errorf("first event after the loss: %+v, want a state event with Gap", ev)
		}
	case <-ctx.Done():
		t.Fatal("no event after the loss")
	}
}
