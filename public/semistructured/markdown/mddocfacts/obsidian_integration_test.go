//go:build integration

package mddocfacts_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocvocab"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// This file is the ingestor's benchmark, run for real: the fixture vault goes
// through the store into a `clickhouse local` state directory, and the
// queries the markdown how-to publishes for Obsidian's graph, backlinks, tag
// resolution and properties are executed against it — authored with the
// leeway SQL surface (LW_COMPONENT, LW_GET_LIST, LW_SEL) and expanded by the
// same client-side passes a host runs. What is asserted is the answer, so a
// change to the extraction, the DTOs, the vocabulary or the passes that
// silently alters what a reader gets goes red here.
//
// Integration lane: it needs the clickhouse binary.

// localConn shells to `clickhouse local` over one state directory, the shape
// lwsqlsurface.Install takes; the store writes through a chexec.LocalExecutor
// over the same directory, so both see one database.
type localConn struct {
	t    *testing.T
	path string
}

func (inst *localConn) exec(sql string) (out string, err error) {
	cmd, err := extbin.ClickHouseLocal.Command(inst.t.Context(), extbin.Opts{},
		"--path", inst.path, "--multiquery", "--output-format", "TSV")
	if err != nil {
		return
	}
	cmd.Stdin = strings.NewReader(sql)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		err = eh.Errorf("clickhouse local: %w: %s\n%s", err, stderr.String(), sql) //boxer:lint disable=CS013 reason="test-only diagnostic; the SQL and stderr are what the reader needs"
		return
	}
	out = stdout.String()
	return
}

func (inst *localConn) Exec(ctx context.Context, sql string) (err error) {
	_, err = inst.exec(sql)
	return
}

func (inst *localConn) Query(ctx context.Context, sql string) (body io.ReadCloser, err error) {
	out, err := inst.exec(sql)
	if err != nil {
		return
	}
	return io.NopCloser(strings.NewReader(out)), nil
}

// rows runs sql and splits the TSV answer.
func (inst *localConn) rows(t *testing.T, sql string) (rows [][]string) {
	t.Helper()
	out, err := inst.exec(sql)
	require.NoError(t, err)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line != "" {
			rows = append(rows, strings.Split(line, "\t"))
		}
	}
	return
}

type vocabLookup map[string]uint64

func (inst vocabLookup) LookupMembership(name string) (id uint64, err error) {
	id, ok := inst[name]
	if !ok {
		err = eh.Errorf("no such mddoc membership: %s", name) //boxer:lint disable=CS013 reason="test-only; the missing name is the whole message"
	}
	return
}

// expander is the client side of the surface, in a host's order: friendly
// `section:column` handles first (the physical names the later passes emit
// carry colons of their own), then the component pass over the store's
// published artefacts, then the extraction pass over the facts schema and
// the mddoc vocabulary.
type expander struct {
	handles   func(string) (string, error)
	component func(string) (string, error)
	extract   func(string) (string, error)
}

func newExpander(t *testing.T) (ex expander) {
	t.Helper()
	reg := componentsql.NewRegistry()
	require.NoError(t, reg.Register(mddocfacts.MddocComponentSQL))
	ex.component = constructsql.ComponentExpandPass(reg, "boxer").Run

	fields := dml.CreateSchemaFacts().Fields()
	cols := make([]string, 0, len(fields))
	for i := range fields {
		cols = append(cols, fields[i].Name)
	}
	ids, err := storegen.MembershipIds(mddocvocab.NkRegistry)
	require.NoError(t, err)
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{mddocfacts.MddocTableName: cols}))
	ex.handles = passes.ResolveColumnNames(resolver, "boxer", nil).Run
	ex.extract = constructsql.ExtractExpandPassWithIds(resolver, vocabLookup(ids), "boxer").Run
	return
}

func (inst expander) run(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := inst.handles(sql)
	require.NoError(t, err, sql)
	out, err = inst.component(out)
	require.NoError(t, err, sql)
	out, err = inst.extract(out)
	require.NoError(t, err, sql)
	require.NotContains(t, out, "LW_COMPONENT", "unexpanded macro left behind")
	require.NotContains(t, out, "LW_GET", "unexpanded macro left behind")
	require.NotContains(t, out, "LW_SEL", "unexpanded macro left behind")
	return
}

