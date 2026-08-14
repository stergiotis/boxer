package lwsqlsurface_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
)

// createRe captures the function name out of one CREATE OR REPLACE FUNCTION
// statement. Test-only: the declared set is declared, never parsed back.
var createRe = regexp.MustCompile(`CREATE OR REPLACE FUNCTION\s+(\w+)\s+AS\s+\(([^)]*)\)\s*->\s*(.*)`)

// TestDeclaredSetIsTheUnion is the pin the whole reconcile hangs on: the
// declared set is exactly the three rosters plus the marker, in that order.
//
// Without it a family can grow a function that the surface neither installs
// nor recognises — which reads, on a correctly provisioned server, as an
// undeclared extra to be dropped. That is the failure mode ADR-0171 §SD2
// exists to remove, so it gets the strictest test in the package.
func TestDeclaredSetIsTheUnion(t *testing.T) {
	var want []string
	var wantFamily []lwsqlsurface.FamilyE
	for _, f := range chpack.Functions() {
		want = append(want, f.Name)
		wantFamily = append(wantFamily, lwsqlsurface.FamilyPack)
	}
	for _, f := range readback.HelperFunctions() {
		want = append(want, f.Name)
		wantFamily = append(wantFamily, lwsqlsurface.FamilyReadback)
	}
	for _, f := range identsql.Functions() {
		want = append(want, f.Name)
		wantFamily = append(wantFamily, lwsqlsurface.FamilyIdentity)
	}
	want = append(want, lwsqlsurface.VersionFunctionName)
	wantFamily = append(wantFamily, lwsqlsurface.FamilySurface)

	got := lwsqlsurface.DeclaredFunctions()
	require.Len(t, got, len(want))
	for i, f := range got {
		require.Equalf(t, want[i], f.Name, "entry %d", i)
		require.Equalf(t, wantFamily[i], f.Family, "%s family", f.Name)
		require.NotEmptyf(t, f.Doc, "%s has no doc line", f.Name)
	}
}

// TestDeclaredNamesUniqueAndSpliceable covers the two properties the
// installer's SQL construction assumes: no duplicate (which would make a
// later family silently shadow an earlier one) and no character that could
// escape the collision check's quoted IN list.
func TestDeclaredNamesUniqueAndSpliceable(t *testing.T) {
	nameRe := regexp.MustCompile(`^LW_[A-Z][A-Z0-9_]*$`)
	seen := make(map[string]struct{})
	for _, f := range lwsqlsurface.DeclaredFunctions() {
		require.Regexpf(t, nameRe, f.Name, "%s is not a spliceable LW_ name", f.Name)
		require.Truef(t, lwsqlsurface.InNamespace(f.Name), "%s is outside the namespace", f.Name)
		_, dup := seen[f.Name]
		require.Falsef(t, dup, "duplicate declared name %s", f.Name)
		seen[f.Name] = struct{}{}
	}
	require.Equal(t, len(seen), len(lwsqlsurface.DeclaredNames()))
}

// TestStatementsMatchDeclaredFunctions pins rendering against declaration:
// one statement per declared function, same order, same name. The three
// families render their SQL three different ways, so this is where a family
// that emits something other than what it declares is caught.
func TestStatementsMatchDeclaredFunctions(t *testing.T) {
	stmts := lwsqlsurface.Statements()
	declared := lwsqlsurface.DeclaredFunctions()
	require.Len(t, stmts, len(declared))

	for i, want := range declared {
		m := createRe.FindStringSubmatch(stmts[i])
		require.NotNilf(t, m, "statement %d is not a CREATE OR REPLACE FUNCTION: %s", i, stmts[i])
		require.Equalf(t, want.Name, m[1], "statement %d", i)
	}
}

// TestMarkerIsLastAndCarriesVersion pins the marker's two load-bearing
// properties: it is the last statement — so its success means the ones
// before it succeeded — and its body is the Version constant, which is what
// a client reads out of create_query without calling the function.
func TestMarkerIsLastAndCarriesVersion(t *testing.T) {
	stmts := lwsqlsurface.Statements()
	last := stmts[len(stmts)-1]

	m := createRe.FindStringSubmatch(last)
	require.NotNil(t, m)
	require.Equal(t, lwsqlsurface.VersionFunctionName, m[1])
	require.Empty(t, strings.TrimSpace(m[2]), "the marker takes no parameters")
	require.Equal(t, strconv.Itoa(lwsqlsurface.Version), strings.TrimSpace(m[3]))

	// No other statement may declare a marker: the invariant is that ONE
	// name answers "what revision is this server at" (ADR-0171 §SD2).
	for _, stmt := range stmts[:len(stmts)-1] {
		require.NotContains(t, stmt, "VERSION AS (")
	}
}

