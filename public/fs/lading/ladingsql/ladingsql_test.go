package ladingsql_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testMount is the id the goldens are written against — 0x3BFE363BCF148001,
// spelled in decimal in the SQL below because that is how a macro call carries
// it. Opaque: the store never interprets one (ADR-0198 §SD3).
const testMount identifier.TaggedId = 4322952322827452417

func openCfg() ladingsql.Config {
	return ladingsql.Config{Visibility: ladingsql.VisibleAll{}}
}

func expandOK(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := ladingsql.Expand(openCfg(), sql)
	require.NoErrorf(t, err, "expand %q", sql)
	return
}

// TestPassthroughWithoutMacro — a statement naming neither macro is not
// rewritten at all, byte for byte. It is what makes the pass safe to put in
// front of every query rather than only the ones that use it.
func TestPassthroughWithoutMacro(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1 FROM system.one",
		"SELECT * FROM boxer.fsmeta",
		"SELECT fs FROM t",
	} {
		assert.Equal(t, sql, expandOK(t, sql), "macro-free SQL must pass through byte-identical")
	}
}

// TestExpansionCarriesTheThreeInvariants. Each of these is a rule the store
// depends on and none of them is visible in the query an author writes.
func TestExpansionCarriesTheThreeInvariants(t *testing.T) {
	out := expandOK(t, "SELECT path FROM fs(4322952322827452417)")

	// The completeness rule: the newest snapshot comes off the index, which
	// holds only committed root rows.
	assert.Contains(t, out, "FROM boxer.fssnap", "the latest snapshot must be resolved from the index")
	assert.Contains(t, out, "max(", "and it is the newest of them")

	// The logical cutoff, on the same column the TTL names, in the outer read
	// and in the index sub-select alike.
	assert.GreaterOrEqual(t, strings.Count(out, `"lc:expiresAt:z64:4::0:" > now64(9, 'UTC')`), 2,
		"every read of either table must carry the cutoff")

	// The pinning: a mount id and nothing else.
	assert.Contains(t, out, `"id:id:u64:47::0:" = 4322952322827452417`)

	// And no macro call survives, which is what makes the pass idempotent.
	assert.NotContains(t, strings.ToLower(out), "fs(")
	_, err := nanopass.Parse(out)
	require.NoError(t, err, "the expansion must re-parse:\n%s", out)
}

// TestSnapshotSelection — the three spellings of "which snapshot".
func TestSnapshotSelection(t *testing.T) {
	latest := expandOK(t, "SELECT path FROM fs(4322952322827452417)")
	assert.Contains(t, latest, "max(", "no snapshot means the newest complete one")

	all := expandOK(t, "SELECT path FROM fs(4322952322827452417, '*')")
	assert.Contains(t, all, `"ts:ts:z64:47::0:" IN (SELECT`, "'*' means every complete snapshot")
	assert.NotContains(t, all, "max(")

	pinned := expandOK(t, "SELECT path FROM fs(4322952322827452417, '2026-08-20 01:02:03.5')")
	assert.Contains(t, pinned, "toDateTime64('2026-08-20 01:02:03.5', 9, 'UTC')",
		"a string snapshot is a datetime literal")

	// A number is Unix nanoseconds, and must NOT go through toDateTime64:
	// that reads a plain number as seconds whatever the scale says, so
	// nanoseconds saturate to 2262 and the predicate matches nothing.
	nanos := expandOK(t, "SELECT path FROM fs(4322952322827452417, 1755000000123456789)")
	assert.Contains(t, nanos, "fromUnixTimestamp64Nano(toInt64(1755000000123456789), 'UTC')")
	assert.NotContains(t, nanos, "toDateTime64(1755000000123456789")
}

// TestMountIdSpellings. A 19-digit literal is awkward to type and hex is how
// these ids are usually read, so both are accepted — and both mean the same
// number.
func TestMountIdSpellings(t *testing.T) {
	want := expandOK(t, "SELECT path FROM fs(4322952322827452417)")
	for _, spelling := range []string{
		"fs('4322952322827452417')",
		"fs('0x3BFE363BCF148001')",
		"fs('0x3bfe363bcf148001')",
		// Bare, unquoted — the spelling the how-to shows, because a hex
		// literal is how these ids are usually read.
		"fs(0x3BFE363BCF148001)",
	} {
		got := expandOK(t, "SELECT path FROM "+spelling)
		assert.Equalf(t, want, got, "%s must expand identically", spelling)
	}
}

