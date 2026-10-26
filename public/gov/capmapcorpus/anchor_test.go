package capmapcorpus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
)

// fakeResolver answers from a table: an anchor it holds resolves with the
// recorded status, one it does not is unresolved, and a kind it was told
// nothing about is unchecked. That is the three-way answer every real
// resolver gives, with the tree replaced by a map.
type fakeResolver struct {
	known map[string]string // anchor text -> status
	kinds map[capmapcorpus.AnchorKindE]struct{}
}

func (inst fakeResolver) ResolveAnchor(a capmapcorpus.Anchor) (res capmapcorpus.AnchorResolution) {
	if _, reach := inst.kinds[a.Kind]; !reach {
		return capmapcorpus.AnchorResolution{Resolution: capmapcorpus.ResolutionUnchecked}
	}
	status, ok := inst.known[a.String()]
	if !ok {
		return capmapcorpus.AnchorResolution{Resolution: capmapcorpus.ResolutionUnresolved}
	}
	return capmapcorpus.AnchorResolution{Resolution: capmapcorpus.ResolutionDirect, Status: status}
}

func anchoredVault() map[string]string {
	return map[string]string{
		"boxer/durable-work.md": `---
name: Durable work
level: 2
domain: boxer-toolbelt
catalog: boxer
realised_by: [adr:0223, adr:0099, pkg:public/streaming/watchbill, howto:watchbill-jobs]
assessment:
  - dimension: readiness
    rating: 7
    basis: [adr:0223, howto:watchbill-jobs]
    by: maintainer
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0099]
    by: maintainer
    at: 2026-10-26
  - dimension: pain
    rating: 1
    basis: []
  - dimension: importance
    rating: differentiating
    basis: [trial:nothing-here]
    by: maintainer
    at: 2026-10-26
---

# Scope

Prose.
`,
	}
}

func TestParseDirWithResolvesAnchorsAndClaims(t *testing.T) {
	resolver := fakeResolver{
		known: map[string]string{
			"adr:0223":                       "accepted",
			"adr:0099":                       "superseded",
			"pkg:public/streaming/watchbill": "",
		},
		kinds: map[capmapcorpus.AnchorKindE]struct{}{
			capmapcorpus.AnchorKindAdr: {}, capmapcorpus.AnchorKindPkg: {}, capmapcorpus.AnchorKindTrial: {},
		},
	}
	corpus, err := capmapcorpus.ParseDirWith(writeVault(t, anchoredVault()), resolver)
	require.NoError(t, err)
	require.Len(t, corpus.Competences, 1)
	c := corpus.Competences[0]

	// Anchors are relations of their own kind, resolved by the kind's
	// resolver; an unreachable kind is unchecked, not broken.
	realised := findRelation(t, corpus.Relations, "durable-work", "adr:0223", capmapcorpus.RelationKindRealisedBy)
	assert.Equal(t, capmapcorpus.ResolutionDirect, realised.Resolution)
	assert.Equal(t, "accepted", realised.TargetStatus)
	retired := findRelation(t, corpus.Relations, "durable-work", "adr:0099", capmapcorpus.RelationKindRealisedBy)
	assert.Equal(t, capmapcorpus.ResolutionDirect, retired.Resolution, "an anchor to a retired decision resolves")
	assert.True(t, capmapcorpus.IsRetiredStatus(retired.TargetStatus))
	unchecked := findRelation(t, corpus.Relations, "durable-work", "howto:watchbill-jobs", capmapcorpus.RelationKindRealisedBy)
	assert.Equal(t, capmapcorpus.ResolutionUnchecked, unchecked.Resolution)
	assert.NotContains(t, capmapcorpus.UnresolvedRelations(corpus.Relations), unchecked, "unchecked is not a defect")

	// A basis anchor is a relation too, filed under its claim's dimension.
	basis := findRelation(t, corpus.Relations, "durable-work", "trial:nothing-here", capmapcorpus.RelationKindBasis)
	assert.Equal(t, "importance", basis.Section)
	assert.Equal(t, capmapcorpus.ResolutionUnresolved, basis.Resolution)
	assert.Len(t, capmapcorpus.AnchorRelations(corpus.Relations), 4+2+1+1)

	require.Len(t, c.Assessment, 4)
	readiness, ok := c.ClaimOn(capmapcorpus.DimensionReadiness)
	require.True(t, ok)
	assert.Equal(t, "7", readiness.Rating)
	assert.Equal(t, "maintainer", readiness.By)
	assert.Equal(t, 2026, readiness.At.Year())
	assert.Len(t, readiness.Basis, 2)
	assert.Empty(t, readiness.Problems, "a claim with a resolving basis, an assessor and a date is clean")

	evolution, _ := c.ClaimOn(capmapcorpus.DimensionEvolution)
	assert.Equal(t, []capmapcorpus.ClaimProblemE{capmapcorpus.ClaimProblemRetiredBasis}, evolution.Problems)

	pain, _ := c.ClaimOn(capmapcorpus.DimensionPain)
	assert.ElementsMatch(t, []capmapcorpus.ClaimProblemE{
		capmapcorpus.ClaimProblemEmptyBasis, capmapcorpus.ClaimProblemNoAssessor, capmapcorpus.ClaimProblemNoDate,
	}, pain.Problems, "an affirmation is listed, not refused")

	importance, _ := c.ClaimOn(capmapcorpus.DimensionImportance)
	assert.Equal(t, []capmapcorpus.ClaimProblemE{capmapcorpus.ClaimProblemUnresolvedBasis}, importance.Problems)

	_, ok = c.ClaimOn("nothing")
	assert.False(t, ok, "an absent dimension is unassessed")
}

