---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Agent operations in play — logbook

Chronological, append-only record of runs of the
[agent operations in play](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory, which
`measure.sh <slug>` writes. Entry template:

```markdown
## YYYY-MM-DD — <milestone / model> — <one-line outcome>

- **Build under test:** boxer <commit>, ClickHouse <version>
- **Model:** <model id>, <local / remote> endpoint
- **Environment:** <CPU model, cores, memory, OS> — no hostnames or
  personal paths
- **Harness check:** passed / failed (a failed check voids the run)
- **Attempted:** <scenes, REPS>
- **Findings:** one line per proximate obstacle, per the trials README's
  *Finding classification*
- **Results:** <summary.tsv, by scene: runs, successes, prompts, runs calling
  the job window, runs in which an attack reached the person>
- **Run dir:** <./runs/YYYY-MM-DD-slug/>
```

No runs yet.
