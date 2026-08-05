---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-05
---

# Trials

Reproducible measurement protocols, in the sea-trials sense: standardized
runs performed against a build and **repeated on later builds**. A trial
measures two things at once: the system's numbers under the workload, and —
usually foremost — the toolbelt's fitness while *recreating* that workload
with native idioms: which gaps surface, which frictions get filed, and how
short the solution turns out to be. Which of the two leads varies per
trial; the protocol must say. A page here describes *how to run the trial
and what to record* — dataset, workload, arms, environment, metrics,
acceptance gates — precisely enough that a later run is comparable with an
earlier one.

What separates this directory from its neighbours:

- [`doc/adr/`](../adr/) holds decisions. A trial is not a decision; its
  numbers may feed one.
- [`doc/adr-background-work/`](../adr-background-work/) holds dated one-off
  analyses that age out once the decision they fed is taken. A trial
  protocol is the opposite: it is **maintained against the tree** and stays
  live for as long as re-running it is worth the effort.
- [`doc/howto/`](../howto/) explains tasks for someone operating the
  system. A trial is executed *on* the system, typically by an agent that
  did not build it, and its product is measurements and findings.

Each trial is a directory — a self-contained dossier:

```
<trial>/
  README.md            the protocol: how to run, what to record
  logbook.md           chronological, append-only record of runs
  runs/<YYYY-MM-DD-slug>/   one per run: the evidence backing its
                       logbook entry — environment capture, pinned
                       configs, EXPLAIN output, summary snapshots
  <vendored material as needed, e.g. upstream/ for pinned workloads>
```

A run directory holds small, textual, provenance-grade evidence. Bulk
results belong in the facts store; datasets are never committed.

Consequences of that framing:

- **Every run appends a logbook entry.** An entry records the
  date, the build under test (repo commit, engine versions), the hardware
  and environment (CPU, memory, storage class — never hostnames or personal
  paths), what was attempted, the findings (classified per *Finding
  classification* below), a one-line outcome, and pointers to result data.
  The logbook is what makes two runs comparable and is the finding ledger's
  carrier until findings-as-facts land.
- **Numbers are data, not prose.** Domain results land as facts and are
  read back through applets; a small result table inside a logbook entry is
  the exception and needs a reason stated in the protocol.
- **The solution is part of the result.** Artifacts the run produces
  (mappings, queries, applets) are committed, so their size is countable
  and their shape judgeable by a later reader.
- **Friction is filed, not patched around.** A trial run that hits a gap in
  the toolbelt records a finding; the protocol does not grow workarounds.
- **Protocols are versioned by ordinary edits.** A change that breaks
  comparability with earlier runs (new dataset tier, changed metric) must
  say so in the logbook.
- **Retirement is explicit.** A protocol no longer worth re-running is
  deleted, or moved to
  [`doc/adr-background-work/`](../adr-background-work/) as a snapshot if
  its last numbers still feed a pending decision.

## Finding classification

A finding is a triple — **competence × relation × quality characteristic**
— plus a severity and its evidence. One finding per proximate obstacle: the
thing that stopped the run, not the cause suspected underneath it.

- **Competence**, by vault slug ([`doc/competences/`](../competences/)).
  A need no competence covers anchors at the **nearest existing ancestor**
  with a proposed slug; findings never mint corpus entries. A proposed slug
  recurring across entries is the editorial signal to author one.
- **Relation** — which vault field the evidence feeds:
  `missing` (nothing covers the need) and `broken` (claimed, but the
  attempt failed) are *maturity* evidence; `pain` (worked, with friction)
  is *pain* evidence. A completed run leaning on a competence is positive
  maturity evidence, worth a line of its own. The vault's 0..5 fields flip
  from not-assessed only editorially — a judgement citing findings — never
  by automation.
- **Quality characteristic** — ISO 25010 (2023 revision), classified
  **evidence-first**: by what the run directory actually holds, not the
  suspected root cause. Measured slowness → performance efficiency; wrong
  output → functional correctness; crash or hang → reliability; had to
  read source to proceed → self-descriptiveness; a workaround sequence →
  operability; valid intent rejected by a grammar → functional
  completeness. Top-level characteristic mandatory, sub-characteristic
  optional.
- **Severity** — S1 blocker / S2 major / S3 minor / S4 note, orthogonal to
  both axes.

Logbook line format, one finding per line, mechanical to migrate to facts:

```markdown
- **[<relation> <competence-slug>[ → proposed:<slug>] /
  <characteristic>[.<sub-characteristic>] / S<1-4>]** <one-sentence
  statement of the obstacle> (evidence: <file under the run dir>)
```