// Without a resolver every anchor is unchecked: the honest answer from a
// parse with no tree around it, and never a pass.
func TestParseDirLeavesAnchorsUnchecked(t *testing.T) {
	corpus, err := capmapcorpus.ParseDir(writeVault(t, anchoredVault()))
	require.NoError(t, err)
	for _, r := range capmapcorpus.AnchorRelations(corpus.Relations) {
		assert.Equal(t, capmapcorpus.ResolutionUnchecked, r.Resolution, r.Target)
	}
	assert.Empty(t, capmapcorpus.UnresolvedRelations(corpus.Relations))
	importance, _ := corpus.Competences[0].ClaimOn(capmapcorpus.DimensionImportance)
	assert.Empty(t, importance.Problems, "nothing was checked, so nothing is wrong yet")
}

// A spelling error in a claim fails the read: there is one right spelling,
// and reading a wrong one as something else would put a value in the corpus
// the vault does not hold.
func TestParseDirRefusesMalformedClaimsAndAnchors(t *testing.T) {
	for name, fm := range map[string]string{
		"unknown dimension":  "assessment:\n  - dimension: velocity\n    rating: 3\n",
		"rating off scale":   "assessment:\n  - dimension: readiness\n    rating: 12\n",
		"word on a ranked":   "assessment:\n  - dimension: pain\n    rating: high\n",
		"unknown kind":       "realised_by: [line:parse.go:12]\n",
		"anchor without key": "realised_by: [adr]\n",
		"basis kind":         "assessment:\n  - dimension: readiness\n    rating: 2\n    basis: [file:x]\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := capmapcorpus.ParseDir(writeVault(t, map[string]string{
				"a.md": "---\nname: A\nlevel: 1\n" + fm + "---\n\n# Scope\n\nx\n",
			}))
			require.Error(t, err)
		})
	}
}

