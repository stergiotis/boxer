---
type: reference
audience: end-user
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-24
title: JSONBench storage by arm
icon: "📐"
tabs: [table]
---

# JSONBench storage by arm

What each arm of the
[jsonbench-on-facts trial](../../../doc/trials/jsonbench-on-facts/README.md)
costs on disk at the 10M tier, holding the same 9,999,994 documents.

As on the latency page, the numbers ride in the page — a committed summary of
`runs/2026-08-06-m4-10m/`, which holds the full evidence — so nothing needs
loading on the server first. Arm descriptions are on that page.

Two results worth reading off this table directly. The facts arms carry
roughly **four times the uncompressed bytes** of the native-JSON arms and
still land smaller than two of them, because the shred's support and
membership lanes compress an order of magnitude harder — the `compression`
column is the point. And `a00`, which declares nothing at all, is the
*smallest* table here: the pinned upstream DDL's `max_dynamic_paths = 0`
forces every non-backbone path into shared data and costs 37 % over letting
the engine discover and type them.

```sql
SET param_ref = 'a00';
WITH s AS (
  SELECT * FROM values(
    'arm String, total_size UInt64, uncompressed UInt64, parts UInt32',
    ('a',   1652215400,  6309069537, 5),
    ('a0',  1814273851,  6785254205, 5),
    ('a00', 1150367898,  4594822359, 5),
    ('b',   1463557309, 24940202647, 8),
    ('c',   1461300356, 24940383073, 2),
    ('d',   1738291789, 25450534022, 2)
  )
),
r AS (SELECT any(total_size) AS ref_size FROM s WHERE arm = {ref:String})
SELECT
  arm,
  formatReadableSize(total_size)                        AS on_disk,
  formatReadableSize(uncompressed)                      AS uncompressed_h,
  round(uncompressed / nullIf(total_size, 0), 1)        AS compression,
  parts,
  round(total_size / nullIf((SELECT ref_size FROM r), 0), 3) AS vs_ref
FROM s
ORDER BY total_size
```
