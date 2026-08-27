package capmap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The guard between a mistyped --out and a vault with two corpora in it.
func TestCheckOutputDirRefusesAPopulatedDirectory(t *testing.T) {
	empty := t.TempDir()
	assert.NoError(t, checkOutputDir(empty, false))

	missing := filepath.Join(t.TempDir(), "not-yet")
	assert.NoError(t, checkOutputDir(missing, false), "a directory that does not exist yet is created by the write")

	populated := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(populated, "something.md"), []byte("x"), 0o644))
	assert.Error(t, checkOutputDir(populated, false))
	assert.NoError(t, checkOutputDir(populated, true), "--force is the operator saying they know")

	assert.Error(t, checkOutputDir("", false))
}

// `ingest` is in ADR-0168, in the applet book's prose and in whatever anybody
// scripted, so renaming the verb must not break it.
func TestLoadKeepsTheIngestAlias(t *testing.T) {
	cmd := NewCliCommand()
	byName := make(map[string][]string, len(cmd.Subcommands))
	for _, sub := range cmd.Subcommands {
		byName[sub.Name] = sub.Aliases
	}
	require.Contains(t, byName, "load")
	assert.Contains(t, byName["load"], "ingest")
	assert.Contains(t, byName, "dump")
	assert.Contains(t, byName, "parse")
	assert.Contains(t, byName, "similar")
}
