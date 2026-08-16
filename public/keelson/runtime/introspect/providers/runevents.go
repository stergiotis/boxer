// runevents — the runtime's own trail as a table (ADR-0191 §SD7).
// keelson('build') says which run this is and keelson('apps') what it can
// open; neither says what actually happened. This one does: one row per
// recorded event of the current run, across both tables the runtime
// persists to.
//
// # Why a provider rather than a SQL applet
//
// The consumer that motivated it — the event-timeline applet — first did
// this extraction in SQL, and it was slow for a reason that had nothing to
// do with ClickHouse. Reading twelve kinds out of `boxer.facts` needs the
// membership vocabulary, which in SQL means a large statement; the
// pre-execute pass pipeline hands a statement between passes as text and so
// re-parses it once per pass — about thirty-four times for one Run, measured.
// At ~7 KB that cost 2.4–3.9 s per Run against a server answering in 90 ms.
// BenchmarkPlayPipeline keeps that buffer as a fixture.
//
// Composed in Go the extraction is written once and compiled once, and the
// applet's buffer becomes a projection over this table. It is the same trade
// keelson('workingsets') records: "reading those otherwise means raw
// boxer.facts SQL plus knowledge of the membership encoding".

package providers

import (
	"context"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// RegisterRunEvents registers the run-event view into r.
//
// facts is the process's facts store, taken as the whole FactsStoreI for
// symmetry with RegisterWorkingsets; the read capability is asserted off it
// (ADR-0191 §SD7), so an in-memory store answers with an empty table rather
// than an absent one. persistExec is the executor the persist store was
// opened over, or nil — the provider opens its own read-only store on it,
// which is why it takes the executor and not the live backend: a scan must
// not contend with the writer's buffer.
func RegisterRunEvents(r *introspect.Registry, facts factsstore.FactsStoreI, persistExec recordstore.ExecutorI) error {
	reader, _ := facts.(factsstore.RunEventReaderI)
	return r.Register(runEventsProvider{reader: reader, persistExec: persistExec})
}

// runEventsProvider exposes the current run's trail as keelson.runtime_events.
//
// Live: both stores are read per query. That is affordable here for the
// reason workingsetsProvider gives — Snapshot runs on the HTTP handler
// goroutine, not on a render thread — and it is what makes the table useful
// while a run is still going.
type runEventsProvider struct {
	reader      factsstore.RunEventReaderI
	persistExec recordstore.ExecutorI
}

func (runEventsProvider) Name() string                         { return "runtime_events" }
func (runEventsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (runEventsProvider) Schema() *arrow.Schema                { return runEventsTable(nil).Schema() }

func (p runEventsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := p.collect()
	sortRunEventsByTime(rows)
	return runEventsTable(rows).Build(proj, len(rows)), nil
}

// sortRunEventsByTime merges the two halves into one chronology, oldest
// first. They are read separately, so without this every persist row would
// follow every facts row whatever their times — a timeline drawn from that
// is wrong in a way a reader would not notice, which is the dangerous kind.
// Stable, so rows sharing a millisecond keep their source's own order.
func sortRunEventsByTime(rows []factsstore.RunEventRow) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Ts.Before(rows[j].Ts) })
}

// collect gathers both halves. Every failure degrades to fewer rows rather
// than to an error: this is a view of what a process did, and a reader
// looking at it during an incident is worse served by an empty table than by
// a partial one. The absence is visible — the trail simply stops.
func (p runEventsProvider) collect() (rows []factsstore.RunEventRow) {
	inst, err := runinfo.Get()
	if err != nil {
		// No runinfo means no run identity, and every row here is scoped to
		// one run. A host that never called runinfo.Init gets an empty table.
		return
	}
	if p.reader != nil {
		got, rerr := p.reader.ListRunEvents(factsstore.RunEventFilter{
			RunId: inst.RunId,
			Since: inst.StartedAt,
		})
		if rerr == nil {
			rows = got
		}
	}
	rows = append(rows, p.persistRows(inst.RunId, inst.StartedAt.UnixMilli())...)
	return
}

// persistRows reads the app-state half. Rows written since ADR-0191 §SD5
// name their run; older ones name none and are placed by timestamp, the same
// rule the facts half applies.
func (p runEventsProvider) persistRows(runId string, sinceMs int64) (rows []factsstore.RunEventRow) {
	if p.persistExec == nil {
		return
	}
	store := persiststore.NewPersistStore(p.persistExec, nil, persiststore.PersistStoreConfig{})
	defer store.Close()
	ctx := context.Background()
	for ent, serr := range store.ScanState(ctx, recordstore.ScanOpts{}) {
		if serr != nil {
			// Partial is better than nothing; the caller sees a short trail.
			return
		}
		if !ent.State.Has {
			continue
		}
		st := ent.State.Val
		if st.RunId != "" {
			if st.RunId != runId {
				continue
			}
		} else if ent.Ts.UnixMilli() < sinceMs {
			continue
		}
		detail := st.Key
		if ent.IsTombstone() {
			detail += " (deleted)"
		}
		rows = append(rows, factsstore.RunEventRow{
			Ts:          ent.Ts,
			Kind:        "persist",
			AppId:       app.AppIdT(st.AppId),
			InstanceKey: st.InstanceKey,
			RunId:       st.RunId,
			Detail:      detail,
			Source:      factsstore.RunEventSourcePersist,
			FactId:      ent.ID,
		})
	}
	return
}

func runEventsTable(rows []factsstore.RunEventRow) *introspect.Table {
	return introspect.NewTable().
		// Milliseconds since the epoch rather than a formatted string: the
		// consumer that motivated this table plots it, and a timeline slot
		// wants a timestamp it can subtract, not one it has to parse.
		Int64("ts_ms", func(i int) int64 { return rows[i].Ts.UnixMilli() }).
		String("kind", func(i int) string { return rows[i].Kind }).
		// Empty for a process-level event (run start, heartbeat) — the
		// runtime spoke, not an app.
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		// Zero is UNATTRIBUTED, not window zero: a row from a service's own
		// client, from a CLI bootstrap, or from before ADR-0191.
		Uint64("instance_key", func(i int) uint64 { return rows[i].InstanceKey }).
		String("run_id", func(i int) string { return rows[i].RunId }).
		String("detail", func(i int) string { return rows[i].Detail }).
		String("source", func(i int) string { return rows[i].Source }).
		String("fact_id", func(i int) string { return rows[i].FactId })
}
