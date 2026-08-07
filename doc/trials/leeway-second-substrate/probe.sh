#!/usr/bin/env bash
#
# M0 capability probe. Turns the README's *(c, unverified)* claims into
# evidence: one labelled statement per capability the ported queries need,
# each run in its own process so a failure records itself instead of aborting
# the file.
#
# Every probe is phrased so that the *answer* is the finding — a probe that
# errors is as informative as one that returns, which is why the runner records
# the engine's error text rather than treating it as a failure of the run.
#
# Usage:
#   ENGINE=duckdb   BIN=duckdb         OUT=<dir> ./probe.sh
#   ENGINE=datafusion BIN=datafusion-cli OUT=<dir> ./probe.sh
#
# Output: $OUT/probe-$ENGINE.tsv — label, status (ok|err), single-line result.

set -uo pipefail

ENGINE="${ENGINE:?}"
BIN="${BIN:?}"
OUT="${OUT:?}"
mkdir -p "$OUT"

# Value-only output, so the TSV carries answers rather than box drawing.
# DataFusion gets `json` rather than `tsv`: its CSV/TSV writers refuse nested
# types outright ("Nested type List(Utf8) is not supported in CSV"), which
# would report a working list function as a failure.
case "$ENGINE" in
duckdb) FLAGS=(-noheader -list) ;;
datafusion) FLAGS=(--format json -q) ;;
*) FLAGS=() ;;
esac

# label ¦ sql. Grouped by what the port needs them for; the README section each
# group answers is named in the comment.
probes=(
  # --- §3a: does the canonical mapping's one read idiom exist at all? ---
  "list_element¦SELECT (['a','b','c'])[2]"
  "list_position¦SELECT list_position(['a','b','c'], 'b')"
  "H3_position_absent¦SELECT coalesce(CAST(list_position(['a','b'], 'zz') AS VARCHAR), '<NULL>')"
  "H3_index_by_null¦SELECT coalesce((['a','b'])[CAST(NULL AS BIGINT)], '<NULL>')"
  "H3_index_by_zero¦SELECT coalesce((['a','b'])[0], '<EMPTY-OR-NULL>')"

  # --- U1-U9 need the list algebra (§2, the USP set) ---
  "unnest¦SELECT count(*) FROM (SELECT unnest([1,2,3]))"
  "list_distinct¦SELECT list_distinct(['a','b','a'])"
  "list_concat¦SELECT list_concat(['a'], ['b'])"
  "flatten_nary¦SELECT flatten([['a'],['b'],['c']])"
  "list_contains¦SELECT list_contains(['a','b'], 'b')"
  "list_transform¦SELECT list_transform([1,2,3], x -> x * 2)"
  "list_filter¦SELECT list_filter(['ab','ba'], p -> starts_with(p, 'a'))"
  "list_reduce¦SELECT list_reduce([1,2,3], (a, b) -> a + b)"
  "list_bool_or¦SELECT list_bool_or(list_transform([1,2,3], v -> v > 2))"
  "list_sum¦SELECT list_sum([1,2,3])"
  "list_len¦SELECT len(['a','b'])"
  "count_equal¦SELECT len(list_filter(['a','b','a'], v -> v = 'a'))"
  "starts_with¦SELECT starts_with('/commit/record', '/commit/')"
  "list_sort¦SELECT list_sort([3,1,2])"

  # --- H2: the facts read path needs a within-row cumulative sum ---
  "H2_list_cumsum_builtin¦SELECT list_cumsum([1,2,3])"
  "H2_cumsum_by_slice¦SELECT list_transform(range(1, len([1,2,3]) + 1), i -> list_sum(([1,2,3])[1:i]))"
  "H2_arg_sort_by_key¦SELECT list_sort([1,2,3], (a, b) -> a > b)"
  "H2_arg_max¦SELECT (['x','y','z'])[list_position([10,30,20], list_max([10,30,20]))]"

  # --- Q3/Q4/Q5 temporal forms, and H4 ---
  "make_timestamp_us¦SELECT make_timestamp(1700000000000000)"
  "H4_hour¦SELECT hour(make_timestamp(1700000000000000))"
  "H4_timestamp_is_naive¦SELECT CAST(make_timestamp(1700000000000000) AS VARCHAR)"
  "date_diff_plural¦SELECT date_diff('milliseconds', make_timestamp(0), make_timestamp(1000000))"
  "date_diff_singular¦SELECT date_diff('millisecond', make_timestamp(0), make_timestamp(1000000))"
  "count_distinct¦SELECT count(DISTINCT x) FROM (SELECT unnest(['a','b','a']) AS x)"
  "in_list¦SELECT 'b' IN ('a','b')"

  # --- the substrate seam: does the engine read what leeway writes? ---
  "read_parquet_fn¦SELECT count(*) FROM (SELECT 1) WHERE false"

  # --- H5: can this engine's JSON address a path known only at runtime? ---
  "H5_json_extract_const¦SELECT json_extract('{\"a\":{\"b\":1}}', '\$.a.b')"
  "H5_json_extract_runtime¦SELECT json_extract(doc, p) FROM (SELECT '{\"a\":1}' AS doc, '\$.a' AS p)"
  "H5_json_keys¦SELECT json_keys('{\"a\":1,\"b\":2}')"
  "H5_json_structure¦SELECT json_structure('{\"a\":1}')"

  # --- packaging: is the function pack expressible as macros? ---
  "macro_define¦CREATE OR REPLACE MACRO co_lookup(keys, lane, k) AS lane[list_position(keys, k)]; SELECT co_lookup(['a','b'], [10,20], 'b')"

  # --- lambda spelling: 1.5.5 deprecates the arrow form the ClickHouse
  # --- queries share, so the ported files must pick one and say why.
  "lambda_arrow_deprecated¦SELECT list_transform([1,2], x -> x + 1)"
  "lambda_new_syntax¦SELECT list_transform([1,2], lambda x: x + 1)"
  "lambda_new_filter¦SELECT list_filter(['ab','ba'], lambda p: starts_with(p, 'a'))"
)

