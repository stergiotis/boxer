package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestContributorAnalyzerMailmap builds a throwaway git repository with two
// commits by the same person under two emails and NO .mailmap on disk (so git
// shortlog does not canonicalize itself). It asserts the injected Go Mailmap
// folds the two shortlog identities into one canonical row (summed commits,
// canonical email) so the row keys join the ownership analyzer's human owner
// ids.
func TestContributorAnalyzerMailmap(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=tester",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run("init", "--quiet")
	write("a.txt", "a1\n")
	run("add", "a.txt")
	run("commit", "--quiet", "--author=Jane Doe <jane_new>", "-m", "A")
	write("b.txt", "b1\n")
	run("add", "b.txt")
	run("commit", "--quiet", "--author=Jane Old <jane_old>", "-m", "B")

	// No .mailmap on disk and no Go Mailmap: two distinct rows.
	none, err := (&ContributorAnalyzer{}).RunSummary(context.Background(), &GitRunner{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Contributors) != 2 || none.TotalCommits != 2 {
		t.Errorf("without mailmap: contributors=%d total=%d, want 2 distinct / 2",
			len(none.Contributors), none.TotalCommits)
	}

	// With the Go Mailmap the two emails fold into one canonical row.
	mm := ParseMailmap("Jane Doe <jane_new> <jane_old>\n")
	merged, err := (&ContributorAnalyzer{Mailmap: mm}).RunSummary(context.Background(), &GitRunner{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Contributors) != 1 {
		t.Fatalf("with mailmap: contributors=%d, want 1 (merged)", len(merged.Contributors))
	}
	row := merged.Contributors[0]
	if row.CommitCount != 2 || row.Percentage != 100.0 {
		t.Errorf("merged row = %+v, want 2 commits / 100%%", row)
	}
	if extractEmail(row.Author) != "jane_new" {
		t.Errorf("merged row Author = %q, email %q, want canonical jane_new", row.Author, extractEmail(row.Author))
	}
}
