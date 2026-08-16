package sqlvocab_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainKindsAreLegible(t *testing.T) {
	seen := make(map[string]sqlvocab.DomainKindE, len(sqlvocab.AllDomainKinds))
	for _, k := range sqlvocab.AllDomainKinds {
		s := k.String()
		assert.NotEqualf(t, "unknown", s, "kind %d has no String()", uint8(k))
		prev, dup := seen[s]
		assert.Falsef(t, dup, "kinds %d and %d share the label %q", uint8(prev), uint8(k), s)
		seen[s] = k
	}
	// AllDomainKinds must not fall behind the iota block, or the "no
	// Unspecified" sweeps below would silently stop covering a kind.
	last := sqlvocab.AllDomainKinds[len(sqlvocab.AllDomainKinds)-1]
	assert.Equal(t, "unknown", sqlvocab.DomainKindE(uint8(last)+1).String())
}

func TestRefDependence(t *testing.T) {
	assert.True(t, sqlvocab.DomainElementOf.IsRefDependent())
	assert.True(t, sqlvocab.DomainGlossKey.IsRefDependent())
	assert.True(t, sqlvocab.DomainSectionColumn.IsRefDependent())
	assert.True(t, sqlvocab.DomainDictionaryAttribute.IsRefDependent())
	assert.False(t, sqlvocab.DomainExpr.IsRefDependent())
	assert.False(t, sqlvocab.DomainComponentKind.IsRefDependent())

	assert.Equal(t, sqlvocab.NoRef, sqlvocab.Expr("x").Domain.Ref)
	assert.Equal(t, sqlvocab.NoRef, sqlvocab.Lit("'K'", sqlvocab.DomainComponentKind).Domain.Ref)
	assert.Equal(t, 0, sqlvocab.ElementOf("'f'", 0).Domain.Ref)
	assert.Equal(t, "tuple element of argument 0", sqlvocab.ElementOf("'f'", 0).Domain.String())
	assert.Equal(t, "component kind", sqlvocab.Lit("'K'", sqlvocab.DomainComponentKind).Domain.String())
}

func fn(name string, where sqlvocab.WhereE, params ...sqlvocab.Param) sqlvocab.Function {
	return sqlvocab.Function{Name: name, Where: where, Family: "test", Available: true, Params: params}
}

