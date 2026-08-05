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
runs performed against a build and **repeated on later builds**, measuring
the system against stated criteria. A page here describes *how to run the
trial and what to record* — dataset, workload, arms, environment, metrics,
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

- **Results are data, not prose.** A run's numbers land as facts and are
  read back through applets; the protocol page keeps only a dated run log
  (run id, build, one-line outcome). Committing result tables into the page
  is the exception and needs a stated reason.
- **Friction is filed, not patched around.** A trial run that hits a gap in
  the toolbelt records a finding; the protocol does not grow workarounds.
- **Protocols are versioned by ordinary edits.** A change that breaks
  comparability with earlier runs (new dataset tier, changed metric) must
  say so in the run log.
- **Retirement is explicit.** A protocol no longer worth re-running is
  deleted, or moved to
  [`doc/adr-background-work/`](../adr-background-work/) as a snapshot if
  its last numbers still feed a pending decision.
