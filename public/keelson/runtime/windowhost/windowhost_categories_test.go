//go:build llm_generated_opus47

package windowhost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func mkManifestCat(id, display, cat string) (m app.Manifest) {
	m = app.Manifest{
		Id:       app.AppIdT(id),
		Version:  "0.1.0",
		Display:  display,
		Category: cat,
		Surface:  app.SurfaceWindowed,
	}
	return
}

func TestGroupByCategory_EmptyInputReturnsNil(t *testing.T) {
	groups := groupByCategory(nil)
	assert.Empty(t, groups)
}

func TestGroupByCategory_PreferredOrderRespected(t *testing.T) {
	// Throw the manifests in in scrambled order; preferred categories
	// must come back in preferredCategoryOrder regardless.
	in := []app.Manifest{
		mkManifestCat("a", "A", "Demos"),
		mkManifestCat("b", "B", "Tools"),
		mkManifestCat("c", "C", "Runtime"),
	}
	groups := groupByCategory(in)
	assert.Equal(t, []string{"Runtime", "Tools", "Demos"},
		categoriesOf(groups))
}

func TestGroupByCategory_UnknownCategoriesAlphabeticAfterPreferred(t *testing.T) {
	// Custom categories not in preferredCategoryOrder land alphabetised
	// after the named buckets but before "Other".
	in := []app.Manifest{
		mkManifestCat("a", "A", "Demos"),
		mkManifestCat("b", "B", "Charts"),
		mkManifestCat("c", "C", "Runtime"),
		mkManifestCat("d", "D", "Widgets"),
		mkManifestCat("e", "E", ""),
	}
	groups := groupByCategory(in)
	assert.Equal(t,
		[]string{"Runtime", "Demos", "Charts", "Widgets", "Other"},
		categoriesOf(groups))
}

func TestGroupByCategory_EmptyCategoryRoutesToOtherBucket(t *testing.T) {
	in := []app.Manifest{
		mkManifestCat("a", "A", ""),
		mkManifestCat("b", "B", "Tools"),
	}
	groups := groupByCategory(in)
	assert.Equal(t, []string{"Tools", "Other"}, categoriesOf(groups))
	// The empty-category manifest lands inside "Other", not dropped.
	other := findGroup(t, groups, "Other")
	assert.Len(t, other.Manifests, 1)
	assert.Equal(t, app.AppIdT("a"), other.Manifests[0].Id)
}

func TestGroupByCategory_OtherBucketAlwaysLast(t *testing.T) {
	// Even when an alphabetised unknown category would otherwise sort
	// after "Other", the catch-all stays at the end.
	in := []app.Manifest{
		mkManifestCat("a", "A", ""),
		mkManifestCat("b", "B", "Zeppelin"),
	}
	groups := groupByCategory(in)
	assert.Equal(t, []string{"Zeppelin", "Other"}, categoriesOf(groups))
}

func TestGroupByCategory_WithinCategorySortsByDisplay(t *testing.T) {
	in := []app.Manifest{
		mkManifestCat("z.id", "Zulu", "Demos"),
		mkManifestCat("a.id", "Alpha", "Demos"),
		mkManifestCat("m.id", "Mike", "Demos"),
	}
	groups := groupByCategory(in)
	demos := findGroup(t, groups, "Demos")
	got := make([]string, 0, len(demos.Manifests))
	for _, m := range demos.Manifests {
		got = append(got, m.Display)
	}
	assert.Equal(t, []string{"Alpha", "Mike", "Zulu"}, got)
}

func TestGroupByCategory_TieBrokenByIdWhenDisplayIdentical(t *testing.T) {
	// Two manifests with identical Display still produce stable
	// ordering: Id breaks the tie. Prevents flaky test output and
	// flicker on legitimate duplicates (e.g., two copies of the same
	// app shipped under different module paths).
	in := []app.Manifest{
		mkManifestCat("zzz.id", "Same", "Demos"),
		mkManifestCat("aaa.id", "Same", "Demos"),
	}
	groups := groupByCategory(in)
	demos := findGroup(t, groups, "Demos")
	assert.Equal(t, app.AppIdT("aaa.id"), demos.Manifests[0].Id)
	assert.Equal(t, app.AppIdT("zzz.id"), demos.Manifests[1].Id)
}

func TestFilterManifests_EmptyQueryReturnsInput(t *testing.T) {
	in := []app.Manifest{
		mkManifestCat("a", "Alpha", "Demos"),
		mkManifestCat("b", "Bravo", "Tools"),
	}
	got := filterManifests(in, "")
	assert.Equal(t, in, got)
}

func TestFilterManifests_WhitespaceOnlyQueryReturnsInput(t *testing.T) {
	// "  " trimmed is empty — same fast-path as the empty case so the
	// launcher doesn't suddenly hide everything when the user
	// accidentally types a space.
	in := []app.Manifest{
		mkManifestCat("a", "Alpha", "Demos"),
	}
	got := filterManifests(in, "   ")
	assert.Equal(t, in, got)
}

