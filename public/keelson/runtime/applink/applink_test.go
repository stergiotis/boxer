package applink

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_RoundTripsEveryForm(t *testing.T) {
	links := []string{
		"boxer:open/app/play",
		"boxer:raise/app/play",
		"boxer:open/app/play/set_sql@1",
		"boxer:open/app/play/set_sql@1/a/b?x=1&y=2#frag",
		"boxer:raise/app/map-view/show_area@12?bbox=1,2,3,4",
		"boxer:open/kind/mdeditLaunch",
		"boxer:open/app/play/set_sql@1?sql=größe",
		"boxer:open/kind/mdeditLaunch/00000000000000aa/20260101T000000Z/notes/a.md?mode=read#L3",
		"boxer:l/" + strings.Repeat("0a", HandleBytes),
	}
	for _, l := range links {
		e, err := Parse(l)
		require.NoError(t, err, l)
		assert.Equal(t, l, Format(e), l)
	}
}

func TestParse_ReadsTheEnvelope(t *testing.T) {
	e, err := Parse("boxer:raise/app/play/run_query@3?sql=SELECT%201")
	require.NoError(t, err)
	assert.Equal(t, FormApp, e.Form)
	assert.Equal(t, VerbRaise, e.Verb)
	assert.Equal(t, "play", e.App)
	assert.Equal(t, "run_query", e.Operation)
	assert.Equal(t, uint16(3), e.Version)
	assert.Equal(t, "?sql=SELECT%201", e.Tail)
	assert.True(t, e.HasCall())
}

func TestParse_Refuses(t *testing.T) {
	bad := map[string]string{
		"not ours":             "https://example.org",
		"clipboard handle":     "boxer:clip/abc",
		"no verb":              "boxer:app/play",
		"unknown verb":         "boxer:show/app/play",
		"unknown form":         "boxer:open/window/3",
		"empty alias":          "boxer:open/app/",
		"alias with dot":       "boxer:open/app/play.x",
		"tail without op":      "boxer:open/app/play?x=1",
		"op without version":   "boxer:open/app/play/set_sql",
		"version zero":         "boxer:open/app/play/set_sql@0",
		"version leading zero": "boxer:open/app/play/set_sql@01",
		"version too large":    "boxer:open/app/play/set_sql@65536",
		"upper-case operation": "boxer:open/app/play/SetSql@1",
		"raise a kind":         "boxer:raise/kind/mdeditLaunch",
		"kind with underscore": "boxer:open/kind/md_edit",
		"space in tail":        "boxer:open/app/play/set_sql@1?sql=a b",
		"control byte":         "boxer:open/app/play/set_sql@1?sql=a\x01",
		"not UTF-8":            "boxer:open/app/play/set_sql@1?sql=\xff",
		"broken escape":        "boxer:open/app/play/set_sql@1?sql=%zz",
		"short handle":         "boxer:l/abcd",
		"upper-case handle":    "boxer:l/" + strings.Repeat("AB", HandleBytes),
		"too long":             "boxer:open/app/play/set_sql@1?sql=" + strings.Repeat("a", MaxLinkBytes),
	}
	for name, l := range bad {
		_, err := Parse(l)
		assert.Error(t, err, name)
	}
}

func TestClaimed(t *testing.T) {
	assert.True(t, Claimed("boxer:open/app/play"))
	assert.True(t, Claimed("boxer:nonsense"), "a malformed boxer: link is claimed, so it never reaches the OS opener")
	assert.False(t, Claimed("boxer:clip/abc"))
	assert.False(t, Claimed("https://example.org"))
	assert.False(t, Claimed("unregisteredscheme://x"))
}

func TestTail_RoundTrips(t *testing.T) {
	tails := []string{"", "/a", "/a%20b/c", "?k=v", "?a=1&b=x+y", "#frag", "/x?k=v#f%20g"}
	for _, s := range tails {
		tl, err := ParseTail(s)
		require.NoError(t, err, s)
		back, err := ParseTail(tl.String())
		require.NoError(t, err, s)
		assert.Equal(t, tl, back, s)
	}
}

func TestTail_IsStrict(t *testing.T) {
	for _, s := range []string{"x", "//a", "/a/", "?", "/a%2", "?k=%80", "/%ff", "#%c3"} {
		_, err := ParseTail(s)
		assert.Error(t, err, s)
	}
	tl, err := ParseTail("?a=1&a=2")
	require.NoError(t, err)
	assert.Error(t, tl.OnlyKeys("a"), "a key given twice")
	tl, err = ParseTail("?b=1")
	require.NoError(t, err)
	assert.Error(t, tl.OnlyKeys("a"), "an unknown key")
	assert.NoError(t, tl.OnlyKeys("a", "b"))
}

func TestHandle_IsKeyedAndStable(t *testing.T) {
	k1, err := NewKey()
	require.NoError(t, err)
	k2, err := NewKey()
	require.NoError(t, err)
	rec := []byte("a record")
	assert.Equal(t, Mint(k1, rec), Mint(k1, rec))
	assert.NotEqual(t, Mint(k1, rec), Mint(k2, rec))
	assert.NotEqual(t, Mint(k1, rec), Mint(k1, []byte("another")))
	h := Mint(k1, rec)
	back, err := ParseHandle(h.String())
	require.NoError(t, err)
	assert.Equal(t, h, back)
	assert.Len(t, h.String(), 2*HandleBytes)
}

func TestFormat_KeepsATailParseWillRefuse(t *testing.T) {
	link := Format(Envelope{Form: FormApp, Verb: VerbOpen, App: "play", Tail: "?sql=x"})
	_, err := Parse(link)
	assert.Error(t, err, "a tail with no operation is refused, not dropped: %s", link)
}
