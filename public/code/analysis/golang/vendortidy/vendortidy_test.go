package vendortidy

import (
	"go/build/constraint"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
}

const fixtureModulesTxt = `## workspace
# example.com/dep v1.2.0
## explicit; go 1.20
# example.com/dep v1.3.0
## explicit; go 1.26
example.com/dep/a
example.com/dep/b
# example.com/other v0.5.0
## explicit; go 1.21
example.com/other
# example.com/nopkg v1.0.0
## explicit; go 1.14
# example.com/linuxonly v0.1.0
## explicit; go 1.18
example.com/linuxonly
# example.com/tool v1.0.0
## explicit; go 1.22
example.com/tool/cmd/t
`

const fixtureMemberGoMod = `module example.com/member

go 1.24

ignore ./skipme

tool example.com/tool/cmd/t

require (
	example.com/dep v1.2.0
	example.com/stale v0.1.0
)

require example.com/nopkg v1.0.0 // indirect
`

// newFixture stages a vendored workspace with a library module and a member
// whose go.mod is stale against the vendor tree.
func newFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	writeTree(t, root, map[string]string{
		"go.work":                               "go 1.27\n\nuse (\n\t./lib\n\t./member\n)\n",
		"vendor/modules.txt":                    fixtureModulesTxt,
		"vendor/example.com/dep/a/a.go":         "package a\n\nimport _ \"example.com/dep/b\"\n",
		"vendor/example.com/dep/b/b.go":         "package b\n\nimport _ \"example.com/other\"\n",
		"vendor/example.com/other/o.go":         "package other\n\nimport _ \"strings\"\n",
		"vendor/example.com/linuxonly/l.go":     "package linuxonly\n",
		"vendor/example.com/tool/cmd/t/main.go": "package main\n\nfunc main() {}\n",
		"lib/go.mod":                            "module example.com/lib\n\ngo 1.27\n\nrequire example.com/dep v1.3.0\n",
		"lib/go.sum":                            "example.com/dep v1.3.0 h1:new=\nexample.com/dep v1.3.0/go.mod h1:newmod=\n",
		"lib/lib.go":                            "package lib\n\nimport _ \"example.com/dep/a\"\n",
		"lib/lib_test.go":                       "package lib\n\nimport _ \"example.com/notvendored\"\n",
		"member/go.mod":                         fixtureMemberGoMod,
		"member/main.go":                        "package main\n\nimport (\n\t\"fmt\"\n\n\t_ \"example.com/lib\"\n)\n\nfunc main() { fmt.Println() }\n",
		"member/main_test.go":                   "package main\n\nimport _ \"example.com/dep/a\"\n",
		"member/x_linux.go":                     "//go:build linux && !cgo\n\npackage main\n\nimport _ \"example.com/linuxonly\"\n",
		"member/ign.go":                         "//go:build ignore\n\npackage main\n\nimport _ \"example.com/missing\"\n",
		"member/skipme/s.go":                    "package skipme\n\nimport _ \"example.com/missing2\"\n",
		"member/testdata/t.go":                  "package t\n\nimport _ \"example.com/missing3\"\n",
		"member/_hidden/h.go":                   "package h\n\nimport _ \"example.com/missing4\"\n",
		"member/nested/go.mod":                  "module example.com/nested\n",
		"member/nested/n.go":                    "package nested\n\nimport _ \"example.com/missing5\"\n",
	})
	return
}

