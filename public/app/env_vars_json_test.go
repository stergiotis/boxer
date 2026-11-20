package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/config/env/envdoc"
	"github.com/stretchr/testify/require"
)

// TestEnvVarsJSONMatchesRegistry pins doc/env-vars.json to the specs this
// binary links (ADR-0009 §4). It lives here rather than in envdoc because
// only this package's import graph registers the full set.
func TestEnvVarsJSONMatchesRegistry(t *testing.T) {
	want, err := envdoc.RenderJSON()
	require.NoError(t, err)
	path := filepath.Join("..", "..", "doc", "env-vars.json")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got),
		"doc/env-vars.json does not match the registry; run `go generate ./public/config/env/...`")
}

// TestKnownNamesMatchRegistry pins the env package's generated name list to
// the specs this binary links, as the JSON snapshot is pinned.
func TestKnownNamesMatchRegistry(t *testing.T) {
	path := filepath.Join("..", "config", "env", "known_names.out.go")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(envdoc.RenderKnownNames()), string(got),
		"public/config/env/known_names.out.go does not match the registry; run `go generate ./public/config/env/...`")
}

// TestRetiredNamesAreNotRegistered keeps a tombstone from naming a live
// variable, and its replacement from naming a dead one.
func TestRetiredNamesAreNotRegistered(t *testing.T) {
	for _, r := range env.RetiredVars() {
		_, live := env.LookupVar(r.Name)
		require.Falsef(t, live, "%s is retired and registered", r.Name)
		if r.ReplacedBy != "" {
			_, live = env.LookupVar(r.ReplacedBy)
			require.Truef(t, live, "%s names %s as its replacement, which is not registered", r.Name, r.ReplacedBy)
		}
		require.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, r.Since, r.Name)
	}
	require.Empty(t, env.CheckEnviron([]string{"BOXER_LLM_MODEL=x"}), "a registered variable is not reported")
}

// TestScriptVarsAreDeclared keeps env.ScriptOwned current: every name in a
// watched namespace that a script under scripts/ (or boxer.sh) spells is
// registered or script-owned, so a process the scripts start does not warn
// about the scripts' own variables.
func TestScriptVarsAreDeclared(t *testing.T) {
	pattern := regexp.MustCompile(`\b(?:BOXER|IMZERO2)_[A-Z0-9_]+\b`)
	var files []string
	require.NoError(t, filepath.WalkDir(filepath.Join("..", "..", "scripts"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	}))
	files = append(files, filepath.Join("..", "..", "boxer.sh"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, name := range pattern.FindAllString(string(b), -1) {
			if _, ok := env.LookupVar(name); ok || slices.Contains(env.ScriptOwned, name) {
				continue
			}
			if slices.ContainsFunc(env.RetiredVars(), func(r env.Retired) bool { return r.Name == name }) {
				continue
			}
			t.Errorf("%s spells %s, which is neither registered nor in env.ScriptOwned", f, name)
		}
	}
}
