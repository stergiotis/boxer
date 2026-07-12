package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOwnershipAnalyzerMailmap builds a throwaway git repository with a
// .mailmap that folds one author's old email into a canonical one, plus
// commits exercising co-author handling. It asserts:
//   - RunCommits canonicalizes the author email/name and exposes exactly the
//     distinct human co-authors (a self-co-author the mailmap folds into the
//     author is dropped, as is a repeat).
//   - a model co-author sets ModelTag and is excluded from CoAuthors.
//   - RunSummary merges the variant emails into one human owner (line-axis
//     merge); a model co-author's commit attributes its lines to the model
//     (provenance), and human co-authors own no surviving blame lines.
//
// Author emails are bare tokens (no '@') so git stores them verbatim and the
// parser exercises the same opaque-key logic as the unit tests.
func TestOwnershipAnalyzerMailmap(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=tester",
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
	// Form 3: fold jane_old into jane_new and fix the canonical name.
	write(".mailmap", "Jane Doe <jane_new> <jane_old>\n")
	write("a.txt", "a1\na2\n")
	run("add", "a.txt")
	run("commit", "--quiet", "--author=Jane Doe <jane_new>", "-m", "A")
	// Author uses the old email (jane_old); the mailmap must fold it to jane_new.
	// Human co-authors: bob, a self-co-author (jane_old -> jane_new, dropped),
	// and a repeat of bob (deduped).
	write("b.txt", "b1\nb2\nb3\n")
	run("add", "b.txt")
	run("commit", "--quiet", "--author=Jane Oldname <jane_old>", "-m",
		"B\n\n"+
			"Co-Authored-By: Bob Smith <bob>\n"+
			"Co-Authored-By: Jane Doe <jane_old>\n"+
			"Co-Authored-By: Bob Smith <bob>")
	// A model co-author on its own commit: sets ModelTag, excluded from
	// CoAuthors, and claims that commit's blame lines (provenance).
	write("c.txt", "c1\n")
	run("add", "c.txt")
	run("commit", "--quiet", "--author=Jane Doe <jane_new>", "-m",
		"C\n\nCo-Authored-By: Claude Fable 5 <fable>")

	raw, err := os.ReadFile(filepath.Join(dir, ".mailmap"))
	if err != nil {
		t.Fatal(err)
	}
	mm := ParseMailmap(string(raw))
	an := &OwnershipAnalyzer{Parallelism: 2, Mailmap: mm}
	ctx := context.Background()
	runner := &GitRunner{RepoPath: dir}

	summary, err := an.RunSummary(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalLines != 6 || summary.AttributedLines != 6 {
		t.Fatalf("line totals = %d/%d, want 6/6", summary.TotalLines, summary.AttributedLines)
	}
	var humans, models []OwnerTotal
	for _, ot := range summary.Owners {
		if ot.Kind == OwnerKindHuman {
			humans = append(humans, ot)
		} else {
			models = append(models, ot)
		}
	}
	// jane_new owns A (2) + B (3) = 5 lines: the old-email commit B folded in.
	if len(humans) != 1 || humans[0].Id != "jane_new" || humans[0].Lines != 5 || humans[0].Display != "Jane Doe" {
		t.Errorf("human owners = %+v, want one jane_new with 5 lines / display Jane Doe", humans)
	}
	// The model owns commit C's 1 line; bob co-authored B but owns none.
	if len(models) != 1 || models[0].Id != "claude" || models[0].Lines != 1 {
		t.Errorf("model owners = %+v, want claude with 1 line", models)
	}

	var commits []CommitRecord
	for rec, recErr := range an.RunCommits(ctx, runner) {
		if recErr != nil {
			t.Fatal(recErr)
		}
		commits = append(commits, rec)
	}
	if len(commits) != 3 {
		t.Fatalf("commits = %d, want 3", len(commits))
	}
	cc, cb, ca := commits[0], commits[1], commits[2] // newest first: C, B, A
	if cc.Subject != "C" || cc.AuthorEmail != "jane_new" || cc.ModelTag != "claude" || len(cc.CoAuthors) != 0 {
		t.Errorf("commit C = %+v, want jane_new, model claude, no human co-authors", cc)
	}
	if cb.Subject != "B" || cb.AuthorEmail != "jane_new" || cb.AuthorName != "Jane Doe" || cb.ModelTag != "" {
		t.Errorf("commit B = %+v, want Jane Doe <jane_new>, folded, no model", cb)
	}
	if len(cb.CoAuthors) != 1 || cb.CoAuthors[0].Email != "bob" || cb.CoAuthors[0].Name != "Bob Smith" {
		t.Errorf("commit B CoAuthors = %+v, want only Bob Smith <bob>", cb.CoAuthors)
	}
	if ca.Subject != "A" || ca.AuthorEmail != "jane_new" || ca.AuthorName != "Jane Doe" || len(ca.CoAuthors) != 0 {
		t.Errorf("commit A = %+v, want Jane Doe <jane_new>, no co-authors", ca)
	}
}

// TestLoadMailmap exercises the .mailmap loader against real git working trees:
// it reads the file from the repository root, finds that same root-level file
// when --repo points into a subdirectory (via git rev-parse --show-toplevel),
// and degrades to a nil map (not an error) when the file is absent or the path
// is not a git work tree — the two cases the analyzers treat as "no mailmap".
func TestLoadMailmap(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	gitInit := func(t *testing.T, dir string) {
		t.Helper()
		cmd := exec.Command("git", "init", "--quiet")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
	}
	const mailmap = "Jane Doe <jane_new> <jane_old>\n"

	t.Run("reads .mailmap from the repository root", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)
		if err := os.WriteFile(filepath.Join(dir, ".mailmap"), []byte(mailmap), 0o644); err != nil {
			t.Fatal(err)
		}
		mm, err := LoadMailmap(ctx, &GitRunner{RepoPath: dir})
		if err != nil {
			t.Fatal(err)
		}
		if mm == nil {
			t.Fatal("expected a non-nil mailmap")
		}
		if n, e := mm.Resolve("Jane", "jane_old"); n != "Jane Doe" || e != "jane_new" {
			t.Errorf("Resolve(Jane, jane_old) = (%q, %q), want (Jane Doe, jane_new)", n, e)
		}
	})

	t.Run("finds the root .mailmap from a subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)
		if err := os.WriteFile(filepath.Join(dir, ".mailmap"), []byte(mailmap), 0o644); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(dir, "pkg", "deep")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		mm, err := LoadMailmap(ctx, &GitRunner{RepoPath: sub})
		if err != nil {
			t.Fatal(err)
		}
		if mm == nil {
			t.Fatal("expected the root .mailmap to resolve from a subdirectory")
		}
	})

	t.Run("missing .mailmap yields nil, no error", func(t *testing.T) {
		dir := t.TempDir()
		gitInit(t, dir)
		mm, err := LoadMailmap(ctx, &GitRunner{RepoPath: dir})
		if err != nil {
			t.Fatal(err)
		}
		if mm != nil {
			t.Errorf("expected nil mailmap for a repo without .mailmap, got %+v", mm)
		}
	})

	t.Run("non-repository yields nil, no error", func(t *testing.T) {
		dir := t.TempDir() // never git-initialized
		mm, err := LoadMailmap(ctx, &GitRunner{RepoPath: dir})
		if err != nil {
			t.Fatalf("expected graceful nil outside a work tree, got error: %v", err)
		}
		if mm != nil {
			t.Errorf("expected nil mailmap outside a work tree, got %+v", mm)
		}
	})
}
