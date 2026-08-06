---
type: reference
audience: end-user
status: draft
title: The data-centricity tax
icon: "⚖️"
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The data-centricity tax

What holding a corpus in the `boxer.facts` data model costs against holding it
in ClickHouse's native `JSON` type — the number the
[jsonbench-on-facts trial](../../../doc/trials/jsonbench-on-facts/README.md)
exists to produce, computed from the trial's own results-as-facts rather than
retyped from prose.

Ratios are hot runtimes, `min(try 2, try 3)`, facts-arm over reference-arm.
Below 1 means the facts arm is faster.

Pick the reference with `ref`:

- **`a00`** — a plain `JSON` column declaring nothing. The only shape a store
  holding a mixture of document shapes could have, and the honest comparison
  for a general fact store. The benchmark's own queries do not even run
  against it without casts.
- **`a0`** — five typed paths declared, no clustered index.
- **`a`** — the benchmark's entry: five typed paths *and* a clustered index on
  them. Reported as the upper bound on what workload-specific schema knowledge
  buys, not as a like-for-like peer.

And the facts arm with `arm`: `b` is the live store's own declaration, `d`
adds `MATERIALIZED` columns for the five backbone paths.

If this page errors with `UNKNOWN_TABLE`, the results have not been loaded on
this server. They are not committed — the run directories under
`doc/trials/jsonbench-on-facts/runs/` hold the numbers as the provenance
record, and the facts table is the queryable copy. To build it:

    jsonbench chpack                                    # the ADR-0162 UDF pack
    jsonbench ddl     --database jsonbench_results --apply
    jsonbench results --database jsonbench_results \
                      --run-dir doc/trials/jsonbench-on-facts/runs/<run>

```sql
SET param_tier = '2026-08-06-m4-10m';
SET param_ref  = 'a00';
SET param_arm  = 'd';
WITH
  `tv:symbol:value:val:s:m:0:24:0::data`                                     AS symV,
  `tv:symbol:lr:lr:u64:2q:0:0:0::data`                                       AS symT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`) AS symI,
  coGather(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
           raggedStarts(`tv:i64Array:len:len:u64:28o:0:0:0::data`))          AS intV,
  `tv:i64Array:lr:lr:u64:2q:0:0:0::data`                                     AS intT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:i64Array:lrcard:lrcard:u64:4gw:0:0:0::data`) AS intI,
  coGather(`tv:f64Array:value:val:f64h:gM:0:0:0::data`,
           raggedStarts(`tv:f64Array:len:len:u64:28o:0:0:0::data`))          AS fV,
  `tv:f64Array:lr:lr:u64:2q:0:0:0::data`                                     AS fT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:f64Array:lrcard:lrcard:u64:4gw:0:0:0::data`) AS fI,
  t AS (
    SELECT
      LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081862, symI) AS arm,
      LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081863, symI) AS query,
      LEEWAY_VALUE_BY_TAG_EQUAL(intV, intT, 6917529027641081864, intI) AS try,
      LEEWAY_VALUE_BY_TAG_EQUAL(fV,  fT,  6917529027641081865, fI)     AS secs,
      LEEWAY_VALUE_BY_TAG_EQUAL(intV, intT, 6917529027641081866, intI) AS mem
    FROM jsonbench_results.facts
    WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081859, symI) = 'jsonbenchTiming'
      AND LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081861, symI) = {tier:String}
  ),
  hot AS (
    SELECT arm, query, min(secs) AS s, min(mem) AS m
    FROM t WHERE try > 1 GROUP BY arm, query
  )
SELECT
  r.query                                  AS query,
  round(r.s, 3)                            AS ref_s,
  round(f.s, 3)                            AS facts_s,
  round(f.s / nullIf(r.s, 0), 2)           AS latency_x,
  round(f.m / nullIf(r.m, 0), 2)           AS memory_x
FROM (SELECT * FROM hot WHERE arm = {ref:String}) AS r
INNER JOIN (SELECT * FROM hot WHERE arm = {arm:String}) AS f USING (query)
ORDER BY query
```
