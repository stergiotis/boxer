//go:build integration

package anchor

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stretchr/testify/require"
)

// snippetsDocPath is play's snippet library, which documents THIS fixture:
// its blocks teach column handles, LW_GET extraction and the LW_ pack against
// anchor.facts. play's own gate (TestHelpCorpusSQLBlocksParse) proves the
// blocks parse; it cannot see a renamed section, a wrong chan:/col: token, a
// handle that no longer resolves, or fixture data drifting out from under the
// prose's row counts. This test pins those, from the side that owns the
// schema and the demo rows.
const snippetsDocPath = "../../../../apps/play/help/snippets.md"

// snippetsMinLeewayBlocks is a drift floor: the corpus currently carries 16
// leeway-flavoured blocks, and a sweep that suddenly selects far fewer means
// the sections were deleted or the selection heuristic broke — both worth a
// loud failure rather than a green run over nothing.
const snippetsMinLeewayBlocks = 12

// snippetSqlFences extracts the ```sql blocks of a markdown document with
// their 1-based content start lines. Twin of play's sqlFences (unexported
// there); deliberately literal, matching only the fence opener the corpus
// uses.
func snippetSqlFences(md string) (out []struct {
	line int
	sql  string
}) {
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
		out = append(out, struct {
			line int
			sql  string
		}{line: start, sql: strings.Join(body, "\n")})
	}
	return
}

// snippetSetParamRe harvests a `SET param_x = 'v';` prelude line the way
// play's ExtractParams does — grammar1 is single-statement, and the harvested
// values ride the request as `param_<name>` bindings.
var snippetSetParamRe = regexp.MustCompile(`(?m)^SET\s+param_(\w+)\s*=\s*'([^']*)'\s*;\s*\n`)

// snippetPlaceholderRe matches a `{name:Type}` server-side binding. The
// leading identifier keeps a `{` inside a JSON string literal from counting.
var snippetPlaceholderRe = regexp.MustCompile(`\{[A-Za-z_]\w*:`)

