package play

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/require"
)

// TestHelpCorpusIndexes guards the wiring of the play app's inline-help corpus
// (apps/play/help/*.md → Manifest.Help via //go:embed + help.MustSub). A broken
// embed directive, a missing file, or a doc that fails to parse would silently
// drop the doc from the index; this asserts both pages are present, titled,
// typed, and parse cleanly.
func TestHelpCorpusIndexes(t *testing.T) {
	// play's init() registers the factory + manifest; sync picks it up.
	help.SyncFromRegistry()

	b, ok := help.Book("github.com/stergiotis/boxer/apps/play")
	require.True(t, ok, "play help book must be indexed from Manifest.Help")

	// The corpus must satisfy the documentation-standard front-matter
	// contract (type/status enums, operator-facing so no adr). This also
	// dogfoods help.BookI.Validate against a real shipped corpus.
	require.Empty(t, b.Validate(), "play help corpus front-matter must conform")

	want := map[string]bool{
		"overview":              false,
		"features":              false,
		"snippets":              false,
		"howto-example-queries": false,
	}
	for _, d := range b.Docs() {
		if _, expected := want[d.Path]; !expected {
			continue
		}
		want[d.Path] = true
		require.NotEmpty(t, d.Title, "doc %q: frontmatter title", d.Path)
		require.NotEmpty(t, d.Type, "doc %q: Diátaxis type", d.Path)
		doc, _, parsed := b.Doc(d.Path)
		require.True(t, parsed, "doc %q must parse", d.Path)
		require.NotNil(t, doc)
	}
	for path, found := range want {
		require.True(t, found, "expected help doc %q in the play book", path)
	}
}

// TestHelpCorpusSQLBlocksParse asserts every fenced SQL block in the corpus
// survives the precondition the whole pre-execute stage shares: the SET-prelude
// harvest (ExtractParams), then a Grammar1 parse of what remains.
//
// It guards a failure mode with no visible symptom. Every unit of the stage is
// best-effort (ADR-0108 §SD3) and every one of them starts by parsing, so a
// block Grammar1 rejects does not fail — it skips CanonicalizeFull,
// ExpandLwIdMacros and ResolveColumnNames alike and ships the buffer verbatim.
// For the leeway how-to that means the `section:column` handles it teaches are
// never resolved and the server rejects the query; the only trace is three warn
// lines in the process log.
//
// The two shapes Grammar1 rejects and ClickHouse accepts are worth naming,
// because both have reached this corpus: more than one statement in a block
// (Grammar1 is single-statement), and a `#` line comment. A `#` inside a string
// literal is fine — the snippet library's `text/markdown` cell carries one.
func TestHelpCorpusSQLBlocksParse(t *testing.T) {
	docs, err := fs.Glob(helpFS, "help/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, docs, "help corpus must not be empty")

	for _, path := range docs {
		raw, readErr := fs.ReadFile(helpFS, path)
		require.NoError(t, readErr, "read %s", path)
		for _, blk := range sqlFences(string(raw)) {
			residual, _, exErr := ExtractParams(blk.sql)
			require.NoErrorf(t, exErr, "%s:%d: SET-prelude harvest rejects this block, so every pre-execute pass is skipped and the SQL ships verbatim\n%s",
				path, blk.line, blk.sql)
			_, parseErr := nanopass.Parse(residual)
			require.NoErrorf(t, parseErr, "%s:%d: Grammar1 rejects this block, so every pre-execute pass is skipped and the SQL ships verbatim\n%s",
				path, blk.line, blk.sql)
		}
	}
}

// sqlFence is one ```sql block: its body and the 1-based line its content
// starts on, so a failure points at the source rather than at a copy of it.
type sqlFence struct {
	line int
	sql  string
}

// sqlFences extracts the ```sql blocks of a markdown document. Deliberately
// literal — it matches the fence openers the corpus actually uses rather than
// re-implementing markdown, and non-SQL fences (the corpus has one `bash`
// block) are not SQL to begin with.
func sqlFences(md string) (out []sqlFence) {
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```sql" {
			continue
		}
		var body []string
		start := i + 2
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			body = append(body, lines[i])
		}
		out = append(out, sqlFence{line: start, sql: strings.Join(body, "\n")})
	}
	return
}
