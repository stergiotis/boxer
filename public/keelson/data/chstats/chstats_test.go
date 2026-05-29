//go:build llm_generated_opus47

package chstats_test

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/keelson/data/chstats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLVQuantilesEmpty(t *testing.T) {
	require.Nil(t, chstats.LVQuantiles(0))
}

func TestLVQuantilesDepthLadder(t *testing.T) {
	// depth 1 → just median
	assert.Equal(t, []float64{0.5}, chstats.LVQuantiles(1))
	// depth 2 → median + quartiles
	assert.Equal(t, []float64{0.5, 0.25, 0.75}, chstats.LVQuantiles(2))
	// depth 3 → + octiles
	assert.Equal(t, []float64{0.5, 0.25, 0.75, 0.125, 0.875}, chstats.LVQuantiles(3))
}

func TestLVQuantilesDeduplicates(t *testing.T) {
	// Depth 1's 0.5 must not appear twice — every subsequent depth
	// emits a distinct pair (2^-k, 1-2^-k) but never 0.5 again.
	qs := chstats.LVQuantiles(8)
	seen := map[float64]int{}
	for _, q := range qs {
		seen[q]++
	}
	for q, c := range seen {
		assert.Equal(t, 1, c, "quantile %v repeated", q)
	}
}

func TestLVQuantilesClampsAtMaxDepth(t *testing.T) {
	// MaxDepth = 16 → at most 1 + 2*15 = 31 distinct quantiles.
	assert.Len(t, chstats.LVQuantiles(255), 1+2*int(letterval.MaxDepth-1))
}

func TestBuildLVSelectShape(t *testing.T) {
	require.Equal(t, "", chstats.BuildLVSelect("latency_ms", 0))

	got := chstats.BuildLVSelect("latency_ms", 2)
	assert.Equal(t, "quantilesTDigest(0.5, 0.25, 0.75)(latency_ms)", got)

	// Deeper depths emit more quantile args.
	got3 := chstats.BuildLVSelect(`"col with space"`, 3)
	assert.Equal(t,
		`quantilesTDigest(0.5, 0.25, 0.75, 0.125, 0.875)("col with space")`,
		got3)
}

func TestLevelsFromArrayStructure(t *testing.T) {
	// At depth 3, LVQuantiles emits [0.5, 0.25, 0.75, 0.125, 0.875].
	// Stub a CH-returned array with known values per position.
	arr := []float64{10, 5, 15, 2, 18}
	const n = int64(1000)
	lvs := chstats.LevelsFromArray(arr, n, 3)
	require.Len(t, lvs, 3)

	// Depth 1: median.
	assert.Equal(t, uint8(1), lvs[0].Depth)
	assert.InDelta(t, 10.0, lvs[0].LowerValue, 1e-12)
	assert.InDelta(t, 10.0, lvs[0].UpperValue, 1e-12)
	assert.Equal(t, int64(500), lvs[0].TailCount) // n/2

	// Depth 2: quartiles.
	assert.Equal(t, uint8(2), lvs[1].Depth)
	assert.InDelta(t, 5.0, lvs[1].LowerValue, 1e-12)
	assert.InDelta(t, 15.0, lvs[1].UpperValue, 1e-12)
	assert.Equal(t, int64(250), lvs[1].TailCount)

	// Depth 3: octiles.
	assert.Equal(t, uint8(3), lvs[2].Depth)
	assert.InDelta(t, 2.0, lvs[2].LowerValue, 1e-12)
	assert.InDelta(t, 18.0, lvs[2].UpperValue, 1e-12)
	assert.Equal(t, int64(125), lvs[2].TailCount)
}

func TestLevelsFromArrayShortInput(t *testing.T) {
	// Caller passed only depth-1 worth of data but asked for depth-3
	// levels — deeper LVs come back zero-bound rather than panicking.
	lvs := chstats.LevelsFromArray([]float64{42}, 100, 3)
	require.Len(t, lvs, 3)
	assert.InDelta(t, 42.0, lvs[0].LowerValue, 1e-12)
	assert.InDelta(t, 0.0, lvs[1].LowerValue, 1e-12)
	assert.InDelta(t, 0.0, lvs[1].UpperValue, 1e-12)
}

