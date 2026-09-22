package identsql

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// Server-truth golden lock (ADR-0106 SD2/SD5): the Go split in the identifier
// package and the SQL split produced here MUST agree bit for bit. The corpus
// spans every uint32 tag-width class, the fibonacci boundaries, raw random
// uint64s and structured invalids; the SQL side is evaluated by a real
// ClickHouse both through the macro expansion pass and through the emitted
// UDFs. Skips when no `clickhouse` binary is on PATH.

func requireClickhouse(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse binary not on PATH — server-truth tests skipped")
	}
}

type goldenRow struct {
	id       uint64
	valid    uint8
	width    uint16
	tagBits  uint64
	body     uint64
	tagValue uint32
}

// goTruth derives every expectation from the identifier package — the Go half
// of the SD2 contract; nothing here re-implements the split.
func goTruth(id uint64) (r goldenRow) {
	tid := identifier.TaggedId(id)
	tag, body := tid.Split()
	r = goldenRow{
		id:       id,
		width:    uint16(tid.GetTagWidth()),
		tagBits:  uint64(tag),
		body:     uint64(body),
		tagValue: uint32(tag.GetValue()),
	}
	if tid.IsValid() {
		r.valid = 1
	}
	return
}

func goldenCorpus() (rows []goldenRow) {
	rnd := rand.New(rand.NewPCG(0x0106, 4))
	tagValues := make([]uint64, 0, 520)
	for tv := uint64(1); tv <= 300; tv++ {
		tagValues = append(tagValues, tv)
	}
	tagValues = append(tagValues, 2971215072, 2971215073, 2971215074, // width 46/47 boundary
		math.MaxUint32-1, math.MaxUint32)
	for range 200 {
		tagValues = append(tagValues, 1+rnd.Uint64N(math.MaxUint32))
	}

	rows = make([]goldenRow, 0, len(tagValues)*4+600)
	for _, tv := range tagValues {
		tag := identifier.TagValue(tv).GetTag()
		maxBody := uint64(tag.GetMaxPossibleIdIncl())
		bodies := []uint64{0, 1, maxBody}
		if maxBody > 1 {
			bodies = append(bodies, 1+rnd.Uint64N(maxBody))
		}
		for _, b := range bodies {
			rows = append(rows, goTruth(uint64(tag)|b))
		}
	}
	// Structured invalids and wide-tag adversarials.
	for _, raw := range []uint64{0, 1, 0b101, 0b11, 0x5555555555555555, 0xAAAAAAAAAAAAAAAA,
		1 << 63, 1<<63 | 0b11, math.MaxUint64} {
		rows = append(rows, goTruth(raw))
	}
	// Raw random uint64s: the Go methods define the truth for anything.
	for range 500 {
		rows = append(rows, goTruth(rnd.Uint64()))
	}
	return
}

func writeGoldenCsv(t *testing.T, dir string, rows []goldenRow) {
	t.Helper()
	var sb strings.Builder
	sb.Grow(len(rows) * 64)
	sb.WriteString("id,expValid,expWidth,expTagBits,expBody,expTagValue\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "%d,%d,%d,%d,%d,%d\n", r.id, r.valid, r.width, r.tagBits, r.body, r.tagValue)
	}
	err := os.WriteFile(filepath.Join(dir, "golden.csv"), []byte(sb.String()), 0o600)
	require.NoError(t, err)
}

// checkQuery uses the unexpanded LW_ID_* names; it is either macro-expanded
// or run against the UDFs verbatim. Every countIf must be zero.
const checkQuery = `SELECT
  count() AS n,
  countIf(toUInt8(LW_ID_IS_VALID(id)) != expValid) AS badValid,
  countIf(LW_ID_TAG_WIDTH(id) != expWidth) AS badWidth,
  countIf(LW_ID_TAG_BITS(id) != expTagBits) AS badTagBits,
  countIf(LW_ID_BODY(id) != expBody) AS badBody,
  countIf(LW_ID_TAG_VALUE(id) != expTagValue) AS badTagValue,
  countIf(LW_ID_HAS_TAG(id, expTagValue) != (expTagValue != 0)) AS badHasTagGeneric,
  countIf(LW_ID_HAS_TAG(id, 7) != (expTagValue = 7)) AS badHasTag7,
  countIf(LW_ID_HAS_TAG(id, 4294967295) != (expTagValue = 4294967295)) AS badHasTagMax
FROM file('golden.csv', 'CSVWithNames', 'id UInt64, expValid UInt8, expWidth UInt16, expTagBits UInt64, expBody UInt64, expTagValue UInt32')`

