package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNameStatus(t *testing.T) {
	cases := []struct {
		line       string
		wantStatus byte
		wantPaths  []string
	}{
		{"A\tsrc/a.go", 'A', []string{"src/a.go"}},
		{"M\tsrc/a.go", 'M', []string{"src/a.go"}},
		{"D\tsrc/a.go", 'D', []string{"src/a.go"}},
		{"R100\told.go\tnew.go", 'R', []string{"old.go", "new.go"}},
		{"C075\tsrc.go\tcopy.go", 'C', []string{"src.go", "copy.go"}},
		{"", 0, nil},
		{"no-tab-here", 0, nil},
	}
	for _, tc := range cases {
		status, paths := parseNameStatus(tc.line)
		if status != tc.wantStatus || strings.Join(paths, "|") != strings.Join(tc.wantPaths, "|") {
			t.Errorf("parseNameStatus(%q) = (%q,%v), want (%q,%v)", tc.line, status, paths, tc.wantStatus, tc.wantPaths)
		}
	}
}

// TestTruckFactorExtractChangeFacts builds a throwaway repository exercising the
// four things extraction must get right — cross-developer files, first
// authorship, rename following (-M), and bot exclusion — and asserts the emitted
// (file, developer) facts.
func TestTruckFactorExtractChangeFacts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	commit := func(name, email, msg string, mutate func()) {
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
			"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email,
			"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email,
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %v\n%s", err, out)
		}
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "--quiet")
	const alice = "alice@example.com"
	const bob = "bob@example.com"

	commit("Alice", alice, "seed", func() { write("a.go", "one\n"); write("b.go", "beta\n") })
	commit("Bob", bob, "edit a", func() { write("a.go", "one\ntwo\n") })
	commit("Bob", bob, "add c", func() { write("c.go", "cee\n") })
	commit("Alice", alice, "rename b to d", func() { git("mv", "b.go", "d.go") })
	commit("CI Bot", "bot@ci.invalid", "bot adds e", func() { write("e.go", "eee\n") })
	commit("Alice", alice, "edit e", func() { write("e.go", "eee\nfff\n") })

	analyzer := &TruckFactorAnalyzer{
		AuthorExcluder: func(email, name string) bool { return strings.Contains(email, "@ci.invalid") },
	}
	facts, totalFiles, err := analyzer.ExtractChangeFacts(context.Background(), &GitRunner{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	// Surviving tracked files: a.go, c.go, d.go, e.go (b.go was renamed away).
	if totalFiles != 4 {
		t.Errorf("totalFiles = %d, want 4", totalFiles)
	}

	type key struct {
		path, email string
	}
	got := make(map[key]FileAuthorChange, len(facts))
	for _, f := range facts {
		got[key{f.Path, f.Email}] = f
	}

	want := []FileAuthorChange{
		{Path: "a.go", Email: alice, Name: "Alice", OwnChanges: 1, FirstAuthor: true},
		{Path: "a.go", Email: bob, Name: "Bob", OwnChanges: 1, FirstAuthor: false},
		{Path: "c.go", Email: bob, Name: "Bob", OwnChanges: 1, FirstAuthor: true},
		{Path: "d.go", Email: alice, Name: "Alice", OwnChanges: 2, FirstAuthor: true},  // add on b.go + the rename commit, followed across the move
		{Path: "e.go", Email: alice, Name: "Alice", OwnChanges: 1, FirstAuthor: false}, // created by the (excluded) bot ⇒ no first author
	}

	if len(facts) != len(want) {
		t.Fatalf("got %d facts, want %d: %+v", len(facts), len(want), facts)
	}
	for _, w := range want {
		g, ok := got[key{w.Path, w.Email}]
		if !ok {
			t.Errorf("missing fact for %s / %s", w.Path, w.Email)
			continue
		}
		if g != w {
			t.Errorf("fact %s / %s = %+v, want %+v", w.Path, w.Email, g, w)
		}
	}
	// The renamed-away path and the bot identity must never surface.
	if _, ok := got[key{"b.go", alice}]; ok {
		t.Error("renamed-away b.go should not appear")
	}
	for _, f := range facts {
		if strings.Contains(f.Email, "@ci.invalid") {
			t.Errorf("excluded bot identity leaked: %+v", f)
		}
	}
}
