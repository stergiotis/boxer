//go:build llm_generated_opus47

package golang_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stretchr/testify/require"
)

// TestAlignAndFormatHeadOutGoIsAligned asserts that every committed
// .out.go file produced by leeway-style generators is in
// betteralign-optimal layout at git HEAD. Running AlignAndFormat on the
// HEAD bytes is expected to be a no-op; if it isn't, the most recent
// commit drifted from `./scripts/dev/betteralign.sh` output.
//
// Reads HEAD content rather than the working tree so the check is robust
// to in-progress changes (between a generator-test run and a
// `betteralign.sh` run).
func TestAlignAndFormatHeadOutGoIsAligned(t *testing.T) {
	moduleRoot := findRepoRoot(t)
	tags, err := golang.FindModuleBuildTags(moduleRoot)
	require.NoError(t, err)

	targets := []string{
		"public/semistructured/leeway/dml/example/dml_testtable.out.go",
		"public/semistructured/leeway/dml/example/dml_json.out.go",
		"public/semistructured/leeway/readaccess/example/readaccess_testtable_ra.out.go",
		"public/semistructured/leeway/readaccess/example/readaccess_testtable_dml.out.go",
		"public/semistructured/leeway/common/lw_system_table_columns_dml.out.go",
	}
	for _, rel := range targets {
		t.Run(rel, func(t *testing.T) {
			cmd := exec.Command("git", "show", "HEAD:"+rel)
			cmd.Dir = moduleRoot
			src, err := cmd.Output()
			if err != nil {
				t.Skipf("git show failed (file not at HEAD?): %v", err)
			}
			abs := filepath.Join(moduleRoot, rel)
			out, err := golang.AlignAndFormat(src, abs, tags)
			require.NoError(t, err)
			require.Equal(t, string(src), string(out),
				"%s at HEAD is not in betteralign-optimal order", rel)
		})
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("no go.mod found above %s", d)
		}
		d = parent
	}
}
