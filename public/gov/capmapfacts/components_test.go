package capmapfacts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra"
)

// Each facts row reads back as the components it was written from, through
// the generated ReadRow: a competence row carries CompetenceRecord,
// CompetenceTags and CompetenceLifecycle on a shared symbol section, each
// claiming its own attributes (ADR-0326 §SD4), and a relation row carries
// RelationRecord and none of them.
func TestFactsRowsReadBackAsTheirComponents(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	corpus := capmapcorpus.Corpus{
		Competences: []capmapcorpus.Competence{
			{Slug: "a", NaturalKey: capmapcorpus.NaturalKey("a"), Name: "A", Level: 1, Maturity: capmapcorpus.NotAssessed},
			{Slug: "b", NaturalKey: capmapcorpus.NaturalKey("b"), Name: "B", Owner: "o", Level: 2,
				Tags:     []string{"t1", "t2"},
				Sections: []capmapcorpus.Section{{Heading: "Vision and Scope", Text: "one"}, {Text: "two"}},
				Lifecycle: []capmapcorpus.LifecycleEvent{
					{Phase: capmapcorpus.PhaseDefined, By: "J", At: now},
					{Phase: capmapcorpus.PhasePlanned, By: "K"},
				},
				Assessment: []capmapcorpus.Claim{
					{Dimension: "readiness", Rating: "7", By: "J", At: now,
						Basis: []capmapcorpus.Anchor{{Kind: capmapcorpus.AnchorKindAdr, Key: "0223"}, {Kind: capmapcorpus.AnchorKindHowto, Key: "jobs"}}},
					{Dimension: "evolution", Rating: "product",
						Basis: []capmapcorpus.Anchor{{Kind: capmapcorpus.AnchorKindAdr, Key: "0223"}}},
					{Dimension: "pain", Rating: "1", By: "J"},
				}},
		},
		Relations: []capmapcorpus.Relation{
			{SourceSlug: "b", Target: "a", Kind: capmapcorpus.RelationKindSimilar, Ncd: 0.25, Resolution: capmapcorpus.ResolutionDirect},
			{SourceSlug: "b", Target: "nowhere", Kind: capmapcorpus.RelationKindWikilink, Section: "S", Resolution: capmapcorpus.ResolutionUnresolved},
		},
	}
	records, _, err := BuildRecords(corpus, now)
	require.NoError(t, err)
	require.Len(t, records, 1)
	defer records[0].Release()
	acc := ra.NewReadAccessFacts()
	acc.SetColumnIndices(acc.GetColumnIndices())
	require.NoError(t, acc.LoadFromRecord(records[0]))
	defer acc.Release()
	sym, str, u8, txt, tm := acc.Symbol, acc.StringArray, acc.U8Array, acc.TextArray, acc.TimeArray
	fk, f64 := acc.ForeignKey, acc.F64Array

	for i, c := range corpus.Competences {
		wantRec, wantTags, wantLifecycle, wantClaims, wantBases := competenceComponents(c)
		rec, present, err := CompetenceRecordReadRow(i,
			sym.GetAttributes(), sym.GetMemberships(),
			str.GetAttributes(), str.GetMemberships(),
			u8.GetAttributes(), u8.GetMemberships(),
			txt.GetAttributes(), txt.GetMemberships(),
			tm.GetAttributes(), tm.GetMemberships())
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, wantRec, rec, "competence %s", c.Slug)
		tags, present, err := CompetenceTagsReadRow(i, sym.GetAttributes(), sym.GetMemberships())
		require.NoError(t, err)
		require.Equal(t, len(wantTags.Tags) > 0, present)
		require.Equal(t, wantTags, tags)
		lifecycle, present, err := CompetenceLifecycleReadRow(i, sym.GetAttributes(), sym.GetMemberships())
		require.NoError(t, err)
		require.Equal(t, len(wantLifecycle.By) > 0, present)
		require.Equal(t, wantLifecycle, lifecycle)
		// The claims spread over three sections and the bases are a
		// component of their own (ADR-0337 §SD7); both read back whole,
		// the dimension parameter keeping each field with its claim.
		claims, present, err := CompetenceClaimsReadRow(i,
			sym.GetAttributes(), sym.GetMemberships(),
			str.GetAttributes(), str.GetMemberships(),
			tm.GetAttributes(), tm.GetMemberships())
		require.NoError(t, err)
		require.Equal(t, len(wantClaims.Rating) > 0, present)
		require.Equal(t, wantClaims, claims)
		bases, present, err := CompetenceClaimBasesReadRow(i, sym.GetAttributes(), sym.GetMemberships())
		require.NoError(t, err)
		require.Equal(t, len(wantBases.Basis) > 0, present)
		require.Equal(t, wantBases, bases)
		_, present, err = RelationRecordReadRow(i,
			sym.GetAttributes(), sym.GetMemberships(),
			fk.GetAttributes(), fk.GetMemberships(),
			f64.GetAttributes(), f64.GetMemberships())
		require.NoError(t, err)
		require.False(t, present)
	}
	for j, rel := range corpus.Relations {
		i := len(corpus.Competences) + j
		got, present, err := RelationRecordReadRow(i,
			sym.GetAttributes(), sym.GetMemberships(),
			fk.GetAttributes(), fk.GetMemberships(),
			f64.GetAttributes(), f64.GetMemberships())
		require.NoError(t, err)
		require.True(t, present)
		require.Equal(t, relationRecord(rel), got)
		_, present, err = CompetenceTagsReadRow(i, sym.GetAttributes(), sym.GetMemberships())
		require.NoError(t, err)
		require.False(t, present)
	}
}
