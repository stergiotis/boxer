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
	records, stats, err := capmapfacts.BuildRecords(corpus, fixedNow)
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
	records, stats, err := capmapfacts.BuildRecords(capmapcorpus.Corpus{}, fixedNow)
	require.NoError(t, err)
	assert.Empty(t, records)
	assert.Zero(t, stats.Rows)
}

// Ids follow the natural key, so two ingests of an unchanged vault produce the
// same rows rather than a second set wearing fresh ids. This is what lets a
// relation address its endpoints without inserting competences first and
// reading their ids back.
func TestDeriveIdIsStableAndFollowsTheSlug(t *testing.T) {
	a := capmapfacts.DeriveId(capmapcorpus.NaturalKey("analytics"))
	assert.Equal(t, a, capmapfacts.DeriveId(capmapcorpus.NaturalKey("analytics")))
	assert.NotEqual(t, a, capmapfacts.DeriveId(capmapcorpus.NaturalKey("robustness")))
	assert.NotZero(t, a)
}

// Encoding must be a pure function of the corpus and the timestamp: a second
// build of the same input has to be byte-identical, or a re-ingest would
// double the corpus instead of replacing it.
func TestBuildRecordsIsDeterministic(t *testing.T) {
	first, _, err := capmapfacts.BuildRecords(sampleCorpus(), fixedNow)
	require.NoError(t, err)
	defer func() {
		for _, r := range first {
			r.Release()
		}
	}()
	second, _, err := capmapfacts.BuildRecords(sampleCorpus(), fixedNow)
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
	stats, err := capmapfacts.Ingest(context.Background(), sampleCorpus(), sink, "", fixedNow)
	require.NoError(t, err)
	assert.Equal(t, 6, stats.Rows)
	assert.Equal(t, 1, sink.calls, "the whole corpus lands in one insert, not one per row")
	assert.Equal(t, capmapfacts.QualifiedTable, sink.table)
	assert.Equal(t, int64(6), totalRows(sink.records))
}

func TestIngestEmptyCorpusDoesNotCallTheSink(t *testing.T) {
	sink := &captureSink{}
	defer sink.release()
	stats, err := capmapfacts.Ingest(context.Background(), capmapcorpus.Corpus{}, sink, "", fixedNow)
	require.NoError(t, err)
	assert.Zero(t, stats.Rows)
	assert.Zero(t, sink.calls, "an empty corpus must not issue an empty insert")
}

func TestIngestRefusesNilSink(t *testing.T) {
	_, err := capmapfacts.Ingest(context.Background(), sampleCorpus(), nil, "", fixedNow)
	require.Error(t, err)
}

// A failing insert must surface with enough context to act on, and must not
// leak the Arrow buffers it was holding.
func TestIngestSurfacesSinkErrors(t *testing.T) {
	sink := &captureSink{err: assert.AnError}
	_, err := capmapfacts.Ingest(context.Background(), sampleCorpus(), sink, "custom.table", fixedNow)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "custom.table")
}
