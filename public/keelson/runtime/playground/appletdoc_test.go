package playground_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
)

// TestParserSlot is the slot's contract: nothing installed refuses with
// the reason, and installing returns what it replaced so a test can restore.
func TestParserSlot(t *testing.T) {
	prev := playground.SetAppletDocParser(nil)
	t.Cleanup(func() { playground.SetAppletDocParser(prev) })

	assert.False(t, playground.HasAppletDocParser())
	_, err := playground.ParseAppletDoc("a.md", []byte("x"))
	assert.ErrorContains(t, err, "no applet document parser")

	fake := func(path string, src []byte) (doc playground.AppletDoc, err error) {
		doc.Title = path
		return
	}
	assert.Nil(t, playground.SetAppletDocParser(fake))
	assert.True(t, playground.HasAppletDocParser())
	doc, err := playground.ParseAppletDoc("a.md", []byte("x"))
	require.NoError(t, err)
	assert.Equal(t, "a.md", doc.Title)
	assert.NotNil(t, playground.SetAppletDocParser(nil), "installing returns the parser it replaced")
}

// TestComposeBundleDocChecksWithTheInstalledParser runs the composer against
// a parser standing in for sqlapplet's, on a document it composed.
func TestComposeBundleDocChecksWithTheInstalledParser(t *testing.T) {
	prev := playground.SetAppletDocParser(func(path string, src []byte) (doc playground.AppletDoc, err error) {
		doc.Datasets = []string{"result"}
		_, rest, _ := strings.Cut(string(src), "```sql\n")
		doc.Sql, _, _ = strings.Cut(rest, "\n```")
		doc.Introspection = true
		return
	})
	t.Cleanup(func() { playground.SetAppletDocParser(prev) })

	doc, err := playground.ComposeBundleDoc(playground.BundleSpec{Alias: "b",
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result"}}})
	require.NoError(t, err)
	assert.Contains(t, string(doc), "endpoint: introspection")

	_, err = playground.ComposeBundleDoc(playground.BundleSpec{Alias: "b",
		Sql: "SELECT * FROM keelson('other')", Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result"}}})
	assert.ErrorContains(t, err, "other")

	_, err = playground.ComposeBundleDoc(playground.BundleSpec{Alias: "b"})
	assert.ErrorContains(t, err, "one or more datasets")
}

func TestValidIdentifier(t *testing.T) {
	assert.True(t, playground.ValidIdentifier("a_b1"))
	assert.False(t, playground.ValidIdentifier(""))
	assert.False(t, playground.ValidIdentifier("1a"))
	assert.False(t, playground.ValidIdentifier("a-b"))
	assert.False(t, playground.ValidIdentifier(string(make([]byte, 65))))
}
