// Package chpack defines and installs the co/ragged ClickHouse function
// pack (ADR-0162): the expression-level leeway query vocabulary, shipped as
// server-side SQL user-defined functions.
//
// A SQL UDF is a macro — inlined during query analysis, plan-identical to
// its handwritten expansion, transparent to index analysis and PREWHERE —
// so the pack costs nothing at runtime. This spec is normative (ADR-0162
// §SD6); doc/howto/leeway-clickhouse-array-idioms.md carries the executable
// background and doc/explanation/leeway-query-algebra.md the model.
//
// Conventions (§SD2): ClickHouse-idiomatic camelCase under the owned
// prefixes `co` and `ragged`, lambda-first parameters, 1-based indexing,
// compositions only — a builtin that already is the operation is used
// directly, not wrapped. Shipped names are append-only in semantics (§SD5):
// a published function's meaning never changes; changed behavior gets a new
// name and a Version bump.
package chpack

import (
	"fmt"
	"strings"
)

// Version is the pack revision reported by LEEWAY_PACK_VERSION(). Bump on any
// roster change; never repurpose a shipped name (ADR-0162 §SD5).
const Version = 2

// VersionFunctionName is the zero-argument marker function that makes
// client/server pack skew a query (ADR-0162 §SD5).
const VersionFunctionName = "LEEWAY_PACK_VERSION"

// Function is one pack entry. Body is a ClickHouse expression over Params —
// and over earlier pack functions only: the roster is dependency-ordered,
// because the server resolves referenced functions at CREATE time.
type Function struct {
	Name   string
	Params []string
	Body   string
	Doc    string
}

// Functions returns the v1 roster (ADR-0162 §SD6) in installation order.
// Bodies are the fused forms (§SD4): no body materializes Array(Array)
// except RAGGED_NEST, whose codomain is nested — it is the explicit
// presentation-boundary operation. All bodies stay total for foreign data
// with zero-length runs; on leeway reads both descriptors are positive and
// the empty-run defaults are dead cases.
func Functions() (fns []Function) {
	fns = []Function{
		{
			Name:   "CO_LOOKUP",
			Params: []string{"keys", "lane", "k"},
			Body:   "arrayElement(lane, indexOf(keys, k))",
			Doc:    "value of the co-lane at the first position where keys equals k; the type default when absent",
		},
		{
			Name:   "CO_LOOKUP_NULL",
			Params: []string{"keys", "lane", "k"},
			Body:   "if(indexOf(keys, k) = 0, NULL, arrayElement(lane, indexOf(keys, k)))",
			Doc:    "CO_LOOKUP with an absent key distinguishable from a stored default: NULL when k is not present",
		},
		{
			Name:   "CO_GATHER",
			Params: []string{"lane", "sel"},
			Body:   "arrayMap(i -> arrayElement(lane, i), sel)",
			Doc:    "project a lane through a position list (argwhere witness, permutation, or parent ids)",
		},
		{
			Name:   "CO_ARG_SORT",
			Params: []string{"keys"},
			Body:   "arraySort(i -> arrayElement(keys, i), arrayEnumerate(keys))",
			Doc:    "permutation that sorts keys; CO_GATHER every sibling lane through it to sort co-lanes consistently",
		},
		{
			Name:   "CO_ARG_MAX",
			Params: []string{"lane", "keys"},
			Body:   "arrayReduce('argMax', lane, keys)",
			Doc:    "the lane value at the position where keys is maximal",
		},
		{
			Name:   "CO_EXISTS_EQ2",
			Params: []string{"a", "x", "b", "y"},
			Body:   "has(a, x) AND has(b, y) AND arrayExists((p, q) -> p = x AND q = y, a, b)",
			Doc:    "same-position equality existence over two co-lanes, with the sargable has-guards bundled (ADR-0162 §SD3)",
		},
		{
			Name:   "RAGGED_STARTS",
			Params: []string{"card"},
			Body:   "arrayMap((h, c) -> h - c + 1, arrayCumSum(card), card)",
			Doc:    "1-based start offset of each run in the flat value stream",
		},
		{
			Name:   "RAGGED_RANGES",
			Params: []string{"card"},
			Body:   "arrayMap((h, c) -> (h - c + 1, c), arrayCumSum(card), card)",
			Doc:    "(start, length) tuples per run, the shape arrayReduceInRanges consumes",
		},
		{
			Name:   "RAGGED_PARENT_IDS",
			Params: []string{"card"},
			Body:   "arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(card), card))",
			Doc:    "instance index of every stream element; CO_GATHER an instance lane through it to broadcast",
		},
		{
			Name:   "RAGGED_IOTA",
			Params: []string{"card"},
			Body:   "arrayMap((e, s) -> e - s + 1, arrayEnumerate(RAGGED_PARENT_IDS(card)), CO_GATHER(RAGGED_STARTS(card), RAGGED_PARENT_IDS(card)))",
			Doc:    "1-based position of every stream element within its own run",
		},
		{
			Name:   "RAGGED_NEST",
			Params: []string{"vals", "card"},
			Body:   "arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c), card, arrayCumSum(card))",
			Doc:    "per-instance lists as Array(Array); boundary operation — it copies the stream, prefer the fused forms (ADR-0162 §SD4)",
		},
		{
			Name:   "RAGGED_REDUCE",
			Params: []string{"agg", "vals", "card"},
			Body:   "arrayReduceInRanges(agg, RAGGED_RANGES(card), vals)",
			Doc:    "per-run aggregate over the flat stream; agg is a constant aggregate-function name, parametrized forms allowed",
		},
		{
			Name:   "RAGGED_EXISTS",
			Params: []string{"f", "vals", "card"},
			Body:   "arrayReduceInRanges('max', RAGGED_RANGES(card), arrayMap(f, vals))",
			Doc:    "per-run existence of a per-element predicate, fused (range-max over the lifted boolean stream)",
		},
		{
			Name:   "RAGGED_COUNT",
			Params: []string{"f", "vals", "card"},
			Body:   "arrayReduceInRanges('sum', RAGGED_RANGES(card), arrayMap(f, vals))",
			Doc:    "per-run count of elements satisfying a per-element predicate, fused",
		},
		{
			Name:   "RAGGED_ELEM",
			Params: []string{"vals", "card", "i", "k"},
			Body:   "arrayElement(vals, arrayElement(arrayCumSum(card), i) - arrayElement(card, i) + k)",
			Doc:    "k-th value of instance i (both 1-based); valid while k does not exceed the instance's cardinality",
		},
		{
			Name:   VersionFunctionName,
			Params: []string{},
			Body:   fmt.Sprintf("%d", Version),
			Doc:    "pack revision marker; SELECT it to detect client/server pack skew",
		},
	}
	return
}

// Statement renders one CREATE OR REPLACE FUNCTION statement.
func Statement(f Function) (sql string) {
	sql = fmt.Sprintf("CREATE OR REPLACE FUNCTION %s AS (%s) -> %s", f.Name, strings.Join(f.Params, ", "), f.Body)
	return
}

// Statements renders the whole pack in installation order.
func Statements() (stmts []string) {
	fns := Functions()
	stmts = make([]string, 0, len(fns))
	for _, f := range fns {
		stmts = append(stmts, Statement(f))
	}
	return
}
