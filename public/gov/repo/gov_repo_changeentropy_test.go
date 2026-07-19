package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestChangeEntropyExtract builds a throwaway repository (fixed author month so
// the assertion is deterministic) and checks the per-(month, file) feature line
// churn: only Feature-Introduction commits contribute, and each file's
// added+deleted lines are summed across them; a bug-fix and a chore commit are
// excluded.
func TestChangeEntropyExtract(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	const when = "2024-03-15T12:00:00"
	commit := func(subject string, mutate func()) {
		t.Helper()
		mutate()
		add := exec.Command("git", "add", "-A")
		add.Dir = dir
		if out, err := add.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
		c := exec.Command("git", "commit", "--quiet", "-m", subject)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Dev", "GIT_AUTHOR_EMAIL=dev@example.com",
			"GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@example.com",
			"GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}
	lines := func(n int) string { return strings.Repeat("x\n", n) }
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	commit("feat: seed a,b", func() { write("a.go", lines(10)); write("b.go", lines(4)) })
	commit("fix: patch a", func() { write("a.go", lines(11)) }) // +1, not feature → excluded
	commit("feat: grow a, add c", func() { write("a.go", lines(14)); write("c.go", lines(5)) })
	commit("chore: add d", func() { write("d.go", lines(2)) }) // not feature → excluded

	analyzer := &ChangeEntropyAnalyzer{
		IsFeatureCommit: func(subject string) bool { return strings.HasPrefix(subject, "feat") },
	}
	facts, featureCommits, totalCommits, err := analyzer.ExtractLineChurn(context.Background(), &GitRunner{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if featureCommits != 2 {
		t.Errorf("featureCommits = %d, want 2", featureCommits)
	}
	if totalCommits != 4 {
		t.Errorf("totalCommits = %d, want 4", totalCommits)
	}

	got := make(map[string]int64, len(facts))
	for _, f := range facts {
		if f.Month != "2024-03" {
			t.Errorf("fact month = %q, want 2024-03: %+v", f.Month, f)
		}
		got[f.Path] = f.ModifiedLines
	}
	want := map[string]int64{"a.go": 13, "b.go": 4, "c.go": 5} // a = 10 (feat1) + 3 (feat2); fix/chore excluded
	if len(got) != len(want) {
		t.Fatalf("facts = %+v, want %v", facts, want)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s modified lines = %d, want %d", p, got[p], w)
		}
	}
	if _, ok := got["d.go"]; ok {
		t.Error("chore-commit file d.go leaked into feature churn")
	}
}