func chLocalIn(t *testing.T, dir string, script string, extraArgs ...string) (out string) {
	t.Helper()
	cmd := exec.Command("clickhouse", append([]string{"local", "-n", "--query", script}, extraArgs...)...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	require.NoError(t, err, "clickhouse local failed:\n%s\nscript:\n%s", string(b), script)
	out = strings.TrimRight(string(b), "\n")
	return
}

func requireAllZeroAfterCount(t *testing.T, out string, wantRows int) {
	t.Helper()
	fields := strings.Split(out, "\t")
	require.Len(t, fields, 9)
	require.Equal(t, fmt.Sprintf("%d", wantRows), fields[0], "row count")
	for i, f := range fields[1:] {
		require.Equal(t, "0", f, "mismatch column %d of %s", i+1, out)
	}
}

func TestServerTruth_MacroExpansion(t *testing.T) {
	requireClickhouse(t)
	dir := t.TempDir()
	rows := goldenCorpus()
	writeGoldenCsv(t, dir, rows)

	expanded, err := ExpandPass.Run(checkQuery)
	require.NoError(t, err)
	require.NotContains(t, strings.ToUpper(expanded), "LW_ID_")

	// The server's own parser must accept the expansion.
	fcmd := exec.Command("clickhouse", "format", "-n")
	fcmd.Stdin = strings.NewReader(expanded)
	fout, ferr := fcmd.CombinedOutput()
	require.NoError(t, ferr, "clickhouse format rejected the expansion:\n%s", string(fout))

	out := chLocalIn(t, dir, expanded)
	requireAllZeroAfterCount(t, out, len(rows))
}

func TestServerTruth_Udfs(t *testing.T) {
	requireClickhouse(t)
	dir := t.TempDir()
	rows := goldenCorpus()
	writeGoldenCsv(t, dir, rows)

	script := strings.Join(UdfDdlStatements(), ";\n") + ";\n" + checkQuery
	out := chLocalIn(t, dir, script)
	requireAllZeroAfterCount(t, out, len(rows))
}

// The pruning lock for the UDF form of LW_ID_HAS_TAG: on a MergeTree keyed
// by id, a literal tag value must reach the primary-key analysis as a
// constant — `intDiv(id, <comma bit>) in [<code>, <code>]` — and read the
// same granules the macro's BETWEEN reads; a column tag value must fall back
// to a full scan without erroring; two calls in one query, and aliases in the
// outer query that reuse the body's names, must not collide. Thirty tags of
// fifty thousand ids each give every tag several granules at the default
// granularity.

var explainGranulesRe = regexp.MustCompile(`Granules: (\d+)/(\d+)`)

func explainSelectedTotal(t *testing.T, explain string) (selected int, total int) {
	t.Helper()
	m := explainGranulesRe.FindStringSubmatch(explain)
	require.NotNil(t, m, "no 'Granules: a/b' line in:\n%s", explain)
	selected, _ = strconv.Atoi(m[1])
	total, _ = strconv.Atoi(m[2])
	return
}

func TestServerTruth_UdfHasTagPrunes(t *testing.T) {
	requireClickhouse(t)
	dir := t.TempDir()

	tagValues := []uint64{1, 2, 3, 5, 7, 12, 13, 20, 21, 33, 34, 50, 54, 55, 88, 89, 100, 144, 233, 377,
		610, 987, 1597, 2584, 4181, 6765, 10946, 65536, 1000000, math.MaxUint32}
	firstIds := make([]string, 0, len(tagValues))
	for _, tv := range tagValues {
		firstIds = append(firstIds, fmt.Sprintf("%d", uint64(identifier.TagValue(tv).GetTag())))
	}
	const bodiesPerTag = 50000 // every uint32 tag keeps at least 2^17 - 1 bodies
	macroBetween, err := ExpandPass.Run("SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, 12)")
	require.NoError(t, err)

	const sep = "SELECT '=====';"
	script := strings.Join(UdfDdlStatements(), ";\n") + ";\n" +
		"CREATE TABLE t (id UInt64, v UInt8) ENGINE = MergeTree ORDER BY id SETTINGS index_granularity = 8192;\n" +
		"INSERT INTO t SELECT lo + number AS id, 1 FROM (SELECT arrayJoin([" + strings.Join(firstIds, ",") + "]::Array(UInt64)) AS lo) AS tags " +
		fmt.Sprintf("CROSS JOIN numbers(%d) AS bodies;\n", bodiesPerTag) +
		"OPTIMIZE TABLE t FINAL;\n" +
		"EXPLAIN indexes = 1 SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, 12);\n" + sep +
		"EXPLAIN indexes = 1 " + macroBetween + ";\n" + sep +
		"EXPLAIN indexes = 1 SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, 12) OR LW_ID_HAS_TAG(id, 4294967295);\n" + sep +
		"EXPLAIN indexes = 1 SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, v * 12);\n" + sep +
		"SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, 12);\n" +
		"SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, 12) OR LW_ID_HAS_TAG(id, 4294967295);\n" +
		"SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, v * 12);\n" +
		"SELECT count() FROM (SELECT 5 AS _lw_div, 6 AS _lw_code, 7 AS _lw_r2, LW_ID_HAS_TAG(id, 12) AS h FROM t) WHERE h;\n" +
		"SELECT count() FROM t WHERE LW_ID_HAS_TAG(id, 0) OR LW_ID_HAS_TAG(id, 4294967296);\n"
	out := chLocalIn(t, dir, script, "--path", filepath.Join(dir, "ch"))
	parts := strings.Split(out, "=====")
	require.Len(t, parts, 5, "unexpected output shape:\n%s", out)

	udfSel, udfTotal := explainSelectedTotal(t, parts[0])
	macroSel, macroTotal := explainSelectedTotal(t, parts[1])
	require.Equal(t, macroTotal, udfTotal)
	require.Less(t, udfSel, udfTotal, "the UDF form did not prune:\n%s", parts[0])
	require.Equal(t, macroSel, udfSel, "the UDF form reads other granules than the macro's BETWEEN:\n%s\n%s", parts[0], parts[1])
	// Tag value 12 is the code 101011: 43 above a 58-bit body, so the comma
	// bit is 2^58 — the folded constants the key analysis must have seen.
	require.Contains(t, parts[0], "intDiv(id, 288230376151711744) in [43, 43]", parts[0])
	// intDiv over an unsigned key with a positive constant reports itself
	// always monotonic, which keeps the mark binary search available.
	require.Contains(t, parts[0], "Search Algorithm: binary search", parts[0])

	orSel, orTotal := explainSelectedTotal(t, parts[2])
	require.Less(t, orSel, orTotal, "an OR of two tags did not prune:\n%s", parts[2])
	require.Greater(t, orSel, udfSel)

	colSel, colTotal := explainSelectedTotal(t, parts[3])
	require.Equal(t, colTotal, colSel, "a column tag value must scan every granule:\n%s", parts[3])

	counts := strings.Split(strings.TrimSpace(parts[4]), "\n")
	require.Equal(t, []string{
		fmt.Sprintf("%d", bodiesPerTag),
		fmt.Sprintf("%d", 2*bodiesPerTag),
		fmt.Sprintf("%d", bodiesPerTag),
		fmt.Sprintf("%d", bodiesPerTag),
		"0",
	}, counts, "row counts:\n%s", parts[4])
}
