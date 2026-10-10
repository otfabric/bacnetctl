// SPDX-License-Identifier: MIT

package bac

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/otfabric/bacnetctl/internal/config"
	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/otfabric/bacnetctl/internal/types"
	bacnet "github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// DefaultCOVLifetime is the lifetime of a subscription when --lifetime is 0, in seconds.
const DefaultCOVLifetime = 300

// Sources of a COV record.
const (
	SourceCOV  = "cov"
	SourcePoll = "poll"
)

// covBuffer is how many notifications of one subscription may wait to be printed.
const covBuffer = 256

// Now returns the time a record is stamped with: UTC, with milliseconds.
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00") }

// MonitorRequest is a parsed `monitor` invocation.
type MonitorRequest struct {
	Objects   []bacnet.ObjectIdentifier
	Property  *bacnet.PropertyReference
	Increment *float32
}

// ParseMonitor parses the arguments and flags of `monitor`.
func ParseMonitor(cfg *config.MonitorConfig, args []string) (MonitorRequest, error) {
	var req MonitorRequest
	if len(args) == 0 {
		return req, fmt.Errorf("give at least one object to monitor, for example analog-input:1")
	}
	var err error
	if req.Objects, err = ParseObjects(args); err != nil {
		return req, err
	}
	if cfg.Property != "" {
		refs, err := ParseProperties(cfg.Property)
		if err != nil {
			return req, err
		}
		if len(refs) != 1 || isSpecial(refs[0].Identifier) {
			return req, fmt.Errorf("--property takes one property, not %q", cfg.Property)
		}
		req.Property = &refs[0]
	}
	if s := strings.TrimSpace(cfg.Increment); s != "" {
		f, err := strconv.ParseFloat(s, 32)
		if err != nil || f < 0 {
			return req, fmt.Errorf("--increment %q: a COV increment is a number that is not negative", cfg.Increment)
		}
		if req.Property == nil || cfg.Poll > 0 {
			return req, fmt.Errorf("--increment goes with --property, and not with --poll")
		}
		inc := float32(f)
		req.Increment = &inc
	}
	if cfg.Poll > 0 && cfg.Confirmed {
		return req, fmt.Errorf("--confirmed has no meaning with --poll")
	}
	return req, nil
}

// CheckMonitor validates the configuration of `monitor`.
func CheckMonitor(cfg *config.MonitorConfig, args []string) error {
	if err := CheckConn(&cfg.ConnConfig); err != nil {
		return err
	}
	_, err := ParseMonitor(cfg, args)
	return err
}

func covRecord(n *service.COVNotification, gap bool) *types.COVRecord {
	remaining := n.TimeRemaining
	rec := &types.COVRecord{
		Timestamp: Now(), Source: SourceCOV, Device: ObjectName(n.InitiatingDevice), ObjectRef: objRef(n.MonitoredObject),
		TimeRemaining: &remaining, Values: make([]types.PropertyValue, 0, len(n.Values)), Gap: gap,
	}
	for _, v := range n.Values {
		rec.Values = append(rec.Values, types.PropertyValue{
			PropertyRef: propRef(v.Property), Value: ValueOf(n.MonitoredObject.Type, v.Property.Identifier, v.Value),
		})
	}
	return rec
}

// stopTimer returns a channel that fires after seconds, or never for 0.
func stopTimer(seconds uint32) (<-chan time.Time, func()) {
	if seconds == 0 {
		return nil, func() {}
	}
	t := time.NewTimer(time.Duration(seconds) * time.Second)
	return t.C, func() { t.Stop() }
}

