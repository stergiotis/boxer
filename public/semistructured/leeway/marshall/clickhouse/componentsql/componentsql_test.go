package componentsql_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func set(store, table string, kinds ...string) (s componentsql.Set) {
	s = componentsql.Set{Store: store, Table: table, Kinds: map[string]componentsql.Artefacts{}}
	for _, k := range kinds {
		s.Kinds[k] = componentsql.Artefacts{
			Presence:   "has(lr, 1)",
			Validator:  "countEqual(lr, 1) = 1",
			Filter:     "has(lr, 1) AND countEqual(lr, 1) = 1",
			Projection: "CAST(tuple(x), 'Tuple(X UInt64)')",
		}
	}
	return
}

func TestRegisterThenLookup(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(set("Sysmetrics", "boxer.facts", "SysCpu", "SysMem")))

	b, ok := r.Lookup("SysMem")
	require.True(t, ok)
	assert.Equal(t, "SysMem", b.Kind)
	assert.Equal(t, "boxer.facts", b.Table)
	assert.Equal(t, "Sysmetrics", b.Store)
	assert.NotEmpty(t, b.Filter)
	assert.NotEmpty(t, b.Projection)

	assert.Equal(t, []string{"SysCpu", "SysMem"}, r.Kinds(), "Kinds is sorted, so a diagnostic listing it is stable")
}

// An unknown kind is absent, not zero-valued: a caller must be able to tell
// "no such component" from "a component with an empty filter", because the
// second would expand to a predicate matching everything.
func TestLookupOfAnUnknownKindIsNotFound(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(set("Sysmetrics", "boxer.facts", "SysCpu")))

	b, ok := r.Lookup("NoSuchKind")
	assert.False(t, ok)
	assert.Empty(t, b.Filter)
}

// Two stores publishing one kind is refused rather than resolved by wiring
// order, and the message names both sides so the collision is actionable.
func TestDuplicateKindIsRefusedAndNamesBothStores(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(set("Sysmetrics", "boxer.facts", "Identity")))

	err := r.Register(set("Persist", "boxer.persiststate", "Identity"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same component kind")

	b, _ := r.Lookup("Identity")
	assert.Equal(t, "Sysmetrics", b.Store, "the first registration stands; a refused one must not overwrite")
}

// A set carrying one bad kind adds none of them. Half a registration is worse
// than none: the caller sees an error and would reasonably assume nothing
// landed.
func TestRegisterIsAllOrNothing(t *testing.T) {
	r := componentsql.NewRegistry()
	s := set("Broken", "boxer.facts", "Good")
	s.Kinds["Bad"] = componentsql.Artefacts{Presence: "x", Validator: "y"} // no Filter, no Projection

	err := r.Register(s)
	require.Error(t, err)
	assert.Empty(t, r.Kinds(), "the good kind must not have landed either")
}

func TestRegisterRejectsAnUnusableSet(t *testing.T) {
	r := componentsql.NewRegistry()

	assert.Error(t, r.Register(componentsql.Set{Store: "S", Kinds: set("S", "t", "K").Kinds}),
		"a set with no table cannot bind its columns to anything")
	assert.Error(t, r.Register(componentsql.Set{Store: "S", Table: "boxer.facts"}),
		"a set carrying no kinds is a wiring mistake, not an empty success")

	noProjection := set("S", "boxer.facts", "K")
	a := noProjection.Kinds["K"]
	a.Projection = ""
	noProjection.Kinds["K"] = a
	assert.Error(t, r.Register(noProjection),
		"a kind that cannot answer a projection must be refused at registration, not at query time")
}

func TestResetEmptiesTheRegistry(t *testing.T) {
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(set("Sysmetrics", "boxer.facts", "SysCpu")))
	r.Reset()
	assert.Empty(t, r.Kinds())

	_, ok := r.Lookup("SysCpu")
	assert.False(t, ok)
}

// The package-level helpers reach Default, which is what a host wires into.
func TestPackageLevelHelpersUseDefault(t *testing.T) {
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)

	require.NoError(t, componentsql.Register(set("Sysmetrics", "boxer.facts", "SysCpu")))
	b, ok := componentsql.Lookup("SysCpu")
	require.True(t, ok)
	assert.Equal(t, "boxer.facts", b.Table)
}

// The Projection's last string literal is the CAST's type argument. Reading it
// has to step over the double-quoted physical column names in front of it,
// which is the only part of the rule that is not obvious.
func TestElementsReadsTheCastType(t *testing.T) {
	a := componentsql.Artefacts{
		Projection: `CAST(tuple("tv:sym:value:val:s:1::I:0::data", "id:id:u64:47::0:"), ` +
			`'Tuple(Id UInt64, Ts DateTime64(9,\'UTC\'), Watts Nullable(Float32))')`,
	}
	elems, err := a.Elements()
	require.NoError(t, err)
	require.Len(t, elems, 3)
	assert.Equal(t, "Id", elems[0].Name)
	assert.Equal(t, "UInt64", elems[0].Type.String())
	assert.Equal(t, "Ts", elems[1].Name)
	assert.Equal(t, "DateTime64(9, 'UTC')", elems[1].Type.String())
	assert.Equal(t, "Watts", elems[2].Name)
	assert.Equal(t, "Nullable(Float32)", elems[2].Type.String())
}

func TestElementsRefusesAnUnreadableProjection(t *testing.T) {
	cases := []struct {
		name       string
		projection string
	}{
		{"no literal at all", `CAST(tuple("a", "b"), Tuple(Id UInt64))`},
		{"the literal is not a type", `CAST(tuple("a"), 'not a type at all!')`},
		{"casts to something other than a tuple", `CAST(x, 'Array(UInt64)')`},
		{"a positional tuple names no slots", `CAST(tuple(1, 2), 'Tuple(UInt64, String)')`},
		{"empty", ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := componentsql.Artefacts{Projection: c.projection}.Elements()
			assert.Error(t, err)
		})
	}
}