// TestTheColumnsAreLogicalNames. The point of the macro: an author writes the
// names the design uses, not the physical leeway columns.
func TestTheColumnsAreLogicalNames(t *testing.T) {
	entries := expandOK(t, "SELECT * FROM fs(4322952322827452417)")
	for _, col := range []string{
		"AS mount", "AS path", "AS snap", "AS node_kind", "AS content", "AS mode",
		"AS block_size", "AS blocks", "AS size", "AS mtime", "AS link_target",
		"AS err", "AS content_hash", "AS text", "AS is_dir", "AS is_symlink",
	} {
		assert.Containsf(t, entries, col, "fs() must expose %q", col)
	}
	// The tree columns come off the materialised ones rather than being
	// recomputed per query.
	for _, col := range []string{"name", "dir", "depth", "ext"} {
		assert.Containsf(t, entries, col, "fs() must expose %q", col)
	}

	blocks := expandOK(t, "SELECT * FROM fsdata(4322952322827452417)")
	for _, col := range []string{"AS path", "AS seq", "AS data", "AS hash", "AS line0"} {
		assert.Containsf(t, blocks, col, "fsdata() must expose %q", col)
	}
	// path and seq are decoded from the natural key, which is where the block
	// ordinal lives.
	assert.Contains(t, blocks, "length(nk) - 5", "path is the key minus the five-byte ordinal suffix")
	assert.Contains(t, blocks, "reinterpretAsUInt32(reverse(", "and the ordinal is big-endian")
}

// TestBothMacrosInOneStatement — the shape every §7 join query has.
func TestBothMacrosInOneStatement(t *testing.T) {
	out := expandOK(t, `SELECT e.path, d.line0 FROM fs(4322952322827452417) AS e
		JOIN fsdata(4322952322827452417) AS d ON e.path = d.path`)
	assert.Contains(t, out, "boxer.fsmeta")
	assert.Contains(t, out, "boxer.fsdata")
	_, err := nanopass.Parse(out)
	require.NoError(t, err)
}

// TestRefusals. Each of these reaches a server as a confusing error, or as
// silently wrong rows, if it is not refused here.
func TestRefusals(t *testing.T) {
	for name, sql := range map[string]string{
		"no arguments":       "SELECT * FROM fs()",
		"too many arguments": "SELECT * FROM fs(1, '*', 3)",
		"a name, not an id":  "SELECT * FROM fs('my-mount')",
		"an expression":      "SELECT * FROM fs(1 + 1)",
		"a zero id":          "SELECT * FROM fs(0)",
	} {
		_, err := ladingsql.Expand(openCfg(), sql)
		assert.Errorf(t, err, "%s must be refused at expansion", name)
	}

	// The honest limit of a text-level check: a string snapshot is spliced as a
	// datetime literal and whether it *is* one is ClickHouse's judgement, made
	// at execution. Expansion does not second-guess it, and pretending
	// otherwise here would mean carrying a datetime parser that has to agree
	// with the server's.
	out, err := ladingsql.Expand(openCfg(), "SELECT * FROM fs(4322952322827452417, 'yesterday')")
	require.NoError(t, err)
	assert.Contains(t, out, "toDateTime64('yesterday', 9, 'UTC')")
}

// TestVisibilityIsCheckedAtExpansion. A mount the caller may not read is
// refused, not filtered: a filter answers "no rows" where a refusal answers
// "not yours", and only the second is honest about a mount that exists.
func TestVisibilityIsCheckedAtExpansion(t *testing.T) {
	const sql = "SELECT path FROM fs(4322952322827452417)"

	// The default refuses everything: a capability check that defaults to open
	// is not one.
	_, err := ladingsql.Expand(ladingsql.Config{}, sql)
	assert.Error(t, err, "a nil visibility must refuse")

	_, err = ladingsql.Expand(ladingsql.Config{Visibility: ladingsql.VisibleSet{}}, sql)
	assert.Error(t, err, "an empty set must refuse")

	_, err = ladingsql.Expand(ladingsql.Config{
		Visibility: ladingsql.VisibleSet{testMount: {}},
	}, sql)
	assert.NoError(t, err, "the listed mount is visible")

	_, err = ladingsql.Expand(ladingsql.Config{
		Visibility: ladingsql.VisibleSet{testMount: {}},
	}, "SELECT path FROM fs(4322952322827452418)")
	assert.Error(t, err, "a neighbouring id is a different mount")

	// The tag shape: every id an owner minted, without a set to maintain.
	byTag := ladingsql.VisibleUnderTag{testMount.GetTag()}
	_, err = ladingsql.Expand(ladingsql.Config{Visibility: byTag}, sql)
	assert.NoError(t, err)
	_, err = ladingsql.Expand(ladingsql.Config{Visibility: byTag},
		"SELECT path FROM fs(1)")
	assert.Error(t, err, "an id under another tag is another owner's")
}

