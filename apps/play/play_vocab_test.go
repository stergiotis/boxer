package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

func vocabByWhere(entries []vocabEntry, where vocabWhereE) (out []vocabEntry) {
	for _, e := range entries {
		if e.Where == where {
			out = append(out, e)
		}
	}
	return
}

func vocabNames(entries []vocabEntry) (out []string) {
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return
}

// TestVocabDeclaredCoversEveryPopulation pins that all three populations
// reach the panel. A roster silently dropping out would leave a section
// permanently empty, which reads as "this build has none" rather than as the
// wiring mistake it is.
func TestVocabDeclaredCoversEveryPopulation(t *testing.T) {
	all := vocabDeclared()
	for _, where := range []vocabWhereE{vocabServer, vocabClient, vocabPlay} {
		require.NotEmptyf(t, vocabByWhere(all, where), "%s section is empty", where)
	}

	server := vocabNames(vocabByWhere(all, vocabServer))
	// One from each server roster, so a dropped import is caught by name.
	assert.Contains(t, server, "LW_CO_GATHER", "chpack roster")
	assert.Contains(t, server, "LW_VALUE_BY_TAG_EQUAL", "read-back roster")
	assert.Contains(t, server, identsql.NameBody, "identity roster")

	client := vocabNames(vocabByWhere(all, vocabClient))
	assert.Contains(t, client, "descriptiveStatistics")
	assert.Contains(t, client, "docsearch")
	assert.Contains(t, client, "keelson")
	for _, f := range constructsql.Functions() {
		assert.Containsf(t, client, f.Name, "%s missing from the client section (ADR-0181 roster)", f.Name)
	}

	assert.Contains(t, vocabNames(vocabByWhere(all, vocabPlay)), "tsProfile")
}

// TestVocabIdentityIsBothPopulations pins ADR-0174 §SD1's one deliberate
// duplication: LW_ID_* is installable AND client-expanded, so it appears in
// both sections. Listing it once would make one of the two answers wrong —
// either "your endpoint lacks it" (when the client expands it anyway) or "it
// always works" (when a listing of installed UDFs should show it missing).
func TestVocabIdentityIsBothPopulations(t *testing.T) {
	all := vocabDeclared()
	server := vocabNames(vocabByWhere(all, vocabServer))
	client := vocabNames(vocabByWhere(all, vocabClient))
	for _, f := range identsql.Functions() {
		assert.Containsf(t, server, f.Name, "%s missing from the server section", f.Name)
		assert.Containsf(t, client, f.Name, "%s missing from the client section", f.Name)
	}
}

// TestVocabServerEntriesAreNamespaced pins the claim ADR-0162 §SD2 makes and
// the panel relies on: every server function this build declares is under
// LW_. A roster member outside it would still list, but the one-LIKE drift
// question the namespace exists for would silently not cover it.
func TestVocabServerEntriesAreNamespaced(t *testing.T) {
	for _, e := range vocabByWhere(vocabDeclared(), vocabServer) {
		assert.Truef(t, strings.HasPrefix(e.Name, "LW_"), "%s is outside the LW_ namespace", e.Name)
	}
}

// TestVocabEntriesAreDescribed pins that nothing lists without a signature
// and a doc line. An undescribed row is a name a reader has to go elsewhere
// to understand, which is the discoverability gap the panel exists to close.
func TestVocabEntriesAreDescribed(t *testing.T) {
	for _, e := range vocabDeclared() {
		assert.NotEmptyf(t, e.Doc, "%s has no doc line", e.Name)
		assert.NotEmptyf(t, e.Family, "%s has no family", e.Name)
		assert.Truef(t, strings.HasPrefix(e.call(), e.Name+"("), "%s renders a bad call template", e.Name)
	}
}

// TestVocabMarkInstalledOnlyTouchesServer pins that the probe's answer never
// leaks onto a population it does not describe: a client macro is not
// "missing" because the endpoint lacks a UDF of that name, which is exactly
// the LW_ID_* case.
func TestVocabMarkInstalledOnlyTouchesServer(t *testing.T) {
	all := vocabDeclared()
	vocabMarkInstalled(all, map[string]string{identsql.NameBody: "CREATE FUNCTION"})
	for _, e := range all {
		if e.Where == vocabServer && e.Name == identsql.NameBody {
			assert.True(t, e.Installed, "the server LW_ID_BODY should be marked installed")
			continue
		}
		assert.Falsef(t, e.Installed, "%s (%s) must not be marked installed", e.Name, e.Where)
	}
}

