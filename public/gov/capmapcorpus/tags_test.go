package capmapcorpus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/tag"
)

// tagCases are the inputs both the frontmatter rule and the inline parser are
// held to. want is the canonical tag, or "" when the input is not one.
var tagCases = []struct {
	in   string
	want string
}{
	{"needs-owner", "needs-owner"},
	{"#needs-owner", "needs-owner"},
	{"  #needs-owner  ", "needs-owner"},
	{"workflow/triage", "workflow/triage"},
	{"_private", "_private"},
	{"v2", "v2"},
	{"", ""},
	{"#", ""},
	{"4", ""}, // Obsidian: `#4` is the number four, not a tag
	{"needs owner", ""},
	{"needs!owner", ""},
	{"-leading", ""},
	{"trailing/", ""},
}

func TestNormalizeTag(t *testing.T) {
	for _, tc := range tagCases {
		got, ok := capmapcorpus.NormalizeTag(tc.in)
		if tc.want == "" {
			assert.Falsef(t, ok, "%q should not be a tag, got %q", tc.in, got)
			continue
		}
		assert.Truef(t, ok, "%q should be a tag", tc.in)
		assert.Equalf(t, tc.want, got, "%q", tc.in)
	}
}

// The frontmatter rule and the inline `#tag` parser must agree on what a tag
// is, because the same vault is read by both — this corpus reads the
// frontmatter key, and the markdown widget renders the body.
//
// They agree on the *whole-string* question only. The inline parser scans a
// prefix out of flowing prose, so `#needs!owner` yields the tag `needs` there;
// in frontmatter that value is a malformed tag, and truncating it would put a
// tag in the corpus that the vault does not contain. So the pin is: what
// [capmapcorpus.NormalizeTag] accepts, the parser must recognise whole; what it
// rejects, the parser must not recognise whole.
func TestNormalizeTagMatchesTheInlineParsersRules(t *testing.T) {
	for _, tc := range tagCases {
		body := tc.in
		if body == "" || body[0] != '#' {
			body = "#" + body
		}
		if body == "#" {
			continue // there is no inline spelling of the empty tag
		}
		got := parseInlineTag(t, body)
		if tc.want == "" {
			assert.NotEqualf(t, body[1:], got,
				"%q is not a frontmatter tag, so the inline parser must not read the whole of it as one", tc.in)
			continue
		}
		assert.Equalf(t, tc.want, got, "inline parse of %q", body)
	}
}

// parseInlineTag returns the tag the Obsidian inline parser finds in src, or ""
// if it found none. Only the first is reported; every case here carries at most
// one.
func parseInlineTag(t *testing.T, src string) (found string) {
	t.Helper()
	md := obsidian.New(obsidian.Options{Features: obsidian.FeatureTag})
	doc := md.Parser().Parse(text.NewReader([]byte(src)))
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || found != "" {
			return ast.WalkContinue, nil
		}
		if node, is := n.(*tag.Node); is {
			found = string(node.Tag)
		}
		return ast.WalkContinue, nil
	})
	require.NoError(t, err)
	return found
}

func TestParseDirReadsTags(t *testing.T) {
	root := writeVault(t, map[string]string{
		"seq.md":    "---\nname: Seq\nlevel: 1\ntags: [needs-owner, \"#merge-candidate\", needs-owner, 4]\n---\n\n# Vision and Scope\n\nx\n",
		"scalar.md": "---\nname: Scalar\nlevel: 1\ntags: needs-owner, workflow/triage\n---\n\n# Vision and Scope\n\nx\n",
		"empty.md":  "---\nname: Empty\nlevel: 1\ntags:\n---\n\n# Vision and Scope\n\nx\n",
		"none.md":   "---\nname: None\nlevel: 1\n---\n\n# Vision and Scope\n\nx\n",
	})
	corpus, err := capmapcorpus.ParseDir(root)
	require.NoError(t, err)
	byName := make(map[string][]string, len(corpus.Competences))
	for _, c := range corpus.Competences {
		byName[c.Name] = c.Tags
	}

	// Authored order survives, the `#` is dropped, a repeat is kept once, and
	// a bare number is not a tag.
	assert.Equal(t, []string{"needs-owner", "merge-candidate"}, byName["Seq"])
	// Obsidian also allows one scalar holding several tags.
	assert.Equal(t, []string{"needs-owner", "workflow/triage"}, byName["Scalar"])
	// An untagged competence carries no tags rather than one empty one — the
	// difference matters to `has(tags, …)` on the read side.
	assert.Empty(t, byName["Empty"])
	assert.Empty(t, byName["None"])
}
