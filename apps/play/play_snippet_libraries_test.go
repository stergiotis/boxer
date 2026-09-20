package play

import (
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
)

const testLibraryDoc = `---
type: reference
audience: end-user
status: draft
title: Test library
---

# Test library

## Birds aloft

` + "```sql\nSELECT 1 AS birds\n```" + `

## Rain

` + "```sql\nSELECT 2 AS rain\n```\n"

func testLibrary(tabID string, dockID uint64) SnippetLibrary {
	return SnippetLibrary{
		TabID: tabID, DockID: dockID, Title: "Test library",
		AppId: "example.test/lib", Doc: "snippets",
		Help: fstest.MapFS{"snippets.md": &fstest.MapFile{Data: []byte(testLibraryDoc)}},
	}
}

// resetSnippetLibraries isolates a test from the package-level registry, which
// in a real binary is filled at init and never emptied.
func resetSnippetLibraries(t *testing.T) {
	t.Helper()
	snippetLibrariesMu.Lock()
	saved := snippetLibraries
	snippetLibraries = nil
	snippetLibrariesMu.Unlock()
	t.Cleanup(func() {
		snippetLibrariesMu.Lock()
		snippetLibraries = saved
		snippetLibrariesMu.Unlock()
	})
}

func TestSnippetLibraryRegistrationIsValidated(t *testing.T) {
	resetSnippetLibraries(t)
	require.NoError(t, RegisterSnippetLibraryE(testLibrary("lib-a", 96)))
	require.Error(t, RegisterSnippetLibraryE(testLibrary("lib-a", 97)), "a tab id registers once")
	require.Error(t, RegisterSnippetLibraryE(testLibrary("lib-b", 96)), "a dock id registers once")
	require.Error(t, RegisterSnippetLibraryE(testLibrary("lib-c", 13)), "dock ids below 64 are the built-ins'")
	bad := testLibrary("lib-d", 98)
	bad.Help = nil
	require.Error(t, RegisterSnippetLibraryE(bad))
	require.Len(t, registeredSnippetLibraries(), 1)
}

// A window opened after a registration carries the library as a tools-zone
// tab beside the built-in Snippets tab; one opened before does not change.
func TestRegisteredLibraryBecomesATabOfNewWindows(t *testing.T) {
	resetSnippetLibraries(t)
	before := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x", nil)
	require.NoError(t, RegisterSnippetLibraryE(testLibrary("lib-a", 96)))
	after := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x", nil)

	find := func(inst *PlayApp, id string) (spec TabSpec, ok bool) {
		for _, s := range inst.Tabs().Specs() {
			if s.ID == id {
				return s, true
			}
		}
		return
	}
	_, had := find(before, "lib-a")
	require.False(t, had)
	spec, has := find(after, "lib-a")
	require.True(t, has)
	require.Equal(t, uint64(96), spec.DockID)
	require.Equal(t, TabZoneTools, spec.Zone)
	require.Equal(t, "Test library", spec.Title)
	// The mark an embedder that strips the editor removes the pane by: a
	// contributed slug is the contributor's, so no list of built-ins names it
	// (ADR-0132 §SD3, sqlapplet's attenuation).
	require.True(t, spec.Contributed)
	builtin, ok := find(after, "snippets")
	require.True(t, ok, "the built-in tab is still there")
	require.Equal(t, builtin.Zone, spec.Zone)
	require.False(t, builtin.Contributed, "the built-in is play's own, removed by its slug")
}

// A contributed library is searched the way the built-in one is: a source
// parses its doc, lists its sections and indexes its book.
func TestSnippetSourceParsesAndIndexesALibrary(t *testing.T) {
	lib := testLibrary("lib-a", 96)
	src := newSnippetSource(lib.AppId, lib.Help, lib.Doc)
	require.NotNil(t, src.doc)
	require.NotEmpty(t, src.sections)
	hits := src.index.Search(search.ParseQueryWith("birds", search.Thesaurus{}), 0)
	require.NotEmpty(t, hits)
	require.Equal(t, "snippets", hits[0].Ref.Doc)

	missing := newSnippetSource(lib.AppId, lib.Help, "no-such-doc")
	require.Nil(t, missing.doc, "an absent doc degrades to the pane's notice, not to an error")
}

func TestBuiltinSnippetSourceStillLoads(t *testing.T) {
	src := builtinSnippetSource()
	require.NotNil(t, src.doc)
	require.NotNil(t, src.index)
	require.Equal(t, playAppId, src.appId)
}
