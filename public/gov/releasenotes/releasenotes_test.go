package releasenotes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBreakingSubject(t *testing.T) {
	for subject, want := range map[string]bool{
		"refactor(task)!: Run takes the context first":   true,
		"refactor!: remove six packages nothing imports": true,
		"fix(audio,imzero2)!: the audio lane recovers":   true,
		"feat(chat): one place for a decision":           false,
		"docs: the word ! appears later!: here":          false,
		"Revert \"x\"!: y":                               false,
		"refactor(task!: unbalanced":                     false,
		"!: no type":                                     false,
		"no colon at all!":                               false,
	} {
		assert.Equal(t, want, IsBreakingSubject(subject), subject)
	}
}

func TestBreaksParagraph(t *testing.T) {
	body := "refactor(task)!: Run takes the context first\n\n" +
		"CS002 wants context.Context first.\n\n" +
		"Breaks: task.Run(ctx, api, opts, work) — the first two arguments swap. Its\n" +
		"two callers are moved.\n\n" +
		"Co-Authored-By: Someone <someone@example.com>\n"
	assert.Equal(t, "task.Run(ctx, api, opts, work) — the first two arguments swap. Its two callers are moved.", BreaksParagraph(body))
	assert.Empty(t, BreaksParagraph("fix: x\n\nNo such paragraph.\n"))
	// The label must open the paragraph, not appear inside it.
	assert.Empty(t, BreaksParagraph("fix: x\n\nThis Breaks: nothing.\n"))
	assert.Equal(t, "a b", BreaksParagraph("x!: y\r\n\r\nBreaks: a\r\nb\r\n"))
}

func TestParseCommitLog(t *testing.T) {
	raw := "aaa" + fieldSep + "refactor(x)!: one" + fieldSep + "refactor(x)!: one\n\nBreaks: old → new.\n" + recordSep + "\n" +
		"bbb" + fieldSep + "feat(y): two" + fieldSep + "feat(y): two\n" + recordSep + "\n"
	commits := ParseCommitLog(raw)
	require.Len(t, commits, 2)
	assert.Equal(t, Commit{Hash: "aaa", Subject: "refactor(x)!: one", Body: "refactor(x)!: one\n\nBreaks: old → new.\n"}, commits[0])
	assert.Equal(t, "bbb", commits[1].Hash)
	breaking := BreakingCommits(commits)
	require.Len(t, breaking, 1)
	assert.Equal(t, BreakingCommit{Hash: "aaa", Subject: "refactor(x)!: one", Breaks: "old → new."}, breaking[0])
	assert.Empty(t, ParseCommitLog(""))
}

func TestPackageChanges(t *testing.T) {
	before := PackageDirs([]string{
		"public/a/a.go", "public/a/a_test.go",
		"public/b/b.go",
		"public/c/c.go", "public/c/d.go",
		"public/x/internal/y/y.go",
		"public/onlytests/t_test.go",
		"doc/thing.go",
	})
	assert.Equal(t, map[string]struct{}{"public/a": {}, "public/b": {}, "public/c": {}}, before)
	after := PackageDirs([]string{"public/a/a.go", "public/c2/c.go", "public/c2/d.go", "public/e/c.go"})
	changes := PackageChanges(before, after, map[string]string{
		"public/c/c.go": "public/c2/c.go",
		"public/c/d.go": "public/c2/d.go",
		"public/b/b.go": "public/gone/b.go", // destination not a package at the end: still a removal
	})
	assert.Equal(t, []PackageChange{
		{Dir: "public/b", Kind: PackageChangeKindRemoved},
		{Dir: "public/c", Kind: PackageChangeKindRenamed, To: "public/c2"},
	}, changes)
}

func TestModuleChanges(t *testing.T) {
	before, err := ParseGoMod([]byte("module m\n\ngo 1.27.0\n\nrequire (\n\ta.example/x v1.0.0\n\tb.example/y v0.1.0 // indirect\n\tc.example/z v1.2.0\n)\n"))
	require.NoError(t, err)
	after, err := ParseGoMod([]byte("module m\n\ngo 1.27.3\n\ntoolchain go1.27.4\n\nrequire (\n\ta.example/x v1.1.0\n\tb.example/y v0.1.0 // indirect\n\td.example/w v0.0.1 // indirect\n)\n"))
	require.NoError(t, err)
	assert.Equal(t, "1.27.0", before.Go)
	assert.Equal(t, "1.27.3", after.Go)
	assert.Equal(t, "go1.27.4", after.Toolchain)
	assert.Equal(t, []ModuleChange{
		{Path: "a.example/x", From: "v1.0.0", To: "v1.1.0"},
		{Path: "c.example/z", From: "v1.2.0"},
		{Path: "d.example/w", To: "v0.0.1", Indirect: true},
	}, ModuleChanges(before, after))
}

func TestRender(t *testing.T) {
	n := Notes{
		From: "v0.0.1", To: "HEAD", NumCommits: 3,
		Breaking: []BreakingCommit{
			{Hash: "0123456789abcdef", Subject: "refactor(x)!: one", Breaks: "old → new."},
			{Hash: "fedcba9876543210", Subject: "refactor!: two"},
		},
		Packages:   []PackageChange{{Dir: "public/b", Kind: PackageChangeKindRemoved}, {Dir: "public/c", Kind: PackageChangeKindRenamed, To: "public/c2"}},
		GoFrom:     "1.27.0",
		GoTo:       "1.27.3",
		Modules:    []ModuleChange{{Path: "a.example/x", From: "v1.0.0", To: "v1.1.0"}, {Path: "d.example/w", To: "v0.0.1", Indirect: true}},
		Migrations: []string{"doc/migration/2026-12-x.md"},
	}
	src := string(Render(n, "doc/releases", "apidiff v0.0.1..HEAD: 1 compared\n"))
	for _, want := range []string{
		"generated: true",
		"# Release notes: v0.0.1..HEAD",
		"- `go` directive: 1.27.0 → 1.27.3.",
		"- `toolchain` line: absent at both ends.",
		"## Breaking changes (2)",
		"### refactor(x)!: one\n\n`0123456789ab` — old → new.",
		"`fedcba987654` — the commit has no `Breaks:` paragraph",
		"- `public/b` (removed)",
		"- `public/c` → `public/c2`",
		"1 direct and 1 indirect requirement(s) changed.",
		"| `a.example/x` | v1.0.0 | v1.1.0 |",
		"No snapshot under `contracts/` changed.",
		"- [2026-12-x.md](../../doc/migration/2026-12-x.md)",
		"```text\napidiff v0.0.1..HEAD: 1 compared\n```",
	} {
		assert.Contains(t, src, want)
	}
	assert.False(t, strings.Contains(src, "d.example/w |"), "indirect requirements are counted, not listed")
	assert.Equal(t, src, string(Render(n, "doc/releases", "apidiff v0.0.1..HEAD: 1 compared\n")), "deterministic")
}