// provision installs the surface and the facts DDL into a fresh state
// directory, then ingests the fixture vault through the store.
func provision(t *testing.T) (conn *localConn, ingested int) {
	t.Helper()
	if _, ok := extbin.ClickHouseLocal.Resolve(); !ok {
		t.Skip("clickhouse not on PATH")
	}
	conn = &localConn{t: t, path: t.TempDir()}
	ctx := context.Background()
	require.NoError(t, lwsqlsurface.Install(ctx, conn))

	// The store never provisions (chstore owns the table); the DDL the
	// generator emitted beside the store is the physical schema it decodes.
	ddl, err := os.ReadFile("facts_ddl_clickhouse.out.sql")
	require.NoError(t, err)
	require.NoError(t, conn.Exec(ctx, string(ddl)))

	exec, err := chexec.NewLocalExecutor(conn.path, nil)
	require.NoError(t, err)
	store := mddocfacts.NewMddocStore(exec, nil, mddocfacts.MddocStoreConfig{})
	t.Cleanup(store.Close)
	require.NoError(t, store.VerifySchema(ctx))

	ts := time.Unix(1_700_000_000, 0).UTC()
	err = filepath.WalkDir(vaultDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return werr
		}
		src, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		rel, rerr := filepath.Rel(vaultDir, path)
		require.NoError(t, rerr)
		rows, ierr := store.IngestDocument(src, filepath.ToSlash(rel), ts)
		require.NoError(t, ierr)
		ingested += rows.Count()
		return nil
	})
	require.NoError(t, err)
	n, err := store.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, ingested, n)
	return
}