func TestRegisterRefusals(t *testing.T) {
	cases := []struct {
		name string
		fn   sqlvocab.Function
	}{
		{"no name", sqlvocab.Function{Where: sqlvocab.WhereClient}},
		{"no population", sqlvocab.Function{Name: "F"}},
		{
			"a parameter with no domain",
			sqlvocab.Function{Name: "F", Where: sqlvocab.WhereClient, Params: []sqlvocab.Param{{Name: "x"}}},
		},
		{
			"a ref pointing past the signature",
			fn("F", sqlvocab.WhereClient, sqlvocab.Expr("x"), sqlvocab.ElementOf("'f'", 5)),
		},
		{
			"a ref pointing at itself",
			fn("F", sqlvocab.WhereClient, sqlvocab.Expr("x"), sqlvocab.ElementOf("'f'", 1)),
		},
		{
			"a non-ref domain carrying a ref",
			sqlvocab.Function{Name: "F", Where: sqlvocab.WhereClient, Params: []sqlvocab.Param{
				{Name: "x", Domain: sqlvocab.Domain{Kind: sqlvocab.DomainExpr, Ref: 0}},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := sqlvocab.NewRegistry()
			assert.Error(t, r.Register(c.fn))
			assert.Equal(t, 0, r.Len(), "a refused Register must add nothing")
		})
	}
}

func TestRegisterIsAllOrNothing(t *testing.T) {
	r := sqlvocab.NewRegistry()
	err := r.Register(
		fn("Good", sqlvocab.WhereClient, sqlvocab.Expr("x")),
		sqlvocab.Function{Name: "Bad", Where: sqlvocab.WhereClient, Params: []sqlvocab.Param{{Name: "y"}}},
	)
	require.Error(t, err)
	assert.Equal(t, 0, r.Len())
	_, ok := r.Lookup("Good")
	assert.False(t, ok)
}

// One name in two populations is the LW_ID_* case and must be allowed; the
// same name twice in one population is drift and must not.
func TestOnePopulationPerNameOnly(t *testing.T) {
	r := sqlvocab.NewRegistry()
	require.NoError(t, r.Register(fn("LW_ID_HAS_TAG", sqlvocab.WhereServer, sqlvocab.Expr("x"))))
	require.NoError(t, r.Register(fn("LW_ID_HAS_TAG", sqlvocab.WhereClient, sqlvocab.Expr("x"))))

	fns, ok := r.Lookup("LW_ID_HAS_TAG")
	require.True(t, ok)
	assert.Len(t, fns, 2)

	assert.Error(t, r.Register(fn("LW_ID_HAS_TAG", sqlvocab.WhereClient, sqlvocab.Expr("x"))))
	assert.Error(t, r.Register(fn("LW_ID_HAS_TAG", sqlvocab.WhereServer|sqlvocab.WhereHost, sqlvocab.Expr("x"))))
	// Disjoint populations still compose.
	require.NoError(t, r.Register(fn("LW_ID_HAS_TAG", sqlvocab.WhereHost, sqlvocab.Expr("x"))))
}

func TestLookupFoldsCase(t *testing.T) {
	r := sqlvocab.NewRegistry()
	require.NoError(t, r.Register(fn("tupleElement", sqlvocab.WhereServer, sqlvocab.Expr("t"))))

	for _, spelling := range []string{"tupleElement", "tupleelement", "TUPLEELEMENT"} {
		_, ok := r.Lookup(spelling)
		assert.Truef(t, ok, "lookup %q", spelling)
	}
	assert.Error(t, r.Register(fn("TUPLEELEMENT", sqlvocab.WhereServer, sqlvocab.Expr("t"))),
		"a fold-equal duplicate is the same function to the server")
}

func TestSignaturePrefersTheDeclarationCarryingParams(t *testing.T) {
	r := sqlvocab.NewRegistry()
	require.NoError(t, r.Register(fn("F", sqlvocab.WhereServer)))
	require.NoError(t, r.Register(fn("F", sqlvocab.WhereClient, sqlvocab.Expr("x"), sqlvocab.Expr("y"))))

	sig, ok := r.Signature("F")
	require.True(t, ok)
	assert.Len(t, sig.Params, 2)

	_, ok = r.Signature("Nonesuch")
	assert.False(t, ok)
}

func TestAllKeepsRegistrationOrder(t *testing.T) {
	r := sqlvocab.NewRegistry()
	require.NoError(t, r.Register(
		fn("C", sqlvocab.WhereClient), fn("A", sqlvocab.WhereClient), fn("B", sqlvocab.WhereClient)))
	names := make([]string, 0, 3)
	for _, f := range r.All() {
		names = append(names, f.Name)
	}
	assert.Equal(t, []string{"C", "A", "B"}, names)

	r.Reset()
	assert.Empty(t, r.All())
}

func TestCallTemplate(t *testing.T) {
	f := fn("LW_GET", sqlvocab.WhereClient,
		sqlvocab.Expr("x"), sqlvocab.Lit("'section'", sqlvocab.DomainSection))
	assert.Equal(t, "LW_GET(x, 'section')", f.Call())
	assert.Equal(t, []string{"x", "'section'"}, sqlvocab.ParamNames(f.Params))
	assert.Equal(t, "LW_NOARG()", fn("LW_NOARG", sqlvocab.WhereClient).Call())
}

func TestWhereString(t *testing.T) {
	assert.Equal(t, "server", sqlvocab.WhereServer.String())
	assert.Equal(t, "server+client", (sqlvocab.WhereServer | sqlvocab.WhereClient).String())
	assert.Equal(t, "unknown", sqlvocab.WhereE(0).String())
}

// The built-in table is a hand-curated list; what a test can hold it to is
// that every entry is well-formed and that the entries the design names by
// spelling are present with the domains it names.
func TestBuiltinsAreWellFormed(t *testing.T) {
	r := sqlvocab.NewRegistry()
	require.NoError(t, r.Register(sqlvocab.Builtins()...), "every builtin must pass the same validation a roster does")

	byName := make(map[string]sqlvocab.Function, 64)
	for _, f := range sqlvocab.Builtins() {
		assert.NotEmptyf(t, f.Doc, "%s has no doc line", f.Name)
		byName[f.Name] = f
	}

	te, ok := byName["tupleElement"]
	require.True(t, ok)
	assert.Equal(t, sqlvocab.DomainExpr, te.Params[0].Domain.Kind)
	assert.Equal(t, sqlvocab.DomainElementOf, te.Params[1].Domain.Kind)
	assert.Equal(t, 0, te.Params[1].Domain.Ref)

	cast, ok := byName["CAST"]
	require.True(t, ok)
	assert.Equal(t, sqlvocab.DomainTypeName, cast.Params[1].Domain.Kind)

	dg, ok := byName["dictGet"]
	require.True(t, ok)
	assert.Equal(t, sqlvocab.DomainDictionary, dg.Params[0].Domain.Kind)
	assert.Equal(t, sqlvocab.DomainDictionaryAttribute, dg.Params[1].Domain.Kind)
	assert.Equal(t, 0, dg.Params[1].Domain.Ref)

	gs, ok := byName["getSetting"]
	require.True(t, ok)
	assert.Equal(t, sqlvocab.DomainSetting, gs.Params[0].Domain.Kind)

	// Every time-zone-taking spelling puts the zone last.
	zoned := 0
	for _, f := range sqlvocab.Builtins() {
		for i := range f.Params {
			if f.Params[i].Domain.Kind == sqlvocab.DomainTimeZone {
				zoned++
				assert.Equalf(t, len(f.Params)-1, i, "%s: the time zone is not the last argument", f.Name)
			}
		}
	}
	assert.Greater(t, zoned, 10)
	assert.True(t, strings.HasPrefix(byName["toStartOfDay"].Name, "toStartOf"))
}
