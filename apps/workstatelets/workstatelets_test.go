package workstatelets

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/keelson/runtime/workstatelet"
)

func parseBook(t *testing.T) (docs map[string]workstatelet.Doc) {
	t.Helper()
	parsed, errs := workstatelet.ParseBook(BookId, help.MustSub(bookFS, "book"), []app.TopicT{app.TopicRuntime})
	for _, err := range errs {
		t.Errorf("book document: %v", err)
	}
	docs = make(map[string]workstatelet.Doc, len(parsed))
	for _, d := range parsed {
		docs[d.Slug] = d
	}
	return
}

// The corpus gate: every committed document parses, mints, names a layout
// the window host has, and carries only configs their kind's probe accepts.
// The probes are those of the kinds this test links; a document using a
// kind of an app outside this package's imports adds the import here.
func TestBookIsOpenable(t *testing.T) {
	docs := parseBook(t)
	require.NotEmpty(t, docs)
	reg, cat := app.NewRegistry(), workstatelet.NewCatalog()
	for slug, doc := range docs {
		_, err := cat.Mint(reg, doc, workstatelet.OriginBook, BookId)
		assert.NoError(t, err, slug)
		if doc.Desktop.Layout != "" {
			_, ok := windowhost.ParseLayout(doc.Desktop.Layout)
			assert.True(t, ok, "%s: layout %q is not one the window host has", slug, doc.Desktop.Layout)
		}
		for i, w := range doc.Windows {
			if len(w.Config) == 0 {
				continue
			}
			assert.NoError(t, kindcheck.Check(w.Kind, w.Config), "%s: window %d", slug, i+1)
		}
	}
}

// desktopReviewLaunch is the launch config desktop-review.md carries, written
// out: base64 in a document says nothing to a reader.
func desktopReviewLaunch() playground.Launch {
	return playground.Launch{
		At: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
		Sql: `SELECT key, app_id, title, tiled, floating, "order"
FROM keelson('windows')
ORDER BY "order"`,
		AutoRun:  true,
		Endpoint: playground.EndpointIntrospection,
	}
}

func TestDesktopReviewCarriesTheStatedConfig(t *testing.T) {
	doc, ok := parseBook(t)["desktop-review"]
	require.True(t, ok)
	require.Len(t, doc.Windows, 2)
	assert.Equal(t, workstatelet.Desktop{Layout: "main-stack", MainRatio: 0.6}, doc.Desktop)
	assert.Equal(t, workstatelet.Window{App: "appcenter"}, doc.Windows[1])

	w := doc.Windows[0]
	assert.Empty(t, w.App, "the playground is named by its kind, so the host resolves it")
	assert.Equal(t, playground.Kind, w.Kind)

	want := desktopReviewLaunch()
	wantBytes, err := buscodec.Encode(want)
	require.NoError(t, err)
	// On a mismatch the message carries the base64 to paste into the
	// document's config.
	require.Equal(t, base64.StdEncoding.EncodeToString(wantBytes), base64.StdEncoding.EncodeToString(w.Config),
		"desktop-review.md's config is not the encoding of desktopReviewLaunch()")

	got, err := buscodec.Decode[playground.Launch](w.Config)
	require.NoError(t, err)
	assert.Equal(t, want.Sql, got.Sql)
	assert.True(t, got.AutoRun)
	assert.Equal(t, playground.EndpointIntrospection, got.Endpoint)
}