// TestReferences states a fact about the SQL and nothing more — it is what a
// dispatcher routes on, and it must never error.
func TestReferences(t *testing.T) {
	got := ladingsql.References(
		`SELECT * FROM fs(4322952322827452417) JOIN fsdata(4322952322827452417) USING path`)
	assert.Equal(t, []identifier.TaggedId{testMount}, got, "deduplicated, in first-appearance order")

	assert.Nil(t, ladingsql.References("SELECT 1"))
	assert.Nil(t, ladingsql.References("this is not sql"))
	assert.Nil(t, ladingsql.References("SELECT * FROM fs('nope')"), "a malformed call is not a reference")
	assert.Nil(t, ladingsql.References("SELECT fs(1)"), "a scalar call is not a table reference")
}

// TestClassifiedAsALocalRead. The macros must classify like `keelson()` — a
// local read — or applet SQL naming a mount would be refused as egress
// (ADR-0132 §SD5).
func TestClassifiedAsALocalRead(t *testing.T) {
	for _, sql := range []string{
		"SELECT path FROM fs(4322952322827452417)",
		"SELECT data FROM fsdata(4322952322827452417)",
	} {
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err)
		class, witnesses, cerr := analysis.ClassifyQuerySecurity(pr)
		require.NoError(t, cerr)
		assert.Equalf(t, analysis.QuerySecurityRead, class, "%s must classify as a local read", sql)
		assert.Emptyf(t, witnesses, "%s must produce no egress witness: %v", sql, witnesses)
	}
}

// TestIdempotentOverTheCorpus is the machine-checked half of the pass's
// declared properties.
func TestPassProperties(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	corpus := make([]string, 0, len(entries)+2)
	for _, e := range entries {
		corpus = append(corpus, e.SQL)
	}
	corpus = append(corpus,
		"SELECT path FROM fs(4322952322827452417)",
		"SELECT data FROM fsdata(4322952322827452417, '*')")
	nanopass.AssertProperties(t, ladingsql.ExpandPass(openCfg()), corpus)
}

// TestCorpusStaysValid — the pass must not damage a statement it has no
// business touching.
func TestCorpusStaysValid(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	for _, e := range entries {
		out, xerr := ladingsql.Expand(openCfg(), e.SQL)
		require.NoErrorf(t, xerr, "corpus entry %s", e.Name)
		assert.Equalf(t, e.SQL, out, "corpus entry %s must be untouched", e.Name)
	}
}

// TestExpansionGoldens pins the emitted SQL so a change to it is visible in
// review rather than only in a downstream failure.
//
// Regenerate with BOXER_LADINGSQL_GOLDEN_REGEN=1.
func TestExpansionGoldens(t *testing.T) {
	cases := map[string]string{
		"entries-latest": "SELECT path, size FROM fs(4322952322827452417) WHERE NOT is_dir ORDER BY size DESC LIMIT 20",
		"entries-all":    "SELECT snap, path FROM fs(4322952322827452417, '*') ORDER BY snap",
		"entries-pinned": "SELECT path FROM fs(4322952322827452417, '2026-08-20 01:02:03')",
		"blocks-latest":  "SELECT path, line0, data FROM fsdata(4322952322827452417)",
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ladingsql.Expand(openCfg(), sql)
			require.NoError(t, err)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "the golden must be parseable SQL")

			path := filepath.Join("testdata", name+".golden.sql")
			if os.Getenv("BOXER_LADINGSQL_GOLDEN_REGEN") != "" {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got+"\n"), 0o644))
				t.Skip("golden rewritten; unset BOXER_LADINGSQL_GOLDEN_REGEN to compare")
			}
			want, rerr := os.ReadFile(path)
			require.NoErrorf(t, rerr, "missing golden; regenerate with BOXER_LADINGSQL_GOLDEN_REGEN=1")
			assert.Equal(t, strings.TrimRight(string(want), "\n"), got)
		})
	}
}
