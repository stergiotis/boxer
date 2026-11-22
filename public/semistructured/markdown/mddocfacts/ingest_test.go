package mddocfacts_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdextract"
)

// vaultDir is the extractor's fixture vault, reused so the encoding is tested
// over the same documents the extraction golden pins.
const vaultDir = "../mdextract/testdata/vault"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(vaultDir, name))
	require.NoError(t, err)
	return src
}

func TestNewMdDocRow_IdentityRules(t *testing.T) {
	t1 := time.Unix(1_700_000_000, 0).UTC()
	t2 := t1.Add(time.Minute)

	a := mddocfacts.NewMdDocRow("same text", "", "", 2, t1)
	b := mddocfacts.NewMdDocRow("same text", "", "", 2, t2)
	c := mddocfacts.NewMdDocRow("other text", "", "", 2, t1)

	assert.Equal(t, a.NaturalKey, b.NaturalKey, "identical content is the same entity")
	assert.Zero(t, a.Id, "the row's id is minted when it is written")
	assert.NotEqual(t, a.NaturalKey, c.NaturalKey)
	assert.Equal(t, a.ContentHash, b.ContentHash)
	assert.Len(t, a.NaturalKey, 32)
	assert.Len(t, a.ContentHash, 64)
	assert.Equal(t, mddocfacts.KindMdDoc, a.Kind)
}

func TestBuildRows_Shape(t *testing.T) {
	ts := time.Unix(1_700_000_000, 0).UTC()
	src := fixture(t, "index.md")
	rows := mddocfacts.BuildRows(src, "index.md", ts, mdextract.Extract(src))

	assert.Equal(t, "Vault Index", rows.Doc.Title)
	assert.Equal(t, "index.md", rows.Doc.FileName)
	assert.NotZero(t, rows.Doc.Words)
	require.NotNil(t, rows.Frontmatter)
	assert.Equal(t, 1+len(rows.Headings)+len(rows.CodeBlocks)+len(rows.Links)+len(rows.Emphases)+len(rows.Tags)+1, rows.Count())

	require.Len(t, rows.Headings, 2)
	assert.Equal(t, mddocfacts.KindMdHeading, rows.Headings[0].Kind)
	assert.False(t, rows.Headings[0].Parent.Has, "a level-1 has no parent")
	assert.True(t, rows.Headings[1].Parent.Has)
	assert.Equal(t, uint64(0), rows.Headings[1].Parent.Val)
	assert.Equal(t, []string{"Vault Index"}, rows.Headings[1].Path)
	assert.False(t, rows.Headings[0].Anchor.Has)

	require.Len(t, rows.Links, 7)
	require.NoError(t, rows.AssignIds(counter(100)))
	assert.EqualValues(t, 100, rows.Doc.Id, "the document's id is the first drawn")
	spellings := map[string]int{}
	for _, l := range rows.Links {
		spellings[l.Spelling]++
		assert.Equal(t, rows.Doc.Id, l.Doc, "every item points at its document row")
		assert.Equal(t, rows.Doc.NaturalKey, l.DocHash, "and carries the document's natural key")
		assert.Equal(t, ts, l.Ts)
	}
	assert.Equal(t, map[string]int{"wikilink": 3, "inline": 1, "autolink": 1, "image": 1, "embed": 1}, spellings)

	var external int
	for _, l := range rows.Links {
		if l.External {
			external++
		}
	}
	assert.Equal(t, 2, external)

	require.Len(t, rows.Tags, 4)
	assert.Equal(t, "body", rows.Tags[0].Source)
	assert.Equal(t, "frontmatter", rows.Tags[3].Source)
	assert.Equal(t, "meta/structure", rows.Tags[3].Name)
	assert.False(t, rows.Tags[3].Section.Has, "a frontmatter tag sits under no heading")
	assert.True(t, rows.Tags[0].Section.Has)

	require.Len(t, rows.CodeBlocks, 1)
	assert.Equal(t, "sql", rows.CodeBlocks[0].Language)
	assert.True(t, rows.CodeBlocks[0].Section.Has)
	assert.Equal(t, uint64(1), rows.CodeBlocks[0].Section.Val, "under the second heading")
}

// counter draws ids from start upwards, as a store's key stream would.
func counter(start uint64) func() (uint64, error) {
	next := start
	return func() (uint64, error) { next++; return next - 1, nil }
}

// TestBuildRows_ItemIdentity pins the item rows' two-level identity: natural
// keys are the same across ingests of one document and distinct across its
// items; ids are the rows', drawn per ingest and distinct within it.
func TestBuildRows_ItemIdentity(t *testing.T) {
	t1 := time.Unix(1_700_000_000, 0).UTC()
	src := fixture(t, "alpha.md")
	ex := mdextract.Extract(src)
	a := mddocfacts.BuildRows(src, "alpha.md", t1, ex)
	b := mddocfacts.BuildRows(src, "alpha.md", t1.Add(time.Hour), ex)
	require.NoError(t, a.AssignIds(counter(1)))
	require.NoError(t, b.AssignIds(counter(1000)))

	require.Equal(t, len(a.Headings), len(b.Headings))
	seen := map[uint64]struct{}{a.Doc.Id: {}}
	for i := range a.Headings {
		assert.NotEqual(t, a.Headings[i].Id, b.Headings[i].Id, "a later ingest is a new row")
		assert.Equal(t, a.Headings[i].NaturalKey, b.Headings[i].NaturalKey, "of the same item")
		assert.Equal(t, b.Doc.Id, b.Headings[i].Doc, "pointing at the document row of its own ingest")
		_, dup := seen[a.Headings[i].Id]
		assert.False(t, dup, "ids are distinct within one ingest")
		seen[a.Headings[i].Id] = struct{}{}
	}
	for _, l := range a.Links {
		_, dup := seen[l.Id]
		assert.False(t, dup)
		seen[l.Id] = struct{}{}
	}
	assert.NotEqual(t, a.Headings[0].NaturalKey, a.Headings[1].NaturalKey)
	assert.NotEqual(t, a.Headings[0].NaturalKey, a.Links[0].NaturalKey, "the kind is part of the key")
}

func TestBuildRows_NoFrontmatterNoItems(t *testing.T) {
	src := fixture(t, "plain.md")
	rows := mddocfacts.BuildRows(src, "plain.md", time.Now(), mdextract.Extract(src))
	assert.Nil(t, rows.Frontmatter)
	assert.Equal(t, 1, rows.Count())
	assert.Equal(t, "", rows.Doc.Title)
}
