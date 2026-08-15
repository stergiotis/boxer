package constructsql_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The artefacts are stand-ins with the shape the real ones have — a hasAll
// presence term, a countEqual validator, a CAST(tuple(...)) projection — kept
// short so an assertion reads. What they say does not matter here; where they
// land does.
const (
	memFilter     = "hasAll(lr, [1, 2]) AND countEqual(lr, 1) = 1"
	memProjection = "CAST(tuple(mem), 'Tuple(TotalBytes UInt64)')"
	cpuFilter     = "hasAll(lr, [3, 4]) AND countEqual(lr, 3) = 1"
	cpuProjection = "CAST(tuple(cpu), 'Tuple(TotalPercent UInt8)')"
)

func testSource(t *testing.T) constructsql.ComponentSourceI {
	t.Helper()
	r := componentsql.NewRegistry()
	require.NoError(t, r.Register(componentsql.Set{
		Store: "Sysmetrics",
		Table: "boxer.facts",
		Kinds: map[string]componentsql.Artefacts{
			"SysMem": {Presence: "hasAll(lr, [1, 2])", Validator: "countEqual(lr, 1) = 1",
				Filter: memFilter, Projection: memProjection},
			"SysCpu": {Presence: "hasAll(lr, [3, 4])", Validator: "countEqual(lr, 3) = 1",
				Filter: cpuFilter, Projection: cpuProjection},
		},
	}))
	require.NoError(t, r.Register(componentsql.Set{
		Store: "Persist",
		Table: "boxer.persiststate",
		Kinds: map[string]componentsql.Artefacts{
			"PersistBlob": {Presence: "has(lr, 9)", Validator: "countEqual(lr, 9) = 1",
				Filter:     "has(lr, 9) AND countEqual(lr, 9) = 1",
				Projection: "CAST(tuple(blob), 'Tuple(Blob String)')"},
		},
	}))
	return r
}

func expand(t *testing.T, sql string) (out string, err error) {
	t.Helper()
	return constructsql.ComponentExpandPass(testSource(t), "boxer").Run(sql)
}

func expandOK(t *testing.T, sql string) (out string) {
	t.Helper()
	out, err := expand(t, sql)
	require.NoError(t, err)
	return
}

// The ADR-0189 §SD4 property, and the reason the pass exists: a projection
// never travels without the filter that makes it exact. ADR-0066 records that
// Projection alone locates an attribute by indexOf and returns the first
// match, so a row carrying a membership twice would read plausibly and wrongly.
func TestProjectionNeverTravelsWithoutItsFilter(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem') AS m FROM boxer.facts")

	assert.Contains(t, out, memProjection, "the projection should be expanded")
	assert.Contains(t, out, "WHERE "+memFilter, "and its filter added to the WHERE it lacked")
	assert.NotContains(t, out, "LW_COMPONENT", "no call may survive expansion")
}

// The filter lands in WHERE rather than wrapping the projection, because the
// presence terms only prune granules from there (ADR-0066, ADR-0189 §SD4).
func TestFilterIsInjectedIntoWhereNotAroundTheProjection(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem') FROM boxer.facts")

	where := strings.Index(out, "WHERE")
	require.Greater(t, where, 0, "a WHERE should have been synthesised")
	assert.NotContains(t, out, "if("+memFilter, "the filter must not be a per-row guard")
	assert.Contains(t, out[where:], memFilter)
}

func TestExistingWhereIsConjoinedAndParenthesised(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem') FROM boxer.facts WHERE a = 1")

	assert.Contains(t, out, "(a = 1) AND "+memFilter,
		"the author's predicate is wrapped, so a disjunction cannot absorb the conjunct")
}

// The parenthesisation is not cosmetic: without it `a OR b AND filter` parses
// as `a OR (b AND filter)`, and rows failing the component filter come back.
func TestDisjunctionIsParenthesisedBeforeConjunction(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem') FROM boxer.facts WHERE a = 1 OR b = 2")

	assert.Contains(t, out, "(a = 1 OR b = 2) AND "+memFilter)
}

// Two kinds in one scope conjoin: a row may carry several components, so the
// AND is the right reading rather than a conflict.
func TestTwoKindsInOneScopeBothInject(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem'), LW_COMPONENT('SysCpu') FROM boxer.facts")

	assert.Contains(t, out, memProjection)
	assert.Contains(t, out, cpuProjection)
	// Sorted, so the emitted conjunction is stable between runs.
	assert.Contains(t, out, "WHERE "+cpuFilter+" AND "+memFilter)
}

// One kind named twice injects one filter: the conjunct is deduplicated per
// kind, not per call.
func TestOneKindTwiceInjectsOneFilter(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem'), LW_COMPONENT('SysMem') FROM boxer.facts")

	assert.Equal(t, 1, strings.Count(out, memFilter), "the filter should appear once")
}

// An author who wrote the filter themselves does not get a second copy.
func TestAnExplicitFilterInWhereDischargesTheInjection(t *testing.T) {
	out := expandOK(t,
		"SELECT LW_COMPONENT('SysMem') FROM boxer.facts WHERE LW_COMPONENT_FILTER('SysMem')")

	assert.Equal(t, 1, strings.Count(out, memFilter),
		"the author's own filter is the injection, not a duplicate of it")
}

// ...but the same call in the projection list filters nothing, so it must not
// discharge the injection. Treating it as satisfying the kind would hand back
// exactly the first-match semantics SD4 exists to prevent.
func TestAFilterInTheProjectionListDoesNotDischargeTheInjection(t *testing.T) {
	out := expandOK(t,
		"SELECT LW_COMPONENT('SysMem'), LW_COMPONENT_FILTER('SysMem') AS conforms FROM boxer.facts")

	assert.Equal(t, 2, strings.Count(out, memFilter),
		"one as the author's boolean column, one injected into WHERE")
	assert.Contains(t, out, "WHERE "+memFilter)
}

