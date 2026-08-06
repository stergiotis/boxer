---
type: reference
audience: end-user
status: draft
title: JSONBench latency by arm
icon: "⏱️"
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# JSONBench latency by arm

Cold and hot runtimes for the five benchmark queries across every arm of the
[jsonbench-on-facts trial](../../../doc/trials/jsonbench-on-facts/README.md),
read out of the trial's own results-as-facts table. Cold is try 1 (measured
after a page-cache drop); hot is `min(try 2, try 3)`, the reduction upstream's
own site applies.

Set `tier` to `2026-08-06-m4-10m` for the 10M run or `2026-08-05-m0-m3-1m` for
the 1M smoke run. The 1M facts arms used an earlier, slower query form and are
not comparable with the 10M ones — see the trial logbook.

The arms: `a` is the benchmark's own entry (five typed JSON paths, clustered
on them); `a0` drops the index; `a00` drops the type hints as well and is the
only shape a store holding a mixture of document shapes could have; `b` is
boxer.facts as the live store declares it; `c` adds data-skipping indices; `d`
materializes the five backbone paths.

Reading this table needs the trial's membership ids, which are `LowCardRef`
uint64s from the vcs-managed registry — `jsonbench vocab` prints them.

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
WITH
  `tv:symbol:value:val:s:m:0:24:0::data`                                     AS symV,
  `tv:symbol:lr:lr:u64:2q:0:0:0::data`                                       AS symT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`) AS symI,
  -- Array-valued sections flatten their values across attributes, so the
  -- value lane needs re-aligning against `len` before it co-indexes with the
  -- membership lanes (ADR-0162 raggedStarts + coGather).
  coGather(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
           raggedStarts(`tv:i64Array:len:len:u64:28o:0:0:0::data`))          AS intV,
  `tv:i64Array:lr:lr:u64:2q:0:0:0::data`                                     AS intT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:i64Array:lrcard:lrcard:u64:4gw:0:0:0::data`) AS intI,
  coGather(`tv:f64Array:value:val:f64h:gM:0:0:0::data`,
           raggedStarts(`tv:f64Array:len:len:u64:28o:0:0:0::data`))          AS fV,
  `tv:f64Array:lr:lr:u64:2q:0:0:0::data`                                     AS fT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:f64Array:lrcard:lrcard:u64:4gw:0:0:0::data`) AS fI,
  rows AS (
    SELECT
      LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081862, symI) AS arm,
      LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081863, symI) AS query,
      LEEWAY_VALUE_BY_TAG_EQUAL(intV, intT, 6917529027641081864, intI) AS try,
      LEEWAY_VALUE_BY_TAG_EQUAL(fV,  fT,  6917529027641081865, fI)     AS secs,
      LEEWAY_VALUE_BY_TAG_EQUAL(intV, intT, 6917529027641081866, intI) AS mem
    FROM jsonbench_results.facts
    WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081859, symI) = 'jsonbenchTiming'
      AND LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081861, symI) = {tier:String}
  )
SELECT
  query,
  arm,
  round(anyIf(secs, try = 1), 3)                     AS cold_s,
  round(minIf(secs, try > 1), 3)                     AS hot_s,
  round(minIf(mem, try > 1) / 1048576, 1)            AS hot_mem_mib
FROM rows
GROUP BY query, arm
ORDER BY query, arm
```
