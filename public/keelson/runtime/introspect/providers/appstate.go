// appstate — every live entry of durable app state, whatever its kind
// (ADR-0185 §SD1). Persist values, workingsets and column-width overrides
// share one store on `boxer.persiststate` (ADR-0105 D3a and its Update of
// 2026-08-15), and every live row there carries an Owner component, so one
// live scan of Owner is every entry of every kind. The table is the read half
// of the app-state manager: what is stored, for which app, how large, and
// who wrote it — the question keelson('apps') and keelson('windows') do not
// answer, and keelson('workingsets') answers for one kind only.

package providers

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// TableAppState is the table's name, as keelson() resolves it and as a
// manager's `keelson.query.<table>` grant names it (ADR-0253 §SD1).
const TableAppState = "app_state"

// RegisterAppState registers the app_state provider into r. persistExec is
// the executor the state store was opened over, or nil: the provider opens
// its own read-only store on it per query — the keelson('runtime_events')
// path (ADR-0191 §SD7), so a scan never contends with the writer's pending
// buffer — and a host with no durable store gets an empty table rather than
// an absent one.
func RegisterAppState(r *introspect.Registry, persistExec recordstore.ExecutorI) error {
	return r.Register(appStateProvider{persistExec: persistExec})
}

// appStateRow is one live entry, flattened across kinds.
type appStateRow struct {
	kind         string
	appId        string
	key          string
	entityId     string
	payloadBytes int64
	detail       string
	writtenAt    time.Time
	runId        string
	instanceKey  uint64
}

type appStateProvider struct {
	persistExec recordstore.ExecutorI
}

func (appStateProvider) Name() string                         { return TableAppState }
func (appStateProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (appStateProvider) Schema() *arrow.Schema                { return appStateTable(nil).Schema() }

// Snapshot reads the store per query. A failed read is reported rather than
// rendered as an empty table: "nothing is stored" and "the store did not
// answer" are different claims to a user deciding what to clear — the
// keelson('workingsets') stance.
func (p appStateProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows, err := p.collect()
	if err != nil {
		return nil, err
	}
	return appStateTable(rows).Build(proj, len(rows)), nil
}

func (p appStateProvider) collect() (rows []appStateRow, err error) {
	if p.persistExec == nil {
		return
	}
	store := persiststore.NewPersistStore(p.persistExec, nil, persiststore.PersistStoreConfig{})
	defer store.Close()
	for ent, serr := range store.ScanLiveOwner(context.Background(), recordstore.ScanOpts{}) {
		if serr != nil {
			err = eh.Errorf("app_state: scan the state store: %w", serr)
			rows = nil
			return
		}
		rows = append(rows, appStateRowOf(ent))
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.appId != b.appId {
			return a.appId < b.appId
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.key < b.key
	})
	return
}

// appStateRowOf flattens one live entity. The payload is described — its
// size and, for a workingset, the launch kind that decodes it — never
// served (ADR-0185 §SD7): workingset configs decode only under the owning
// app's codec and persist values are bytes an app chose. A column width is
// all metadata, so its detail carries it whole.
//
// The kind and key come from persiststore.KindOf / EntryKeyOf, the same
// naming runtime.appstate.delete takes back, so a row read here addresses
// its entry there. A row of a kind this build does not know is shown as
// `unknown` under its store key — still stored state, so the manager must
// not hide it.
func appStateRowOf(ent *persiststore.PersistEntity) (r appStateRow) {
	r = appStateRow{
		kind:      persiststore.KindOf(ent),
		key:       persiststore.EntryKeyOf(ent),
		entityId:  ent.ID,
		writtenAt: ent.Ts,
	}
	if ent.Owner.Has {
		ow := ent.Owner.Val
		r.appId, r.runId, r.instanceKey = ow.AppId, ow.RunId, ow.InstanceKey
	}
	switch {
	case ent.State.Has:
		r.payloadBytes = int64(len(ent.State.Val.Value))
	case ent.Workingset.Has:
		v := ent.Workingset.Val
		r.payloadBytes = int64(len(v.Config))
		r.detail = joinDetail(v.Kind, v.Reason)
	case ent.ColumnWidth.Has:
		v := ent.ColumnWidth.Val
		r.detail = strconv.FormatFloat(v.Points, 'g', -1, 64) + " pt @ " + strconv.FormatFloat(v.FontSize, 'g', -1, 64)
	}
	return
}

// appStateTable declares the row shape (ADR-0185 §SD1). There is no column
// holding payload bytes, and the shape test pins that: §SD7's cut is what
// lets a cross-app reader exist at all.
func appStateTable(rows []appStateRow) *introspect.Table {
	return introspect.NewTable().
		String("kind", func(i int) string { return rows[i].kind }).
		String("app_id", func(i int) string { return rows[i].appId }).
		// The entry's identity within its app and kind; entity_id is the
		// store's own key, kind-prefixed and escaped.
		String("key", func(i int) string { return rows[i].key }).
		String("entity_id", func(i int) string { return rows[i].entityId }).
		Int64("payload_bytes", func(i int) int64 { return rows[i].payloadBytes }).
		String("detail", func(i int) string { return rows[i].detail }).
		// The live version's write time, RFC3339 as keelson('workingsets')
		// renders its saved_at.
		String("written_at", func(i int) string { return rows[i].writtenAt.Format(time.RFC3339) }).
		// Provenance, not identity (ADR-0191 §SD5): the run joins to
		// keelson('build').run_id, the window key to keelson('windows').
		String("run_id", func(i int) string { return rows[i].runId }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].instanceKey })
}
