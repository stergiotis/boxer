package doclint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// dl013Repo stands up a repository shape: a .git marker, a go.mod, one
// package with an exported function, one test file importing a foreign
// package under a name that collides with nothing in the module, and the
// given Markdown document.
func dl013Repo(t *testing.T, md string) (doc string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	write("go.mod", "module example.com/m\n\ngo 1.24\n")
	write("lib/widget/widget.go", "package widget\n\nfunc New() int { return 0 }\n\ntype Fluid struct{}\n")
	write("lib/arrow/arrow.go", "package arrow\n\nconst Local = 1\n")
	write("lib/widget/widget_test.go", "package widget\n\nimport \"github.com/apache/arrow-go/v18/arrow\"\n\nvar _ arrow.Schema\n")
	write("doc/page.md", md)
	doc = filepath.Join(root, "doc", "page.md")
	return
}

func dl013Messages(t *testing.T, doc string) (msgs []string) {
	t.Helper()
	for _, f := range collectFindings(t, NewRuleDL013(), []string{doc}) {
		require.Equal(t, "DL013", f.RuleId)
		require.Equal(t, FindingSeverityError, f.Severity)
		msgs = append(msgs, f.Message)
	}
	return
}

func TestRuleDL013ReportsMissingIdentifier(t *testing.T) {
	doc := dl013Repo(t, "---\ntype: how-to\nstatus: draft\n---\n\n"+
		"```go\nw := widget.New()\n_ = widget.Old(w)\nvar f widget.Fluid\n```\n")
	msgs := dl013Messages(t, doc)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0], "`widget.Old`")
}

func TestRuleDL013SkipsWhatItCannotVouchFor(t *testing.T) {
	doc := dl013Repo(t, "---\ntype: how-to\nstatus: draft\n---\n\n"+
		// a local variable named like a package, a stdlib qualifier, a
		// name the module shares with a dependency, a fence that does not
		// parse, and a non-Go fence.
		"```go\nwidget := thing()\n_ = widget.Old\n_ = strings.Nope\nvar s arrow.Schema\n```\n\n"+
		"```go\nfunc (widget *T) Run() { widget.Old() }\n```\n\n"+
		"```go\nx := widget.Old(…)\n```\n\n"+
		"```sql\nSELECT widget.Old\n```\n")
	require.Empty(t, dl013Messages(t, doc))
}

func TestRuleDL013ResolvesThroughFenceImports(t *testing.T) {
	doc := dl013Repo(t, "---\ntype: how-to\nstatus: draft\n---\n\n"+
		"```go\nimport (\n\tw \"example.com/m/lib/widget\"\n\t\"github.com/apache/arrow-go/v18/arrow\"\n)\n\nvar _ = w.New()\nvar _ = w.Gone\nvar _ arrow.Anything\n```\n")
	msgs := dl013Messages(t, doc)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0], "`w.Gone`")
}

func TestRuleDL013OptOut(t *testing.T) {
	doc := dl013Repo(t, "---\ntype: how-to\nstatus: draft\n---\n\n"+
		"<!-- boxer:lint disable=DL013 reason=\"proposed API\" -->\n\n"+
		"```go\n_ = widget.Proposed()\n```\n\n"+
		"- item\n\n  ```go\n  _ = widget.Indented()\n  ```\n")
	msgs := dl013Messages(t, doc)
	require.Len(t, msgs, 1)
	require.Contains(t, msgs[0], "`widget.Indented`")
}