// ---------------------------------------------------------------------------
// Integration test against clickhouse-local subprocess.
//
// Skips when clickhouse-local is unavailable; the project's reference
// invariant (memory: reference_clickhouse_local) says /usr/bin/
// clickhouse-local is installed locally. CI runners may or may not
// satisfy this — gated explicitly so the unit tests above always run.
// ---------------------------------------------------------------------------

func TestRoundTrip_QuantilesTDigest_clickhouseLocal(t *testing.T) {
	bin, lookErr := exec.LookPath("clickhouse-local")
	if lookErr != nil {
		t.Skipf("clickhouse-local not in PATH: %v", lookErr)
	}

	// 10k Gaussian samples — large enough that t-digest approximation
	// is reasonable, small enough that the subprocess round-trip stays
	// under a second.
	rnd := rand.New(rand.NewSource(42))
	const n = 10_000
	const maxDepth = uint8(6)
	data := make([]float64, n)
	for i := range data {
		data[i] = rnd.NormFloat64()
	}

	// Stream samples as a single-column TSV table over stdin —
	// clickhouse-local exposes stdin as `table` when `--structure` and
	// `--input-format` are set, sidestepping ARG_MAX limits a VALUES
	// literal hits at 10k+ rows.
	var stdinBuf bytes.Buffer
	for _, v := range data {
		stdinBuf.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		stdinBuf.WriteByte('\n')
	}

	sql := fmt.Sprintf("SELECT %s FROM table FORMAT TabSeparated",
		chstats.BuildLVSelect("x", maxDepth))

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin,
		"--input-format", "TabSeparated",
		"--structure", "x Float64",
		"--query", sql)
	cmd.Stdin = &stdinBuf
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("clickhouse-local: %v; stderr=%s", err, stderr.String())
	}

	arr := parseTSVArray(t, strings.TrimSpace(stdout.String()))
	require.Equal(t, len(chstats.LVQuantiles(maxDepth)), len(arr),
		"array length must match LVQuantiles count")

	lvs := chstats.LevelsFromArray(arr, n, maxDepth)
	require.Len(t, lvs, int(maxDepth))

	// Compare against sort-exact reference (cap depth at 5 — past that
	// each tail has < ~300 Gaussian samples and individual extremes
	// dominate over the sketch error).
	sorted := append([]float64(nil), data...)
	sort.Float64s(sorted)
	for i, lv := range lvs {
		if lv.Depth > 5 {
			break
		}
		expLow := exactQuantile(sorted, lv.LowerQ)
		expHigh := exactQuantile(sorted, lv.UpperQ)
		// CH's t-digest accuracy budget on Gaussian — same regime as
		// the boxer-side tdigest end-to-end test.
		assert.InDelta(t, expLow, lv.LowerValue, 0.05,
			"depth %d lower (rank %v) sketch=%v exact=%v",
			lv.Depth, lv.LowerQ, lv.LowerValue, expLow)
		assert.InDelta(t, expHigh, lv.UpperValue, 0.05,
			"depth %d upper (rank %v) sketch=%v exact=%v",
			lv.Depth, lv.UpperQ, lv.UpperValue, expHigh)
		_ = i
	}
}

// parseTSVArray decodes ClickHouse's TabSeparated array literal
// "[1.2,3.4,5.6]" into a []float64. The driver-level decoders use a
// more sophisticated parser; this is enough for the lone-array
// fixture this test produces.
func parseTSVArray(t *testing.T, s string) (out []float64) {
	t.Helper()
	s = strings.TrimSpace(s)
	require.True(t, strings.HasPrefix(s, "["), "expected leading '[', got %q", s)
	require.True(t, strings.HasSuffix(s, "]"), "expected trailing ']', got %q", s)
	s = s[1 : len(s)-1]
	if s == "" {
		return nil
	}
	for tok := range strings.SplitSeq(s, ",") {
		tok = strings.TrimSpace(tok)
		v, err := strconv.ParseFloat(tok, 64)
		require.NoError(t, err, "parse %q", tok)
		out = append(out, v)
	}
	return
}

func exactQuantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[len(sorted)-1]
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	t := pos - float64(lo)
	return sorted[lo]*(1-t) + sorted[hi]*t
}
