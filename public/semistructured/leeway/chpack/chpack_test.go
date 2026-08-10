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
		"CREATE OR REPLACE FUNCTION LW_CO_LOOKUP AS (keys, lane, k) -> arrayElement(lane, indexOf(keys, k))",
		"CREATE OR REPLACE FUNCTION LW_CO_LOOKUP_NULL AS (keys, lane, k) -> if(indexOf(keys, k) = 0, NULL, arrayElement(lane, indexOf(keys, k)))",
		"CREATE OR REPLACE FUNCTION LW_CO_GATHER AS (lane, sel) -> arrayMap(i -> arrayElement(lane, i), sel)",
		"CREATE OR REPLACE FUNCTION LW_CO_ARG_SORT AS (keys) -> arraySort(i -> arrayElement(keys, i), arrayEnumerate(keys))",
		"CREATE OR REPLACE FUNCTION LW_CO_ARG_MAX AS (lane, keys) -> arrayReduce('argMax', lane, keys)",
		"CREATE OR REPLACE FUNCTION LW_CO_EXISTS_EQ2 AS (a, x, b, y) -> has(a, x) AND has(b, y) AND arrayExists((p, q) -> p = x AND q = y, a, b)",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_STARTS AS (card) -> arrayMap((h, c) -> h - c + 1, arrayCumSum(card), card)",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_RANGES AS (card) -> arrayMap((h, c) -> (h - c + 1, c), arrayCumSum(card), card)",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_PARENT_IDS AS (card) -> arrayFlatten(arrayMap((i, c) -> arrayWithConstant(c, i), arrayEnumerate(card), card))",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_IOTA AS (card) -> arrayMap((e, s) -> e - s + 1, arrayEnumerate(LW_RAGGED_PARENT_IDS(card)), LW_CO_GATHER(LW_RAGGED_STARTS(card), LW_RAGGED_PARENT_IDS(card)))",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_NEST AS (vals, card) -> arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c), card, arrayCumSum(card))",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_REDUCE AS (agg, vals, card) -> arrayReduceInRanges(agg, LW_RAGGED_RANGES(card), vals)",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_EXISTS AS (f, vals, card) -> arrayReduceInRanges('max', LW_RAGGED_RANGES(card), arrayMap(f, vals))",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_COUNT AS (f, vals, card) -> arrayReduceInRanges('sum', LW_RAGGED_RANGES(card), arrayMap(f, vals))",
		"CREATE OR REPLACE FUNCTION LW_RAGGED_ELEM AS (vals, card, i, k) -> arrayElement(vals, arrayElement(arrayCumSum(card), i) - arrayElement(card, i) + k)",
	}
	expected = append(expected,
		"CREATE OR REPLACE FUNCTION LW_ASPECT_SEG_ENC AS (name) -> if(length(splitByChar(':', name)) = 11, arrayElement(splitByChar(':', name), 6), if(length(splitByChar(':', name)) = 7, arrayElement(splitByChar(':', name), 4), ''))",
		"CREATE OR REPLACE FUNCTION LW_ASPECT_SEG_USE AS (name) -> if(length(splitByChar(':', name)) = 11, arrayElement(splitByChar(':', name), 7), if(length(splitByChar(':', name)) = 7, '', ''))",
		"CREATE OR REPLACE FUNCTION LW_ASPECT_SEG_SEM AS (name) -> if(length(splitByChar(':', name)) = 11, arrayElement(splitByChar(':', name), 8), if(length(splitByChar(':', name)) = 7, arrayElement(splitByChar(':', name), 5), ''))",
		"CREATE OR REPLACE FUNCTION LW_ASPECT_DECODE AS (seg) -> if(seg = '' OR seg = '0', CAST([], 'Array(UInt8)'), arrayMap(c -> toUInt8(position('0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz', c) - 2 + 0 * throwIf(position('0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz', c) < 2 OR c = 'z', 'LW_ASPECT_DECODE: char outside the v0 aspect range')), splitByString('', seg)))",
	)
	// The six transform-bearing statements carry the full enum tables; the
	// expectations are assembled here from the same enums with independent
	// string assembly, so the shape is pinned while the tables stay owned by
	// the vocabulary packages (their canonical tests pin those).
	expected = append(expected, expectedAspectTransformStatements()...)
	expected = append(expected, "CREATE OR REPLACE FUNCTION LW_PACK_VERSION AS () -> 4")
	require.Equal(t, expected, chpack.Statements())
}

