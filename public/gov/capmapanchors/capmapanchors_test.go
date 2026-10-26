package capmapanchors

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
)

// fixtureTree writes the smallest checkout every file-backed kind can be
// resolved against: one accepted and one superseded decision, a package with
// both test lanes and one with none, a page of each doc kind, an app, an
// applet, a scene and a skill.
func fixtureTree(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		"go.mod": "module example.test\n",
		"doc/adr/0001-first.md": "---\nstatus: accepted\ndate: 2026-01-01\n---\n# ADR-0001: First\n\n" +
			"## Milestones\n\n- **M0 — one.** ✓\n- **M1 — two.**\n",
		"doc/adr/0002-old.md":                    "---\nstatus: superseded\nsuperseded-by: ADR-0001\n---\n# ADR-0002: Old\n",
		"doc/howto/do-it.md":                     "# Do it\n",
		"doc/howto/README.md":                    "# not a page\n",
		"doc/explanation/why.md":                 "# Why\n",
		"doc/explanation/positioning/thing.md":   "# Thing\n",
		"doc/trials/speed/README.md":             "# Speed\n",
		"doc/skills/drive/SKILL.md":              "# Drive\n",
		"doc/env-vars.md":                        "# Environment variables\n\n| Name | Type |\n|---|---|\n| `BOXER_FIXTURE_VAR` | path |\n",
		"apps/thing/app_register.go":             "package thing\n\nvar manifest = app.Manifest{}\n",
		"apps/thing/scenes/thing-walk.scene.md":  "---\n---\n",
		"apps/sqlapplet/bookx/x-lens.md":         "---\n---\n",
		"public/tested/tested.go":                "package tested\n",
		"public/tested/tested_test.go":           "package tested\n",
		"public/tested/live_integration_test.go": "//go:build integration\n\npackage tested\n",
		"public/bare/bare.go":                    "package bare\n",
		"public/onlytests/x_test.go":             "package onlytests\n",
		"vendor/dep/dep.go":                      "package dep\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return root
}

func resolve(t *testing.T, r *TreeResolver, text string) (res capmapcorpus.AnchorResolution) {
	t.Helper()
	a, err := capmapcorpus.ParseAnchor(text)
	require.NoError(t, err)
	return r.ResolveAnchor(a)
}

// One anchor of each kind that must resolve, and one of each that must not:
// a resolver reporting direct for everything is the failure the verification
// plan names.
func TestTreeResolverAnswersEveryFileBackedKind(t *testing.T) {
	r := New(fixtureTree(t))
	for _, text := range []string{
		"adr:0001", "adr:0002", "pkg:public/tested", "pkg:public/bare", "pkg:public", "app:thing",
		"howto:do-it", "explanation:why", "positioning:thing", "trial:speed",
		"applet:x-lens", "scene:thing-walk", "skill:drive", "table:boxer.facts", "env:BOXER_FIXTURE_VAR",
	} {
		assert.Equal(t, capmapcorpus.ResolutionDirect, resolve(t, r, text).Resolution, text)
	}
	for _, text := range []string{
		"adr:0003", "adr:abc", "pkg:public/nothing", "pkg:public/onlytests", "pkg:vendor/dep", "app:other",
		"howto:README", "howto:missing", "explanation:do-it", "positioning:why", "trial:nothing",
		"applet:nothing", "scene:thing", "skill:fly", "table:boxer.other", "table:other.facts", "env:BOXER_ADR_DIR",
	} {
		assert.Equal(t, capmapcorpus.ResolutionUnresolved, resolve(t, r, text).Resolution, text)
	}
	assert.Equal(t, "accepted", resolve(t, r, "adr:0001").Status)
	assert.Equal(t, "superseded", resolve(t, r, "adr:0002").Status, "a retired decision resolves, with its status")
}

// Without a checkout the file-backed kinds are unchecked, and so are the
// registry-backed ones in a process nothing has registered into.
func TestTreeResolverIsUncheckedWithoutACorpus(t *testing.T) {
	r := &TreeResolver{}
	for _, text := range []string{"adr:0001", "pkg:x", "howto:x", "app:x", "table:keelson/competence"} {
		assert.Equal(t, capmapcorpus.ResolutionUnchecked, resolve(t, r, text).Resolution, text)
	}
	// The facts table is a compile-time coordinate, reachable from anywhere.
	assert.Equal(t, capmapcorpus.ResolutionDirect, resolve(t, r, "table:boxer.facts").Resolution)
	// With no generated page to read, env falls back to the registry in this
	// process, which holds whatever the packages linked into the binary
	// declared — the decision corpus's own variables, here — so a name it
	// lacks is unresolved rather than unchecked.
	assert.Equal(t, capmapcorpus.ResolutionDirect, resolve(t, r, "env:BOXER_ADR_DIR").Resolution)
	assert.Equal(t, capmapcorpus.ResolutionUnresolved, resolve(t, r, "env:BOXER_NOT_A_VARIABLE").Resolution)
}

