package mdeditlink

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp/sftptree"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

var (
	testMount = identifier.TaggedId(0x84000003A3EC7AEA)
	testSnap  = time.Unix(0, 1761574725307515194).UTC()
)

// A link round-trips through Format and Parse: the mount and snapshot as
// the SFTP tree spells them, a path with a space encoded, the mode as a
// query and the fragment as written — a block id's caret included.
func TestFormatParseRoundTrip(t *testing.T) {
	r := Ref{Mount: testMount, Snap: testSnap, Path: "notes/my note.md", Mode: ModeRead, Fragment: "^abc-1"}
	s := Format(r)
	assert.Equal(t, Prefix+sftptree.MountName(testMount)+"/"+sftptree.SnapshotName(testSnap)+"/notes/my%20note.md?mode=read#^abc-1", s)
	assert.True(t, Claimed(s))
	got, err := Parse(s)
	require.NoError(t, err)
	assert.Equal(t, r, got)

	// No mode, no fragment: the shortest spelling, and Split at the top.
	plain := Ref{Mount: testMount, Snap: testSnap, Path: "a.md"}
	got, err = Parse(Format(plain))
	require.NoError(t, err)
	assert.Equal(t, plain, got)
	assert.NotContains(t, Format(plain), "?")
}

// Parse is strict: another scheme, a mount that is not a 16-digit hex id,
// a snapshot that is not a snapshot name, a missing path, an unknown mode
// and an unknown query key are all refused.
func TestParseRefusesMalformedLinks(t *testing.T) {
	base := Prefix + sftptree.MountName(testMount) + "/" + sftptree.SnapshotName(testSnap)
	for _, bad := range []string{
		"https://example.com/a.md",
		"mdedit://nothex/" + sftptree.SnapshotName(testSnap) + "/a.md",
		"mdedit://" + sftptree.MountName(testMount) + "/latest/a.md",
		base,
		base + "/",
		base + "/a.md?mode=sideways",
		base + "/a.md?zoom=2",
		base + "//a.md",
		base + "/../a.md",
		base + "/notes/../a.md",
		base + "/notes//a.md",
		base + "/.",
	} {
		_, err := Parse(bad)
		assert.Error(t, err, bad)
	}
	assert.False(t, Claimed("https://example.com"))
	assert.False(t, Claimed(""))
}

// The three fragment forms and the empty one; FormatLocation is the inverse.
func TestParseLocation(t *testing.T) {
	cases := map[string]Location{
		"":                 {Kind: LocationTop},
		"creating-a-table": {Kind: LocationHeading, Slug: "creating-a-table"},
		"^para-1":          {Kind: LocationBlock, BlockId: "para-1"},
		"L12":              {Kind: LocationLine, Line: 12},
		"l12":              {Kind: LocationHeading, Slug: "l12"},
		"Lx":               {Kind: LocationHeading, Slug: "Lx"},
	}
	for frag, want := range cases {
		got, err := ParseLocation(frag)
		require.NoError(t, err, frag)
		assert.Equal(t, want, got, frag)
		assert.Equal(t, frag, FormatLocation(got), frag)
	}
	for _, bad := range []string{"^", "L0"} {
		_, err := ParseLocation(bad)
		assert.Error(t, err, bad)
	}
}

// The launch config a link becomes: the snapshot triple, no text, the
// basename as the window's name, the mode by name and the fragment as
// written.
func TestLaunch(t *testing.T) {
	l := Launch(Ref{Mount: testMount, Snap: testSnap, Path: "notes/readme.md", Mode: ModeBlocks, Fragment: "L3"})
	assert.Equal(t, "", l.Text)
	assert.Equal(t, "readme.md", l.Name)
	assert.Equal(t, testMount.Value(), l.Mount)
	assert.Equal(t, testSnap.UnixNano(), l.SnapNanos)
	assert.Equal(t, "notes/readme.md", l.Path)
	assert.Equal(t, "blocks", l.Mode)
	assert.Equal(t, "L3", l.Location)
	m, ok := ParseMode(l.Mode)
	assert.True(t, ok)
	assert.Equal(t, ModeBlocks, m)
	_, ok = ParseMode("Read")
	assert.False(t, ok, "mode names are lower-case")
}

// Open refuses a link that does not parse before touching the bus.
func TestOpenRefusesABadLinkWithoutABus(t *testing.T) {
	_, err := Open(nil, "https://example.com")
	assert.Error(t, err)
}
