//go:build integration

package mdquality

import (
	"os"
	"path/filepath"
	"testing"
)

// The steps of one run, in order, each a test so that measure.sh can run
// them one at a time between the captures it takes outside Go. All skip
// without MDQ_WORK_DIR; metrics and tally also need MDQ_RUN_DIR.

func workDir(t *testing.T) (work string) {
	t.Helper()
	work = os.Getenv("MDQ_WORK_DIR")
	if work == "" {
		t.Skip("MDQ_WORK_DIR unset")
	}
	return
}

func runDir(t *testing.T) (run string) {
	t.Helper()
	run = os.Getenv("MDQ_RUN_DIR")
	if run == "" {
		t.Skip("MDQ_RUN_DIR unset")
	}
	return
}

func repoRoot(t *testing.T) (root string) {
	t.Helper()
	var err error
	if root, err = filepath.Abs(filepath.Join("..", "..", "..", "..")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the harness must run from its own directory: %v", err)
	}
	return
}

func TestPrepare(t *testing.T) {
	if err := prepare(repoRoot(t), workDir(t)); err != nil {
		t.Fatal(err)
	}
}

func TestMetrics(t *testing.T) {
	if err := metrics(workDir(t), runDir(t)); err != nil {
		t.Fatal(err)
	}
}

func TestVote(t *testing.T) {
	if err := vote(workDir(t)); err != nil {
		t.Fatal(err)
	}
}

func TestTally(t *testing.T) {
	if err := tally(workDir(t), runDir(t)); err != nil {
		t.Fatal(err)
	}
}
