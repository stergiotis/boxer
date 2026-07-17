package sysmetricsbus

import (
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// HostSnapshot is one host's latest bundle as held by [LatestHolder].
type HostSnapshot struct {
	// Host is the {host} subject token the bundle arrived under.
	Host string
	// ReceivedAtUnixMs stamps local arrival — staleness is judged against
	// this, not the producer's own clock.
	ReceivedAtUnixMs int64
	// Snap is the decoded bundle. Never nil in a returned HostSnapshot.
	// Shared and immutable-by-convention: consumers must not mutate.
	Snap *sysmsnap.BundleSnapshot
}

// LatestHolder subscribes to every host's whole-bundle subject and keeps
// the most recent snapshot per host — the process-lifetime, host-scoped
// consumer the keelson.procs/keelson.sockets tables read (ADR-0126 §SD5).
// imztop's consumer is mount-gated; this one lives as long as the host.
//
// It subscribes on the bus directly rather than through [Consumer]
// because it needs the message subject (the host token), which the
// Consumer handler signature drops.
type LatestHolder struct {
	log   zerolog.Logger
	codec Codec
	nowFn func() time.Time

	mu     sync.RWMutex
	byHost map[string]HostSnapshot

	unsubscribe func()
}

// LatestHolderOptions configures StartLatestHolder. Bus is required.
type LatestHolderOptions struct {
	// Bus is the subscribing client. It must hold a Sub capability on
	// [SubjectWildcard] (or the bundle wildcard).
	Bus app.BusI
	// Codec decodes bundle payloads; nil means the CBOR codec.
	Codec Codec
	// NowFunc overrides the arrival clock when non-nil.
	NowFunc func() time.Time
	Log     zerolog.Logger
}

// StartLatestHolder subscribes and returns a running holder. Close
// unsubscribes.
func StartLatestHolder(opts LatestHolderOptions) (inst *LatestHolder, err error) {
	if opts.Bus == nil {
		err = eh.Errorf("sysmetricsbus: latest holder needs a Bus")
		return
	}
	if opts.Codec == nil {
		opts.Codec = NewCBORCodec()
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &LatestHolder{
		log:    opts.Log,
		codec:  opts.Codec,
		nowFn:  opts.NowFunc,
		byHost: map[string]HostSnapshot{},
	}
	unsub, serr := opts.Bus.Subscribe(BundleSubjectWildcard(), inst.onMsg)
	if serr != nil {
		inst = nil
		err = eh.Errorf("sysmetricsbus: latest holder subscribe: %w", serr)
		return
	}
	inst.unsubscribe = unsub
	return
}

// onMsg decodes one bundle and replaces its host's entry. A decode
// failure or an off-shape subject is logged and dropped — one corrupt
// frame must not tear down the stream (the Consumer rule).
func (inst *LatestHolder) onMsg(msg *app.Msg) {
	host, ok := ParseBundleSubjectHost(msg.Subject)
	if !ok {
		inst.log.Warn().Str("subject", msg.Subject).Msg("sysmetricsbus: latest holder: unexpected subject shape")
		return
	}
	snap, derr := inst.codec.Decode(msg.Payload)
	if derr != nil {
		inst.log.Warn().Err(derr).Str("subject", msg.Subject).Msg("sysmetricsbus: latest holder: decode error")
		return
	}
	entry := HostSnapshot{Host: host, ReceivedAtUnixMs: inst.nowFn().UnixMilli(), Snap: snap}
	inst.mu.Lock()
	inst.byHost[host] = entry
	inst.mu.Unlock()
}

// Hosts returns every host's latest snapshot, sorted by host token —
// the stable enumeration the table providers flatten. Empty (not nil
// semantics — just zero rows downstream) until a first bundle arrives.
func (inst *LatestHolder) Hosts() (out []HostSnapshot) {
	inst.mu.RLock()
	out = make([]HostSnapshot, 0, len(inst.byHost))
	for _, hs := range inst.byHost {
		out = append(out, hs)
	}
	inst.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return
}

// Close unsubscribes. Safe to call more than once.
func (inst *LatestHolder) Close() (err error) {
	if inst.unsubscribe != nil {
		inst.unsubscribe()
		inst.unsubscribe = nil
	}
	return
}
