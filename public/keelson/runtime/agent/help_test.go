package agent

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docHelp is the doc app's inline help.
var docHelp = fstest.MapFS{
	"overview.md": {Data: []byte("---\ntype: explanation\n---\n# The doc\n\nA doc holds one text.\n\n" +
		"## Editing\n\nReplace the text with set_text.\n\n### Undo\n\nThe person undoes in the UI.\n\n" +
		"## Exporting\n\nexport copies the text out; the person confirms it.\n\n" +
		"## Long\n\n" + strings.Repeat("A line about margins and widths.\n", 600) + "\n### Margins\n\nNarrow.\n")},
}

func TestHelpListsSearchesAndReadsAnAppsDocuments(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()

	apps, err := r.cli.Describe(ctx, DescribeRequest{App: string(docAppId)})
	require.NoError(t, err)
	require.Len(t, apps, 1)
	assert.True(t, apps[0].Help, "describe says the app ships help")

	list, err := r.cli.Help(ctx, HelpRequest{App: string(docAppId)})
	require.NoError(t, err)
	require.Len(t, list.Docs, 1)
	assert.Equal(t, "overview", list.Docs[0].Doc)
	assert.Equal(t, "The doc", list.Docs[0].Title)
	var slugs []string
	for _, s := range list.Docs[0].Sections {
		slugs = append(slugs, s.Slug)
	}
	assert.Contains(t, slugs, "editing")
	assert.NotContains(t, slugs, "undo", "the listing keeps to the top two levels")

	found, err := r.cli.Help(ctx, HelpRequest{Search: "confirms"})
	require.NoError(t, err)
	require.NotEmpty(t, found.Hits)
	assert.Equal(t, "exporting", found.Hits[0].Section)

	sec, err := r.cli.Help(ctx, HelpRequest{App: string(docAppId), Doc: "overview", Section: "editing"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sec.Text, "## Editing"), "a section opens with its own heading line")
	assert.False(t, strings.HasSuffix(strings.TrimSpace(sec.Text), "##"), "and does not carry the next heading's marker")
	assert.Contains(t, sec.Text, "Replace the text")
	assert.Contains(t, sec.Text, "undoes in the UI", "a section holds its subsections")
	assert.NotContains(t, sec.Text, "export copies", "and stops at the next section of its level")

	whole, err := r.cli.Help(ctx, HelpRequest{App: string(docAppId), Doc: "overview"})
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(whole.Text, "---"), "the frontmatter is left out")
	assert.True(t, whole.Truncated)
	assert.LessOrEqual(t, len(whole.Text), HelpMaxBytes)

	long, err := r.cli.Help(ctx, HelpRequest{App: string(docAppId), Doc: "overview", Section: "long"})
	require.NoError(t, err)
	assert.True(t, long.Truncated)
	require.NotEmpty(t, long.Sections)
	assert.Equal(t, "margins", long.Sections[0].Slug, "a cut section names its subsections to read instead")

	_, err = r.cli.Help(ctx, HelpRequest{App: string(docAppId), Doc: "nope"})
	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Contains(t, refused.Reason, "no help document")
}
