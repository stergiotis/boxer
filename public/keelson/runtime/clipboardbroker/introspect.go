package clipboardbroker

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableCopies is keelson('clipboard_copies'): the broker's verdicts, one
// row per copy it kept a record of (ADR-0317, update of 2026-10-13). The
// ring is bounded, oldest dropped first.
const TableCopies = "clipboard_copies"

// RecordsI is the broker as a read side.
type RecordsI interface {
	Records() (records []CopyRecord)
}

// RegisterIntrospect exposes the broker's records as
// keelson('clipboard_copies'). A nil broker registers an empty table, so
// the table name does not depend on whether the host started one.
func RegisterIntrospect(reg *introspect.Registry, recs RecordsI) (err error) {
	return reg.Register(copiesProvider{recs: recs})
}

type copiesProvider struct{ recs RecordsI }

func (copiesProvider) Name() string                         { return TableCopies }
func (copiesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (copiesProvider) Schema() *arrow.Schema                { return copiesTable(nil).Schema() }
func (inst copiesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []CopyRecord
	if inst.recs != nil {
		rows = inst.recs.Records()
	}
	return copiesTable(rows).Build(proj, len(rows)), nil
}

// copiesTable leaves the handle id out: it is the key that resolves to the
// text, and a reader of the table holds no right to it.
func copiesTable(rows []CopyRecord) *introspect.Table {
	r := func(i int) *CopyRecord { return &rows[i] }
	return introspect.NewTable().
		Int64("at_unix_ms", func(i int) int64 { return r(i).At.UnixMilli() }).
		String("from", func(i int) string { return string(r(i).From) }).
		// refused, handle or plaintext.
		String("verdict", func(i int) string { return r(i).Verdict() }).
		String("digest", func(i int) string { return r(i).Digest }).
		Int32("level", func(i int) int32 { return int32(r(i).Level) }).
		Int32("tier", func(i int) int32 { return int32(r(i).Tier) }).
		String("source", func(i int) string { return r(i).Source }).
		// The policy only recorded an exit above the free tier.
		Bool("recorded", func(i int) bool { return r(i).Recorded }).
		String("approval", func(i int) string { return r(i).Approval }).
		Bool("raw", func(i int) bool { return r(i).Raw }).
		String("reason", func(i int) string { return r(i).Reason }).
		Int64("bytes", func(i int) int64 { return int64(r(i).Bytes) })
}
