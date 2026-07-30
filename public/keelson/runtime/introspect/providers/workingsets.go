// workingsets — the stored app workingsets (ADR-0148 §SD7, the follow-up
// that §SD7 records; ADR-0094 for the provider pattern). keelson('apps')
// answers what this process declares and keelson('windows') what it
// currently holds; neither answers what is *stored* — the records a plain
// open would restore from. Reading those otherwise means raw boxer.facts
// SQL plus knowledge of the membership encoding.

package providers

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// RegisterWorkingsets registers a workingsets provider reading facts into r
// (ADR-0148 §SD7). It takes the FactsStoreI interface, never chstore, so this
// package stays importable from headless contexts; a host that has no facts
// store passes nil and gets an empty table rather than an absent one — the
// keelson('windows') precedent. Like RegisterTopology, it registers through
// its own function because it needs a live object the static set cannot see.
func RegisterWorkingsets(r *introspect.Registry, facts factsstore.FactsStoreI) error {
	return r.Register(workingsetsProvider{facts: facts})
}

// workingsetsProvider exposes the stored workingset records as
// keelson.workingsets: one row per stored record — the set a restore would
// find, not the write trail. The trail stays a boxer.facts query, since
// history-as-rows is ADR-0148's stance rather than a table.
//
// Live: the store is read per query. With ClickHouse down the runtime runs on
// the in-memory facts store, and the table then shows this process's own
// saves only — ADR-0148's documented degradation, not a bug in this provider.
type workingsetsProvider struct{ facts factsstore.FactsStoreI }

func (workingsetsProvider) Name() string                         { return "workingsets" }
func (workingsetsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (workingsetsProvider) Schema() *arrow.Schema                { return workingsetsTable(nil).Schema() }

// Snapshot runs on the HTTP handler goroutine, so a ClickHouse round-trip per
// query is affordable here — unlike the restore lookup, which sits on the
// window opener's thread.
func (p workingsetsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []factsstore.WorkingsetRow
	if p.facts != nil {
		var err error
		rows, err = p.facts.ListWorkingsets()
		if err != nil {
			return nil, err
		}
	}
	return workingsetsTable(rows).Build(proj, len(rows)), nil
}

// workingsetsTable declares the row shape. The config bytes are deliberately
// not a column: they are facts-CBOR decodable only by the owning app's codec,
// and up to 64 KiB each — so the table reports their size and their kind, the
// keelson('windows').config_bytes precedent.
func workingsetsTable(rows []factsstore.WorkingsetRow) *introspect.Table {
	return introspect.NewTable().
		// Identity is (app_id, name) — the durable app id plus a
		// caller-chosen name (ADR-0148 §SD3). v1 writes exactly one name,
		// "default", so today there is at most one row per participating app.
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		String("name", func(i int) string { return rows[i].Name }).
		// The record IS an instance of the app's Manifest.LaunchKind DTO
		// (§SD2); kind is stored as its own column because the facts wire
		// carries no kind marker, so a reader that sniffed the bytes would be
		// guessing.
		String("kind", func(i int) string { return rows[i].Kind }).
		Int64("config_bytes", func(i int) int64 { return int64(len(rows[i].Config)) }).
		// Provenance, not identity: which window wrote the winning record and
		// why it closed ("user-close" / "shutdown" / …), plus the run it was
		// written in — which joins to keelson('build').run_id and to the
		// app-lifecycle rows in boxer.facts.
		Uint64("tile_key", func(i int) uint64 { return rows[i].TileKey }).
		String("reason", func(i int) string { return rows[i].Reason }).
		String("run_id", func(i int) string { return rows[i].RunId }).
		// The winning row's write time, RFC3339 as keelson('build').started_at
		// renders it.
		String("saved_at", func(i int) string { return rows[i].Ts.Format(time.RFC3339) })
}
