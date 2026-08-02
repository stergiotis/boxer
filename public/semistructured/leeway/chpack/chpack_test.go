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
		"CREATE OR REPLACE FUNCTION coLookup AS (keys, lane, k) -> arrayElement(lane, indexOf(keys, k))",
		"CREATE OR REPLACE FUNCTION coLookupNull AS (keys, lane, k) -> if(indexOf(keys, k) = 0, NULL, arrayElement(lane, indexOf(keys, k)))",
		"CREATE OR REPLACE FUNCTION coGather AS (lane, sel) -> arrayMap(i -> arrayElement(lane, i), sel)",
		"CREATE OR REPLACE FUNCTION coArgSort AS (keys) -> arraySort(i -> arrayElement(keys, i), arrayEnumerate(keys))",
		"CREATE OR REPLACE FUNCTION coArgMax AS (lane, keys) -> arrayReduce('argMax', lane, keys)",
		"CREATE OR REPLACE FUNCTION coExistsEq2 AS (a, x, b, y) -> has(a, x) AND has(b, y) AND arrayExists((p, q) -> p = x AND q = y, a, b)",
		"CREATE OR REPLACE FUNCTION raggedStarts AS (card) -> arrayMap((h, c) -> h - c + 1, arrayCumSum(card), card)",
		"CREATE OR REPLACE FUNCTION raggedRanges AS (card) -> arrayMap((h, c) -> (h - c + 1, c), arrayCumSum(card), card)",
		"CREATE OR REPLACE FUNCTION raggedParentIds AS (card) -> arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(card), card))",
		"CREATE OR REPLACE FUNCTION raggedIota AS (card) -> arrayMap((e, s) -> e - s + 1, arrayEnumerate(raggedParentIds(card)), coGather(raggedStarts(card), raggedParentIds(card)))",
		"CREATE OR REPLACE FUNCTION raggedNest AS (vals, card) -> arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c), card, arrayCumSum(card))",
		"CREATE OR REPLACE FUNCTION raggedReduce AS (agg, vals, card) -> arrayReduceInRanges(agg, raggedRanges(card), vals)",
		"CREATE OR REPLACE FUNCTION raggedExists AS (f, vals, card) -> arrayReduceInRanges('max', raggedRanges(card), arrayMap(f, vals))",
		"CREATE OR REPLACE FUNCTION raggedCount AS (f, vals, card) -> arrayReduceInRanges('sum', raggedRanges(card), arrayMap(f, vals))",
		"CREATE OR REPLACE FUNCTION raggedElem AS (vals, card, i, k) -> arrayElement(vals, arrayElement(arrayCumSum(card), i) - arrayElement(card, i) + k)",
		"CREATE OR REPLACE FUNCTION leewayPackVersion AS () -> 1",
	}
	require.Equal(t, expected, chpack.Statements())
}

// TestRosterInvariants checks the structural rules of ADR-0162 §SD2/§SD5
// that the golden test alone would not attribute: naming, prefixes,
// lambda-first, dependency order, and the version marker.
func TestRosterInvariants(t *testing.T) {
	fns := chpack.Functions()
	require.NotEmpty(t, fns)

	// Names are camelCase alphanumerics — also what makes splicing them
	// into the collision-check SQL safe.
	nameRe := regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	seen := make(map[string]struct{}, len(fns))
	for _, f := range fns {
		require.Regexp(t, nameRe, f.Name)
		_, dup := seen[f.Name]
		require.Falsef(t, dup, "duplicate name %s", f.Name)
		seen[f.Name] = struct{}{}

		validPrefix := strings.HasPrefix(f.Name, "co") ||
			strings.HasPrefix(f.Name, "ragged") ||
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
