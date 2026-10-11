package agentrun

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// RegisterIntrospect registers keelson('agent_runs') and
// keelson('agent_run_entries') over store (ADR-0308 §SD10): the same rows
// the service serves, for keelson readers granted them. A nil store
// registers empty tables. Both tables are bounded to the DefaultListLimit
// newest runs: a live snapshot of every body of every run would grow
// without bound. The entries of those runs are read in one pass where the
// store can (EntriesOfI), else run by run.
func RegisterIntrospect(reg *introspect.Registry, store StoreI) (err error) {
	if err = reg.Register(runsProvider{store: store}); err != nil {
		return
	}
	return reg.Register(entriesProvider{store: store})
}

type runsProvider struct{ store StoreI }

func (runsProvider) Name() string                         { return TableRuns }
func (runsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (runsProvider) Schema() *arrow.Schema                { return runsTable(nil).Schema() }

func (inst runsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var runs []Run
	if inst.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		if runs, err = inst.store.ListRuns(ctx, "", 0); err != nil {
			return
		}
	}
	rec = runsTable(runs).Build(proj, len(runs))
	return
}

func runsTable(rows []Run) *introspect.Table {
	return introspect.NewTable().
		String("id", func(i int) string { return rows[i].Id }).
		String("cell", func(i int) string { return rows[i].Cell }).
		String("conversation", func(i int) string { return rows[i].Conversation }).
		String("owner_app", func(i int) string { return rows[i].OwnerApp }).
		String("coordinator", func(i int) string { return rows[i].Coordinator }).
		String("state", func(i int) string { return rows[i].State.String() }).
		String("fingerprint", func(i int) string { return rows[i].Fingerprint }).
		String("build", func(i int) string { return rows[i].Build }).
		String("profile", func(i int) string { return rows[i].Profile }).
		String("settings", func(i int) string { return rows[i].Settings }).
		String("label_level", func(i int) string { return rows[i].Label.Level.String() }).
		String("label_tier", func(i int) string { return rows[i].Label.Tier.String() }).
		StringList("label_exposure", func(i int) []string { return rows[i].Label.Exposure }).
		StringList("label_origin", func(i int) []string { return rows[i].Label.Origin }).
		Int64("created_ms", func(i int) int64 { return ms(rows[i].Created) }).
		Uint64("last_ordinal", func(i int) uint64 { return uint64(rows[i].LastOrdinal) }).
		Int64("closed_ms", func(i int) int64 { return ms(rows[i].Closed) }).
		String("digest", func(i int) string { return rows[i].Digest }).
		String("head", func(i int) string { return rows[i].Head }).
		Uint64("version", func(i int) uint64 { return uint64(rows[i].Version) }).
		String("deleted_by", func(i int) string { return rows[i].DeletedBy })
}

type entriesProvider struct{ store StoreI }

func (entriesProvider) Name() string                         { return TableEntries }
func (entriesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (entriesProvider) Schema() *arrow.Schema                { return entriesTable(nil).Schema() }

func (inst entriesProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var entries []Entry
	if inst.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		if entries, err = allEntries(ctx, inst.store); err != nil {
			return
		}
	}
	rec = entriesTable(entries).Build(proj, len(entries))
	return
}

// allEntries reads the entries of the DefaultListLimit newest runs: in
// one pass where the store can, else run by run.
func allEntries(ctx context.Context, store StoreI) (entries []Entry, err error) {
	runs, err := store.ListRuns(ctx, "", 0)
	if err != nil {
		return
	}
	if of, ok := store.(EntriesOfI); ok {
		ids := make([]string, 0, len(runs))
		for _, r := range runs {
			ids = append(ids, r.Id)
		}
		return of.EntriesOf(ctx, ids)
	}
	for _, r := range runs {
		es, eerr := store.Entries(ctx, r.Id, 0)
		if eerr != nil {
			return nil, eerr
		}
		entries = append(entries, es...)
	}
	return
}

func entriesTable(rows []Entry) *introspect.Table {
	return introspect.NewTable().
		String("run", func(i int) string { return rows[i].Run }).
		Uint64("ordinal", func(i int) uint64 { return uint64(rows[i].Ordinal) }).
		Int64("at_ms", func(i int) int64 { return ms(rows[i].At) }).
		String("kind", func(i int) string { return rows[i].Kind.String() }).
		String("key", func(i int) string { return rows[i].Key }).
		String("role", func(i int) string { return rows[i].Role }).
		String("body", func(i int) string { return rows[i].Body }).
		String("calls", func(i int) string { return rows[i].Calls }).
		String("body_digest", func(i int) string { return rows[i].BodyDigest }).
		String("calls_digest", func(i int) string { return rows[i].CallsDigest }).
		String("name", func(i int) string { return rows[i].Name }).
		String("book", func(i int) string { return rows[i].Book }).
		String("form", func(i int) string { return rows[i].Form }).
		Uint64("target", func(i int) uint64 { return uint64(rows[i].Target) }).
		Uint64("from", func(i int) uint64 { return uint64(rows[i].From) }).
		String("effect", func(i int) string { return rows[i].Effect }).
		String("turn", func(i int) string { return rows[i].Turn }).
		String("label_level", func(i int) string { return rows[i].Label.Level.String() }).
		String("label_tier", func(i int) string { return rows[i].Label.Tier.String() }).
		StringList("label_exposure", func(i int) []string { return rows[i].Label.Exposure }).
		StringList("label_origin", func(i int) []string { return rows[i].Label.Origin }).
		String("writer_run", func(i int) string { return rows[i].WriterRun }).
		String("writer_turn", func(i int) string { return rows[i].WriterTurn }).
		Uint64("writer_ordinal", func(i int) uint64 { return uint64(rows[i].WriterOrdinal) }).
		Uint64("prev", func(i int) uint64 { return uint64(rows[i].Prev) }).
		String("digest", func(i int) string { return rows[i].Digest }).
		String("key_id", func(i int) string { return rows[i].KeyId }).
		Uint64("parent", func(i int) uint64 { return uint64(rows[i].Parent) }).
		String("attachments", func(i int) string { return rows[i].Attachments })
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
