package sqlapplet

import (
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
)

// The shared topic every minted applet's book carries names every operation
// and resource the applet catalog serves (keelson-app A4), conforms to the
// documentation standard and loses nothing in the lowering (ADR-0180 M2).
func TestSharedAgentTopicNamesEveryAppletOperation(t *testing.T) {
	b, err := help.NewBook(app.AppIdT(appletIdPrefix+"help"), appletHelp(nil))
	require.NoError(t, err)
	require.Empty(t, b.Validate())
	doc, info, ok := b.Doc(appletHelpDoc)
	require.True(t, ok, "the shared topic must index")
	require.NotEmpty(t, info.Title)
	require.Empty(t, doc.Dropped(), "the topic loses content in the lowering: %+v", doc.Dropped())
	src, ok := b.Source(appletHelpDoc)
	require.True(t, ok)
	catalog := appletOps.Catalog()
	require.NotEmpty(t, catalog.Operations)
	for _, spec := range catalog.Operations {
		assert.Containsf(t, string(src), "`"+spec.Name+"`", "operation %q is not named in the shared topic", spec.Name)
	}
	for _, r := range catalog.Resources {
		assert.Containsf(t, string(src), "`"+r.Name+"`", "resource %q is not named in the shared topic", r.Name)
	}
}

// A minted manifest's Help indexes the book's own pages and the shared topic
// together; the book's directory itself is left as the applet parser reads
// it.
func TestMintedAppletHelpCarriesTheBookAndTheSharedTopic(t *testing.T) {
	fsys := fstest.MapFS{"reader.md": docMD("title: Reader", sqlFence)}
	reg := app.NewRegistry()
	minted, errs := mintBooks(reg, zerolog.Nop(), []registeredBook{{id: "t", fsys: fsys, topics: []app.TopicT{app.TopicRuntime}}})
	require.Empty(t, errs)
	require.Equal(t, 1, minted)
	m, ok := reg.LookupManifest(app.AppIdT(appletIdPrefix + "reader"))
	require.True(t, ok)
	require.NotNil(t, m.Help)
	b, err := help.NewBook(m.Id, m.Help)
	require.NoError(t, err)
	var paths []string
	for _, d := range b.Docs() {
		paths = append(paths, d.Path)
	}
	assert.ElementsMatch(t, []string{"reader", appletHelpDoc}, paths)
	// The book directory is not changed by the join: the parser still sees
	// one document.
	defs, perrs := parseBookLazy("t", fsys)
	require.Empty(t, perrs)
	require.Len(t, defs, 1)
}
