package capmapfacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapfacts"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/storage/recordstore/recordstoretest"
)

// captureSink records what an ingest would have written. The encoding is
// testable with no ClickHouse because the sink is one method wide.
type captureSink struct {
	table   string
	records []arrow.RecordBatch
	calls   int
	err     error
}

func (inst *captureSink) InsertArrow(_ context.Context, table string, records []arrow.RecordBatch) (err error) {
	inst.calls++
	inst.table = table
	if inst.err != nil {
		return inst.err
	}
	// Retain: Ingest releases the batches when it returns, and a test that
	// inspects them afterwards would otherwise be reading freed buffers.
	for _, r := range records {
		r.Retain()
		inst.records = append(inst.records, r)
	}
	return nil
}

func (inst *captureSink) release() {
	for _, r := range inst.records {
		r.Release()
	}
	inst.records = nil
}

var fixedNow = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

// sampleCorpus is a two-competence corpus exercising every branch the encoder
// has: a resolved parent, an unresolved link, a citation, and a scored
// similarity.
func sampleCorpus() (corpus capmapcorpus.Corpus) {
	return capmapcorpus.Corpus{
		Competences: []capmapcorpus.Competence{
			{
				Slug: "analytics", NaturalKey: capmapcorpus.NaturalKey("analytics"),
				VaultPath: "analytics/competence.md", Name: "Analytics",
				Domain: "boxer-toolbelt", Catalog: "boxer", Level: 1,
				Maturity: capmapcorpus.NotAssessed, Pain: capmapcorpus.NotAssessed,
				Sections: []capmapcorpus.Section{{Heading: "Vision and Scope", Text: "root prose"}},
			},
			{
				Slug: "robustness", NaturalKey: capmapcorpus.NaturalKey("robustness"),
				VaultPath: "analytics/robustness.md", Name: "Robustness", Abbrev: "Rob",
				Domain: "boxer-toolbelt", Catalog: "boxer", Owner: "Platform Lead", Level: 2,
				Maturity: 3, Pain: 0,
				Tags:     []string{"needs-owner", "workflow/triage"},
				Sections: []capmapcorpus.Section{{Heading: "Standards", Text: "cites [[Jouppi-1990]]"}},
				Lifecycle: []capmapcorpus.LifecycleEvent{
					{Phase: capmapcorpus.PhaseDefined, By: "J. Smith", At: fixedNow},
				},
			},
		},
		Relations: []capmapcorpus.Relation{
			{SourceSlug: "robustness", Target: "analytics", Kind: capmapcorpus.RelationKindParent, Resolution: capmapcorpus.ResolutionDirect},
			{SourceSlug: "robustness", Target: "nowhere", Kind: capmapcorpus.RelationKindWikilink, Section: "Activities", Resolution: capmapcorpus.ResolutionUnresolved},
			{SourceSlug: "robustness", Target: "Jouppi-1990", Kind: capmapcorpus.RelationKindWikilink, Section: "Standards", Resolution: capmapcorpus.ResolutionExternal},
			{SourceSlug: "robustness", Target: "analytics", Kind: capmapcorpus.RelationKindSimilar, Ncd: 0.25, Resolution: capmapcorpus.ResolutionDirect},
		},
	}
}

func totalRows(records []arrow.RecordBatch) (n int64) {
	for _, r := range records {
		n += r.NumRows()
	}
	return n
}

func TestBuildRecordsEncodesEveryRow(t *testing.T) {
	corpus := sampleCorpus()
	records, stats, err := capmapfacts.BuildRecords(context.Background(), corpus, fixedNow, capmapfacts.Origin{Run: "run-capmap"}, testIds())
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()

	assert.Equal(t, 2, stats.Competences)
	assert.Equal(t, 4, stats.Relations)
	assert.Equal(t, 6, stats.Rows)
	assert.Equal(t, int64(6), totalRows(records), "one facts row per competence and per relation")

	// The schema is the facts schema, not something this package invents.
	require.NotEmpty(t, records)
	assert.Contains(t, records[0].Schema().String(), "tv:symbol:value:val:s",
		"rows must be shaped by the generated boxer.facts schema")
}

func TestBuildRecordsEmptyCorpusWritesNothing(t *testing.T) {
	records, stats, err := capmapfacts.BuildRecords(context.Background(), capmapcorpus.Corpus{}, fixedNow, capmapfacts.Origin{Run: "run-capmap"}, testIds())
	require.NoError(t, err)
	assert.Empty(t, records)
	assert.Zero(t, stats.Rows)
}

// testIds is a keyed generator as a load is given one: the same natural key
// resolves to the same id for the life of the value.
func testIds() identifier.IdGeneratorI {
	return recordstoretest.KeyedStream(recordstoretest.FactsRowTag)
}

// idsOf reads the id column of the encoded rows, in row order.
func idsOf(t *testing.T, records []arrow.RecordBatch) (ids []uint64) {
	t.Helper()
	for _, r := range records {
		idx := r.Schema().FieldIndices("id:id:u64:47::0:")
		require.Len(t, idx, 1)
		ids = append(ids, r.Column(idx[0]).(*array.Uint64).Uint64Values()...)
	}
	return
}

