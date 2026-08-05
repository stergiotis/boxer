---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** This directory and the convention
> described here were introduced 2026-08-05 and have not been reviewed.

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

Consequences of that framing:

- **Every run appends a logbook entry.** Each protocol has a companion
  `<name>.logbook.md`: chronological, append-only. An entry records the
  date, the build under test (repo commit, engine versions), the hardware
  and environment (CPU, memory, storage class — never hostnames or personal
  paths), what was attempted, the findings — gaps, frictions, elegance
  notes — a one-line outcome, and pointers to result data. The logbook is
  what makes two runs comparable and is the finding ledger's carrier until
  findings-as-facts land.
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
