package defaults

import (
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
	require.Len(t, es, 5)
	require.Equal(t, "ExpandDescriptiveStatistics", es[0].Pass.Name, "ADR-0161 expansion orders first (75)")
	require.Equal(t, "DocsearchExpand", es[1].Pass.Name, "ADR-0164 expansion between the two (80)")
	require.Equal(t, "ExpandLwIdMacros", es[2].Pass.Name)
	require.Equal(t, "LwConstructExpand", es[3].Pass.Name, "ADR-0181 constructor expansion orders after identsql (130)")
	require.Equal(t, "GlossExpand", es[4].Pass.Name, "ADR-0186 gloss(…) expansion orders after the constructors (140)")
	for _, e := range es {
		require.NotEmpty(t, e.Description)
		require.NotEmpty(t, e.Provenance)
	}

	// Registering twice into the same registry must fail loudly (duplicate
	// key), not silently double the entries.
	require.Error(t, RegisterStandard(r))
	require.Len(t, r.Entries(passreg.StagePreExecute), 5)
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

	// Concrete entries are the four expansions (descriptiveStatistics,
	// docsearch, LW_ID_*, LW_ constructors); the three schema-bound passes —
	// handle resolution, LW_GET extraction, and the target-adopting
	// constructor variant (ADR-0181 §SD8 M2) — are factories.
	require.Len(t, r.Entries(passreg.StagePreExecute), 5)
	fs := r.Factories(passreg.StagePreExecute)
	require.Len(t, fs, 3)
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
