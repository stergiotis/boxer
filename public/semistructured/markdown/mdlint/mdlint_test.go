package mdlint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdspan"
)

func rulesOf(fs []Finding) (ids []string) {
	for _, f := range fs {
		ids = append(ids, f.Rule)
	}
	return
}

func lintOnly(t *testing.T, id string, src string) []Finding {
	t.Helper()
	l := NewDefaultLinter()
	for _, r := range l.Rules() {
		if r.Id() != id {
			l.Disable(r.Id())
		}
	}
	return l.LintSource([]byte(src))
}

const clean = `---
title: A note
tags: [project/alpha, todo]
---
# Top

See [[#Details]], [[#Top#Details|the details]], [the end](#end) and the block [[#^b1]].
Also [[Other page#Missing]] — another page, not checked.

## Details

A claim.[^1] Some ` + "`[^x] [[#Nowhere]]`" + ` literal text.

> [!tip] Hint
> body

` + "```md\n[[#Nowhere]] [^y]\n```" + `

A paragraph to point at. ^b1

## End

[^1]: The note.
`

func TestCleanDocument(t *testing.T) {
	fs := NewDefaultLinter().LintSource([]byte(clean))
	assert.Empty(t, fs, "%+v", fs)
}

func TestEachRule(t *testing.T) {
	cases := []struct {
		id, src string
		n       int
	}{
		{"ML001", "# A\n\n[[#B]] ![[#A]] [[#^nope]] [x](#missing) [y](#a)\n", 3},
		{"ML002", "# A\n### C\n## B\n", 1},
		{"ML003", "# A\n## Same\n## same\n", 1},
		{"ML004", "---\na: [1\n---\nbody\n", 1},
		{"ML004", "---\n- a\n- b\n---\n", 1},
		{"ML004", "---\na: b\n", 1},
		{"ML005", "text\n\n```go\nnever closed\n", 1},
		{"ML006", "> [!sparkle] x\n> y\n\n> [!NOTE]\n> z\n", 1},
		{"ML007", "text [^a] and [^b]\n\n[^b]: def\n", 1},
		{"ML008", "text [^a]\n\n[^a]: def\n[^z]: orphan\n", 1},
		{"ML009", "---\ntags: [ok, 123, \"has space\"]\n---\n", 2},
		{"ML010", "[a]() ![b]() [[|alias]] [c](d)\n", 3},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			fs := lintOnly(t, c.id, c.src)
			assert.Len(t, fs, c.n, "%+v", fs)
			for _, f := range fs {
				assert.Equal(t, c.id, f.Rule)
				assert.Positive(t, f.Line)
			}
		})
	}
}

func TestFindingPositions(t *testing.T) {
	fs := lintOnly(t, "ML004", "---\ntitle: x\nbad: [\n---\n")
	require.Len(t, fs, 1)
	assert.Equal(t, int32(3), fs[0].Line, fs[0].Message)
	assert.Equal(t, SeverityError, fs[0].Severity)

	fs = lintOnly(t, "ML001", "# A\n\nsee [[#Nope]] here\n")
	require.Len(t, fs, 1)
	assert.Equal(t, int32(3), fs[0].Line)
	assert.Equal(t, int32(5), fs[0].Col)
	assert.Equal(t, int32(14), fs[0].EndCol)
}

func TestDisableAndWithin(t *testing.T) {
	src := "# A\n### B\n### B\n"
	l := NewDefaultLinter()
	assert.Equal(t, []string{"ML002", "ML003"}, rulesOf(l.LintSource([]byte(src))))
	l.Disable("ML002")
	assert.False(t, l.IsEnabled("ML002"))
	fs := l.LintSource([]byte(src))
	assert.Equal(t, []string{"ML003"}, rulesOf(fs))
	assert.Empty(t, Within(fs, mdspan.LineSpan{First: 1, Last: 2}))
	assert.Len(t, Within(fs, mdspan.LineSpan{First: 3, Last: 3}), 1)
	l.Enable("ML002")
	assert.Len(t, l.LintSource([]byte(src)), 2)
}

func TestRuleIdsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range AllRules() {
		assert.False(t, seen[r.Id()], r.Id())
		seen[r.Id()] = true
		assert.NotEmpty(t, r.Name())
		assert.NotEmpty(t, r.Summary())
	}
}
