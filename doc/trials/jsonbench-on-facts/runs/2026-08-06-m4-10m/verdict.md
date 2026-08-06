---
type: explanation
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Verdict

Written after the 10M run and four rounds of review, each of which moved the
headline. Numbers in [`results.md`](./results.md); the reasoning behind the
corrections is in the [logbook](../../logbook.md).

**The facts data model is not expensive. The way you read it is — and every
expensive thing this trial measured turned out to be a capability that already
existed in the repository and that the trial failed to find.**

## The side-product question: what does the data model cost?

At 10M, with the five backbone paths materialised, facts is at parity with or
faster than ClickHouse's native JSON type:

| Reference | What it declares | Facts + materialized backbone |
| --- | --- | --- |
| A00 — nothing | — | **0.28–0.69× latency** (faster), ~parity memory, 1.51× storage |
| A0 — five typed paths | schema | 0.72–1.09× latency, 0.99–1.15× memory, **0.958× storage** |
| A — the benchmark's entry | schema + a clustered index on it | 1.03–1.38× latency |

Storage is a saving against every reference but A00, which is itself the
smallest table of any arm — the pinned upstream DDL's `max_dynamic_paths = 0`
costs 37 % over letting the engine discover and type its own paths.

With **both** levers spent — the backbone materialised *and* the table
re-keyed on it (arm E) — facts runs the benchmark at **0.59–1.18× of the
reference's own entry**, faster on Q4 and Q5, at 0.954× its storage. The two
are independent and additive: materialisation is worth 3.8–13.8× and costs
18.8 % storage; re-keying is worth a further 1.07–1.76× and *saves* 9.3 %.

Two things did not work. **`ORDER BY ts` prunes nothing** — §4's standing
hypothesis, confirmed at both tiers, every query reading every granule. And
**data-skipping indices recover nothing**: 2 granules of 2394, the same
verdict at 1M and at 10M, because a bloom filter over a section's value lane
answers "does this granule contain the value anywhere" and at 8192 rows per
granule the answer is almost always yes.

## The primary question: how did the toolbelt carry the workload?

Badly, in a specific and correctable way. Arm B's number moved **four times**
under review, and not once because the model was slow:

| What was wrong | What it cost |
| --- | --- |
| Open-coded lane arithmetic instead of the query vocabulary | ~3× |
| Path resolved per row instead of a `MATERIALIZED` column | 7× time, 8× memory |
| Compared against a reference carrying schema knowledge facts cannot have | 1.02–1.73× latency, 9.8 % storage |
| Hand-rolled ragged read instead of `LEEWAY_LIST_BY_TAG_EQUAL` | up to 2.4× |

Every one of those was already in the repository. `chpack` shipped and was not
installed on the trial server. `LEEWAY_LIST_BY_TAG_EQUAL` did precisely what
the trial wrote by hand, and did it more correctly. The materialised-column
pattern was already in use in a sibling experiment.

**So the verdict on the toolbelt is not "slow" — it is "undiscoverable from
where a task-level consumer stands."** Nothing on the path this trial walked
(the leeway skills, the `mapping` package, the generated DML and RA packages)
points at the read vocabulary. That is the finding the trial was built to
produce, and it took four wrong headlines to surface it.

## What carried the workload without complaint

`leeway-dml-codegen` took an ingestion shape it was not designed for —
arbitrary shredded JSON, 121.2M attributes across five sections — at
30,252 docs/s in 384 MB of RSS, with no generated-code changes and no escape
hatch. The benchmark table came straight from the live store's own DDL
composer, so arm B is provably the shape the store declares rather than an
approximation. Every arm returned byte-identical results at every tier.

Solution size: ~700 lines of Go and ~350 of harness. **Manual interventions:
one** — the redundant `has()` conjunct arm C needs, which is itself filed as
the reason arm C cannot work.

## What this verdict does not cover

- **The benchmark may be the wrong instrument.** Its own queries do not run
  against unhinted JSON — `Dynamic` columns are refused by `GROUP BY` and by
  `IN`, and Q3 cannot execute without a cast. It measures a homogeneous
  corpus, which is not what a fact store holds.
- **100M was never run.** The gate passes (~77 GiB, ~2 h, ingest-bound).
- **The 1M tier is worthless for this question** — every arm-A query finishes
  in tens of milliseconds. 10M is the first tier whose numbers mean anything,
  and its facts arms are not comparable with the 1M run's at all.
- **One machine, shared, and cold runs only from the second run on.**
- **The operator is a confounder.** Four of the trial's own errors dominated
  its measurements, and each was found only because a reviewer pushed back. A
  different operator would have produced different numbers from the same code,
  which is worth remembering when reading any single figure above.
