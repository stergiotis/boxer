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

No runs yet. The workload pin is the sibling trial's
([`../jsonbench-on-facts/upstream/PIN.md`](../jsonbench-on-facts/upstream/PIN.md));
the engine versions are unpinned until M0, which is the first thing any run
must record.