// A row's id is what the generator resolves its natural key to: ids of the
// facts table's stream, one per entity, the same on a second ingest through
// the same generator — which is what makes a re-ingest restate the corpus
// rather than add a second set wearing new ids (ADR-0405 §SD2).
func TestIdsAreResolvedFromTheNaturalKey(t *testing.T) {
	ids := testIds()
	build := func(now time.Time) []uint64 {
		records, _, err := capmapfacts.BuildRecords(context.Background(), sampleCorpus(), now, capmapfacts.Origin{Run: "run-capmap"}, ids)
		require.NoError(t, err)
		defer func() {
			for _, r := range records {
				r.Release()
			}
		}()
		return idsOf(t, records)
	}
	first := build(fixedNow)
	require.Len(t, first, 6)
	seen := map[uint64]struct{}{}
	for _, id := range first {
		assert.Equal(t, recordstoretest.FactsRowTag, identifier.TaggedId(id).GetTag().GetValue())
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, 6, "an id per entity")
	assert.Equal(t, first, build(fixedNow.Add(time.Hour)), "the second ingest restates the same ids")

	// The competence's id is the one its natural key resolves to.
	want, fresh, err := ids.GetId(context.Background(), capmapcorpus.NaturalKey("analytics"))
	require.NoError(t, err)
	assert.False(t, fresh)
	assert.Equal(t, want.Value(), first[0])
	assert.NotEqual(t, capmapfacts.LegacyId(capmapcorpus.NaturalKey("analytics")), first[0], "not the truncated key rows carried before")

	_, _, err = capmapfacts.BuildRecords(context.Background(), sampleCorpus(), fixedNow, capmapfacts.Origin{Run: "run-capmap"}, nil)
	require.Error(t, err, "without a generator there is no id to give a row")
}

// Encoding must be a function of the corpus, the timestamp and the ids its
// natural keys resolve to: a second
// build of the same input has to be byte-identical, or a re-ingest would
// double the corpus instead of replacing it.
func TestBuildRecordsIsDeterministic(t *testing.T) {
	ids := testIds()
	first, _, err := capmapfacts.BuildRecords(context.Background(), sampleCorpus(), fixedNow, capmapfacts.Origin{Run: "run-capmap"}, ids)
	require.NoError(t, err)
	defer func() {
		for _, r := range first {
			r.Release()
		}
	}()
	second, _, err := capmapfacts.BuildRecords(context.Background(), sampleCorpus(), fixedNow, capmapfacts.Origin{Run: "run-capmap"}, ids)
	require.NoError(t, err)
	defer func() {
		for _, r := range second {
			r.Release()
		}
	}()

	require.Equal(t, len(first), len(second))
	for i := range first {
		assert.Truef(t, array.RecordEqual(first[i], second[i]),
			"batch %d differs between two builds of the same corpus", i)
	}
}

func TestIngestWritesToTheSinkOnce(t *testing.T) {
	sink := &captureSink{}
	defer sink.release()
	stats, err := capmapfacts.Ingest(context.Background(), sampleCorpus(), sink, "", fixedNow, capmapfacts.Origin{Run: "run-capmap"}, testIds())
	require.NoError(t, err)
	assert.Equal(t, 6, stats.Rows)
	assert.Equal(t, 1, sink.calls, "the whole corpus lands in one insert, not one per row")
	assert.Equal(t, capmapfacts.QualifiedTable, sink.table)
	assert.Equal(t, int64(6), totalRows(sink.records))
}

func TestIngestEmptyCorpusDoesNotCallTheSink(t *testing.T) {
	sink := &captureSink{}
	defer sink.release()
	stats, err := capmapfacts.Ingest(context.Background(), capmapcorpus.Corpus{}, sink, "", fixedNow, capmapfacts.Origin{Run: "run-capmap"}, testIds())
	require.NoError(t, err)
	assert.Zero(t, stats.Rows)
	assert.Zero(t, sink.calls, "an empty corpus must not issue an empty insert")
}

func TestIngestRefusesNilSink(t *testing.T) {
	_, err := capmapfacts.Ingest(context.Background(), sampleCorpus(), nil, "", fixedNow, capmapfacts.Origin{Run: "run-capmap"}, testIds())
	require.Error(t, err)
}

// A failing insert must surface with enough context to act on, and must not
// leak the Arrow buffers it was holding.
func TestIngestSurfacesSinkErrors(t *testing.T) {
	sink := &captureSink{err: assert.AnError}
	_, err := capmapfacts.Ingest(context.Background(), sampleCorpus(), sink, "custom.table", fixedNow, capmapfacts.Origin{Run: "run-capmap"}, testIds())
	require.Error(t, err)
	assert.Equal(t, "custom.table", ebtest.Fields(t, err)["table"], "the error names the table it tried to write")
}