func TestSignalsAndSupportedRung(t *testing.T) {
	r := New(fixtureTree(t))
	vault := t.TempDir()
	note := "---\nname: Thing\nlevel: 2\nrealised_by: [adr:0001, adr:0002, pkg:public/tested, pkg:public/bare, app:thing, howto:do-it, positioning:thing, adr:0009]\n---\n\n# Scope\n\nx\n"
	require.NoError(t, os.WriteFile(filepath.Join(vault, "thing.md"), []byte(note), 0o644))
	corpus, err := capmapcorpus.ParseDirWith(vault, r)
	require.NoError(t, err)

	signals := r.SignalsFor("thing", corpus.Relations)
	byKey := map[string]Signal{}
	for _, s := range signals {
		byKey[s.Anchor+"|"+s.Name] = s
	}
	assert.Equal(t, "accepted", byKey["adr:0001|"+SignalDecision].Value)
	assert.Equal(t, rungDecided, byKey["adr:0001|"+SignalDecision].Rung)
	assert.Equal(t, "1/2", byKey["adr:0001|"+SignalMilestones].Value)
	assert.Equal(t, rungPartial, byKey["adr:0001|"+SignalMilestones].Rung)
	assert.Equal(t, 0, byKey["adr:0002|"+SignalDecision].Rung, "a retired decision decides nothing")
	assert.Equal(t, "default+integration", byKey["pkg:public/tested|"+SignalTests].Value)
	assert.Equal(t, rungExercised, byKey["pkg:public/tested|"+SignalTests].Rung)
	assert.Equal(t, "none", byKey["pkg:public/bare|"+SignalTests].Value)
	assert.Equal(t, rungEndToEnd, byKey["app:thing|"+SignalSurface].Rung)
	assert.Equal(t, rungDocumented, byKey["howto:do-it|"+SignalDocumented].Rung)
	assert.Equal(t, rungPositioned, byKey["positioning:thing|"+SignalPositioned].Rung)
	assert.Equal(t, "unresolved", byKey["adr:0009|"+SignalAnchored].Value)
	_, has := byKey["adr:0009|"+SignalDecision]
	assert.False(t, has, "an unresolved anchor yields no evidence beyond its resolution")

	// Rungs 1..7 all hold; 8 needs a trial, so the supported rung is 7.
	assert.Equal(t, 7, SupportedRung(signals))

	// Take the howto away and rung 6 fails: the positioning page above it
	// cannot carry the rung, which is the ISO 33020 rule and the point.
	var withoutHowto []Signal
	for _, s := range signals {
		if s.Anchor != "howto:do-it" {
			withoutHowto = append(withoutHowto, s)
		}
	}
	assert.Equal(t, 5, SupportedRung(withoutHowto))
	assert.Equal(t, 0, SupportedRung(nil))
}

// A package tree carries the union of its packages' lanes, so a subsystem
// anchored by its directory is exercised when any package in it is.
func TestPackageTreeFoldsItsLanes(t *testing.T) {
	r := New(fixtureTree(t))
	idx := r.index()
	assert.Equal(t, pkgInfo{DefaultTests: true, IntegrationTests: true}, idx.pkgs["public"])
	assert.Equal(t, pkgInfo{}, idx.pkgs["public/bare"])
	_, onlyTests := idx.pkgs["public/onlytests"]
	assert.False(t, onlyTests)
}

func TestAppKey(t *testing.T) {
	assert.Equal(t, "watchbill", appKey("github.com/example/boxer/apps/watchbill"))
	assert.Equal(t, "sqlapplet", appKey("github.com/example/boxer/apps/sqlapplet/comp-browser"))
	assert.Equal(t, "thing", appKey("thing"))
}

func TestFindRootFromInsideACheckout(t *testing.T) {
	root := fixtureTree(t)
	nested := filepath.Join(root, "public", "tested")
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(nested))
	t.Cleanup(func() { _ = os.Chdir(wd) })
	got, ok := FindRoot()
	require.True(t, ok)
	assert.Equal(t, root, got)
	assert.Equal(t, root, New("").Root())
}
