package chpack_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
)

// TestStatementsGolden pins the exact emitted DDL. The spec is normative
// (ADR-0162 §SD6) and shipped names are append-only in semantics (§SD5), so
// any diff here is either a new function (append below, bump Version) or a
// mistake.
func TestStatementsGolden(t *testing.T) {
	expected := []string{
		"CREATE OR REPLACE FUNCTION CO_LOOKUP AS (keys, lane, k) -> arrayElement(lane, indexOf(keys, k))",
		"CREATE OR REPLACE FUNCTION CO_LOOKUP_NULL AS (keys, lane, k) -> if(indexOf(keys, k) = 0, NULL, arrayElement(lane, indexOf(keys, k)))",
		"CREATE OR REPLACE FUNCTION CO_GATHER AS (lane, sel) -> arrayMap(i -> arrayElement(lane, i), sel)",
		"CREATE OR REPLACE FUNCTION CO_ARG_SORT AS (keys) -> arraySort(i -> arrayElement(keys, i), arrayEnumerate(keys))",
		"CREATE OR REPLACE FUNCTION CO_ARG_MAX AS (lane, keys) -> arrayReduce('argMax', lane, keys)",
		"CREATE OR REPLACE FUNCTION CO_EXISTS_EQ2 AS (a, x, b, y) -> has(a, x) AND has(b, y) AND arrayExists((p, q) -> p = x AND q = y, a, b)",
		"CREATE OR REPLACE FUNCTION RAGGED_STARTS AS (card) -> arrayMap((h, c) -> h - c + 1, arrayCumSum(card), card)",
		"CREATE OR REPLACE FUNCTION RAGGED_RANGES AS (card) -> arrayMap((h, c) -> (h - c + 1, c), arrayCumSum(card), card)",
		"CREATE OR REPLACE FUNCTION RAGGED_PARENT_IDS AS (card) -> arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(card), card))",
		"CREATE OR REPLACE FUNCTION RAGGED_IOTA AS (card) -> arrayMap((e, s) -> e - s + 1, arrayEnumerate(RAGGED_PARENT_IDS(card)), CO_GATHER(RAGGED_STARTS(card), RAGGED_PARENT_IDS(card)))",
		"CREATE OR REPLACE FUNCTION RAGGED_NEST AS (vals, card) -> arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c), card, arrayCumSum(card))",
		"CREATE OR REPLACE FUNCTION RAGGED_REDUCE AS (agg, vals, card) -> arrayReduceInRanges(agg, RAGGED_RANGES(card), vals)",
		"CREATE OR REPLACE FUNCTION RAGGED_EXISTS AS (f, vals, card) -> arrayReduceInRanges('max', RAGGED_RANGES(card), arrayMap(f, vals))",
		"CREATE OR REPLACE FUNCTION RAGGED_COUNT AS (f, vals, card) -> arrayReduceInRanges('sum', RAGGED_RANGES(card), arrayMap(f, vals))",
		"CREATE OR REPLACE FUNCTION RAGGED_ELEM AS (vals, card, i, k) -> arrayElement(vals, arrayElement(arrayCumSum(card), i) - arrayElement(card, i) + k)",
		"CREATE OR REPLACE FUNCTION LEEWAY_PACK_VERSION AS () -> 2",
	}
	require.Equal(t, expected, chpack.Statements())
}

// TestRosterInvariants checks the structural rules of ADR-0162 §SD2/§SD5
// that the golden test alone would not attribute: naming, prefixes,
// lambda-first, dependency order, and the version marker.
func TestRosterInvariants(t *testing.T) {
	fns := chpack.Functions()
	require.NotEmpty(t, fns)

	// Names are UPPER_SNAKE — one style across every leeway UDF family, so a
	// reader never has to remember which pack a function came from. Also what
	// makes splicing them into the collision-check SQL safe.
	nameRe := regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	seen := make(map[string]struct{}, len(fns))
	for _, f := range fns {
		require.Regexp(t, nameRe, f.Name)
		_, dup := seen[f.Name]
		require.Falsef(t, dup, "duplicate name %s", f.Name)
		seen[f.Name] = struct{}{}

		validPrefix := strings.HasPrefix(f.Name, "CO_") ||
			strings.HasPrefix(f.Name, "RAGGED_") ||
			f.Name == chpack.VersionFunctionName
		require.Truef(t, validPrefix, "name %s outside the owned prefixes", f.Name)

		// Lambda-first convention: a lambda parameter is spelled f and
		// leads the parameter list, mirroring arrayExists(f, arr).
		for i, p := range f.Params {
			if p == "f" {
				require.Zerof(t, i, "%s: lambda parameter must come first", f.Name)
			}
		}
		require.NotEmpty(t, f.Doc, f.Name)
	}

	// Dependency order: a body may reference pack names declared earlier
	// only — the server resolves referenced functions at CREATE time.
	for i, f := range fns {
		for j, other := range fns {
			if i == j {
				continue
			}
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(other.Name) + `\b`)
			if re.MatchString(f.Body) {
				require.Lessf(t, j, i, "%s references %s but is installed before it", f.Name, other.Name)
			}
		}
	}

	// The version marker is the last entry, zero-arg, and its body is the
	// Version constant — Functions() derives it, this guards the derivation.
	last := fns[len(fns)-1]
	require.Equal(t, chpack.VersionFunctionName, last.Name)
	require.Empty(t, last.Params)
	require.Equal(t, strconv.Itoa(chpack.Version), last.Body)
}
