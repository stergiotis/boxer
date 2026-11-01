package trailviewer

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// RecorderI is the trail's recorder as the viewer uses it: whether its
// rows land, a run's seal verification, and the write of a review mark
// (ADR-0350 §SD7), as attention's auditor writes its events.
type RecorderI interface {
	VerifySeals(ctx context.Context, run string) (broken evidence.SealBreak, unnamed []uint64, unanchored []uint64, err error)
	// NotDurable says why the trail's rows do not land, "" when they do.
	NotDurable() (why string)
	// Event buffers one audit event, stamped with the call identity on ctx.
	Event(ctx context.Context, at time.Time, c trail.Context, row trail.AuditEvent) (err error)
	// OriginOf is the origin of an app's window in this run.
	OriginOf(appId app.AppIdT, instance uint64) (o trail.Origin)
	// FlushSoon wakes the background flusher.
	FlushSoon()
}

var _ RecorderI = (*trail.Recorder)(nil)

// tableEvidenceChecks is keelson('evidence_checks'), the relation the
// current run's verdict line is read from (ADR-0327 §SD7). Spelled here:
// its provider lives in introspecthost, which the viewer does not import.
const tableEvidenceChecks = "evidence_checks"

// Source is what the host hands the viewer: the executor the marts are
// read through, the recorder whose seals verify a run, the current run,
// and the introspection registry the evidence relation is served from. A
// host without ClickHouse leaves Exec nil and the viewer says why, from
// the recorder's NotDurable or NotPersisted.
type Source struct {
	// Exec reads the marts; nil when the trail is not persisted.
	Exec recordstore.ExecutorI
	// Trail verifies a run's seals; nil leaves every run unverified.
	Trail RecorderI
	// Run returns this process's run; nil or "" when not known.
	Run func() string
	// Registry serves keelson('evidence_checks') for the current run's
	// verdict; nil, or a registry without the relation, verifies the
	// current run through Trail like any other.
	Registry *introspect.Registry
	// Database is where the views live; empty is factsschema.DatabaseName.
	Database string
	// NotPersisted is why Exec is nil when Trail cannot say.
	NotPersisted string
}

func (inst *Source) persisted() bool { return inst != nil && inst.Exec != nil }

func (inst *Source) run() (runId string) {
	if inst != nil && inst.Run != nil {
		runId = inst.Run()
	}
	return
}

func (inst *Source) database() (db string) {
	db = factsschema.DatabaseName
	if inst != nil && inst.Database != "" {
		db = inst.Database
	}
	return
}

// evidenceChecks reads the trail's rows of keelson('evidence_checks') in
// process; ok is false when no registry serves the relation.
func (inst *Source) evidenceChecks() (checks []verdict.Check, ok bool, err error) {
	if inst == nil || inst.Registry == nil {
		return
	}
	p, found := inst.Registry.Lookup(tableEvidenceChecks)
	if !found {
		return
	}
	rec, err := p.Snapshot(introspect.Columns("store", "scope", "law", "holds", "broken", "why"))
	if err != nil {
		return nil, true, err
	}
	defer rec.Release()
	checks, err = chrows.DecodeRows[verdict.Check](rec)
	return checks, true, err
}

// sourceRef holds the host-registered source. The host writes it once
// during start-up; every frame and every read loads it.
var sourceRef atomic.Pointer[Source]

// Register hands the viewer its source. Call it once the trail and the
// introspection registry exist, before the first frame of any viewer
// window; a later call replaces it for every window, and nil leaves the
// viewer with none, which it reports as not persisted.
func Register(s *Source) {
	sourceRef.Store(s)
}

// notPersistedReason is what a window and a refused operation say when no
// executor reads the marts.
func notPersistedReason(src *Source) (reason string) {
	why := "the host did not register a trail source"
	switch {
	case src != nil && src.Trail != nil && src.Trail.NotDurable() != "":
		why = src.Trail.NotDurable()
	case src != nil && src.NotPersisted != "":
		why = src.NotPersisted
	}
	reason = "the trail is not persisted on this host (" + why + "), so there is nothing to read"
	return
}
