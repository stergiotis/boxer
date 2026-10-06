package sysmetricsbus

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/planebus"
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
type LatestHolder struct {
	inner *planebus.LatestHolder[sysmsnap.BundleSnapshot]
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
	var codec planebus.CodecI[sysmsnap.BundleSnapshot] = NewCBORCodec()
	if opts.Codec != nil {
		codec = opts.Codec
	}
	inner, err := planebus.StartLatestHolder(planebus.LatestHolderOptions[sysmsnap.BundleSnapshot]{
		Bus:     opts.Bus,
		Subject: BundleSubjectWildcard(),
		Codec:   codec,
		Key:     ParseBundleSubjectHost,
		NowFunc: opts.NowFunc,
		Log:     opts.Log,
	})
	if err != nil {
		err = eh.Errorf("sysmetricsbus: latest holder: %w", err)
		return
	}
	inst = &LatestHolder{inner: inner}
	return
}

// Hosts returns every host's latest snapshot, sorted by host token —
// the stable enumeration the table providers flatten. Empty (not nil
// semantics — just zero rows downstream) until a first bundle arrives.
func (inst *LatestHolder) Hosts() (out []HostSnapshot) {
	entries := inst.inner.Entries()
	out = make([]HostSnapshot, 0, len(entries))
	for _, e := range entries {
		out = append(out, HostSnapshot{Host: e.Key, ReceivedAtUnixMs: e.ReceivedAtUnixMs, Snap: e.Value})
	}
	return
}

// Close unsubscribes. Safe to call more than once.
func (inst *LatestHolder) Close() (err error) {
	err = inst.inner.Close()
	return
}
