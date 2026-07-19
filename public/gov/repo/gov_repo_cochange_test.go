package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCoChangeExtract builds a throwaway repository and asserts the co-change
// memberships: a two-file commit couples its files, a single-file commit
// contributes nothing (no pair), and a mass commit above MaxChangesetSize is
// dropped so it cannot couple unrelated files.
func TestCoChangeExtract(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	commit := func(msg string, mutate func()) {
		t.Helper()
		mutate()
		add := exec.Command("git", "add", "-A")
		add.Dir = dir
		if out, err := add.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
		c := exec.Command("git", "commit", "--quiet", "-m", msg)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Dev", "GIT_AUTHOR_EMAIL=dev@example.com",
			"GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	commit("seed a,b", func() { write("a.go", "1\n"); write("b.go", "1\n") })
	commit("add c", func() { write("c.go", "1\n") }) // single file → no pair
	commit("edit a,b", func() { write("a.go", "2\n"); write("b.go", "2\n") })
	commit("edit a,c", func() { write("a.go", "3\n"); write("c.go", "2\n") })
	commit("mass", func() { // 4 files > MaxChangesetSize=3 → dropped
		write("m1.go", "1\n")
		write("m2.go", "1\n")
		write("m3.go", "1\n")
		write("m4.go", "1\n")
	})

	analyzer := &CoChangeAnalyzer{MaxChangesetSize: 3}
	facts, commits, err := analyzer.ExtractCoChanges(context.Background(), &GitRunner{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if commits != 3 {
		t.Errorf("commits = %d, want 3 (mass + single dropped)", commits)
	}

	// Group by commit id into sorted file lists; the specific ids are
	// meaningless, only the co-membership groups are.
	byId := make(map[int32][]string)
	for _, f := range facts {
		byId[f.CommitId] = append(byId[f.CommitId], f.Path)
	}
	groups := make([]string, 0, len(byId))
	for _, g := range byId {
		sort.Strings(g)
		groups = append(groups, strings.Join(g, ","))
	}
	sort.Strings(groups)

	want := []string{"a.go,b.go", "a.go,b.go", "a.go,c.go"}
	if strings.Join(groups, " | ") != strings.Join(want, " | ") {
		t.Errorf("co-change groups = %v, want %v", groups, want)
	}

	// The mass commit's files must never appear.
	for _, f := range facts {
		if strings.HasPrefix(f.Path, "m") {
			t.Errorf("mass-commit file leaked into co-changes: %s", f.Path)
		}
	}
}
