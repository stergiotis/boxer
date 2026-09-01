---
type: reference
audience: end-user
status: draft
title: Coverage overview
summary: "Count covered and total statements for this build"
icon: "🎯"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Coverage overview

What this process has executed so far, as one column of numbers: the build it
is (the meta hash that keys every other coverage question), how many samples
the ADR-0169 sampler has folded, and the covered/total pairs at statement,
unit, function and package grain.

Everything here is **cumulative since process start** — coverage is a
monotone signal, so the numbers only ever rise, and "statement coverage %"
is the honest headline: statements are what a person means by "how much of
the code ran", units and functions are the finer and coarser grains around
it.

On a binary built without `-cover -covermode=atomic` every table this book
reads is empty, and this overview shows a single empty result — that is the
signal to build the cover lane (`scripts/dev/cover-build.sh`), not a defect.

```sql
WITH
  s AS (SELECT * FROM keelson('coverage_status')),
  pk AS (
    SELECT count() AS pkgs, countIf(covered_units > 0) AS touched
    FROM keelson('coverage_pkgs')
  ),
  n AS (
    SELECT
      (SELECT any(substring(meta_hash, 1, 12)) FROM s) AS build,
      (SELECT any(mode) FROM s) AS mode,
      (SELECT toUInt64(sum(samples)) FROM s) AS samples,
      (SELECT toUInt64(sum(covered_stmts)) FROM s) AS cst,
      (SELECT toUInt64(sum(total_stmts)) FROM s) AS tst,
      (SELECT toUInt64(sum(covered_units)) FROM s) AS cu,
      (SELECT toUInt64(sum(total_units)) FROM s) AS tu,
      (SELECT toUInt64(sum(covered_funcs)) FROM s) AS cf,
      (SELECT toUInt64(sum(total_funcs)) FROM s) AS tf,
      (SELECT touched FROM pk) AS touched,
      (SELECT pkgs FROM pk) AS pkgs
  )
SELECT tupleElement(t, 1) AS metric, tupleElement(t, 2) AS value
FROM (
  SELECT arrayJoin([
    ('build', build),
    ('covermode', mode),
    ('samples folded', toString(samples)),
    ('statement coverage %', toString(round(100 * cst / nullIf(tst, 0), 2))),
    ('statements', concat(toString(cst), ' / ', toString(tst))),
    ('units', concat(toString(cu), ' / ', toString(tu))),
    ('functions entered', concat(toString(cf), ' / ', toString(tf))),
    ('packages touched', concat(toString(touched), ' / ', toString(pkgs)))
  ]) AS t
  FROM n
)
```

## Reading it honestly

- **This is run coverage, not test coverage.** The numbers say what this
  interactive session has exercised; the CI test lane measures a different
  population with the same instrument.
- **"Functions entered" counts entry, not completion** — a function whose
  first block ran counts, however much of its body did not. The map and the
  uncovered browser carry the finer story.
- **`samples folded` is liveness**, not information: an idle process folds
  heartbeats whose numbers do not move.