// TestVocabMarkInstalledUnansweredIsNoClaim pins the rule the whole panel
// hangs on: an unanswered probe leaves everything unmarked, so the render's
// ready flag is what decides between "?" and "MISSING". Marking here would
// make an in-flight query indistinguishable from an empty server.
func TestVocabMarkInstalledUnansweredIsNoClaim(t *testing.T) {
	all := vocabDeclared()
	vocabMarkInstalled(all, nil)
	for _, e := range all {
		assert.Falsef(t, e.Installed, "%s marked from a nil probe", e.Name)
	}
}

// TestVocabExtras covers the other half of the drift question: what the
// endpoint has that no roster claims.
func TestVocabExtras(t *testing.T) {
	declared := vocabDeclared()
	installed := map[string]string{
		"LW_CO_GATHER":  "CREATE FUNCTION LW_CO_GATHER AS (lane, sel) -> x", // declared
		"CO_GATHER":     "CREATE FUNCTION CO_GATHER AS (lane, sel) -> x",    // a stale pre-rename spelling
		"myOwnHelper":   "CREATE FUNCTION myOwnHelper AS (x) -> x",
		"anotherHelper": "CREATE FUNCTION anotherHelper AS (x) -> x",
	}
	got := vocabNames(vocabExtras(installed, declared))
	assert.Equal(t, []string{"anotherHelper", "CO_GATHER", "myOwnHelper"}, got,
		"declared names excluded, remainder sorted case-insensitively")

	assert.Empty(t, vocabExtras(nil, declared), "an unanswered probe claims nothing")
	assert.Empty(t, vocabExtras(map[string]string{}, declared), "an empty answer claims nothing")
}

// TestParseMarkerVersion covers reading a revision out of the marker's own
// definition — the reason the probe never CALLS LW_SURFACE_VERSION(), which
// would fail with unknown-function on exactly the servers whose version
// matters most.
func TestParseMarkerVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"current shape", "CREATE FUNCTION LW_SURFACE_VERSION AS () -> 1", 1},
		{"retired marker", "CREATE FUNCTION LW_PACK_VERSION AS () -> 3", 3},
		{"trailing space", "CREATE FUNCTION LW_SURFACE_VERSION AS () ->  7  ", 7},
		{"absent", "", -1},
		{"no arrow", "CREATE FUNCTION LW_SURFACE_VERSION AS ()", -1},
		{"hand-edited body", "CREATE FUNCTION LW_SURFACE_VERSION AS () -> 'three'", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseMarkerVersion(tc.in))
		})
	}
}

// TestVocabSurfaceSkew pins that an unknown revision says nothing rather
// than guessing, that a match and a mismatch are different sentences, and
// that a pre-surface endpoint gets its own sentence instead of being
// reported as unknown — it is a server that works, provisioned by a build
// from before the three families shared one marker (ADR-0171 §SD2).
func TestVocabSurfaceSkew(t *testing.T) {
	_, ok := vocabSurfaceSkew(-1, -1)
	assert.False(t, ok, "unknown revision must not produce a line")

	line, ok := vocabSurfaceSkew(lwsqlsurface.Version, -1)
	require.True(t, ok)
	assert.Contains(t, line, "matches this build")

	line, ok = vocabSurfaceSkew(lwsqlsurface.Version-1, -1)
	require.True(t, ok)
	assert.Contains(t, line, "older definitions")

	line, ok = vocabSurfaceSkew(-1, 4)
	require.True(t, ok)
	assert.Contains(t, line, "pre-surface endpoint")
	assert.Contains(t, line, "pack v4")

	// A server carrying both markers is mid-migration or hand-patched; the
	// surface marker is the one that answers the question, so it wins.
	line, ok = vocabSurfaceSkew(lwsqlsurface.Version, 4)
	require.True(t, ok)
	assert.Contains(t, line, "matches this build")
	assert.NotContains(t, line, "pre-surface")
}

