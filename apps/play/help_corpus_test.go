package play

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stretchr/testify/assert"
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
		helpOperationsDoc:       false,
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

// TestHelpCorpusDropsNothing is the ADR-0180 M2 zero-drops gate over this
// book.
//
// The markdown lowering's failure mode for a construct it does not know is
// INVISIBLE: the node reaches a default branch, and the node plus the prose it
// covered is dropped from the rendered document with nothing to say so. A page
// reads short and no test notices. That is how a parser feature once deleted
// the text it covered.
//
// [markdown.Doc.Dropped] counts every skip by AST kind; asserting zero over a
// real corpus turns the failure mode into a property. What fails here is not
// only a regression — a future goldmark feature whose node the lowering has no
// case for lands here first, which is the point.
func TestHelpCorpusDropsNothing(t *testing.T) {
	help.SyncFromRegistry()
	b, ok := help.Book("github.com/stergiotis/boxer/apps/play")
	require.True(t, ok, "play help book must be indexed from Manifest.Help")

	docs := b.Docs()
	require.NotEmpty(t, docs, "help corpus must not be empty")
	for _, info := range docs {
		doc, _, parsed := b.Doc(info.Path)
		require.Truef(t, parsed, "doc %q must parse", info.Path)
		require.Emptyf(t, doc.Dropped(), "doc %q loses content in the lowering: %+v",
			info.Path, doc.Dropped())
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

// helpOperationsDoc is the help topic that lists the catalog for an agent
// (keelson-app A4). TestHelpCorpusNamesEveryOperation keeps it equal to the
// catalog in code.
const helpOperationsDoc = "reference-operating-play-through-the-catalog"

// helpOperationRow matches a table row whose first cell is an operation name
// in backticks — the one shape the operations topic lists operations in, so
// a name mentioned in prose (a signal, a resource, an argument) is not
// mistaken for a claim that the catalog has it.
var helpOperationRow = regexp.MustCompile("(?m)^\\| `([a-z][a-z0-9_]*)` \\|")

// TestHelpCorpusNamesEveryOperation holds the operations topic and the
// catalog together in both directions: every operation playOps declares has
// a row in the topic, and every row names an operation the catalog has. A
// new operation without a row, a renamed one, or a row kept past its
// operation's removal fails here rather than teaching an agent a name the
// window does not answer to.
func TestHelpCorpusNamesEveryOperation(t *testing.T) {
	raw, err := fs.ReadFile(helpFS, "help/"+helpOperationsDoc+".md")
	require.NoError(t, err)

	documented := map[string]int{}
	for _, m := range helpOperationRow.FindAllStringSubmatch(string(raw), -1) {
		documented[m[1]]++
	}
	require.NotEmpty(t, documented, "the operations topic lists no operation rows")

	catalog := map[string]bool{}
	for _, name := range playOps.Names() {
		catalog[name] = true
		assert.Contains(t, documented, name, "operation %q is in the catalog and has no row in %s.md", name, helpOperationsDoc)
	}
	for name, n := range documented {
		assert.Truef(t, catalog[name], "%s.md lists %q, which the catalog does not declare", helpOperationsDoc, name)
		assert.Equalf(t, 1, n, "%s.md lists %q %d times; one row per operation", helpOperationsDoc, name, n)
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