# Second pass, run only where the first pass reported absences that are really
# dialect differences. Filing "missing" for a function the engine spells
# differently would be a wrong finding, so every absence above is re-asked here
# in the engine's own vocabulary before it is believed.
#
# For DataFusion the substantive question is narrower than "which names exist":
# it has 433 routines and *no* higher-order array function at all — no
# transform, filter or reduce under any spelling (checked against
# information_schema.routines). So the probes below ask whether the lane
# algebra survives without lambdas, by explosion (`unnest`) plus ordinary
# relational operators.
probes_datafusion=(
  "df_no_higher_order¦SELECT count(*) FROM information_schema.routines WHERE routine_name LIKE '%transform%' OR routine_name LIKE '%filter%' OR routine_name LIKE '%reduce%' OR routine_name LIKE '%lambda%'"
  "df_routines_total¦SELECT count(*) FROM information_schema.routines"
  "df_element_uncast¦SELECT array_element(make_array('x','y'), array_position(make_array('/a','/b'), '/b'))"
  "df_element_cast¦SELECT array_element(make_array('x','y'), CAST(array_position(make_array('/a','/b'), '/b') AS BIGINT))"
  "df_H3_absent¦SELECT coalesce(array_element(make_array('x','y'), CAST(array_position(make_array('/a','/b'), '/zz') AS BIGINT)), '<NULL>')"
  "df_array_length¦SELECT array_length(make_array('a','b'))"
  "df_array_has¦SELECT array_has(make_array('a','b'), 'b')"
  "df_array_distinct¦SELECT array_distinct(make_array('a','b','a'))"
  "df_flatten¦SELECT flatten(make_array(make_array('a'), make_array('b')))"
  "df_Q3_hour¦SELECT date_part('hour', to_timestamp_micros(1700000000000000))"
  "df_Q45_span_on_ints¦SELECT (1700000001000000 - 1700000000000000) / 1000"
  "df_U1_unnest_distinct¦SELECT count(*) FROM (SELECT unnest(array_distinct(array_concat(make_array('a','b'), make_array('a')))))"
  "df_U4_rewrite¦SELECT count(*) FROM (SELECT unnest(make_array('/x/1','/y/2')) AS p) WHERE starts_with(p, '/x/')"
  "df_U5_rewrite¦SELECT sum(v) FROM (SELECT unnest(make_array(1,2,3)) AS v)"
  "df_U8_rewrite¦SELECT count(*) FROM (SELECT unnest(make_array(1,9)) AS v) WHERE v > 5"
)

if [[ "$ENGINE" == "datafusion" ]]; then
  probes+=("${probes_datafusion[@]}")
fi

: > "$OUT/probe-$ENGINE.tsv"
printf 'label\tstatus\tresult\n' >> "$OUT/probe-$ENGINE.tsv"

for p in "${probes[@]}"; do
  label="${p%%¦*}"
  sql="${p#*¦}"
  if res=$("$BIN" "${FLAGS[@]}" -c "$sql" 2>&1); then
    status=ok
  else
    status=err
  fi
  # Collapse to one line; the full text of an interesting failure goes in the
  # logbook entry, not here.
  res=$(printf '%s' "$res" | tr '\n\t' ' ' | tr -s ' ' | cut -c1-200)
  printf '%s\t%s\t%s\n' "$label" "$status" "$res" >> "$OUT/probe-$ENGINE.tsv"
done

column -t -s $'\t' "$OUT/probe-$ENGINE.tsv"
