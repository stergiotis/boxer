package chpack

// RetiredNames lists every leeway SQL-UDF name this repository has shipped
// and since withdrawn — earlier spellings of the pack, and the read-back
// family's pre-`LW_` names. Install drops them.
//
// The list exists because `CREATE OR REPLACE FUNCTION` cannot remove a
// function that has been renamed: provisioning a new roster leaves the old
// one installed and callable. That is not hypothetical — the jsonbench trial
// found a server still carrying `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX`, retired here
// months earlier, while three current functions were absent, and nothing
// detected the skew (ADR-0171 Context). The 2026-08-06 rename then reproduced
// it deliberately, leaving 16 stale functions to be dropped by hand.
//
// Entries are append-only and never removed: a server may have been
// provisioned at any point in this repository's history, so a name stays on
// the list for as long as any server might still carry it — which is
// forever, since we cannot know. Duplicates across generations are harmless;
// every drop is IF EXISTS.
//
// Deliberately a list rather than the general reconcile. Now that the whole
// vocabulary sits under one namespace, the reconcile a server actually wants
// is "drop every `LW\_%` function the build does not declare" — but the full
// declared set spans chpack, the read-back family and identsql, and no
// package here holds all three. That reconciler is ADR-0171 §SD2, still
// proposed; this list is the part that needs no new decision, and it removes
// the manual drop step for every rename to date.
//
// deferred: replace with the ADR-0171 §SD2 reconciler once it is accepted.
func RetiredNames() (names []string) {
	names = []string{
		// v1 roster, camelCase (retired 2026-08-06).
		"coLookup", "coLookupNull", "coGather", "coArgSort", "coArgMax", "coExistsEq2",
		"raggedStarts", "raggedRanges", "raggedParentIds", "raggedIota", "raggedNest",
		"raggedReduce", "raggedExists", "raggedCount", "raggedElem", "leewayPackVersion",

		// v2 roster, UPPER_SNAKE without the namespace (retired 2026-08-07).
		"CO_LOOKUP", "CO_LOOKUP_NULL", "CO_GATHER", "CO_ARG_SORT", "CO_ARG_MAX", "CO_EXISTS_EQ2",
		"RAGGED_STARTS", "RAGGED_RANGES", "RAGGED_PARENT_IDS", "RAGGED_IOTA", "RAGGED_NEST",
		"RAGGED_REDUCE", "RAGGED_EXISTS", "RAGGED_COUNT", "RAGGED_ELEM", "LEEWAY_PACK_VERSION",

		// Read-back family (ADR-0066) before the namespace (retired 2026-08-07).
		"LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL", "LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL",
		"LEEWAY_LU_VAL_BY_MEMB_IDX", "LEEWAY_LU_ATTR_BY_TAG", "LEEWAY_LU_MEMBS_OF_VAL_IDX",
		"LEEWAY_VALUE_BY_TAG_EQUAL", "LEEWAY_LIST_BY_TAG_EQUAL",

		// Withdrawn outright, not renamed: both became the pack's
		// LW_RAGGED_NEST / LW_RAGGED_PARENT_IDS (ADR-0162 Update 2026-08-02).
		"LEEWAY_UNFLATTEN", "LEEWAY_LU_MEMB_IDX_TO_VAL_IDX",
	}
	return
}