// TestRetiredNamesDisjointFromRoster guards the one way RetiredNames can do
// damage: naming something the build still installs, which dropRetired would
// then delete straight after Install put it there. The guard in dropRetired
// makes that survivable at runtime; this makes it a build failure.
func TestRetiredNamesDisjointFromRoster(t *testing.T) {
	current := make(map[string]struct{}, len(chpack.Functions()))
	for _, f := range chpack.Functions() {
		current[f.Name] = struct{}{}
	}
	for _, name := range chpack.RetiredNames() {
		_, live := current[name]
		require.Falsef(t, live, "%s is both retired and in the current roster", name)
	}
}

// TestRetiredNamesCoverPriorRosters pins that every name this repository has
// shipped and moved away from is on the drop list. A rename that forgets to
// retire its old spellings leaves them installed and callable on every server
// provisioned before it — the drift ADR-0171 was written about, and which the
// 2026-08-06 rename reproduced.
func TestRetiredNamesCoverPriorRosters(t *testing.T) {
	retired := make(map[string]struct{}, len(chpack.RetiredNames()))
	for _, n := range chpack.RetiredNames() {
		retired[n] = struct{}{}
	}
	for _, n := range []string{
		// v1, camelCase.
		"coLookup", "raggedParentIds", "leewayPackVersion",
		// v2, UPPER_SNAKE without the namespace.
		"CO_LOOKUP", "RAGGED_PARENT_IDS", "LEEWAY_PACK_VERSION",
		// The read-back family's pre-namespace spellings.
		"LEEWAY_VALUE_BY_TAG_EQUAL", "LEEWAY_LIST_BY_TAG_EQUAL", "LEEWAY_LU_ATTR_BY_TAG",
		// Withdrawn outright.
		"LEEWAY_UNFLATTEN", "LEEWAY_LU_MEMB_IDX_TO_VAL_IDX",
	} {
		_, ok := retired[n]
		require.Truef(t, ok, "%s was shipped and withdrawn but is not on the drop list", n)
	}
}

// TestRosterInvariants checks the structural rules of ADR-0162 §SD2/§SD5
// that the golden test alone would not attribute: naming, prefixes,
// lambda-first, dependency order, and the version marker.
func TestRosterInvariants(t *testing.T) {
	fns := chpack.Functions()
	require.NotEmpty(t, fns)

	// Names are UPPER_SNAKE under one namespace — `LW_`, shared with the
	// read-back family and identsql's LW_ID_*, so a reader never has to
	// remember which package a function came from and a server can be asked
	// for the whole vocabulary with one LIKE. Also what makes splicing them
	// into the collision-check SQL safe.
	nameRe := regexp.MustCompile(`^LW_[A-Z][A-Z0-9_]*$`)
	seen := make(map[string]struct{}, len(fns))
	for _, f := range fns {
		require.Regexp(t, nameRe, f.Name)
		_, dup := seen[f.Name]
		require.Falsef(t, dup, "duplicate name %s", f.Name)
		seen[f.Name] = struct{}{}

		// Within the namespace, a family segment names the axis of the
		// algebra the function belongs to.
		validPrefix := strings.HasPrefix(f.Name, "LW_CO_") ||
			strings.HasPrefix(f.Name, "LW_RAGGED_") ||
			strings.HasPrefix(f.Name, "LW_ASPECT_") ||
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
