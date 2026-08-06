//go:build integration

package codevol

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The default lane cannot see what this package exists to measure: a `go test`
// binary carries no dependency list, so the module index holds one entry and
// every third-party package falls through to std. Here we build a real binary
// and read it the way a running process reads itself.
func TestBuiltBinary_ModulesAndSymbolsAgree(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe")

	// A boxer package with genuine third-party imports, so the built binary
	// necessarily contains all three parties.
	build := exec.Command("go", "build", "-tags", buildTags(t), "-o", bin,
		"github.com/stergiotis/boxer/public/app")
	build.Dir = repoRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", out)

	bi, err := buildinfo.ReadFile(bin)
	require.NoError(t, err)
	mods := modulesFrom(bi)

	var main, third int
	for _, m := range mods {
		if m.IsMain {
			main++
			continue
		}
		require.Equal(t, PartyThird, m.Party)
		require.NotEmpty(t, m.Version, "a dependency module carries a version: %s", m.Path)
		third++
	}
	require.Equal(t, 1, main)
	require.Greater(t, third, 10, "the CLI links many third-party modules")

	rep, err := readSymbolsFile(bin, NewModuleIndex(mods))
	require.NoError(t, err)
	require.True(t, rep.ModuleExact)
	require.Greater(t, rep.TotalText, uint64(1<<20))

	byParty := map[Party]uint64{}
	pkgsByParty := map[Party]int{}
	for _, p := range rep.Packages {
		byParty[p.Party] += p.TextBytes
		pkgsByParty[p.Party]++
	}
	// The three-way split is the property the whole ADR-0173 question rests
	// on; every party must carry real machine code in a real binary.
	for _, party := range []Party{PartyFirst, PartyThird, PartyStdlib} {
		require.Greater(t, byParty[party], uint64(0), "party %s has no text bytes", party)
		require.Greater(t, pkgsByParty[party], 0, "party %s has no packages", party)
	}

	// Every module a symbol resolved to must be one the binary declares —
	// attribution must never invent a module.
	declared := map[string]bool{"std": true}
	for _, m := range mods {
		declared[m.Path] = true
	}
	for _, p := range rep.Packages {
		require.True(t, declared[p.ModulePath],
			"package %s attributed to undeclared module %s", p.PkgPath, p.ModulePath)
	}

	// Per-package text must reconcile with the total, less what was left
	// unattributed on purpose (itabs and the like).
	var sum uint64
	for _, p := range rep.Packages {
		sum += p.TextBytes
	}
	require.LessOrEqual(t, sum, rep.TotalText)
	require.Greater(t, sum, rep.TotalText/2, "most text should attribute to a package")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		require.NotEqual(t, parent, d, "no go.mod above the working directory")
		d = parent
	}
}

func buildTags(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "tags"))
	require.NoError(t, err)
	return string(trimSpace(b))
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
