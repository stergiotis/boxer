---
type: reference
audience: end-user
status: draft
title: Shipped vs executed
summary: "Compare machine code shipped against code actually run"
icon: "🔬"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Shipped vs executed

Two lenses on the same modules, side by side: how much machine code each one
put into the binary, and how much of its code this process has actually
run. The gap between them is the interesting part — most third-party code
ships and never executes.

The two columns come from different instruments and answer different
questions, which is why they are shown together rather than combined:

- **shipped** is what the linker kept — a fact about the build, available in
  every binary.
- **executed** is what the ADR-0169 coverage sampler has observed since this
  process started — a fact about *this session*, cumulative and monotone,
  and available only in a binary built with `-cover -covermode=atomic`.

On an uninstrumented build the executed columns are all zero and the
`instrumented` column says so. That is the signal to build the cover lane,
not a defect.

**The ranking is by unexecuted shipped code**, so the top of the list is the
code you are carrying and not using in this session.

```sql
WITH
  shipped AS (
    SELECT module_path, party, sum(text_bytes) AS text_bytes
    FROM keelson('go_symbols')
    GROUP BY module_path, party
  ),
  executed AS (
    SELECT module_path,
           sum(covered_stmts) AS covered_stmts,
           sum(total_stmts)   AS total_stmts
    FROM keelson('coverage_pkgs')
    GROUP BY module_path
  ),
  instrumented AS (SELECT count() > 0 AS yes FROM keelson('coverage_pkgs'))
SELECT
  s.module_path                     AS module,
  s.party                           AS party,
  s.text_bytes                      AS shipped_text_bytes,
  ifNull(e.total_stmts, 0)          AS instrumented_stmts,
  ifNull(e.covered_stmts, 0)        AS executed_stmts,
  round(100 * ifNull(e.covered_stmts, 0) / nullIf(ifNull(e.total_stmts, 0), 0), 1) AS executed_pct,
  (SELECT yes FROM instrumented)    AS instrumented
FROM shipped AS s
LEFT JOIN executed AS e ON e.module_path = s.module_path
ORDER BY
  if(ifNull(e.covered_stmts, 0) = 0, 1, 0) DESC,
  s.text_bytes DESC
```

## Reading it honestly

- **The two lenses have different scopes and will not reconcile.** Coverage
  instruments whatever `-coverpkg` selected — by default the main module
  only, so third-party modules show zero instrumented statements even in an
  instrumented build unless the lane was widened. A module with
  `shipped_text_bytes > 0` and `instrumented_stmts = 0` means "not
  instrumented", not "not executed"; only a module with instrumented
  statements and zero covered ones is evidence of unused code.
- **Coverage is per-session and monotone.** A module reading zero five
  minutes in may simply not have been exercised yet. Drive the feature, then
  look again.
- **Bytes and statements are not comparable units** and are deliberately not
  divided into each other. The columns sit side by side so a reader can see
  the shapes disagree; any single ratio combining them would be invented.
- **A module absent from the executed side entirely** was either not
  instrumented or contributed no instrumented package — `keelson('coverage_status')`
  says which lane is running.
