package adrindex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
)

func TestRenderEscapesCells(t *testing.T) {
	out := string(Render([]adrcorpus.Adr{{Num: 7, Path: "doc/adr/0007-a.md", Title: "a | b", Status: "accepted", Date: "2026-01-02"}}))
	assert.Contains(t, out, "| [0007](./0007-a.md) | a \\| b | accepted | 2026-01-02 |  |\n")
}

func TestGenerateAndCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	write("0001-first.md", "---\ntype: adr\nstatus: superseded\nsuperseded-by: ADR-0002\ndate: 2026-01-01\n---\n\n# ADR-0001: First\n")
	write("0002-second.md", "---\ntype: adr\nstatus: accepted\ndate: 2026-01-05\n---\n\n# ADR-0002: Second\n")
	write("README.md", "not an ADR\n")
	out := filepath.Join(dir, "INDEX.md")
	require.NoError(t, Generate(dir, out))
	require.NoError(t, Check(dir, out))
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Contains(t, string(got), "| [0001](./0001-first.md) | First | superseded | 2026-01-01 | ADR-0002 |\n")
	assert.Contains(t, string(got), "| [0002](./0002-second.md) | Second | accepted | 2026-01-05 |  |\n")
	write("0003-third.md", "---\ntype: adr\nstatus: proposed\ndate: 2026-01-09\n---\n\n# ADR-0003: Third\n")
	require.Error(t, Check(dir, out), "a new ADR without a regeneration is drift")
}

// TestIndexIsCurrent gates doc/adr/INDEX.md against the ADRs it indexes: a
// new ADR, or a status or date change, not followed by a regeneration fails
// here.
func TestIndexIsCurrent(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "doc", "adr")
	require.NoError(t, Check(dir, filepath.Join(dir, "INDEX.md")),
		"regenerate with `boxer gov adrindex` (or go generate ./public/gov/adrindex)")
}
