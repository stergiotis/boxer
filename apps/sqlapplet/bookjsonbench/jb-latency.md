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

The 10M-tier result of the
[jsonbench-on-facts trial](../../../doc/trials/jsonbench-on-facts/README.md):
how long each of the five benchmark queries takes on each arm, and what the
facts arms cost against a reference you pick.

The numbers ride in the page. They are a committed summary of one run —
`runs/2026-08-06-m4-10m/`, which holds the full per-try evidence — so this
page answers without a dataset having been loaded on the server first. Cold is
try 1, taken after a page-cache drop; hot is `min(try 2, try 3)`, the
reduction upstream's own site applies. All arms held 9,999,994 documents and
returned byte-identical results.

The arms:

| | |
| --- | --- |
| `a` | the benchmark's entry — five typed JSON paths, clustered on them |
| `a0` | the same, clustered index removed |
| `a00` | plain `JSON`, nothing declared — the only shape a store holding a mixture of document shapes could have |
| `b` | boxer.facts as the live store declares it, `ORDER BY ts` |
| `c` | `b` plus data-skipping indices |
| `d` | `b` plus `MATERIALIZED` columns for the five backbone paths |

Set `ref` to the arm the ratio columns compare against. `a00` is the honest
peer for a general fact store; `a` is the upper bound on what workload-specific
schema knowledge buys, not a like-for-like comparison.

```sql
SET param_ref = 'a00';
WITH m AS (
  SELECT * FROM values(
    'arm String, query String, cold_s Float64, hot_s Float64, hot_mem_b UInt64',
    ('a',   'Q1', 0.013, 0.011,    5254388), ('a',   'Q2', 0.212, 0.192,  271802375),
    ('a',   'Q3', 0.057, 0.043,  209509501), ('a',   'Q4', 0.070, 0.044,   94772988),
    ('a',   'Q5', 0.079, 0.058,  105619094),
    ('a0',  'Q1', 0.015, 0.013,    7522013), ('a0',  'Q2', 0.280, 0.253,  321663925),
    ('a0',  'Q3', 0.057, 0.044,  158700442), ('a0',  'Q4', 0.094, 0.076,  292717962),
    ('a0',  'Q5', 0.094, 0.070,  321052586),
    ('a00', 'Q1', 0.082, 0.049,   77638455), ('a00', 'Q2', 0.449, 0.385,  526590089),
    ('a00', 'Q3', 0.175, 0.159,  213949776), ('a00', 'Q4', 0.210, 0.197,  418649021),
    ('a00', 'Q5', 0.218, 0.192,  427888354),
    ('b',   'Q1', 0.180, 0.142,   36931868), ('b',   'Q2', 2.400, 2.272, 1130292304),
    ('b',   'Q3', 0.354, 0.334,   73697448), ('b',   'Q4', 0.917, 0.832,  525860178),
    ('b',   'Q5', 0.967, 0.837,  553577385),
    ('c',   'Q1', 0.160, 0.120,   53623712), ('c',   'Q2', 2.199, 2.085,  999779790),
    ('c',   'Q3', 0.366, 0.344,  200098013), ('c',   'Q4', 0.912, 0.805,  538508576),
    ('c',   'Q5', 0.900, 0.841,  551705455),
    ('d',   'Q1', 0.029, 0.014,    8621352), ('d',   'Q2', 0.341, 0.265,  342948106),
    ('d',   'Q3', 0.068, 0.048,  169761465), ('d',   'Q4', 0.114, 0.055,  297680544),
    ('d',   'Q5', 0.114, 0.060,  317376496)
  )
),
r AS (SELECT query AS q, hot_s AS ref_s, hot_mem_b AS ref_m FROM m WHERE arm = {ref:String})
SELECT
  query                                      AS query,
  arm                                        AS arm,
  cold_s                                     AS cold_s,
  hot_s                                      AS hot_s,
  round(hot_mem_b / 1048576, 1)              AS hot_mem_mib,
  round(hot_s / nullIf(ref_s, 0), 2)         AS vs_ref_latency,
  round(hot_mem_b / nullIf(ref_m, 0), 2)     AS vs_ref_memory
FROM m INNER JOIN r ON query = q
ORDER BY query, arm
```