func TestFilterManifests_CaseInsensitiveDisplayMatch(t *testing.T) {
	in := []app.Manifest{
		mkManifestCat("a", "Log Viewer", "Runtime"),
		mkManifestCat("b", "SQL Playground", "Tools"),
		mkManifestCat("c", "Hacker News explorer", "Tools"),
	}
	got := filterManifests(in, "log")
	require.Len(t, got, 1)
	assert.Equal(t, app.AppIdT("a"), got[0].Id)
	// And the uppercase query hits the same row.
	got = filterManifests(in, "LOG")
	require.Len(t, got, 1)
	assert.Equal(t, app.AppIdT("a"), got[0].Id)
}

func TestFilterManifests_CategoryMatchSurfacesEntireBucket(t *testing.T) {
	// Typing "demo" returns every Demos entry even when no individual
	// Display contains "demo" — the use case is "show me the demos"
	// without remembering each app's specific name.
	in := []app.Manifest{
		mkManifestCat("a", "Widget gallery", "Demos"),
		mkManifestCat("b", "Treemap", "Demos"),
		mkManifestCat("c", "Regex explorer", "Tools"),
	}
	got := filterManifests(in, "demo")
	require.Len(t, got, 2)
	gotIds := []app.AppIdT{got[0].Id, got[1].Id}
	assert.ElementsMatch(t, []app.AppIdT{"a", "b"}, gotIds)
}

func TestFilterManifests_NoMatchReturnsEmpty(t *testing.T) {
	in := []app.Manifest{
		mkManifestCat("a", "Alpha", "Demos"),
		mkManifestCat("b", "Bravo", "Tools"),
	}
	got := filterManifests(in, "xyzzy")
	assert.Empty(t, got)
}

func TestFilterManifests_PreservesInputOrder(t *testing.T) {
	// The launcher's flat hit list applies its own sort downstream
	// (sortManifestsByDisplay); the filter itself just walks the input
	// and keeps matches in the order they appeared. Pin that contract.
	in := []app.Manifest{
		mkManifestCat("c", "Zulu match", "X"),
		mkManifestCat("a", "Alpha match", "X"),
		mkManifestCat("b", "Bravo nope", "X"),
		mkManifestCat("d", "Mike match", "X"),
	}
	got := filterManifests(in, "match")
	require.Len(t, got, 3)
	assert.Equal(t, app.AppIdT("c"), got[0].Id)
	assert.Equal(t, app.AppIdT("a"), got[1].Id)
	assert.Equal(t, app.AppIdT("d"), got[2].Id)
}

func TestFilterManifests_IdAndTitleNotMatched(t *testing.T) {
	// Id and Title aren't part of the match surface (Id is the full
	// import path and would match every entry on common substrings
	// like "github"; Title is usually a wordier Display variant).
	in := []app.Manifest{
		{
			Id:      "github.com/example/foo",
			Display: "Foo",
			Title:   "Foo Pro",
			Surface: app.SurfaceWindowed,
		},
	}
	// "github" would match the Id, but we don't search Id.
	got := filterManifests(in, "github")
	assert.Empty(t, got)
	// "Pro" lives only in Title; not in scope.
	got = filterManifests(in, "Pro")
	assert.Empty(t, got)
}

func TestSortManifestsByDisplay_DisplayThenId(t *testing.T) {
	in := []app.Manifest{
		mkManifestCat("z", "Charlie", "X"),
		mkManifestCat("y", "Alpha", "X"),
		mkManifestCat("aaa", "Charlie", "X"),
		mkManifestCat("x", "Bravo", "X"),
	}
	sortManifestsByDisplay(in)
	displays := make([]string, 0, len(in))
	ids := make([]app.AppIdT, 0, len(in))
	for _, m := range in {
		displays = append(displays, m.Display)
		ids = append(ids, m.Id)
	}
	assert.Equal(t, []string{"Alpha", "Bravo", "Charlie", "Charlie"}, displays)
	// Tie on Display=Charlie broken by Id ascending: "aaa" before "z".
	assert.Equal(t, []app.AppIdT{"y", "x", "aaa", "z"}, ids)
}

func categoriesOf(groups []manifestGroup) (cats []string) {
	cats = make([]string, 0, len(groups))
	for _, g := range groups {
		cats = append(cats, g.Category)
	}
	return
}

func findGroup(t *testing.T, groups []manifestGroup, cat string) (g manifestGroup) {
	t.Helper()
	for _, x := range groups {
		if x.Category == cat {
			g = x
			return
		}
	}
	t.Fatalf("expected a group with Category=%q; got %v", cat, categoriesOf(groups))
	return
}
