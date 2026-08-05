---
type: reference
audience: end-user
status: draft
title: JSONBench overview
icon: "📐"
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# JSONBench overview

One row per arm: how much disk it takes and how it compares with the two
reference variants, for a chosen run of the
[jsonbench-on-facts trial](../../../doc/trials/jsonbench-on-facts/README.md).

`a00` is the ratio that matters for a general fact store — a plain `JSON`
column declaring nothing, which is the only shape a table holding a mixture of
document shapes could have. `a` is the benchmark's own entry, which declares
five typed paths and clusters on them; it is reported as the upper bound on
what workload-specific schema knowledge buys, not as a like-for-like peer.

If a ratio column is empty, that run has no such reference arm — the 1M run
predates both.

```sql
SET param_tier = '2026-08-06-m4-10m';
WITH
  `tv:symbol:value:val:s:m:0:24:0::data`                                     AS symV,
  `tv:symbol:lr:lr:u64:2q:0:0:0::data`                                       AS symT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`) AS symI,
  coGather(`tv:i64Array:value:val:i64h:g:0:0:0::data`,
           raggedStarts(`tv:i64Array:len:len:u64:28o:0:0:0::data`))          AS intV,
  `tv:i64Array:lr:lr:u64:2q:0:0:0::data`                                     AS intT,
  LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:i64Array:lrcard:lrcard:u64:4gw:0:0:0::data`) AS intI,
  m AS (
    SELECT
      LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081862, symI) AS arm,
      LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081867, symI) AS metric,
      LEEWAY_VALUE_BY_TAG_EQUAL(intV, intT, 6917529027641081868, intI) AS value
    FROM facts
    WHERE LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081860, symI) = 'jsonbenchSize'
      AND LEEWAY_VALUE_BY_TAG_EQUAL(symV, symT, 6917529027641081861, symI) = {tier:String}
  ),
  w AS (
    SELECT arm,
           anyIf(value, metric = 'count')        AS rows,
           anyIf(value, metric = 'total_size')   AS total_size,
           anyIf(value, metric = 'uncompressed') AS uncompressed,
           anyIf(value, metric = 'parts')        AS parts
    FROM m GROUP BY arm
  ),
  ref AS (
    SELECT anyIf(total_size, arm = 'a')   AS a,
           anyIf(total_size, arm = 'a00') AS a00
    FROM w
  )
SELECT
  arm,
  rows,
  formatReadableSize(total_size)                                    AS on_disk,
  round(uncompressed / nullIf(total_size, 0), 1)                    AS compression,
  parts,
  round(total_size / nullIf((SELECT a FROM ref), 0), 3)             AS vs_a,
  round(total_size / nullIf((SELECT a00 FROM ref), 0), 3)           AS vs_a00
FROM w
ORDER BY arm
```
