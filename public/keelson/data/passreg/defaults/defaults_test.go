package defaults

import (
	"fmt"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
)

// stubResolver is a no-op ColumnResolverI. Build only type-asserts the binding;
// it never calls Resolve, so the verdict is immaterial.
type stubResolver struct{}

func (stubResolver) Resolve(dbName, tableName, handle string) passes.ResolveResult {
	return passes.ResolveResult{}
}

func TestRegisterStandard(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	es := r.Entries(passreg.StagePreExecute)
	require.Len(t, es, 6)
	require.Equal(t, "ExpandDescriptiveStatistics", es[0].Pass.Name, "ADR-0161 expansion orders first (75)")
	require.Equal(t, "DocsearchExpand", es[1].Pass.Name, "ADR-0164 expansion between the two (80)")
	require.Equal(t, "ExpandLwIdMacros", es[2].Pass.Name)
	require.Equal(t, "LwComponentExpand", es[3].Pass.Name, "ADR-0189 component expansion orders after identsql and before extraction (110)")
	require.Equal(t, "LwConstructExpand", es[4].Pass.Name, "ADR-0181 constructor expansion orders after identsql (130)")
	require.Equal(t, "GlossExpand", es[5].Pass.Name, "ADR-0186 gloss(…) expansion orders after the constructors (140)")
	for _, e := range es {
		require.NotEmpty(t, e.Description)
		require.NotEmpty(t, e.Provenance)
	}

	// Registering twice into the same registry must fail loudly (duplicate
	// key), not silently double the entries.
	require.Error(t, RegisterStandard(r))
	require.Len(t, r.Entries(passreg.StagePreExecute), 6)
}

// TestStandardSetExpandsDescriptiveStatistics proves the ADR-0161 wiring end
// to end: the macro leaves the pre-execute stage as the contract query.
func TestStandardSetExpandsDescriptiveStatistics(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	out := r.ApplyBestEffort(passreg.StagePreExecute, "SELECT descriptiveStatistics(x) FROM t", zerolog.Nop())
	require.NotContains(t, out, "descriptiveStatistics", "macro call must be expanded")
	require.Contains(t, out, "quantilesTDigest(")
	require.Contains(t, out, "AS series")
	require.Contains(t, out, "FROM t", "surrounding query must survive")
}

// TestStandardSetExpandsLwIdMacros proves the wiring end to end: a query
// carrying an LW_ID_* call leaves the pre-execute stage expanded.
func TestStandardSetExpandsLwIdMacros(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	out := r.ApplyBestEffort(passreg.StagePreExecute, "SELECT LW_ID_IS_VALID(id) FROM t", zerolog.Nop())
	require.NotContains(t, out, "LW_ID_IS_VALID", "macro call must be expanded")
	require.Contains(t, out, "FROM t", "surrounding query must survive")
}

// TestStandardSetExpandsDocsearch proves the ADR-0164 wiring end to end:
// the macro leaves the pre-execute stage as the documentation-search
// UNION, its keelson(...) references intact for the executor-boundary
// pass.
func TestStandardSetExpandsDocsearch(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	out := r.ApplyBestEffort(passreg.StagePreExecute, "SELECT * FROM docsearch('argMax') ORDER BY score DESC", zerolog.Nop())
	require.NotContains(t, out, "docsearch", "macro call must be expanded")
	require.Contains(t, out, "keelson('helpsections')")
	require.Contains(t, out, "keelson('adrsections')")
	require.Contains(t, out, "system.documentation")
	require.Contains(t, out, "ORDER BY score DESC", "surrounding query must survive")
}

// TestStandardSetRegistersResolveColumnNamesFactory proves the leeway resolver
// is registered as a late-bound Factory (ADR-0108 §SD7): it appears in the
// catalog after identsql, and its Build accepts a ColumnResolverI binding while
// declining anything else — so the unbound /query path never applies it.
func TestStandardSetRegistersResolveColumnNamesFactory(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	// Concrete entries are the expansions that need no per-consumer binding
	// (descriptiveStatistics, docsearch, LW_ID_*, LW_COMPONENT, LW_
	// constructors, gloss); the four bound passes — handle resolution, LW_GET
	// extraction, the target-adopting constructor variant (ADR-0181 §SD8 M2)
	// and the lading macros (ADR-0198 §SD7, bound to a mount visibility) — are
	// factories. LW_COMPONENT is an entry despite needing a registry: that
	// registry is a host-wired global, not a per-consumer binding
	// (ADR-0189 §SD7).
	require.Len(t, r.Entries(passreg.StagePreExecute), 6)
	fs := r.Factories(passreg.StagePreExecute)
	require.Len(t, fs, 4)
	var f passreg.Factory
	for _, cand := range fs {
		if cand.Name == "ResolveColumnNames" {
			f = cand
		}
	}
	require.Equal(t, "ResolveColumnNames", f.Name)
	require.Equal(t, 200, f.Order, "must order after identsql (100)")

	// The catalog lists it as late-bound.
	found := false
	for _, row := range r.Catalog() {
		if row.Name == "ResolveColumnNames" {
			found = true
			require.True(t, row.LateBound, "resolver row must be late-bound")
			require.Equal(t, passreg.StagePreExecute, row.Stage)
		}
	}
	require.True(t, found, "ResolveColumnNames must appear in keelson('sql_passes')")

	// Build realises the pass only for a ColumnResolverI binding.
	p, ok := f.Build(stubResolver{})
	require.True(t, ok, "a ColumnResolverI binding must be accepted")
	require.Equal(t, "ResolveColumnNames", p.Name)
	_, ok = f.Build("not a resolver")
	require.False(t, ok, "a non-resolver binding must be declined")
	_, ok = f.Build(nil)
	require.False(t, ok, "a nil binding must be declined")
}

