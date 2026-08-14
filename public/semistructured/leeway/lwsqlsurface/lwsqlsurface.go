// Package lwsqlsurface is the entry point to leeway's SQL read surface
// (ADR-0171): the three server-side function families, declared as one set,
// installed by one call, and reported by one version marker.
//
// The layering, stated once (§SD1):
//
//   - chpack (ADR-0162) is the lane algebra — the expression-level vocabulary
//     over co-lanes and ragged streams, with no knowledge of leeway schemas.
//   - The read-back family (ADR-0066) is the leeway-schema-aware layer on top
//     of it: locate an attribute by its membership tag, extract its value.
//   - identsql (ADR-0106) is the identity vocabulary — the bit arithmetic of
//     Fibonacci-tagged identifiers, which a query needs to read a membership
//     id whichever family produced it.
//
// Column handles (ADR-0116) are the fourth piece of the surface but not of
// this package: they are expanded client-side and install nothing.
//
// # The handshake
//
// One marker, LW_SURFACE_VERSION, carries one invariant: the marker present
// at revision N means all three families are installed at revision N. That
// is why Install provisions all three and why no family publishes a marker
// of its own — a per-family scheme answers three questions a caller then has
// to combine, and the jsonbench trial's failure was not knowing which
// combination it was looking at (ADR-0171 §SD2).
//
// Each family keeps declaring its own roster beside its own SQL. This
// package declares only the union, which is what a reconciler needs and no
// family can see on its own.
package lwsqlsurface

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
)

// Version is the surface revision reported by LW_SURFACE_VERSION().
//
// Bump on any change to any family's roster: one marker covers all three, so
// a pack function added without a bump leaves servers reporting current
// while carrying less than the build declares.
//
// TestDeclaredSetPinned is what puts that under an author's nose. It holds
// the declared names as a literal list, so a roster change fails until
// someone edits that list — and the failure message says to bump this
// constant while they are there. A test that derived its expectation from
// the same rosters could not do that: both sides would move together and
// stay green.
//
// 1 — the surface marker replaces LW_PACK_VERSION (ADR-0171 §SD2).
const Version = 1

// VersionFunctionName is the zero-argument marker that makes client/server
// surface skew a query.
const VersionFunctionName = "LW_SURFACE_VERSION"

// PreSurfaceVersionFunctionName is the pack's retired marker. A server
// provisioned before the surface marker existed carries this one instead,
// so a client diagnosing an unreconciled endpoint falls back to reading its
// definition — see chpack.RetiredNames for why it is not simply gone.
//
// Read the revision out of the function's create_query rather than calling
// it: calling fails with unknown-function on exactly the servers whose
// revision matters.
const PreSurfaceVersionFunctionName = "LW_PACK_VERSION"

// FamilyE names which family declares a function. It is reporting detail —
// a drift report that says only "LW_LU_ATTR_BY_TAG is missing" makes the
// reader find out for themselves which install path to blame.
type FamilyE uint8

const (
	// FamilyPack is chpack's lane algebra (ADR-0162).
	FamilyPack FamilyE = iota + 1
	// FamilyReadback is the leeway-schema-aware read-back family (ADR-0066).
	FamilyReadback
	// FamilyIdentity is the identifier bit arithmetic (ADR-0106).
	FamilyIdentity
	// FamilySurface is this package's own marker function.
	FamilySurface
)

// String renders the family for reports and logs.
func (inst FamilyE) String() (s string) {
	switch inst {
	case FamilyPack:
		s = "pack"
	case FamilyReadback:
		s = "readback"
	case FamilyIdentity:
		s = "identity"
	case FamilySurface:
		s = "surface"
	default:
		s = "unknown"
	}
	return
}

// Function is one entry of the declared set: the name a query spells, the
// parameters in order, one line on what it does, and which family owns it.
//
// Bodies are deliberately absent. Three families render their SQL three
// ways — chpack from a Body field, the read-back family from an embedded
// .sql file, identsql from generated bit arithmetic — and restating them
// here would create a fourth spelling that can disagree with all three.
// Statements is where the SQL comes from; this is what it is supposed to
// contain.
type Function struct {
	Name   string
	Params []string
	Doc    string
	Family FamilyE
}

// DeclaredFunctions returns every function this build declares, in
// installation order: pack, then the read-back family that layers on it,
// then the identity family, then the marker.
//
// The marker is last for the same reason chpack put its own marker last: it
// is the statement whose success means the ones before it succeeded.
func DeclaredFunctions() (fns []Function) {
	pack := chpack.Functions()
	helpers := readback.HelperFunctions()
	ids := identsql.Functions()

	fns = make([]Function, 0, len(pack)+len(helpers)+len(ids)+1)
	for _, f := range pack {
		fns = append(fns, Function{Name: f.Name, Params: f.Params, Doc: f.Doc, Family: FamilyPack})
	}
	for _, f := range helpers {
		fns = append(fns, Function{Name: f.Name, Params: f.Params, Doc: f.Doc, Family: FamilyReadback})
	}
	for _, f := range ids {
		fns = append(fns, Function{Name: f.Name, Params: f.Params, Doc: f.Doc, Family: FamilyIdentity})
	}
	fns = append(fns, Function{
		Name:   VersionFunctionName,
		Params: []string{},
		Doc:    "surface revision marker; SELECT it to detect client/server skew across all three families",
		Family: FamilySurface,
	})
	return
}

// MarkerStatement renders the CREATE for the surface marker.
func MarkerStatement() (sql string) {
	sql = chpack.Statement(chpack.Function{
		Name:   VersionFunctionName,
		Params: []string{},
		Body:   strconv.Itoa(Version),
	})
	return
}

// Statements renders the whole surface in installation order — the same
// order DeclaredFunctions lists, which is also dependency order: the server
// resolves referenced functions at CREATE time, and the read-back family
// calls the pack.
func Statements() (stmts []string) {
	pack := chpack.Statements()
	helpers := readback.FamilyStatements()
	ids := identsql.UdfDdlStatements()

	stmts = make([]string, 0, len(pack)+len(helpers)+len(ids)+1)
	stmts = append(stmts, pack...)
	stmts = append(stmts, helpers...)
	stmts = append(stmts, ids...)
	stmts = append(stmts, MarkerStatement())
	return
}

// RetiredNames lists the names this repository has shipped and withdrawn,
// which Install drops after the new roster verifies.
//
// It delegates to chpack, which is where the list has always lived and
// where it already covers more than the pack — the read-back family's
// pre-`LW_` spellings are on it. Keeping one append-only list is the point:
// a second one here would be a second place to forget.
func RetiredNames() (names []string) {
	names = chpack.RetiredNames()
	return
}

// DeclaredNames returns the declared set as a lookup, for callers asking
// "does this build declare that name" — the question a drift report is.
func DeclaredNames() (names map[string]FamilyE) {
	fns := DeclaredFunctions()
	names = make(map[string]FamilyE, len(fns))
	for _, f := range fns {
		names[f.Name] = f.Family
	}
	return
}

// Namespace is the prefix every leeway-owned SQL function carries. One
// namespace is what makes the surface enumerable on a server — the escaped
// form below reaches every family regardless of which package declares it.
const Namespace = "LW_"

// namespaceLike is Namespace as a LIKE pattern: `_` is a single-character
// wildcard in LIKE, so an unescaped `LW_%` would also match `LWX...`.
const namespaceLike = `LW\_%`

// InNamespace reports whether a server-side function name is leeway-owned.
func InNamespace(name string) (ok bool) {
	ok = strings.HasPrefix(name, Namespace)
	return
}
