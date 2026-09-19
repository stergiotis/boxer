package licensegate

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFixture(t *testing.T, name string, content string) (path string) {
	path = filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return
}

const registrySource = `"registry+https://github.com/rust-lang/crates.io-index"`

// cargoFixture is a `cargo metadata` document with one package per line of
// packages, each already a JSON object.
func cargoFixture(packages ...string) (doc string) {
	doc = `{"packages":[`
	for i, p := range packages {
		if i > 0 {
			doc += ","
		}
		doc += p
	}
	doc += `],"version":1}`
	return
}

func readCSV(t *testing.T, path string) (records [][]string) {
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	records, err = csv.NewReader(f).ReadAll()
	require.NoError(t, err)
	return
}

func TestRunCargoPasses(t *testing.T) {
	treeA := writeFixture(t, "a.json", cargoFixture(
		// The workspace's own crate: no source, no license, not an inbound question.
		`{"name":"own-crate","version":"0.1.0","license":null,"license_file":null,"source":null}`,
		`{"name":"common","version":"1.0.0","license":"MIT OR Apache-2.0","source":`+registrySource+`}`,
		`{"name":"r-efi-like","version":"5.3.0","license":"MIT OR Apache-2.0 OR LGPL-2.1-or-later","source":`+registrySource+`}`,
	))
	// A second tree resolving the same crate: classified once.
	treeB := writeFixture(t, "b.json", cargoFixture(
		`{"name":"common","version":"1.0.0","license":"MIT OR Apache-2.0","source":`+registrySource+`}`,
	))
	csvPath := filepath.Join(t.TempDir(), "inventory.csv")

	violations, err := Run("", []string{treeA, treeB}, csvPath)
	require.NoError(t, err)
	assert.Equal(t, 0, violations)

	assert.Equal(t, [][]string{
		{"module", "version", "spdx_id", "category", "ecosystem", "declared"},
		{"common", "1.0.0", "MIT", "notice", "cargo", "MIT OR Apache-2.0"},
		{"r-efi-like", "5.3.0", "MIT", "notice", "cargo", "MIT OR Apache-2.0 OR LGPL-2.1-or-later"},
	}, readCSV(t, csvPath))
}

func TestRunCargoViolations(t *testing.T) {
	tree := writeFixture(t, "tree.json", cargoFixture(
		`{"name":"fine","version":"1.0.0","license":"MIT","source":`+registrySource+`}`,
		`{"name":"copyleft","version":"1.0.0","license":"GPL-3.0-only","source":`+registrySource+`}`,
		// The regression ADR-0246 SD3 exists to prevent: an unclassifiable
		// conjunct must not clear a copyleft one.
		`{"name":"masked","version":"1.0.0","license":"`+unknownID+` AND GPL-3.0-only","source":`+registrySource+`}`,
		`{"name":"offered-a-choice","version":"1.0.0","license":"MIT OR GPL-3.0-only","source":`+registrySource+`}`,
	))
	violations, err := Run("", []string{tree}, "")
	require.NoError(t, err)
	assert.Equal(t, 2, violations)
}

func TestCargoRowsUnresolved(t *testing.T) {
	tree := writeFixture(t, "tree.json", cargoFixture(
		`{"name":"silent","version":"1.0.0","license":null,"source":`+registrySource+`}`,
		`{"name":"file-only","version":"1.0.0","license":null,"license_file":"LICENSE","source":`+registrySource+`}`,
		`{"name":"unreadable","version":"1.0.0","license":"MIT OR","source":`+registrySource+`}`,
		`{"name":"unmapped","version":"1.0.0","license":"`+unknownID+`","source":`+registrySource+`}`,
	))
	rows, unresolved, err := cargoRows([]string{tree})
	require.NoError(t, err)

	// All four go to review; only the unmapped one also has a row, because only
	// it has a classification to put in the inventory.
	assert.Equal(t, []string{
		"silent@1.0.0 (cargo: no license declared)",
		"file-only@1.0.0 (cargo: license-file only, no SPDX expression)",
		"unreadable@1.0.0 (cargo: unreadable expression MIT OR)",
		"unmapped@1.0.0 (cargo: " + unknownID + ")",
	}, unresolved)
	require.Len(t, rows, 1)
	assert.Equal(t, CategoryUnknown, rows[0].category)
}

// TestRunGoPathUnchanged pins that an SBOM-only invocation keeps the ADR-0004
// behaviour: one row per detected identifier, boxer's own module skipped.
func TestRunGoPathUnchanged(t *testing.T) {
	sbom := writeFixture(t, "sbom.json", `{"components":[
		{"name":"github.com/stergiotis/boxer","version":"v0.0.0","purl":"pkg:golang/github.com/stergiotis/boxer?type=module"},
		{"name":"github.com/example/dual","version":"v1.0.0","purl":"pkg:golang/github.com/example/dual@v1.0.0",
		 "evidence":{"licenses":[{"license":{"id":"MIT"}},{"license":{"id":"Apache-2.0"}}]}}
	]}`)
	csvPath := filepath.Join(t.TempDir(), "inventory.csv")

	violations, err := Run(sbom, nil, csvPath)
	require.NoError(t, err)
	assert.Equal(t, 0, violations)
	assert.Equal(t, [][]string{
		{"module", "version", "spdx_id", "category", "ecosystem", "declared"},
		{"github.com/example/dual", "v1.0.0", "Apache-2.0", "notice", "go", "Apache-2.0"},
		{"github.com/example/dual", "v1.0.0", "MIT", "notice", "go", "MIT"},
	}, readCSV(t, csvPath))
}

func TestRunNeedsAnInput(t *testing.T) {
	_, err := Run("", nil, "")
	assert.Error(t, err)
}
