package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/docsearchsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// testVocabRegistry is a fresh registry through the host's own wiring, so a
// test sees exactly what a running play sees without touching the process
// registry another test may have populated.
func testVocabRegistry(t *testing.T) *sqlvocab.Registry {
	t.Helper()
	r := sqlvocab.NewRegistry()
	require.NoError(t, RegisterVocabulary(r))
	return r
}

func testVocabDeclared(t *testing.T) []vocabEntry {
	t.Helper()
	return vocabDeclared(testVocabRegistry(t))
}

func vocabByWhere(entries []vocabEntry, where sqlvocab.WhereE) (out []vocabEntry) {
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
	all := testVocabDeclared(t)
	for _, where := range []sqlvocab.WhereE{vocabServer, vocabClient, vocabPlay} {
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
	all := testVocabDeclared(t)
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
	for _, e := range vocabByWhere(testVocabDeclared(t), vocabServer) {
		assert.Truef(t, strings.HasPrefix(e.Name, "LW_"), "%s is outside the LW_ namespace", e.Name)
	}
}

// TestVocabEntriesAreDescribed pins that nothing lists without a signature
// and a doc line. An undescribed row is a name a reader has to go elsewhere
// to understand, which is the discoverability gap the panel exists to close.
func TestVocabEntriesAreDescribed(t *testing.T) {
	for _, e := range testVocabDeclared(t) {
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
	all := testVocabDeclared(t)
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
	all := testVocabDeclared(t)
	vocabMarkInstalled(all, nil)
	for _, e := range all {
		assert.Falsef(t, e.Installed, "%s marked from a nil probe", e.Name)
	}
}

// TestVocabExtras covers the other half of the drift question: what the
// endpoint has that no roster claims.
func TestVocabExtras(t *testing.T) {
	declared := testVocabDeclared(t)
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
//
// The marks lost the trailing "·" they carried while they led the row: it
// separated the mark from the call beside it, and the mark now has a column of
// its own.
func TestVocabRowMark(t *testing.T) {
	srv := vocabEntry{Name: "LW_CO_GATHER", Where: vocabServer, Declared: true, Available: true}

	mark, _ := vocabRowMark(srv, vocabServer, false)
	assert.Equal(t, vocabMarkUnknown, mark, "before the probe answers")

	present := srv
	present.Installed = true
	mark, weak := vocabRowMark(present, vocabServer, true)
	assert.Equal(t, vocabMarkPresent, mark)
	assert.True(t, weak, "the state every row is in on a provisioned endpoint recedes")

	mark, weak = vocabRowMark(srv, vocabServer, true)
	assert.Equal(t, vocabMarkMissing, mark, "answered and absent")
	assert.False(t, weak, "the one state a user must act on is the one that is not recessed")

	extra := vocabEntry{Name: "myOwnHelper", Where: vocabServer, Declared: false, Available: true}
	mark, _ = vocabRowMark(extra, vocabServer, true)
	assert.Equal(t, vocabMarkExtra, mark)

	cli := vocabEntry{Name: "docsearch", Where: vocabClient, Declared: true, Available: true}
	mark, _ = vocabRowMark(cli, vocabClient, false)
	assert.Empty(t, mark, "a client macro never carries an endpoint mark")

	reserved := vocabEntry{Name: "tsMotifs", Where: vocabPlay, Declared: true, Available: false}
	mark, _ = vocabRowMark(reserved, vocabPlay, true)
	assert.Equal(t, vocabMarkReserved, mark)
}

// TestVocabMarkGlyphAndNote pins the split the endpoint column was narrowed
// for: a MARK is a glyph and a VERDICT is a sentence, and no state may fall
// down the gap between them.
//
// The property under test is coverage, not spelling. Every verdict
// [vocabRowMark] can return has to leave the reader something — a glyph, a
// note, or deliberately both — because the two renderers now draw from
// different halves of it and a state that neither claims renders as an empty
// row saying nothing at all.
func TestVocabMarkGlyphAndNote(t *testing.T) {
	srv := vocabEntry{Name: "LW_CO_GATHER", Where: vocabServer, Declared: true, Available: true}

	// Present is the state every row is in on a provisioned endpoint: a glyph
	// and NO note, or the doc column carries a word on all 39 of them.
	present := srv
	present.Installed = true
	glyph, hover := vocabMarkGlyph(vocabMarkPresent)
	assert.Equal(t, icons.PhCheck, glyph)
	assert.NotEmpty(t, hover, "the column has no header, so the glyph carries its own")
	note, _, _ := vocabDocNote(present, vocabMarkPresent, true)
	assert.Empty(t, note, "the common case says nothing twice")

	// Missing is the one state a user must act on, and it is the one state
	// that says it in both columns.
	glyph, _ = vocabMarkGlyph(vocabMarkMissing)
	assert.Equal(t, icons.PhX, glyph, "not ✗ — U+2717 is baselined tofu, see vocabMarkGlyph")
	note, full, _ := vocabDocNote(srv, vocabMarkMissing, true)
	assert.Equal(t, vocabMarkMissing, note)
	assert.Contains(t, full, "provisioning")

	// An extra IS on the endpoint — that is how the probe found it — so the
	// glyph is a check and only the note says what is unusual about it.
	extra := vocabEntry{Name: "myOwnHelper", Where: vocabServer, Declared: false, Available: true}
	glyph, _ = vocabMarkGlyph(vocabMarkExtra)
	assert.Equal(t, icons.PhCheck, glyph, "an extra is present, whatever else it is")
	note, _, _ = vocabDocNote(extra, vocabMarkExtra, true)
	assert.Equal(t, vocabMarkExtra, note)

	// Reserved is a fact about the BUILD, so the endpoint column stays empty
	// and the note is the ONLY carrier. This is the state the split could
	// have dropped.
	reserved := vocabEntry{Name: "tsMotifs", Where: vocabPlay, Declared: true, Available: false}
	glyph, _ = vocabMarkGlyph(vocabMarkReserved)
	assert.Empty(t, glyph, "a client-side name is not an endpoint fact")
	note, _, _ = vocabDocNote(reserved, vocabMarkReserved, true)
	assert.Equal(t, vocabMarkReserved, note, "and so the note has to say it")

	// Unknown: a glyph, and no note — the status line above the tree already
	// says the probe is out.
	glyph, _ = vocabMarkGlyph(vocabMarkUnknown)
	assert.Equal(t, icons.PhQuestion, glyph)
	note, _, _ = vocabDocNote(srv, vocabMarkUnknown, false)
	assert.Empty(t, note)

	// A client row with no verdict at all: neither half draws, which is the
	// empty cell the layout expects rather than a hole either renderer left.
	cli := vocabEntry{Name: "docsearch", Where: vocabClient, Declared: true, Available: true}
	glyph, _ = vocabMarkGlyph("")
	assert.Empty(t, glyph)
	note, _, _ = vocabDocNote(cli, "", true)
	assert.Empty(t, note)
}

// TestVocabDocNoteDependencies pins §SD6's note in the column it moved to,
// and the two things the move bought.
//
// It names EVERY missing dependency now: naming only the first was the 96-point
// column's constraint, not the fact's. And it no longer displaces the row's own
// endpoint verdict, because that verdict is drawn by a different column — the
// precedence stays, the loss it used to cause does not.
func TestVocabDocNoteDependencies(t *testing.T) {
	e := vocabEntry{
		Name: "LW_SEL", Where: vocabClient, Declared: true, Available: true,
		MissingDeps: []string{"LW_CO_GATHER", "LW_RAGGED_STARTS"},
	}

	note, full, _ := vocabDocNote(e, vocabMarkReserved, true)
	assert.Contains(t, note, "LW_CO_GATHER")
	assert.Contains(t, note, "LW_RAGGED_STARTS", "every dependency, not the first")
	assert.Contains(t, full, "MISSING on this endpoint")

	// The row's own verdict still reaches the reader, through the glyph the
	// dependency note used to overwrite.
	glyph, _ := vocabMarkGlyph(vocabMarkMissing)
	assert.Equal(t, icons.PhX, glyph)

	// An unanswered probe reports nothing: empty MissingDeps means "not
	// known", never "all present" (vocabMarkInstalled's contract).
	note, _, _ = vocabDocNote(e, vocabMarkUnknown, false)
	assert.Empty(t, note)
}

// TestVocabExtractionDependencies pins ADR-0174 §SD6's marking on the entry
// it was designed for (ADR-0181 §SD7): the extraction family is expanded
// client-side, so a per-name "installed?" verdict says nothing useful about
// it — what decides whether a call works is whether the endpoint carries
// what the expansion emits.
func TestVocabExtractionDependencies(t *testing.T) {
	all := testVocabDeclared(t)
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
	full := testVocabDeclared(t)
	vocabMarkInstalled(full, installed)
	for _, e := range full {
		if e.Name == constructsql.NameGet {
			assert.Empty(t, e.MissingDeps, "nothing is missing on a fully provisioned endpoint")
		}
	}

	// Strip one dependency and it is named — on the client row, which no
	// "installed?" column would ever have flagged.
	delete(installed, "LW_VALUE_BY_TAG_EQUAL")
	partial := testVocabDeclared(t)
	vocabMarkInstalled(partial, installed)
	for _, e := range partial {
		if e.Name == constructsql.NameGet {
			assert.Equal(t, []string{"LW_VALUE_BY_TAG_EQUAL"}, e.MissingDeps)
		}
	}

	// An unanswered probe claims nothing, the same rule the rest of the
	// panel follows.
	unanswered := testVocabDeclared(t)
	vocabMarkInstalled(unanswered, nil)
	for _, e := range unanswered {
		assert.Empty(t, e.MissingDeps, "%s: an unanswered probe must not claim a missing dependency", e.Name)
	}
}

// TestVocabularyRegistryCoversRosters pins that RegisterVocabulary reaches
// every roster this build declares (ADR-0190 §SD4).
//
// The union used to be written out in vocabDeclared; now it is the wiring
// site's job, and the way it can go wrong is a roster silently dropped from
// the wiring rather than from an import. So the check is by name, per
// population, one name from each family.
func TestVocabularyRegistryCoversRosters(t *testing.T) {
	r := testVocabRegistry(t)

	byWhere := make(map[sqlvocab.WhereE]map[string]struct{}, 3)
	for _, w := range sqlvocab.AllWheres {
		byWhere[w] = map[string]struct{}{}
	}
	for _, f := range r.All() {
		for _, w := range sqlvocab.AllWheres {
			if f.Where&w != 0 {
				byWhere[w][f.Name] = struct{}{}
			}
		}
	}

	for _, f := range lwsqlsurface.DeclaredFunctions() {
		assert.Containsf(t, byWhere[sqlvocab.WhereServer], f.Name, "%s missing from the server population", f.Name)
	}
	for _, f := range identsql.Functions() {
		assert.Containsf(t, byWhere[sqlvocab.WhereClient], f.Name, "%s missing from the client population", f.Name)
	}
	for _, fns := range [][]constructsql.Function{
		constructsql.Functions(), constructsql.ExtractFunctions(), constructsql.ComponentFunctions(),
	} {
		for _, f := range fns {
			assert.Containsf(t, byWhere[sqlvocab.WhereClient], f.Name, "%s missing from the client population", f.Name)
		}
	}
	for _, f := range glosssql.Functions() {
		assert.Containsf(t, byWhere[sqlvocab.WhereClient], f.Name, "%s missing from the client population", f.Name)
	}
	for _, name := range []string{distsql.FuncName, docsearchsql.FuncName, keelsonsql.FuncName} {
		assert.Containsf(t, byWhere[sqlvocab.WhereClient], name, "%s missing from the client population", name)
	}
	for _, f := range tsFuncs {
		assert.Containsf(t, byWhere[sqlvocab.WhereHost], f.Name, "%s missing from the host population", f.Name)
	}
}

// TestVocabularyDomainsAreDeclared is ADR-0190 §SD4's floor: nothing in the
// registry says nothing about its arguments.
//
// Register refuses the zero Domain, so this restates what the wiring already
// proved — deliberately, because the failure it guards is a roster author
// reaching for a plain []Param literal and leaving the domain out. The named
// assertions below are the positions the design leans on; if one of them
// regresses to DomainExpr the pane goes silent where it should be exact.
func TestVocabularyDomainsAreDeclared(t *testing.T) {
	r := testVocabRegistry(t)
	for _, f := range r.All() {
		for i, p := range f.Params {
			assert.NotEqualf(t, sqlvocab.DomainUnspecified, p.Domain.Kind,
				"%s argument %d (%s) declares no domain", f.Name, i, p.Name)
		}
	}

	domainAt := func(name string, ordinal int) sqlvocab.Domain {
		t.Helper()
		f, ok := r.Signature(name)
		require.Truef(t, ok, "%s is not registered", name)
		require.Greaterf(t, len(f.Params), ordinal, "%s has no argument %d", name, ordinal)
		return f.Params[ordinal].Domain
	}

	assert.Equal(t, sqlvocab.DomainComponentKind, domainAt(constructsql.NameComponent, 0).Kind)
	assert.Equal(t, sqlvocab.DomainComponentKind, domainAt(constructsql.NameComponentFilter, 0).Kind)
	assert.Equal(t, sqlvocab.DomainIntrospectionTable, domainAt(keelsonsql.FuncName, 0).Kind)
	assert.Equal(t, sqlvocab.DomainSection, domainAt(constructsql.NameGet, 0).Kind)
	assert.Equal(t, sqlvocab.DomainMembership, domainAt(constructsql.NameGet, 1).Kind)
	assert.Equal(t, sqlvocab.DomainExtractionToken, domainAt(constructsql.NameGet, 2).Kind)
	assert.Equal(t, sqlvocab.DomainSection, domainAt(constructsql.NameTagged, 1).Kind)
	assert.Equal(t, sqlvocab.DomainCanonicalType, domainAt(constructsql.NameTagged, 3).Kind)
	assert.Equal(t, sqlvocab.DomainChannel, domainAt(constructsql.NameMembership, 2).Kind)
	assert.Equal(t, sqlvocab.DomainSupportRole, domainAt(constructsql.NameSupport, 2).Kind)
	assert.Equal(t, sqlvocab.DomainIdentityTag, domainAt(identsql.NameHasTag, 1).Kind)

	gloss := domainAt(glosssql.FuncName, 1)
	assert.Equal(t, sqlvocab.DomainGloss, gloss.Kind)
	assert.Equal(t, sqlvocab.DomainGlossKey, domainAt(glosssql.FuncName, 2).Kind)
	assert.Equal(t, 1, domainAt(glosssql.FuncName, 2).Ref, "the key's vocabulary depends on the gloss named beside it")
}
