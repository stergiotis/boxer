package coverage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) (data []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "fixture missing — regenerate via BOXER_COVDECODE_REGEN=1 go test -run TestRegenFixtures")
	return
}

func pkgByPath(t *testing.T, prof *covsnap.MetaProfile, path string) (pkg *covsnap.PkgMeta) {
	t.Helper()
	for i := range prof.Pkgs {
		if prof.Pkgs[i].Path == path {
			return &prof.Pkgs[i]
		}
	}
	require.Failf(t, "package not found", "path %q not in decoded profile", path)
	return nil
}

func funcByName(t *testing.T, pkg *covsnap.PkgMeta, name string) (fn *covsnap.FuncMeta) {
	t.Helper()
	for i := range pkg.Funcs {
		if pkg.Funcs[i].Name == name {
			return &pkg.Funcs[i]
		}
	}
	require.Failf(t, "function not found", "func %q not in package %q", name, pkg.Path)
	return nil
}

func TestDecodeMetaFixture(t *testing.T) {
	prof, err := DecodeMeta(readFixture(t, "covmeta.bin"))
	require.NoError(t, err)
	require.NotEqual(t, [16]byte{}, prof.Hash)
	require.Equal(t, covsnap.ModeAtomic, prof.Mode)
	require.Equal(t, covsnap.GranularityPerBlock, prof.Granularity)
	require.Len(t, prof.Pkgs, 2)

	mainPkg := pkgByPath(t, prof, "covprobe")
	require.Equal(t, "main", mainPkg.Name)
	require.Equal(t, "covprobe", mainPkg.ModulePath)
	innerPkg := pkgByPath(t, prof, "covprobe/inner")
	require.Equal(t, "inner", innerPkg.Name)

	alpha := funcByName(t, mainPkg, "alpha")
	require.True(t, strings.HasSuffix(alpha.SrcFile, "main.go"), "src file %q", alpha.SrcFile)
	require.GreaterOrEqual(t, len(alpha.Units), 2, "a function with a loop spans several coverable units")
	require.GreaterOrEqual(t, alpha.NumStmts, uint32(3))
	funcByName(t, mainPkg, "neverCalled")
	funcByName(t, innerPkg, "Unused")

	// The global unit index is the meta enumeration order: contiguous,
	// non-overlapping, summing to the profile totals (ADR-0169 §SD3).
	next := uint32(0)
	stmts := uint32(0)
	for i := range prof.Pkgs {
		pkg := &prof.Pkgs[i]
		require.Equal(t, next, pkg.UnitBase)
		for j := range pkg.Funcs {
			fn := &pkg.Funcs[j]
			require.Equal(t, next, fn.UnitBase)
			next += uint32(len(fn.Units))
			stmts += fn.NumStmts
		}
		require.Equal(t, next-pkg.UnitBase, pkg.NumUnits)
	}
	require.Equal(t, next, prof.TotalUnits)
	require.Equal(t, stmts, prof.TotalStmts)
	require.Positive(t, prof.TotalUnits)
}

func TestDecodeCountersFixture(t *testing.T) {
	prof, err := DecodeMeta(readFixture(t, "covmeta.bin"))
	require.NoError(t, err)
	snap, err := DecodeCounters(readFixture(t, "covcounters.bin"))
	require.NoError(t, err)
	require.Equal(t, prof.Hash, snap.MetaHash)
	require.NotEmpty(t, snap.Funcs)
	require.Contains(t, snap.Args, "argc")

	executed := make(map[string]bool, len(snap.Funcs))
	sawPositive := false
	for _, fc := range snap.Funcs {
		_, fn, ok := prof.LookupFunc(fc.PkgIdx, fc.FuncIdx)
		require.True(t, ok, "counter entry (%d,%d) must resolve against the meta", fc.PkgIdx, fc.FuncIdx)
		require.Len(t, fc.Counters, len(fn.Units), "one counter per coverable unit of %q", fn.Name)
		executed[fn.Name] = true
		for _, c := range fc.Counters {
			if c > 0 {
				sawPositive = true
			}
		}
	}
	require.True(t, sawPositive)
	require.True(t, executed["main"])
	require.True(t, executed["alpha"])
	require.True(t, executed["Used"])
	// Counter emission skips never-executed functions.
	require.False(t, executed["neverCalled"])
	require.False(t, executed["Unused"])
}

func TestDecodeRejectsForeignAndFutureBlobs(t *testing.T) {
	meta := readFixture(t, "covmeta.bin")
	counters := readFixture(t, "covcounters.bin")

	badMagic := append([]byte(nil), meta...)
	badMagic[2] ^= 0xff
	_, err := DecodeMeta(badMagic)
	require.ErrorContains(t, err, "magic")

	future := append([]byte(nil), meta...)
	binary.LittleEndian.PutUint32(future[4:8], 99)
	_, err = DecodeMeta(future)
	require.ErrorContains(t, err, "version")

	badMagicC := append([]byte(nil), counters...)
	badMagicC[2] ^= 0xff
	_, err = DecodeCounters(badMagicC)
	require.ErrorContains(t, err, "magic")

	futureC := append([]byte(nil), counters...)
	binary.LittleEndian.PutUint32(futureC[4:8], 99)
	_, err = DecodeCounters(futureC)
	require.ErrorContains(t, err, "version")

	_, err = DecodeMeta(counters)
	require.Error(t, err)
	_, err = DecodeCounters(meta)
	require.Error(t, err)
}

// Corrupt input of any shape must come back as an error, never a panic or
// an unbounded allocation: every prefix truncation and every single-byte
// flip of both fixtures.
func TestDecodeSurvivesTruncationAndBitFlips(t *testing.T) {
	for _, name := range []string{"covmeta.bin", "covcounters.bin"} {
		data := readFixture(t, name)
		for n := 0; n <= len(data); n++ {
			_, _ = DecodeMeta(data[:n])
			_, _ = DecodeCounters(data[:n])
		}
		for i := range data {
			mut := append([]byte(nil), data...)
			mut[i] ^= 0xff
			_, _ = DecodeMeta(mut)
			_, _ = DecodeCounters(mut)
		}
	}
}