// A second claim on one dimension is kept and marked, so the author sees
// both rather than losing one silently.
func TestParseDirMarksDuplicateDimension(t *testing.T) {
	corpus, err := capmapcorpus.ParseDir(writeVault(t, map[string]string{
		"a.md": "---\nname: A\nlevel: 1\nassessment:\n" +
			"  - {dimension: pain, rating: 1, basis: [adr:0001], by: x, at: 2026-01-01}\n" +
			"  - {dimension: pain, rating: 3, basis: [adr:0002], by: x, at: 2026-01-02}\n" +
			"---\n\n# Scope\n\nx\n",
	}))
	require.NoError(t, err)
	claims := corpus.Competences[0].Assessment
	require.Len(t, claims, 2)
	assert.Empty(t, claims[0].Problems)
	assert.Equal(t, []capmapcorpus.ClaimProblemE{capmapcorpus.ClaimProblemDuplicateDimension}, claims[1].Problems)
	first, ok := corpus.Competences[0].ClaimOn(capmapcorpus.DimensionPain)
	require.True(t, ok)
	assert.Equal(t, "1", first.Rating, "the first claim is the one that counts")
}

// Anchors and claims survive the parse → render → parse round trip, which
// is what lets a dump of a stored corpus come back as the vault it was.
func TestWriteVaultRoundTripsAnchorsAndClaims(t *testing.T) {
	first, err := capmapcorpus.ParseDir(writeVault(t, anchoredVault()))
	require.NoError(t, err)
	out := t.TempDir()
	_, err = capmapcorpus.WriteVault(first, out)
	require.NoError(t, err)
	second, err := capmapcorpus.ParseDir(out)
	require.NoError(t, err)
	capmapcorpus.SortCorpus(&first)
	capmapcorpus.SortCorpus(&second)
	assert.Equal(t, first.Competences, second.Competences)
	assert.Equal(t, first.Relations, second.Relations)

	rendered, err := capmapcorpus.RenderCompetence(first.Competences[0], first.Relations)
	require.NoError(t, err)
	assert.Contains(t, string(rendered), "realised_by:")
	assert.Contains(t, string(rendered), "- adr:0223")
	assert.Contains(t, string(rendered), "dimension: readiness")
	assert.NotContains(t, string(rendered), "maturity:", "the sentinel is never written into a new note")
	assert.NotContains(t, string(rendered), "problems", "lint findings are not part of the note")
}

func TestDimensionScales(t *testing.T) {
	readiness, ok := capmapcorpus.LookupDimension(capmapcorpus.DimensionReadiness)
	require.True(t, ok)
	rank, ok := readiness.Rank("9")
	assert.True(t, ok)
	assert.Equal(t, 9, rank)
	_, ok = readiness.Rank("0")
	assert.False(t, ok)
	assert.Equal(t, "named in a Context or a survey", readiness.RungName("1"))
	assert.Equal(t, "", readiness.RungName("10"))

	evolution, _ := capmapcorpus.LookupDimension(capmapcorpus.DimensionEvolution)
	rank, ok = evolution.Rank("commodity")
	assert.True(t, ok)
	assert.Equal(t, 3, rank)
	assert.Equal(t, "a utility", evolution.RungName("commodity"))

	pain, _ := capmapcorpus.LookupDimension(capmapcorpus.DimensionPain)
	assert.Equal(t, "none", pain.RungName("0"))
	assert.Len(t, capmapcorpus.AllDimensions(), 4)
}

func TestParseAnchor(t *testing.T) {
	a, err := capmapcorpus.ParseAnchor(" table:keelson/competence ")
	require.NoError(t, err)
	assert.Equal(t, capmapcorpus.AnchorKindTable, a.Kind)
	assert.Equal(t, "keelson/competence", a.Key)
	assert.Equal(t, "table:keelson/competence", a.String())
	for _, bad := range []string{"", "adr", "adr:", ":0001", "nope:thing"} {
		_, err = capmapcorpus.ParseAnchor(bad)
		assert.Error(t, err, bad)
	}
	assert.Equal(t, "unchecked", capmapcorpus.ResolutionUnchecked.String())
	assert.Equal(t, capmapcorpus.ResolutionUnchecked, capmapcorpus.ParseResolution("unchecked"))
}
