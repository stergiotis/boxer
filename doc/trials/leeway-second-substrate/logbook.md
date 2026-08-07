---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# leeway on a second substrate — logbook

Chronological, append-only record of runs of the
[leeway on a second substrate](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory. Entry
template:

```markdown
## YYYY-MM-DD — <milestone / arm> — <one-line outcome>

- **Build under test:** boxer <commit>, <engine versions, workload pin>
- **Environment:** <CPU model, cores, memory, storage class, OS> — no
  hostnames or personal paths
- **Attempted:** <what this run set out to do>
- **Findings:** one line per proximate obstacle, per the trials README's
  *Finding classification*:
  **[<relation> <competence-slug> / <characteristic> / S#]** <statement>
  (evidence: <file in run dir>) — plus positive-maturity lines for
  competences the run leaned on successfully
- **Solution size:** <artifacts touched: files, lines — when applicable>
- **Results:** <the run's numbers, per README §4 — small tables are the
  expected form for this trial>
- **Run dir:** <./runs/YYYY-MM-DD-slug/ — evidence backing this entry>
```

The workload pin is the sibling trial's
([`../jsonbench-on-facts/upstream/PIN.md`](../jsonbench-on-facts/upstream/PIN.md)).

## 2026-08-07 — M0, acquire and pin — both engines installed; the gate passes for DuckDB and passes *differently* for DataFusion

- **Build under test:** boxer `237ffca5`; ClickHouse client 26.7.3.19,
  DuckDB v1.5.5 (Variegata), datafusion-cli 54.1.0. No corpus loaded — M0
  probes capability only.
- **Environment:** 32-core AMD Ryzen AI MAX+ PRO 395, 93.3 GiB RAM, Fedora
  Linux 44, kernel 7.1.6. **Server timezone `Europe/Zurich`** on both the OS
  and the ClickHouse server — see finding 4.
- **Attempted:** install and pin both target engines, then turn every
  *(c, unverified)* claim in README §3 into evidence, via
  [`probe.sh`](./probe.sh) — one labelled statement per capability, each in its
  own process so a failure records itself rather than aborting the file.
- **Outcome on the gate.** README §6 M0 said: *if DataFusion lacks the lambda
  forms U1–U9 need, descope arm J-df to Q1–Q5 and file the coverage finding.*
  DataFusion does lack them — completely — but the descope is **wrong**, and
  the probe found the reason. See finding 2.

**H-status after M0:**

| | Predicted | Found |
| --- | --- | --- |
| H2 | no general `arrayCumSum` counterpart | **holds** on both. DuckDB has no `list_cumsum`; the slice rewrite works but is quadratic. `list_sort` takes no key lambda, so the pack's key-sorted argsort has no direct form either |
| H3 | absent path yields NULL, not the type default | **holds, on both engines identically** |
| H4 | Q3 timezone-incomparable | **holds, and is live**: DuckDB timestamps are naive while the ClickHouse server runs `Europe/Zurich`, so Q3 differs by the offset unless pinned |
| H5 | the USP thesis is partly about ClickHouse | **refuted for DuckDB** — `json_extract(doc, p)` with `p` a *column* returns a value (evidence: `probe-duckdb.tsv`, `H5_json_extract_runtime`). The limit USP §2a describes is a ClickHouse limit, not a JSON-column limit |

- **Findings:**
  - **[pain leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / functional-suitability.functional-correctness / S2]**
    Absent-path lookups diverge silently: `indexOf`+`arrayElement` returns the
    type default in ClickHouse where `list_position`+`[i]` returns NULL in both
    targets, so a ported query returns a NULL bucket where the sibling trial
    reports an empty-string one, with no error on either side. The
    byte-identical-results check both prior runs passed cannot hold without an
    explicit coalesce (evidence: `probe-duckdb.tsv` and `probe-datafusion.tsv`,
    rows `H3_*`).
  - **[missing leeway-read-access-codegen → proposed:leeway-query-vocabulary-portability / functional-suitability.functional-appropriateness / S3]**
    leeway's lane algebra is written down only in its higher-order form
    (`arrayMap` / `arrayFilter` / `arrayReduce` and their DuckDB counterparts).
    DataFusion 54.1.0 has **433 routines and none of them higher-order** — no
    `transform`, `filter`, `reduce` or lambda under any spelling — so nothing
    in the repo tells a reader how to read a leeway table there. The probe
    shows the algebra nonetheless survives, by explosion: `unnest` plus
    ordinary relational operators express U4/U5/U8 exactly (evidence:
    `probe-datafusion.tsv`, rows `df_no_higher_order`, `df_U*_rewrite`).
    **This is why the pre-registered descope was wrong** — the right move is a
    second rendering of the same queries, not fewer queries.
  - **[pain leeway-read-access-codegen / usability.operability / S4]**
    DataFusion's `array_position` returns an unsigned type and `array_element`
    accepts a signed one, so the composed form — the single idiom the canonical
    mapping reads through — is a planning error until cast: `coercion from
    List(Utf8), UInt64 … failed` (evidence: `probe-datafusion.tsv`,
    `df_element_uncast` vs `df_element_cast`).
  - **[pain — trial process / S3]** The measurement box runs
    `Europe/Zurich`, not UTC, on both the OS and the ClickHouse server. Q3 is
    the query the sibling trial pre-registered as timezone-dependent, and the
    ported engines have no server timezone at all, so a Q3 comparison across
    the two trials is invalid until the offset is pinned or Q3 is dropped
    (evidence: `env.tsv`).
  - **[note — engine, not toolbelt / S4]** DuckDB 1.5.5 deprecates the `->`
    lambda arrow that the ClickHouse query files use, announcing removal in
    the next release. The ported files must use `lambda x: …`, so the two
    engines' query files cannot share lambda syntax even where the function
    names match (evidence: `probe-duckdb.tsv`, `lambda_*`).
  - **[note — engine, not toolbelt / S4]** `datafusion-cli` ships no JSON
    functions and no `CREATE MACRO`. Arms N-text and N-struct therefore have
    no DataFusion counterpart, and the USP head-to-head is DuckDB-only; a
    function pack cannot be installed there in any form.
  - **Positive maturity: none to record.** M0 exercised no boxer competence —
    no leeway code ran, nothing was generated, no corpus was touched. Stating
    that is more useful than crediting the trial for a successful download.
- **Solution size:** [`probe.sh`](./probe.sh), 1 file, ~120 lines, plus two
  evidence TSVs and two environment captures. No repo code changed.
- **Results:** capability only, no domain numbers. `probe-duckdb.tsv` — 39
  probes, 2 err, both of them H2's (`list_cumsum` absent, `list_sort` refuses a
  key lambda). `probe-datafusion.tsv` — 54 probes, 25 err: 24 in the shared
  ClickHouse/DuckDB-idiom set, and 1 in the 15-probe dialect pass, that one
  deliberate (`df_element_uncast`, which exists to show the cast is required).
  The gap between 24 and 1 is the finding: almost every DataFusion "failure" is
  a dialect difference, and the residue that is not — the higher-order
  functions — is what finding 2 is about.
- **Run dir:** [`./runs/2026-08-07-m0/`](./runs/2026-08-07-m0/)
