package mdoutline

// Tests for the hierarchy and the section lookup: the pure half, with no
// binding imported, the way the tree widget's own flatten is tested.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// heading is the terse constructor these tables are written with. The byte
// offset is only read by the click path, so it is a distinct number rather
// than a real one except where a test says otherwise.
func heading(level uint8, slug string) markdown.HeadingInfo {
	return markdown.HeadingInfo{Text: slug, Slug: slug, Level: level, ByteOffset: int(level) * 100}
}

// TestBuildNestsByHeadingLevel covers the rule and the shapes a hand-written
// document takes that a strict level-equals-depth reading gets wrong.
func TestBuildNestsByHeadingLevel(t *testing.T) {
	cases := []struct {
		name     string
		headings []markdown.HeadingInfo
		parents  []int32
	}{
		{
			name:     "plain nesting",
			headings: []markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(3, "c")},
			parents:  []int32{-1, 0, 1},
		},
		{
			name:     "siblings share a parent",
			headings: []markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(2, "c")},
			parents:  []int32{-1, 0, 0},
		},
		{
			name:     "a skipped level nests under the nearest smaller one",
			headings: []markdown.HeadingInfo{heading(1, "a"), heading(3, "b")},
			parents:  []int32{-1, 0},
		},
		{
			name:     "no level 1 at all leaves a forest of level 2s",
			headings: []markdown.HeadingInfo{heading(2, "a"), heading(3, "b"), heading(2, "c")},
			parents:  []int32{-1, 0, -1},
		},
		{
			name: "coming back up re-parents to the right ancestor",
			headings: []markdown.HeadingInfo{
				heading(1, "a"), heading(2, "b"), heading(3, "c"), heading(2, "d"), heading(1, "e"),
			},
			parents: []int32{-1, 0, 1, 0, -1},
		},
		{
			name:     "a deeper heading before any shallower one is a root",
			headings: []markdown.HeadingInfo{heading(3, "a"), heading(1, "b"), heading(2, "c")},
			parents:  []int32{-1, -1, 1},
		},
		{
			name: "a slugless heading is dropped, and does not break the nesting",
			headings: []markdown.HeadingInfo{
				heading(1, "a"), {Text: "", Slug: "", Level: 2}, heading(2, "b"),
			},
			parents: []int32{-1, 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m Model
			m.Build(tc.headings)
			assert.Equal(t, tc.parents, m.Tree().Parents)
			// Whatever the shape, the widget must accept it: a tree that
			// fails validation draws an error message instead of an outline.
			require.NoError(t, m.Tree().Validate())
		})
	}
}

func TestBuildDescendantCounts(t *testing.T) {
	var m Model
	m.Build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(3, "c"), heading(3, "d"), heading(2, "e"),
		heading(1, "f"),
	})
	// a holds everything under it at any depth, not just its own children.
	assert.Equal(t, 4, m.Node(0).Descendants, "a")
	assert.Equal(t, 2, m.Node(1).Descendants, "b")
	assert.Equal(t, 0, m.Node(2).Descendants, "c is a leaf")
	assert.Equal(t, 0, m.Node(4).Descendants, "e is a leaf")
	assert.Equal(t, 0, m.Node(5).Descendants, "f is a leaf")
}

func TestBuildKeysDisambiguateDuplicateSlugs(t *testing.T) {
	var m Model
	m.Build([]markdown.HeadingInfo{heading(1, "notes"), heading(1, "notes"), heading(1, "other")})
	assert.Equal(t, []string{"notes#0", "notes#1", "other#0"}, m.Tree().Keys,
		"two sections with the same title must not share collapse state")
}

// TestBuildReusesItsSlices is the allocation property the retained model
// exists for: a rebuild refills in place rather than reallocating.
func TestBuildReusesItsSlices(t *testing.T) {
	var m Model
	m.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(2, "c")})
	before := m.labels
	m.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})
	require.Equal(t, 2, m.Len())
	assert.Equal(t, cap(before), cap(m.labels), "the backing array is reused")
}

func TestNodeBySlugFindsTheFirst(t *testing.T) {
	var m Model
	m.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(2, "b")})
	assert.Equal(t, int32(1), m.NodeBySlug("b"))
	assert.Equal(t, int32(-1), m.NodeBySlug("zz"))
	assert.Equal(t, int32(-1), m.NodeBySlug(""))
}

func TestCount(t *testing.T) {
	assert.Equal(t, 2, Count([]markdown.HeadingInfo{heading(1, "a"), {Level: 2}, heading(2, "b")}))
	assert.Equal(t, 0, Count(nil))
}

// sectionDoc is a source whose heading text offsets are deliberately a
// couple of bytes past each line start, which is what the markdown package
// reports.
const sectionDoc = "intro prose\n\n# First\n\nbody one\n\n## Second\n\nbody two\n"

// sectionHeadings is what the markdown package would produce for sectionDoc:
// ByteOffset points at the heading text, not at the `#`.
func sectionHeadings() (hs []markdown.HeadingInfo) {
	return []markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 15},   // after "# "
		{Text: "Second", Slug: "second", Level: 2, ByteOffset: 35}, // after "## "
	}
}

// TestSectionFixtureOffsets guards the hand-computed offsets: a drifting
// fixture would make every assertion below vacuous rather than failing.
func TestSectionFixtureOffsets(t *testing.T) {
	for _, h := range sectionHeadings() {
		assert.Equal(t, h.Text, sectionDoc[h.ByteOffset:h.ByteOffset+len(h.Text)])
	}
}

func TestSectionAt(t *testing.T) {
	hs := sectionHeadings()
	cases := []struct {
		name string
		off  int
		want string
	}{
		{"before any heading is the doc-level section", 3, ""},
		{"on the heading marker resolves to that heading", 13, "first"},
		{"on the heading text", 16, "first"},
		{"inside the first body", 25, "first"},
		{"on the second marker", 34, "second"},
		{"inside the second body", 48, "second"},
		{"past the end stays in the last section", 999, "second"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SectionAt(sectionDoc, hs, tc.off))
		})
	}
}

// TestSectionAtMarkerNormalisation pins the line-start normalisation: a
// caret on the `#` sits before the reported heading offset, and without it
// the caret would resolve to the previous section.
func TestSectionAtMarkerNormalisation(t *testing.T) {
	markerOff := 13
	require.Equal(t, byte('#'), sectionDoc[markerOff], "fixture drift")
	assert.Equal(t, "first", SectionAt(sectionDoc, sectionHeadings(), markerOff))
}

// TestSectionAtDegenerateHeading covers the `##`-alone case, which the
// markdown package reports with ByteOffset -1 and an empty slug. It names no
// section, so it must not shadow the heading above it.
func TestSectionAtDegenerateHeading(t *testing.T) {
	hs := []markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 15},
		{Text: "", Slug: "", Level: 2, ByteOffset: -1},
	}
	assert.Equal(t, "first", SectionAt(sectionDoc, hs, 40))
}

func TestSectionAtNoHeadings(t *testing.T) {
	assert.Equal(t, "", SectionAt("just prose\n", nil, 4))
}
