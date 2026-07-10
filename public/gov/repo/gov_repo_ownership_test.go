package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDefaultModelMatcher(t *testing.T) {
	cases := []struct {
		coauthor string
		wantTag  string
		wantOk   bool
	}{
		{"Claude Opus 4.8 (1M context) <noreply@anthropic.com>", "claude", true},
		{"claude something", "claude", true},
		{"Gemini 3 Pro <g@example.com>", "gemini", true},
		{"Jane Doe <jane@example.com>", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		tag, ok := DefaultModelMatcher(tc.coauthor)
		if tag != tc.wantTag || ok != tc.wantOk {
			t.Errorf("DefaultModelMatcher(%q) = (%q,%v), want (%q,%v)", tc.coauthor, tag, ok, tc.wantTag, tc.wantOk)
		}
	}
}

func TestIsBlameHeaderLine(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef01234567"
	if !isBlameHeaderLine(hash + " 1 1 3") {
		t.Error("header with group count not recognized")
	}
	if !isBlameHeaderLine(hash) {
		t.Error("bare 40-hex header not recognized")
	}
	if isBlameHeaderLine("author Jane Doe") {
		t.Error("metadata line misread as header")
	}
	if isBlameHeaderLine(hash + "x 1 1") {
		t.Error("41-char non-space suffix misread as header")
	}
}

// TestOwnershipAnalyzerRunSummary builds a throwaway git repository with
// one human commit and one model-co-authored commit, then asserts the
// blame join attributes surviving lines and sponsorship correctly.
func TestOwnershipAnalyzerRunSummary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Jane Doe", "GIT_AUTHOR_EMAIL=jane@example.com",
			"GIT_COMMITTER_NAME=Jane Doe", "GIT_COMMITTER_EMAIL=jane@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name string, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet")
	write("a.txt", "one\ntwo\n")
	run("add", "a.txt")
	run("commit", "--quiet", "-m", "human commit")
	write("b.txt", "alpha\nbeta\ngamma\n")
	run("add", "b.txt")
	run("commit", "--quiet", "-m", "model commit\n\nCo-Authored-By: Claude Fable 5 <noreply@anthropic.com>")

	analyzer := &OwnershipAnalyzer{Parallelism: 2}
	summary, err := analyzer.RunSummary(context.Background(), &GitRunner{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	if summary.TotalLines != 5 || summary.AttributedLines != 5 || summary.UncommittedLines != 0 {
		t.Fatalf("line totals = %d/%d/%d, want 5/5/0",
			summary.TotalLines, summary.AttributedLines, summary.UncommittedLines)
	}
	if len(summary.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(summary.Files))
	}
	if len(summary.Owners) != 2 {
		t.Fatalf("owners = %v, want human + model", summary.Owners)
	}
	// Sorted by lines: the model commit owns 3 lines, the human 2.
	top := summary.Owners[0]
	if top.Kind != OwnerKindModel || top.Id != "claude" || top.Lines != 3 || top.DominantFiles != 1 {
		t.Errorf("top owner = %+v, want model claude with 3 lines / 1 dominant file", top)
	}
	second := summary.Owners[1]
	if second.Kind != OwnerKindHuman || second.Id != "jane@example.com" || second.Lines != 2 || second.Display != "Jane Doe" {
		t.Errorf("second owner = %+v, want Jane Doe with 2 lines", second)
	}
	if len(summary.Sponsors) != 1 {
		t.Fatalf("sponsors = %v, want exactly one", summary.Sponsors)
	}
	sp := summary.Sponsors[0]
	if sp.ModelId != "claude" || sp.AuthorEmail != "jane@example.com" || sp.Commits != 1 || sp.AuthorName != "Jane Doe" {
		t.Errorf("sponsor = %+v", sp)
	}
}