// TestStandardSetExpandsLeewayConstructors proves the ADR-0181 §SD7 wiring
// end to end: a constructor call leaves the pre-execute stage as an aliased
// expression minting the physical name, and an inert query is untouched.
func TestStandardSetExpandsLeewayConstructors(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	out := r.ApplyBestEffort(passreg.StagePreExecute, "SELECT LW_PLAIN(sum(x), 'total-revenue', 'u64', 'item:oq') FROM t", zerolog.Nop())
	require.NotContains(t, out, "LW_PLAIN", "constructor call must be expanded")
	require.Contains(t, out, `sum(x) AS "oq:total-revenue:u64:::0:"`)
	require.Contains(t, out, "FROM t", "surrounding query must survive")

	const inert = "SELECT a FROM t WHERE b = 1"
	require.Equal(t, inert, r.ApplyBestEffort(passreg.StagePreExecute, inert, zerolog.Nop()))
}

// TestStandardSetOrdersIdentityBeforeConstructors pins the 100 → 130
// ordering claim: an LW_ID_* macro inside a constructor's expression
// argument is expanded first, so the kept span carries bit arithmetic, and
// the constructor then mints the alias around it.
func TestStandardSetOrdersIdentityBeforeConstructors(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	out := r.ApplyBestEffort(passreg.StagePreExecute, "SELECT LW_PLAIN(LW_ID_TAG_VALUE(id), 'tag', 'u32', 'item:oq') FROM t", zerolog.Nop())
	require.NotContains(t, out, "LW_ID_TAG_VALUE", "inner identity macro must expand")
	require.NotContains(t, out, "LW_PLAIN", "outer constructor must expand")
	require.Contains(t, out, `AS "oq:tag:u32:::0:"`)
	require.Contains(t, out, "bitAnd(", "the kept span carries the identity expansion")
}

// TestStandardSetOmitsExposeSelectionConditions pins ADR-0121 §SD7: ExposeSelectionConditions changes a
// query's result schema, so it is opt-in per host (play applies it from
// buildResidual behind a toggle) and must never join the standard set — a bound
// stage must leave a retrieval query's SELECT and WHERE exactly as written.
func TestStandardSetOmitsExposeSelectionConditions(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	for _, f := range r.Factories(passreg.StagePreExecute) {
		require.NotEqual(t, "ExposeSelectionConditions", f.Name, "ExposeSelectionConditions must stay opt-in, not standard")
	}
	for _, e := range r.Entries(passreg.StagePreExecute) {
		require.NotEqual(t, "ExposeSelectionConditions", e.Pass.Name, "ExposeSelectionConditions must stay opt-in, not standard")
	}

	const q = "SELECT a FROM tt WHERE c = 1"
	out := r.ApplyBestEffortBound(passreg.StagePreExecute, q, stubResolver{}, zerolog.Nop())
	require.Equal(t, q, out, "the standard set must not add condition columns")
}

// TestStandardSetRegistersExtractFactory proves the ADR-0181 §SD3/§SD7
// wiring: LwExtractExpand is late-bound like handle resolution, because it
// needs a schema to turn a section name into physical lanes, and it declines
// a binding that cannot answer that question rather than silently not
// expanding.
func TestStandardSetRegistersExtractFactory(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	var f passreg.Factory
	for _, cand := range r.Factories(passreg.StagePreExecute) {
		if cand.Name == constructsql.ExtractPassName {
			f = cand
		}
	}
	require.Equal(t, constructsql.ExtractPassName, f.Name)
	require.Equal(t, 120, f.Order, "must order after identsql (100), before the constructors (130)")

	found := false
	for _, row := range r.Catalog() {
		if row.Name == constructsql.ExtractPassName {
			found = true
			require.True(t, row.LateBound, "the extraction row must be late-bound")
			require.Equal(t, passreg.StagePreExecute, row.Stage)
		}
	}
	require.True(t, found, "LwExtractExpand must appear in keelson('sql_passes')")

	_, ok := f.Build("not a lane source")
	require.False(t, ok, "a binding that cannot answer for a section must be declined")
	_, ok = f.Build(nil)
	require.False(t, ok, "a nil binding must be declined")
}

// TestStandardSetExpandsLadingMacrosWhenBound pins the wiring the macros were
// missing: they are declared to the security classifier and routed server-side
// by play's dispatch, so a statement naming one must actually expand somewhere
// — otherwise `SELECT path FROM fs(…)` is classified as a local read, sent to
// the server, and answered with "unknown table function fs".
//
// Bound, it expands; unbound, the factory declines and the statement is
// untouched, which is what makes the visibility an explicit decision rather
// than a default (ADR-0198 §SD7).
func TestStandardSetExpandsLadingMacrosWhenBound(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	const mount = uint64(0xF5F5_0198_0000_0001)
	src := fmt.Sprintf("SELECT path FROM fs(%d)", mount)

	bound := r.ApplyBestEffortBound(passreg.StagePreExecute, src,
		ladingsql.VisibleAll{}, zerolog.Nop())
	require.NotContains(t, bound, "fs("+strconv.FormatUint(mount, 10)+")",
		"a bound visibility must expand the macro before the statement ships")
	require.Contains(t, bound, ladingschema.TableNameMeta,
		"the expansion reads the store's entry table")

	unbound := r.ApplyBestEffortBound(passreg.StagePreExecute, src, nil, zerolog.Nop())
	require.Contains(t, unbound, "fs("+strconv.FormatUint(mount, 10)+")",
		"with no visibility bound the factory declines and nothing expands")
}