// TestSnippetsAgainstFixture seeds the fixture exactly as TestLeewayClickHouse
// does (DDL, surface install, three demo domains), then drives every
// leeway-flavoured snippet block through the client pass chain play applies —
// resolve handles, expand LW_GET, expand constructors — and executes the
// result. QualifyTables is deliberately absent from the chain: play does not
// qualify, so the snippets must carry their own `anchor.facts`.
//
// Blocks that keep an unbound `{name:Type}` placeholder (play's signals) stop
// after the pipeline; the keelson('…') blocks are not selected at all, since
// those tables exist only in-process, never on a raw server.
func TestSnippetsAgainstFixture(t *testing.T) {
	ctx := context.Background()
	ch := newAnchorChClient()
	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not available on %s, skipping test: %v", chclient.ConfigFromEnv().URL, err)
	}

	require.NoError(t, setupClickHouseDdl(ctx, ch))
	require.NoError(t, lwsqlsurface.Install(ctx, ch))
	records, err := GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = GenerateDroneMissionEvents(records)
	require.NoError(t, err)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	require.NoError(t, ch.InsertArrow(ctx, "anchor.facts", records))

	raw, err := os.ReadFile(snippetsDocPath)
	require.NoError(t, err)

	resolver := NewDqlResolver()
	var diags []passes.ColumnDiagnostic
	chain := nanopass.Sequence("SnippetsProbe",
		passes.StripComments,
		passes.CanonicalizeFull(100),
		passes.ResolveColumnNames(resolver, "anchor", func(d passes.ColumnDiagnostic) { diags = append(diags, d) }),
		constructsql.ExtractExpandPass(resolver, "anchor"),
		constructsql.ExpandPass,
	)

	nSelected := 0
	for _, f := range snippetSqlFences(string(raw)) {
		if !strings.Contains(f.sql, "anchor.facts") && !strings.Contains(f.sql, "LW_") {
			continue
		}
		nSelected++

		params := map[string]string{}
		sql := snippetSetParamRe.ReplaceAllStringFunc(f.sql, func(m string) string {
			sub := snippetSetParamRe.FindStringSubmatch(m)
			params[sub[1]] = sub[2]
			return ""
		})

		diags = diags[:0]
		expanded, perr := chain.Run(sql)
		require.NoErrorf(t, perr, "snippets.md:%d: pipeline rejects block\n%s", f.line, sql)
		require.Emptyf(t, diags, "snippets.md:%d: unresolved handles %v\n%s", f.line, diags, sql)
		require.NotContainsf(t, strings.ToUpper(expanded), "LW_GET", "snippets.md:%d: extraction did not expand", f.line)
		require.NotContainsf(t, strings.ToUpper(expanded), "LW_PLAIN", "snippets.md:%d: constructor did not expand", f.line)

		if snippetPlaceholderRe.MatchString(expanded) && len(params) == 0 {
			continue // signals block: nothing binds the placeholder here
		}

		body, qerr := ch.QueryParams(ctx, expanded+"\nFORMAT TabSeparatedWithNames", params)
		require.NoErrorf(t, qerr, "snippets.md:%d: server rejected\n%s", f.line, expanded)
		b, rerr := io.ReadAll(body)
		_ = body.Close()
		require.NoError(t, rerr)
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		dataRows := len(lines) - 1 // header

		// Value pins, keyed by content rather than position so reordering
		// sections never misattributes an assertion. The expected numbers
		// derive from the generators: cyber mints 20 incidents with
		// PORT_SCAN/22 on odd i except i%5==0 (⇒ 8), DDOS/53 on i ∈ {5,15}
		// (⇒ 2), and ASN 3356+i on geoPoint (⇒ 3360 is i=4, one row).
		switch {
		case strings.Contains(f.sql, "event_on_port_22"):
			require.Equalf(t, 8, dataRows, "snippets.md:%d", f.line)
			for _, ln := range lines[1:] {
				require.Contains(t, ln, "PORT_SCAN")
			}
		case strings.Contains(f.sql, "origin_lat"):
			require.Equalf(t, 1, dataRows, "snippets.md:%d", f.line)
			require.Contains(t, lines[1], "INC-2026-CH-4")
			require.Contains(t, lines[1], "56.15")
			require.Contains(t, lines[1], "37.21")
		case strings.Contains(f.sql, "co_lookup"):
			require.Equalf(t, 1, dataRows, "snippets.md:%d", f.line)
			require.Contains(t, lines[1], "[[10,20],[30,40,50],[60]]")
			require.Contains(t, lines[1], "[30,120,60]")
			require.Contains(t, lines[1], "[0,1,1]")
			require.Contains(t, lines[1], "68.1")
		case strings.Contains(f.sql, "LW_SURFACE_VERSION"):
			require.Equalf(t, "1", strings.TrimSpace(lines[1]), "snippets.md:%d", f.line)
		case strings.Contains(f.sql, "'attack-count'"):
			require.Containsf(t, lines[0], "attack-count", "snippets.md:%d: minted physical name must be the result column", f.line)
			require.Equalf(t, 60, dataRows, "snippets.md:%d", f.line)
		case strings.Contains(f.sql, "{event:String}"):
			require.Equalf(t, 2, dataRows, "snippets.md:%d: DDOS incidents", f.line)
		case strings.Contains(f.sql, "`id:id` = 10005"):
			require.Equalf(t, 1, dataRows, "snippets.md:%d", f.line)
		case strings.Contains(f.sql, "_tl_time"):
			require.Equalf(t, 60, dataRows, "snippets.md:%d: every entity carries timeRange", f.line)
		}
	}
	require.GreaterOrEqual(t, nSelected, snippetsMinLeewayBlocks,
		"the sweep selected suspiciously few leeway-flavoured snippet blocks")
}
