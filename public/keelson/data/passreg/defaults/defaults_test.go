package defaults

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
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
	require.Len(t, es, 1)
	require.Equal(t, "ExpandLwIdMacros", es[0].Pass.Name)
	require.NotEmpty(t, es[0].Description)
	require.NotEmpty(t, es[0].Provenance)

	// Registering twice into the same registry must fail loudly (duplicate
	// key), not silently double the entries.
	require.Error(t, RegisterStandard(r))
	require.Len(t, r.Entries(passreg.StagePreExecute), 1)
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

// TestStandardSetRegistersResolveColumnNamesFactory proves the leeway resolver
// is registered as a late-bound Factory (ADR-0108 §SD7): it appears in the
// catalog after identsql, and its Build accepts a ColumnResolverI binding while
// declining anything else — so the unbound /query path never applies it.
func TestStandardSetRegistersResolveColumnNamesFactory(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	// Concrete entries are identsql-only; the resolver is a factory.
	require.Len(t, r.Entries(passreg.StagePreExecute), 1)
	fs := r.Factories(passreg.StagePreExecute)
	require.Len(t, fs, 1)
	f := fs[0]
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