func TestObsidianQueriesOverTheFixtureVault(t *testing.T) {
	conn, ingested := provision(t)
	ex := newExpander(t)

	total := conn.rows(t, "SELECT count() FROM boxer.facts")
	require.Equal(t, [][]string{{itoa(ingested)}}, total, "every buffered row landed")

	t.Run("documents", func(t *testing.T) {
		got := conn.rows(t, ex.run(t, `
SELECT d.FileName, d.Title, d.Words
FROM (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts)
ORDER BY d.FileName`))
		require.Len(t, got, 5)
		assert.Equal(t, []string{"alpha.md", "Alpha"}, got[0][:2])
		assert.Equal(t, []string{"broken-frontmatter.md", "Still Parsed"}, got[1][:2])
		assert.Equal(t, []string{"index.md", "Vault Index"}, got[2][:2])
		assert.Equal(t, []string{"plain.md", "", "11"}, got[3])
		assert.Equal(t, []string{"projects/beta.md", "Beta"}, got[4][:2])
	})

	// The graph: one edge per internal link, source and target resolved to
	// documents by path, by basename (Obsidian's shortest-path rule) or by
	// alias. Resolution is the join a reader writes; the store carries
	// targets as written. A LEFT JOIN miss is an empty string in ClickHouse,
	// not a NULL, hence the nullIf around each candidate.
	const graphSQL = `
WITH docs AS (
  SELECT d.Id AS id, d.FileName AS file,
         lower(replaceRegexpOne(d.FileName, '\\.md$', '')) AS stem,
         lower(splitByChar('/', replaceRegexpOne(d.FileName, '\\.md$', ''))[-1]) AS base
  FROM (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts)
),
aliases AS (
  SELECT LW_GET('foreignKey', 'mdFrontmatterDoc', 'chan:low-card-ref') AS id,
         lower(one) AS alias
  FROM boxer.facts
  ARRAY JOIN LW_CO_GATHER("stringArray:value",
                          LW_SEL_ATTRS('stringArray', 'mdFrontmatterPath',
                                       'chan:low-card-ref-high-card-params', 'param:/aliases/_')) AS one
  WHERE LW_GET('symbol', 'mdFrontmatterKind', 'chan:low-card-ref') = 'mdFrontmatter'
),
links AS (
  SELECT l.Doc AS src, lower(replaceRegexpOne(l.Target, '\\.md$', '')) AS target
  FROM (SELECT LW_COMPONENT('MdLink') AS l FROM boxer.facts)
  WHERE NOT l.External
)
SELECT s.file AS source,
       coalesce(nullIf(t1.file, ''), nullIf(t2.file, ''), nullIf(t3.file, '')) AS target
FROM links
JOIN docs AS s ON s.id = links.src
LEFT JOIN docs AS t1 ON t1.stem = links.target
LEFT JOIN docs AS t2 ON t2.base = links.target
LEFT JOIN (SELECT a.alias AS alias, d.file AS file FROM aliases AS a JOIN docs AS d ON d.id = a.id) AS t3 ON t3.alias = links.target
WHERE target IS NOT NULL
ORDER BY source, target`

	t.Run("graph edges", func(t *testing.T) {
		got := conn.rows(t, ex.run(t, graphSQL))
		assert.Equal(t, [][]string{
			{"alpha.md", "alpha.md"},
			{"alpha.md", "index.md"},
			{"alpha.md", "projects/beta.md"},
			{"alpha.md", "projects/beta.md"},
			{"broken-frontmatter.md", "alpha.md"},
			{"index.md", "alpha.md"},
			{"index.md", "alpha.md"},
			{"index.md", "alpha.md"},
			{"index.md", "projects/beta.md"},
			{"projects/beta.md", "alpha.md"},
			{"projects/beta.md", "index.md"},
		}, got, "[[Home]] resolves through the alias; the image link resolves to nothing")
	})

	t.Run("backlinks", func(t *testing.T) {
		got := conn.rows(t, ex.run(t, `
SELECT target, groupUniqArray(source) AS sources FROM (`+graphSQL+`)
GROUP BY target ORDER BY target`))
		require.Len(t, got, 3)
		assert.Equal(t, "alpha.md", got[0][0])
		for _, src := range []string{"index.md", "projects/beta.md", "broken-frontmatter.md", "alpha.md"} {
			assert.Contains(t, got[0][1], "'"+src+"'")
		}
		assert.Equal(t, "index.md", got[1][0])
		assert.Equal(t, "projects/beta.md", got[2][0])
	})

	t.Run("tag resolution", func(t *testing.T) {
		got := conn.rows(t, ex.run(t, `
SELECT g.Name, groupUniqArray(d.FileName) AS files
FROM (SELECT LW_COMPONENT('MdTag') AS g FROM boxer.facts) AS tags
JOIN (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts) AS docs ON d.Id = g.Doc
GROUP BY g.Name ORDER BY g.Name`))
		names := make([]string, 0, len(got))
		for _, r := range got {
			names = append(names, r[0])
		}
		assert.Equal(t, []string{"alpha", "hub", "meta/structure", "project"}, names)
		// #alpha is written in beta's body and in alpha's frontmatter; both
		// sources answer one question.
		assert.Contains(t, got[0][1], "'projects/beta.md'")
		assert.Contains(t, got[0][1], "'alpha.md'")

		// A parent tag resolves its children by prefix.
		nested := conn.rows(t, ex.run(t, `
SELECT count() FROM (SELECT LW_COMPONENT('MdTag') AS g FROM boxer.facts)
WHERE g.Name = 'meta' OR startsWith(g.Name, 'meta/')`))
		assert.Equal(t, [][]string{{"2"}}, nested, "body and frontmatter occurrence of meta/structure")
	})

	t.Run("headings and sections", func(t *testing.T) {
		got := conn.rows(t, ex.run(t, `
SELECT h.Level, h.Text, h.Slug, h.Path
FROM (SELECT LW_COMPONENT('MdHeading') AS h FROM boxer.facts) AS hs
JOIN (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts) AS docs ON d.Id = h.Doc
WHERE d.FileName = 'alpha.md' ORDER BY h.Ordinal`))
		assert.Equal(t, [][]string{
			{"1", "Alpha", "alpha", "[]"},
			{"2", "Setup steps", "setup", "['Alpha']"},
			{"3", "Notes", "notes", "['Alpha','Setup steps']"},
			{"2", "Status", "status", "['Alpha']"},
		}, got)

		// A `[[Alpha#Setup steps]]` resolves to a heading row by slug.
		hit := conn.rows(t, ex.run(t, `
SELECT count()
FROM (SELECT LW_COMPONENT('MdLink') AS l FROM boxer.facts) AS ls
JOIN (SELECT LW_COMPONENT('MdHeading') AS h FROM boxer.facts) AS hs
  ON lower(h.Text) = lower(l.Fragment) OR h.Slug = lower(replaceAll(l.Fragment, ' ', '-'))
WHERE l.Fragment != '' AND l.Spelling IN ('wikilink', 'embed')`))
		assert.Equal(t, [][]string{{"2"}}, hit,
			"the wikilink and the embed to Setup steps resolve by heading text; the heading's slug is its explicit anchor")

		// Items know the heading they sit under.
		code := conn.rows(t, ex.run(t, `
SELECT c.Language, h.Text
FROM (SELECT LW_COMPONENT('MdCodeBlock') AS c FROM boxer.facts) AS cs
JOIN (SELECT LW_COMPONENT('MdHeading') AS h FROM boxer.facts) AS hs
  ON h.Doc = c.Doc AND h.Ordinal = c.Section
ORDER BY c.Language`))
		assert.Equal(t, [][]string{{"go", "Setup steps"}, {"sql", "Reading list"}}, code)
	})

	t.Run("emphasis", func(t *testing.T) {
		got := conn.rows(t, ex.run(t, `
SELECT e.Style, arraySort(groupArray(e.Text))
FROM (SELECT LW_COMPONENT('MdEmphasis') AS e FROM boxer.facts)
GROUP BY e.Style ORDER BY e.Style`))
		assert.Equal(t, [][]string{
			{"bold", "['bold','key','vault']"},
			{"highlight", "['highlighted']"},
			{"italic", "['Italic','key','setup']"},
			{"strikethrough", "['struck']"},
		}, got)
	})

	// The frontmatter row: leaves read by path through the mixed channel.
	// `param:` names the path — the low-cardinality half of the address —
	// and every *Array section reads through LW_GET_LIST.
	t.Run("frontmatter leaves", func(t *testing.T) {
		const fm = "WHERE LW_GET('symbol', 'mdFrontmatterKind', 'chan:low-card-ref') = 'mdFrontmatter'"
		got := conn.rows(t, ex.run(t, `
SELECT LW_GET_LIST('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/title')[1] AS title,
       LW_GET_LIST('f64Array', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/rating')[1] AS rating,
       LW_GET('bool', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/draft') AS draft,
       LW_GET_LIST('symbolArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/notes')[1] AS notes,
       LW_GET_LIST('symbolArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/extra')[1] AS extra,
       toString(LW_GET_LIST('timeArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/created')[1]) AS created
FROM boxer.facts `+fm+`
  AND LW_GET_LIST('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/title')[1] != ''`))
		assert.Equal(t, [][]string{{"Vault Index", "4.5", "false", "null", "{}", "2024-03-01 00:00:00.000000000"}}, got,
			"a YAML date lands in timeArray, midnight UTC")

		// A list property: every attribute under the template, with the
		// elided index alongside, through the selector pair.
		lists := conn.rows(t, ex.run(t, `
SELECT LW_CO_GATHER("stringArray:value", LW_SEL_ATTRS('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/tags/_')) AS tags
FROM boxer.facts `+fm+` AND length(tags) > 0 ORDER BY tags`))
		assert.Equal(t, [][]string{{"['hub','meta/structure']"}}, lists,
			"index.md's list; alpha.md's comma-string tags is a scalar at /tags")

		scalar := conn.rows(t, ex.run(t, `
SELECT LW_GET_LIST('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/tags')[1] AS tags,
       LW_GET_LIST('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/status')[1] AS status
FROM boxer.facts `+fm+` AND status != ''`))
		assert.Equal(t, [][]string{{"project, alpha", "active"}}, scalar)

		// Nested lists: the params lane carries both indices.
		nested := conn.rows(t, ex.run(t, `
SELECT arrayMap((p, a) -> ("stringArray:mrhp"[p], "stringArray:value"[a]),
                LW_SEL('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/reviewers/_/roles/_'),
                LW_SEL_ATTRS('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/reviewers/_/roles/_')) AS roles
FROM boxer.facts `+fm+` AND length(roles) > 0`))
		require.Len(t, nested, 1)
		assert.Contains(t, nested[0][0], "'/reviewers/_/roles/_'", "the path lane")
		assert.Contains(t, nested[0][0], "'lead'")
		assert.Contains(t, nested[0][0], "'editor'")

		// The broken block is present with no leaves; documents without a
		// block have no row.
		count := conn.rows(t, ex.run(t, "SELECT count() FROM boxer.facts "+fm))
		assert.Equal(t, [][]string{{"3"}}, count, "index, alpha, broken-frontmatter")
	})
}

func itoa(n int) (s string) {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
