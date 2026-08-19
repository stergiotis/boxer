//go:build integration

package coverage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The toolchain-drift guard (ADR-0169 §SD2): build the probe with the LIVE
// toolchain, decode its blobs with our pinned decoder, and reconcile every
// total against `go tool covdata textfmt` as the independent oracle. A Go
// bump that changes the format turns this red instead of silently skewing
// the sampler.
func TestDecodeAgainstLiveToolchain(t *testing.T) {
	metaData, counterData, coverDir := buildAndRunProbe(t)

	prof, err := DecodeMeta(metaData)
	require.NoError(t, err)
	snap, err := DecodeCounters(counterData)
	require.NoError(t, err)
	require.Equal(t, prof.Hash, snap.MetaHash)

	outPath := filepath.Join(t.TempDir(), "profile.txt")
	cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i="+coverDir, "-o", outPath)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go tool covdata textfmt failed: %s", out)
	profileTxt, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var oracleUnits, oracleStmts, oracleCovUnits, oracleCovStmts uint64
	for line := range strings.SplitSeq(string(profileTxt), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		require.Len(t, fields, 3, "unexpected textfmt line %q", line)
		stmts, err := strconv.ParseUint(fields[1], 10, 32)
		require.NoError(t, err)
		count, err := strconv.ParseUint(fields[2], 10, 64)
		require.NoError(t, err)
		oracleUnits++
		oracleStmts += stmts
		if count > 0 {
			oracleCovUnits++
			oracleCovStmts += stmts
		}
	}

	require.EqualValues(t, oracleUnits, prof.TotalUnits, "unit totals disagree with covdata")
	require.EqualValues(t, oracleStmts, prof.TotalStmts, "statement totals disagree with covdata")

	var covUnits, covStmts uint64
	for _, fc := range snap.Funcs {
		_, fn, ok := prof.LookupFunc(fc.PkgIdx, fc.FuncIdx)
		require.True(t, ok)
		require.Len(t, fc.Counters, len(fn.Units))
		for i, c := range fc.Counters {
			if c > 0 {
				covUnits++
				covStmts += uint64(fn.Units[i].NxStmts)
			}
		}
	}
	require.Equal(t, oracleCovUnits, covUnits, "covered-unit totals disagree with covdata")
	require.Equal(t, oracleCovStmts, covStmts, "covered-statement totals disagree with covdata")
}