// The filter alone is usable without a projection, and injects nothing.
func TestFilterAloneExpandsAndInjectsNothing(t *testing.T) {
	out := expandOK(t, "SELECT id FROM boxer.facts WHERE LW_COMPONENT_FILTER('SysMem')")

	assert.Equal(t, "SELECT id FROM boxer.facts WHERE "+memFilter, out)
}

// Each scope carries its own injection: an inner SELECT's component must not
// be filtered by the outer one's WHERE, or vice versa.
func TestInjectionIsPerScope(t *testing.T) {
	out := expandOK(t,
		"SELECT * FROM (SELECT LW_COMPONENT('SysMem') AS m FROM boxer.facts) WHERE 1")

	// Asserted whole: the filter belongs to the inner SELECT — inside the
	// subquery's parentheses — and the outer WHERE is left as the author
	// wrote it.
	assert.Equal(t,
		"SELECT * FROM (SELECT "+memProjection+" AS m FROM boxer.facts WHERE "+memFilter+") WHERE 1",
		out)
}

func TestExpansionIsIdempotent(t *testing.T) {
	once := expandOK(t, "SELECT LW_COMPONENT('SysMem') FROM boxer.facts")
	twice := expandOK(t, once)
	assert.Equal(t, once, twice, "an expanded statement carries no call, so re-running changes nothing")
}

// --- ADR-0189 §SD5: the pass declines rather than guessing ---

func TestUnknownKindIsRefusedAndListsTheAlternatives(t *testing.T) {
	_, err := expand(t, "SELECT LW_COMPONENT('SysNope') FROM boxer.facts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no registered store publishes this component kind")
	assert.Contains(t, err.Error(), "SysMem", "the message should list what is registered")
}

// A component read whose SELECT reads a different table would emit column
// names that bind to whatever that table happens to have.
func TestWrongTableIsRefused(t *testing.T) {
	_, err := expand(t, "SELECT LW_COMPONENT('SysMem') FROM boxer.persiststate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not read the table the component is stored in")
}

// The artefacts carry unqualified column names, so a join is refused until
// they can be emitted qualified (ADR-0189 §SD6).
func TestJoinIsRefused(t *testing.T) {
	_, err := expand(t,
		"SELECT LW_COMPONENT('SysMem') FROM boxer.facts AS f JOIN boxer.other AS o ON f.id = o.id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one table")
}

func TestCteOrSubquerySourceIsRefused(t *testing.T) {
	_, err := expand(t,
		"WITH c AS (SELECT 1) SELECT LW_COMPONENT('SysMem') FROM c")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CTE, subquery or table function")
}

func TestBadArgumentsAreRefused(t *testing.T) {
	_, err := expand(t, "SELECT LW_COMPONENT('SysMem', 'extra') FROM boxer.facts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one argument")

	_, err = expand(t, "SELECT LW_COMPONENT(7) FROM boxer.facts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quoted name, not a number")
}

// The default database resolves an unqualified FROM, so a statement written
// against `facts` binds the same component as one written against
// `boxer.facts`.
func TestUnqualifiedTableResolvesThroughTheDefaultDatabase(t *testing.T) {
	out := expandOK(t, "SELECT LW_COMPONENT('SysMem') FROM facts")
	assert.Contains(t, out, memProjection)
	assert.Contains(t, out, memFilter)
}

// A statement with no component call is returned untouched — the pass must be
// free to sit in every pipeline.
func TestStatementWithoutComponentsIsUntouched(t *testing.T) {
	const sql = "SELECT a, b FROM boxer.facts WHERE c = 1"
	assert.Equal(t, sql, expandOK(t, sql))
}

func TestNilSourceIsANoOp(t *testing.T) {
	const sql = "SELECT LW_COMPONENT('SysMem') FROM boxer.facts"
	out, err := constructsql.ComponentExpandPass(nil, "boxer").Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, out, "an unwired host should leave the statement alone, not fail it")
}

// The family is declared for the vocabulary panel, and the two names differ in
// what they need installed (ADR-0189 §SD8).
func TestFunctionsAreDeclaredForThePanel(t *testing.T) {
	names := make([]string, 0, 2)
	for _, f := range constructsql.ComponentFunctions() {
		names = append(names, f.Name)
		assert.NotEmpty(t, f.Doc, "%s should carry a one-line doc", f.Name)
	}
	assert.ElementsMatch(t, []string{"LW_COMPONENT", "LW_COMPONENT_FILTER"}, names)
	assert.NotEmpty(t, constructsql.ComponentExpansionDependencies(),
		"the projection calls the read-back family, so the panel must be able to mark it")
}

// Every pass in the standard registry is wired with an empty default
// database, so an unqualified FROM must still bind — the server resolves it
// against the session database. Refusing it would make the family unusable
// through passreg.
func TestUnqualifiedTableBindsWhenNoDefaultDatabaseIsConfigured(t *testing.T) {
	out, err := constructsql.ComponentExpandPass(testSource(t), "").
		Run("SELECT LW_COMPONENT('SysMem') FROM facts")
	require.NoError(t, err)
	assert.Contains(t, out, memProjection)
	assert.Contains(t, out, "WHERE "+memFilter)
}

// A qualifier that is present and disagrees is still refused: "some other
// database also has a table called facts" is not a component read.
func TestAnotherDatabasesTableOfTheSameNameIsRefused(t *testing.T) {
	_, err := constructsql.ComponentExpandPass(testSource(t), "").
		Run("SELECT LW_COMPONENT('SysMem') FROM elsewhere.facts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different database's table")
}