func testGoRoot(t *testing.T) string {
	t.Helper()
	out, err := extbin.Go.Output(t.Context(), extbin.Opts{}, "env", "GOROOT")
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func tidyFixture(t *testing.T, root string, dir string) (ws *Workspace, res Result) {
	t.Helper()
	var err error
	ws, err = LoadWorkspace(filepath.Join(root, "go.work"), testGoRoot(t))
	require.NoError(t, err)
	res, err = ws.Tidy(filepath.Join(root, dir))
	require.NoError(t, err)
	return
}

func TestTidyComputesRequirements(t *testing.T) {
	root := newFixture(t)
	_, res := tidyFixture(t, root, "member")

	assert.Equal(t, "example.com/member", res.Module)
	assert.Equal(t, []Requirement{
		{Path: "example.com/dep", Version: "v1.3.0"},
		{Path: "example.com/linuxonly", Version: "v0.1.0"},
		{Path: "example.com/nopkg", Version: "v1.0.0", Indirect: true, Kept: true},
		{Path: "example.com/other", Version: "v0.5.0", Indirect: true},
		{Path: "example.com/tool", Version: "v1.0.0", Indirect: true},
	}, res.Requires)
	assert.Equal(t, "1.26", res.GoVersion, "raised to the highest go version a required module declares")
	assert.False(t, res.HasUnresolved(), "ignored files, ignore-listed, testdata, _ dirs and nested modules are not walked")
	assert.Equal(t, []Problem{{Path: "example.com/lib", Kind: ProblemKindWorkspaceOnly}}, res.Problems)
}

func TestTidyRefusesUnresolvedImport(t *testing.T) {
	root := newFixture(t)
	writeTree(t, root, map[string]string{"member/extra.go": "package main\n\nimport _ \"example.com/missing\"\n"})
	_, res := tidyFixture(t, root, "member")
	require.True(t, res.HasUnresolved())
	assert.Contains(t, res.Problems, Problem{Path: "example.com/missing", Importer: "example.com/member", Kind: ProblemKindUnresolvedImport})
}

// A vendored module no workspace go.mod requires has no `## explicit` line, so
// a require of it fails Go's vendor check. It is reported, not required, and a
// require already in go.mod is dropped.
func TestTidyDoesNotRequireNotExplicitModule(t *testing.T) {
	root := newFixture(t)
	const memberGoMod = fixtureMemberGoMod + "\nrequire example.com/loose v0.3.0 // indirect\n"
	writeTree(t, root, map[string]string{
		"vendor/modules.txt":            fixtureModulesTxt + "# example.com/loose v0.3.0\nexample.com/loose\n",
		"vendor/example.com/loose/l.go": "package loose\n",
		"member/go.mod":                 memberGoMod,
		"member/extra.go":               "package main\n\nimport _ \"example.com/loose\"\n",
	})
	_, res := tidyFixture(t, root, "member")

	assert.False(t, res.HasUnresolved(), "the package is vendored")
	assert.Contains(t, res.Problems, Problem{Path: "example.com/loose", Version: "v0.3.0", Kind: ProblemKindNotExplicit})
	for _, r := range res.Requires {
		assert.NotEqual(t, "example.com/loose", r.Path)
	}

	changes, err := CompareRequires([]byte(memberGoMod), res)
	require.NoError(t, err)
	assert.Contains(t, changes, VersionChange{Path: "example.com/loose", OldVersion: "v0.3.0", Kind: VersionChangeKindRemoved, Indirect: true})
	out, err := FormatGoMod([]byte(memberGoMod), res)
	require.NoError(t, err)
	assert.NotContains(t, string(out), "example.com/loose")
}

func TestTidyRejectsDirOutsideWorkspace(t *testing.T) {
	root := newFixture(t)
	ws, err := LoadWorkspace(filepath.Join(root, "go.work"), testGoRoot(t))
	require.NoError(t, err)
	_, err = ws.Tidy(filepath.Join(root, "member", "nested"))
	require.Error(t, err)
}

func TestFormatGoMod(t *testing.T) {
	root := newFixture(t)
	_, res := tidyFixture(t, root, "member")
	out, err := FormatGoMod([]byte(fixtureMemberGoMod), res)
	require.NoError(t, err)
	mf, err := modfile.Parse("go.mod", out, nil)
	require.NoError(t, err)
	assert.Equal(t, "1.26", mf.Go.Version)
	assert.Len(t, mf.Ignore, 1, "other directives are kept")
	assert.Len(t, mf.Tool, 1)
	got := make([]string, 0, len(mf.Require))
	for _, r := range mf.Require {
		s := r.Mod.String()
		if r.Indirect {
			s += " indirect"
		}
		got = append(got, s)
	}
	assert.ElementsMatch(t, []string{
		"example.com/dep@v1.3.0", "example.com/linuxonly@v0.1.0",
		"example.com/nopkg@v1.0.0 indirect", "example.com/other@v0.5.0 indirect", "example.com/tool@v1.0.0 indirect",
	}, got)

	again, err := FormatGoMod(out, res)
	require.NoError(t, err)
	assert.Equal(t, string(out), string(again), "formatting is a fixpoint")
}

func TestFormatGoModDropsRedundantToolchain(t *testing.T) {
	src := "module example.com/m\n\ngo 1.26.0\n\ntoolchain go1.26.0\n"
	out, err := FormatGoMod([]byte(src), Result{GoVersion: "1.26.0"})
	require.NoError(t, err)
	assert.NotContains(t, string(out), "toolchain")

	src = "module example.com/m\n\ngo 1.26.0\n\ntoolchain go1.27.3\n"
	out, err = FormatGoMod([]byte(src), Result{GoVersion: "1.26.0"})
	require.NoError(t, err)
	assert.Contains(t, string(out), "toolchain go1.27.3")
}

func TestFormatGoModLayoutFollowsGoVersion(t *testing.T) {
	src := "module example.com/m\n\ngo 1.27\n\nrequire (\n\texample.com/a v1.0.0\n)\n\nrequire (\n\texample.com/x v1.0.0 // indirect\n)\n"
	res := Result{GoVersion: "1.27", Requires: []Requirement{
		{Path: "example.com/a", Version: "v1.0.0"},
		{Path: "example.com/x", Version: "v1.0.0", Indirect: true},
		{Path: "example.com/y", Version: "v1.0.0", Indirect: true},
	}}
	out, err := FormatGoMod([]byte(src), res)
	require.NoError(t, err)
	assert.Equal(t, "module example.com/m\n\ngo 1.27\n\nrequire example.com/a v1.0.0\n\n"+
		"require (\n\texample.com/x v1.0.0 // indirect\n\texample.com/y v1.0.0 // indirect\n)\n",
		string(out), "a new indirect joins the existing indirect block")
}

func TestCompareRequires(t *testing.T) {
	root := newFixture(t)
	_, res := tidyFixture(t, root, "member")
	changes, err := CompareRequires([]byte(fixtureMemberGoMod), res)
	require.NoError(t, err)
	byPath := make(map[string]VersionChange, len(changes))
	for _, c := range changes {
		byPath[c.Path] = c
	}
	assert.Equal(t, VersionChangeKindUpgraded, byPath["example.com/dep"].Kind)
	assert.Equal(t, VersionChangeKindRemoved, byPath["example.com/stale"].Kind)
	assert.Equal(t, VersionChangeKindAdded, byPath["example.com/other"].Kind)
	_, nopkg := byPath["example.com/nopkg"]
	assert.False(t, nopkg, "a kept requirement is unchanged")

	down, err := CompareRequires([]byte("module m\n\ngo 1.26\n\nrequire example.com/dep v1.4.0\n"), res)
	require.NoError(t, err)
	assert.Contains(t, down, VersionChange{Path: "example.com/dep", OldVersion: "v1.4.0", NewVersion: "v1.3.0", Kind: VersionChangeKindDowngraded})
}

func TestMergeGoSum(t *testing.T) {
	root := newFixture(t)
	ws, res := tidyFixture(t, root, "member")
	idx, err := ws.LoadSums()
	require.NoError(t, err)

	old := strings.Join([]string{
		"example.com/dep v1.2.0 h1:old=",
		"example.com/dep v1.2.0/go.mod h1:oldmod=",
		"example.com/nopkg v1.0.0/go.mod h1:nopkg=",
		"example.com/stale v0.1.0 h1:stale=",
		"example.com/stale v0.1.0/go.mod h1:stalemod=",
		"example.com/testdep v0.9.0 h1:testdep=",
		"",
	}, "\n")
	out, missing, err := MergeGoSum([]byte(old), []byte(fixtureMemberGoMod), res, idx)
	require.NoError(t, err)
	assert.Equal(t, strings.Join([]string{
		"example.com/dep v1.3.0 h1:new=",
		"example.com/dep v1.3.0/go.mod h1:newmod=",
		"example.com/nopkg v1.0.0/go.mod h1:nopkg=",
		"example.com/testdep v0.9.0 h1:testdep=",
		"",
	}, "\n"), string(out), "changed modules are swapped, dropped ones removed, the rest kept and sorted")
	assert.ElementsMatch(t, []module.Version{
		{Path: "example.com/linuxonly", Version: "v0.1.0"},
		{Path: "example.com/other", Version: "v0.5.0"},
		{Path: "example.com/tool", Version: "v1.0.0"},
	}, missing, "added modules the fixture carries no checksum for")

	assert.Empty(t, MissingSums(out, res, idx))
	assert.Equal(t, []string{"example.com/dep v1.3.0 h1:new=", "example.com/dep v1.3.0/go.mod h1:newmod="}, MissingSums([]byte(old), res, idx))
}

func TestSortSumLinesFollowsModuleOrder(t *testing.T) {
	set := map[string]struct{}{
		"example.com/a v1.10.0 h1:x=":         {},
		"example.com/a v1.9.0/go.mod h1:y=":   {},
		"example.com/a v1.9.0 h1:z=":          {},
		"example.com/a/b v1.0.0/go.mod h1:w=": {},
	}
	assert.Equal(t, []string{
		"example.com/a v1.9.0 h1:z=",
		"example.com/a v1.9.0/go.mod h1:y=",
		"example.com/a v1.10.0 h1:x=",
		"example.com/a/b v1.0.0/go.mod h1:w=",
	}, sortSumLines(set))
}

func TestParseModulesTxtSelectsHighestVersion(t *testing.T) {
	root := newFixture(t)
	v, err := ParseModulesTxt(filepath.Join(root, "vendor"))
	require.NoError(t, err)
	m, ok := v.Selected("example.com/dep")
	require.True(t, ok)
	assert.Equal(t, "v1.3.0", m.Version)
	assert.True(t, v.IsExplicit(module.Version{Path: "example.com/dep", Version: "v1.2.0"}), "a lower version some module requires is explicit too")
	assert.True(t, v.IsProvidingPackages("example.com/dep"))
	assert.False(t, v.IsProvidingPackages("example.com/nopkg"))
}

func TestAnyTags(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"//go:build linux", true},
		{"//go:build !linux", true},
		{"//go:build linux && !linux", true},
		{"//go:build ignore", false},
		{"//go:build !ignore", true},
		{"//go:build linux && ignore", false},
		{"//go:build foo-bar", false},
	}
	for _, c := range cases {
		x, err := constraint.Parse(c.expr)
		if err != nil {
			assert.False(t, c.want, c.expr)
			continue
		}
		assert.Equal(t, c.want, evalAnyTags(x, true), c.expr)
	}
}

func TestIsBuiltUnderAnyTags(t *testing.T) {
	assert.False(t, isBuiltUnderAnyTags([]byte("// +build ignore\n\npackage p\n")), "a +build line before the blank line counts")
	assert.True(t, isBuiltUnderAnyTags([]byte("// +build ignore\npackage p\n")), "a +build line not followed by a blank line does not")
	assert.False(t, isBuiltUnderAnyTags([]byte("/* licence */\n//go:build ignore\n\npackage p\n")))
	assert.True(t, isBuiltUnderAnyTags([]byte("package p\n\n//go:build ignore\n")), "a //go:build line after the package clause is not a constraint")
	assert.False(t, isBuiltUnderAnyTags([]byte("//go:build linux\n//go:build darwin\n\npackage p\n")), "two //go:build lines")
}
