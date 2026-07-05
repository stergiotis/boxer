package identsql

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
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
	for i := 0; i < 200; i++ {
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
	for i := 0; i < 500; i++ {
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

func chLocalIn(t *testing.T, dir string, script string) (out string) {
	t.Helper()
	cmd := exec.Command("clickhouse", "local", "-n", "--query", script)
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