// Monitor subscribes to the changes of value of objects and emits every notification,
// until ctx ends, --duration has passed or --count notifications were emitted. With
// --poll it reads the property at an interval instead. The subscriptions are cancelled on
// the device before it returns.
func Monitor(ctx context.Context, cfg *config.MonitorConfig, args []string, logw io.Writer, emit func(*types.COVRecord) error) (summary types.MonitorSummary, err error) {
	summary.What = "notification"
	req, _ := ParseMonitor(cfg, args)
	s, err := Open(ctx, &cfg.SocketConfig, logw)
	if err != nil {
		return summary, err
	}
	defer func() { _ = s.Close() }()
	target, err := s.Target(ctx, &cfg.ConnConfig)
	if err != nil {
		return summary, err
	}
	for i, id := range req.Objects {
		if req.Objects[i], err = s.ResolveObject(ctx, target, id); err != nil {
			return summary, err
		}
	}
	summary.Target = TargetText(target)
	start := time.Now()
	defer func() { summary.Duration = ms(time.Since(start)) }()
	if cfg.Poll > 0 {
		summary.What = "change"
		return summary, s.poll(ctx, cfg, req, target, &summary, emit)
	}

	opts := client.COVOptions{Lifetime: cfg.Lifetime, IssueConfirmed: cfg.Confirmed, BufferSize: covBuffer}
	if opts.Lifetime == 0 {
		opts.Lifetime = DefaultCOVLifetime
	}
	var subs []client.Subscription
	events := make(chan client.SubscriptionEvent)
	done := make(chan struct{})
	var wg sync.WaitGroup
	// However monitoring ends: release the readers, then cancel the subscriptions, which
	// tells the device to stop sending and ends the event stream of each.
	defer func() {
		close(done)
		for i, sub := range subs {
			if err := sub.Close(); err != nil && logw != nil {
				_, _ = fmt.Fprintf(logw, "warning: the subscription to %s could not be cancelled (%v); it ends with its lifetime of %d s\n",
					ObjectName(req.Objects[i]), WrapError(err), opts.Lifetime)
			}
		}
		wg.Wait()
	}()
	for _, id := range req.Objects {
		var sub client.Subscription
		if req.Property != nil {
			sub, err = s.SubscribeCOVProperty(ctx, target, id, *req.Property, opts, req.Increment)
		} else {
			sub, err = s.SubscribeCOV(ctx, target, id, opts)
		}
		if err != nil {
			return summary, WrapError(err)
		}
		subs = append(subs, sub)
		summary.Subscriptions++
		if logw != nil {
			_, _ = fmt.Fprintf(logw, "subscribed to %s at %s (lifetime %d s)\n", ObjectName(id), summary.Target, opts.Lifetime)
		}
		wg.Add(1)
		go func(id bacnet.ObjectIdentifier) {
			defer wg.Done()
			for ev := range sub.Events() {
				if ev.Notification == nil && ev.Err == nil {
					continue
				}
				if ev.Notification == nil && logw != nil {
					_, _ = fmt.Fprintf(logw, "warning: %s: renewing the subscription failed: %v\n", ObjectName(id), WrapError(ev.Err))
					continue
				}
				select {
				case events <- ev:
				case <-done:
					return
				}
			}
		}(id)
	}

	stop, cancel := stopTimer(cfg.Duration)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return summary, nil
		case <-stop:
			return summary, nil
		case ev := <-events:
			if ev.Notification == nil {
				continue
			}
			if err := emit(covRecord(ev.Notification, ev.Gap)); err != nil {
				return summary, errs.Output(errs.CodeOutputWriteFailed, err)
			}
			summary.Received++
			if cfg.Count > 0 && uint32(summary.Received) >= cfg.Count {
				return summary, nil
			}
		}
	}
}

// poll reads the property of every object at an interval and emits it when it differs
// from what was read before.
func (s *Session) poll(ctx context.Context, cfg *config.MonitorConfig, req MonitorRequest, target client.Target,
	summary *types.MonitorSummary, emit func(*types.COVRecord) error) error {
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	if req.Property != nil {
		ref = *req.Property
	}
	specs := make([]service.ReadAccessSpecification, 0, len(req.Objects))
	for _, id := range req.Objects {
		specs = append(specs, service.ReadAccessSpecification{Object: id, Properties: []bacnet.PropertyReference{ref}})
	}
	last := make([]string, len(specs))
	stop, cancel := stopTimer(cfg.Duration)
	defer cancel()
	tick := time.NewTicker(time.Duration(cfg.Poll) * time.Millisecond)
	defer tick.Stop()
	for {
		results, err := s.ReadMany(ctx, target, specs)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return WrapError(err)
		}
		for i, r := range results {
			if len(r.Properties) == 0 {
				continue
			}
			p := r.Properties[0]
			if p.Err != nil {
				// A property that cannot be read at all is the result of the command.
				return WrapError(p.Err)
			}
			v := ValueOf(r.Object.Type, ref.Identifier, p.Value)
			if key := v.Type + ":" + v.Text; key != last[i] {
				last[i] = key
				rec := &types.COVRecord{Timestamp: Now(), Source: SourcePoll, ObjectRef: objRef(r.Object),
					Values: []types.PropertyValue{{PropertyRef: propRef(ref), Value: v}}}
				if err := emit(rec); err != nil {
					return errs.Output(errs.CodeOutputWriteFailed, err)
				}
				summary.Received++
				if cfg.Count > 0 && uint32(summary.Received) >= cfg.Count {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-stop:
			return nil
		case <-tick.C:
		}
	}
}
