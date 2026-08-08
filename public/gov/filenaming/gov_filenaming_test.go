package filenaming

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture writes files (path -> contents) under a temp dir and returns it.
func fixture(t *testing.T, files map[string]string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	return
}

func strs(findings []Finding) (out []string) {
	out = make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.String())
	}
	return
}

func TestN1FileBasenames(t *testing.T) {
	dir := fixture(t, map[string]string{
		"public/a/ok_name.go":  "package a\n",
		"public/a/BadName.go":  "package a\n",
		"public/a/has-dash.go": "package a\n",
		"public/a/9leading.go": "package a\n",
	})
	f, err := CheckE(Config{Dir: dir})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"file:public/a/BadName.go",
		"file:public/a/has-dash.go",
		"file:public/a/9leading.go",
	}, strs(f))
}

func TestN1SkipsGeneratedAndAttic(t *testing.T) {
	dir := fixture(t, map[string]string{
		"public/a/ok.go":          "package a\n",
		"public/a/BadName.out.go": "package a\n",
		"public/a/BadName.gen.go": "package a\n",
		"public/attic/BadName.go": "package a\n",
	})
	f, err := CheckE(Config{Dir: dir})
	require.NoError(t, err)
	assert.Empty(t, strs(f), "generated files and attic are out of scope")
}

func TestN6PackageNames(t *testing.T) {
	dir := fixture(t, map[string]string{
		"public/good/x.go":  "package good\n",
		"public/under/x.go": "package bad_name\n",
		"public/upper/x.go": "package BadName\n",
	})
	f, err := CheckE(Config{Dir: dir})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"package:public/under",
		"package:public/upper",
	}, strs(f))
}

// An external <pkg>_test package is Go-standard; a directory holding only those
// yields no package name and is skipped rather than reported.
func TestN6ExemptsExternalTestPackages(t *testing.T) {
	dir := fixture(t, map[string]string{
		"public/a/a_test.go": "package a_test\n",
	})
	f, err := CheckE(Config{Dir: dir})
	require.NoError(t, err)
	assert.Empty(t, strs(f))
}

func TestN7AppPrefix(t *testing.T) {
	dir := fixture(t, map[string]string{
		"apps/play/play_thing.go": "package main\n",
		"apps/play/loose.go":      "package main\n",
		// Exemptions.
		"apps/play/main.go":         "package main\n",
		"apps/play/doc.go":          "package main\n",
		"apps/play/app_register.go": "package main\n",
		"apps/play/play.go":         "package main\n",
		"apps/play/play_test.go":    "package main\n",
		// Nested files are not "directly under" apps/<n>/.
		"apps/play/sub/loose.go": "package sub\n",
	})
	f, err := CheckE(Config{Dir: dir})
	require.NoError(t, err)
	assert.Equal(t, []string{"app-prefix:apps/play/loose.go"}, strs(f))
}

func TestMissingRootIsNotAnError(t *testing.T) {
	dir := fixture(t, map[string]string{"public/a/ok.go": "package a\n"})
	f, err := CheckE(Config{Dir: dir})
	require.NoError(t, err, "a repository with no apps/ tree is well-formed")
	assert.Empty(t, strs(f))
}

func TestConfigRootsOverride(t *testing.T) {
	dir := fixture(t, map[string]string{
		"src/a/BadName.go":    "package a\n",
		"public/a/BadName.go": "package a\n",
	})
	f, err := CheckE(Config{Dir: dir, Roots: []string{"src"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"file:src/a/BadName.go"}, strs(f))
}

func TestDiffSeparatesNewFromStale(t *testing.T) {
	findings := []Finding{
		{Rule: RuleN1, Path: "public/a/BadName.go"},
		{Rule: RuleN1, Path: "public/b/Other.go"},
	}
	baseline := []string{
		"file:public/a/BadName.go",
		"file:public/gone/Removed.go",
	}
	fresh, stale := Diff(findings, baseline)
	assert.Equal(t, []string{"file:public/b/Other.go"}, fresh)
	assert.Equal(t, []string{"file:public/gone/Removed.go"}, stale,
		"a baseline entry keeping nothing alive must surface")
}

func TestLoadBaselineE(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "baseline.txt")
	require.NoError(t, os.WriteFile(p, []byte(
		"# a comment\n\nfile:public/a/X.go\n  package:public/b  \nfile:public/a/X.go\n"), 0o644))

	got, err := LoadBaselineE(p)
	require.NoError(t, err)
	assert.Equal(t, []string{"file:public/a/X.go", "package:public/b"}, got)

	got, err = LoadBaselineE(filepath.Join(dir, "absent.txt"))
	require.NoError(t, err, "an absent baseline means an empty one")
	assert.Empty(t, got)
}

// Findings render as the baseline line format, which is what makes an existing
// baseline file keep matching after the port from shell.
func TestFindingStringIsTheBaselineFormat(t *testing.T) {
	assert.Equal(t, "file:a/b.go", Finding{Rule: RuleN1, Path: "a/b.go"}.String())
	assert.Equal(t, "package:a/b", Finding{Rule: RuleN6, Path: "a/b"}.String())
	assert.Equal(t, "app-prefix:apps/x/y.go", Finding{Rule: RuleN7, Path: "apps/x/y.go"}.String())
}

func TestExcludeSuppressesFindings(t *testing.T) {
	dir := fixture(t, map[string]string{
		"public/a/BadName.go":     "package a\n",
		"public/vendored/Bad.go":  "package vendored\n",
		"public/b/AlsoBadName.go": "package b\n",
	})
	f, err := CheckE(Config{Dir: dir, Exclude: []string{"vendored/"}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"file:public/a/BadName.go",
		"file:public/b/AlsoBadName.go",
	}, strs(f))
}
