package play_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// The document publish_result composes parses with sqlapplet's own parser,
// which the bundle's readers open it with (ADR-0288 (proposed) §SD4).
func TestThePublishedDocumentParses(t *testing.T) {
	src, err := play.ComposeResultDocForTest("counts", "result", "SELECT n, count() AS c FROM keelson('result') GROUP BY n",
		[]string{"chart", "table"}, "quarterly \"counts\"\t\x01", "SELECT number AS n FROM numbers(3)")
	require.NoError(t, err)
	doc, err := play.ParseAppletDocForTest("counts.md", []byte(src))
	require.NoError(t, err)
	assert.Equal(t, "quarterly \"counts\"\t\x01", doc.Title, "quotes, tabs and control characters survive the frontmatter")
	assert.Equal(t, []string{"result"}, doc.Datasets)
	assert.Equal(t, "chart", doc.Tab)
	assert.True(t, doc.Introspection)
	assert.True(t, doc.Runnable)
	assert.Equal(t, "SELECT n, count() AS c FROM keelson('result') GROUP BY n", doc.Sql, "the buffer is the bundle's SQL, not the provenance query")
}

// publish_result is consequential: the person confirms each publish
// (ADR-0269 §SD5).
func TestPublishResultIsConsequential(t *testing.T) {
	spec, ok := play.OperationSpecForTest("publish_result")
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectConsequential, spec.Effect)
	assert.True(t, spec.Agents)
}

// The constructor refuses a document that reads a dataset the bundle does
// not carry, under sqlapplet's parser.
func TestComposeBundleDocRefusesAForeignDataset(t *testing.T) {
	_, err := play.ComposeBundleDocE(play.BundleSpec{Alias: "b", Sql: "SELECT * FROM keelson('other')",
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keelson('other')")
	_, err = play.ComposeBundleDocE(play.BundleSpec{Alias: "b", Prose: "```sql\nSELECT 1\n```",
		Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result"}}})
	assert.Error(t, err, "a fence in the prose would become a buffer")
	doc, err := play.ComposeBundleDocE(play.BundleSpec{Alias: "b", Datasets: []adhocdata.BundleDatasetInput{{LocalName: "result"}, {LocalName: "rules"}},
		Sql: "SELECT * FROM keelson('result') JOIN keelson('rules') USING (k)"})
	require.NoError(t, err)
	assert.Contains(t, string(doc), "datasets: [result, rules]")
}