// TestRetiredNamesDisjointFromDeclared guards the one way RetiredNames can
// do damage: naming something the build still installs, which dropRetired
// would then delete straight after Install put it there.
func TestRetiredNamesDisjointFromDeclared(t *testing.T) {
	declared := lwsqlsurface.DeclaredNames()
	for _, name := range lwsqlsurface.RetiredNames() {
		_, live := declared[name]
		require.Falsef(t, live, "%s is both retired and declared", name)
	}
}

// TestPackMarkerIsRetiredNotDeclared pins the migration ADR-0171 §SD2
// decided: the pack's own marker is gone from the declared set and on the
// retired list, so an install drops it. Two markers that can disagree are
// the ambiguity the surface marker removes.
//
// The constant naming it survives on purpose — a client diagnosing a server
// nobody has reconciled yet still reads it — and that is not a
// contradiction, because that read happens before the install that drops it.
func TestPackMarkerIsRetiredNotDeclared(t *testing.T) {
	_, declared := lwsqlsurface.DeclaredNames()[lwsqlsurface.PreSurfaceVersionFunctionName]
	require.False(t, declared, "the pack marker must not be declared any more")
	require.Contains(t, lwsqlsurface.RetiredNames(), lwsqlsurface.PreSurfaceVersionFunctionName)
}

// TestFamilyStringIsTotal keeps the reporting label honest for a family
// value that has no name yet — a report saying "" would read as a function
// belonging to nothing.
func TestFamilyStringIsTotal(t *testing.T) {
	for _, fam := range []lwsqlsurface.FamilyE{
		lwsqlsurface.FamilyPack, lwsqlsurface.FamilyReadback,
		lwsqlsurface.FamilyIdentity, lwsqlsurface.FamilySurface,
	} {
		require.NotEmpty(t, fam.String())
		require.NotEqual(t, "unknown", fam.String())
	}
	require.Equal(t, "unknown", lwsqlsurface.FamilyE(0).String())
}

// declaredSetGolden is the surface as this Version declares it, written out
// rather than derived.
//
// Deriving it from the three rosters — which is what TestDeclaredSetIsTheUnion
// does, for a different purpose — cannot catch a roster change, because both
// sides move together. This list is the one place where adding a function
// costs an edit, and that edit is next to Version.
var declaredSetGolden = []string{
	"LW_CO_LOOKUP", "LW_CO_LOOKUP_NULL", "LW_CO_GATHER", "LW_CO_ARG_SORT",
	"LW_CO_ARG_MAX", "LW_CO_EXISTS_EQ2",
	"LW_RAGGED_STARTS", "LW_RAGGED_RANGES", "LW_RAGGED_PARENT_IDS",
	"LW_RAGGED_IOTA", "LW_RAGGED_NEST", "LW_RAGGED_REDUCE",
	"LW_RAGGED_EXISTS", "LW_RAGGED_COUNT", "LW_RAGGED_ELEM",
	"LW_ASPECT_SEG_ENC", "LW_ASPECT_SEG_USE", "LW_ASPECT_SEG_SEM",
	"LW_ASPECT_DECODE", "LW_ASPECT_NAMES_ENC", "LW_ASPECT_NAMES_USE",
	"LW_ASPECT_NAMES_SEM", "LW_ASPECT_HAS_ENC", "LW_ASPECT_HAS_USE",
	"LW_ASPECT_HAS_SEM",
	"LW_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL", "LW_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL",
	"LW_LU_VAL_BY_MEMB_IDX", "LW_LU_ATTR_BY_TAG", "LW_LU_MEMBS_OF_VAL_IDX",
	"LW_VALUE_BY_TAG_EQUAL", "LW_LIST_BY_TAG_EQUAL",
	"LW_ID_IS_VALID", "LW_ID_TAG_WIDTH", "LW_ID_TAG_BITS", "LW_ID_BODY",
	"LW_ID_TAG_VALUE", "LW_ID_HAS_TAG",
	"LW_SURFACE_VERSION",
}

// TestDeclaredSetPinned is the guard Version's doc promises: a roster change
// cannot reach a server without someone editing this file, where the version
// constant lives.
//
// One marker covers all three families, so a function added anywhere without
// a bump leaves every server reporting "matches this build" while carrying
// less than the build declares — the ambiguity LW_PACK_VERSION was replaced
// to remove.
func TestDeclaredSetPinned(t *testing.T) {
	got := make([]string, 0, len(lwsqlsurface.DeclaredFunctions()))
	for _, f := range lwsqlsurface.DeclaredFunctions() {
		got = append(got, f.Name)
	}
	require.Equal(t, declaredSetGolden, got,
		"the declared set changed: update this list AND bump lwsqlsurface.Version, "+
			"or every server will keep reporting the old revision as current")
}
