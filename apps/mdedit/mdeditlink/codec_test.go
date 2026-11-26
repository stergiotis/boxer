package mdeditlink

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
)

func TestLinkCodec_RoundTrips(t *testing.T) {
	require.NoError(t, applink.CheckCodec(LinkCodec))
}

func TestLinkCodec_TheAliasIsTheKindLink(t *testing.T) {
	link := Format(Ref{Mount: identifier.TaggedId(LinkCodec.Examples[1].Mount), Snap: time.Unix(0, LinkCodec.Examples[1].SnapNanos), Path: "a b/c.md", Mode: ModeRead, Fragment: "^block-1"})
	assert.True(t, applink.Claimed(link))
	e, err := applink.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, applink.FormKind, e.Form)
	assert.Equal(t, launchcfg.Kind, e.Kind)
	codec, ok := applink.KindCodec(e.Kind)
	require.True(t, ok)
	args, err := codec.ParseLink(e.Tail)
	require.NoError(t, err)
	back, err := codec.FormatLink(args)
	require.NoError(t, err)
	assert.Equal(t, e.Tail, back)
	assert.Equal(t, "boxer:open/kind/"+launchcfg.Kind+e.Tail, applink.Format(e))
}

func TestLinkCodec_RefusesTextItCannotSpell(t *testing.T) {
	cfg := LinkCodec.Examples[0]
	cfg.Text = "handed text"
	_, err := formatLaunch(cfg)
	assert.Error(t, err)
}

// FuzzLinkCodec feeds the codec tails nobody wrote: reading one never
// panics, and a tail it accepts spells one call however it was written.
func FuzzLinkCodec(f *testing.F) {
	for _, ex := range LinkCodec.LinkExamples() {
		tail, err := LinkCodec.FormatLink(ex)
		require.NoError(f, err)
		f.Add(tail)
	}
	f.Add("/0000000000000000/x/y?mode=split")
	f.Add("/%zz")
	f.Fuzz(func(t *testing.T, tail string) {
		if err := applink.CheckParsed(LinkCodec, tail); err != nil {
			t.Fatal(err)
		}
	})
}

// A heading slug may hold letters outside ASCII, '#' and '%': mdedit's own
// Copy link writes it, so the link opens and reads back the same location.
func TestFormat_EscapesWhatAFragmentCannotCarry(t *testing.T) {
	for _, slug := range []string{"überblick", "c#-notes", "50%-done", "^block-1", "L12", "a b"} {
		ref := Ref{Mount: identifier.TaggedId(0x84000003A3EC7AEA), Snap: time.Unix(0, 1761574725307515194), Path: "notes/doc.md", Fragment: slug}
		link := Format(ref)
		back, err := Parse(link)
		require.NoError(t, err, link)
		assert.Equal(t, slug, back.Fragment, link)
		e, err := applink.Parse(link)
		require.NoError(t, err, link)
		require.NoError(t, applink.CheckParsed(LinkCodec, e.Tail), link)
	}
	assert.Contains(t, Format(Ref{Mount: identifier.TaggedId(0x84000003A3EC7AEA), Snap: time.Unix(0, 1761574725307515194), Path: "a.md", Fragment: "^id"}), "#^id",
		"a block id reads as Obsidian writes it")
}