// TestVocabRowMark pins the four server states apart, and that the two
// non-server populations are never marked missing — they do not depend on the
// endpoint, so any endpoint-derived mark on them would be a lie.
func TestVocabRowMark(t *testing.T) {
	srv := vocabEntry{Name: "LW_CO_GATHER", Where: vocabServer, Declared: true, Available: true}

	mark, _ := vocabRowMark(srv, vocabServer, false)
	assert.Equal(t, "? ·", mark, "before the probe answers")

	present := srv
	present.Installed = true
	mark, _ = vocabRowMark(present, vocabServer, true)
	assert.Equal(t, "✓ ·", mark)

	mark, weak := vocabRowMark(srv, vocabServer, true)
	assert.Equal(t, "MISSING ·", mark, "answered and absent")
	assert.False(t, weak, "the one state a user must act on is the one that is not recessed")

	extra := vocabEntry{Name: "myOwnHelper", Where: vocabServer, Declared: false, Available: true}
	mark, _ = vocabRowMark(extra, vocabServer, true)
	assert.Equal(t, "extra ·", mark)

	cli := vocabEntry{Name: "docsearch", Where: vocabClient, Declared: true, Available: true}
	mark, _ = vocabRowMark(cli, vocabClient, false)
	assert.Empty(t, mark, "a client macro never carries an endpoint mark")

	reserved := vocabEntry{Name: "tsMotifs", Where: vocabPlay, Declared: true, Available: false}
	mark, _ = vocabRowMark(reserved, vocabPlay, true)
	assert.Equal(t, "reserved ·", mark)
}

// TestVocabExtractionDependencies pins ADR-0174 §SD6's marking on the entry
// it was designed for (ADR-0181 §SD7): the extraction family is expanded
// client-side, so a per-name "installed?" verdict says nothing useful about
// it — what decides whether a call works is whether the endpoint carries
// what the expansion emits.
func TestVocabExtractionDependencies(t *testing.T) {
	all := vocabDeclared()
	client := vocabByWhere(all, vocabClient)
	for _, f := range constructsql.ExtractFunctions() {
		assert.Containsf(t, vocabNames(client), f.Name, "%s missing from the client section", f.Name)
	}

	var entry vocabEntry
	for _, e := range all {
		if e.Name == constructsql.NameGet {
			entry = e
		}
	}
	require.NotEmpty(t, entry.Dependencies, "the extraction family must declare what its expansion emits")

	// Every declared dependency must be a name some roster actually
	// declares. A dependency on a name that does not exist would mark the
	// family missing on every endpoint, forever.
	declared := lwsqlsurface.DeclaredNames()
	for _, dep := range entry.Dependencies {
		assert.Containsf(t, declared, dep, "%s is declared as a dependency but no roster declares it", dep)
	}

	// An endpoint carrying the whole surface leaves nothing marked.
	installed := make(map[string]string, len(declared))
	for name := range declared {
		installed[name] = "CREATE FUNCTION " + name
	}
	full := vocabDeclared()
	vocabMarkInstalled(full, installed)
	for _, e := range full {
		if e.Name == constructsql.NameGet {
			assert.Empty(t, e.MissingDeps, "nothing is missing on a fully provisioned endpoint")
		}
	}

	// Strip one dependency and it is named — on the client row, which no
	// "installed?" column would ever have flagged.
	delete(installed, "LW_VALUE_BY_TAG_EQUAL")
	partial := vocabDeclared()
	vocabMarkInstalled(partial, installed)
	for _, e := range partial {
		if e.Name == constructsql.NameGet {
			assert.Equal(t, []string{"LW_VALUE_BY_TAG_EQUAL"}, e.MissingDeps)
		}
	}

	// An unanswered probe claims nothing, the same rule the rest of the
	// panel follows.
	unanswered := vocabDeclared()
	vocabMarkInstalled(unanswered, nil)
	for _, e := range unanswered {
		assert.Empty(t, e.MissingDeps, "%s: an unanswered probe must not claim a missing dependency", e.Name)
	}
}
