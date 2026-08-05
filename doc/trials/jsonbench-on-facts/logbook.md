---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# JSONBench-on-facts — logbook

Chronological, append-only record of runs of the
[jsonbench-on-facts](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory. Entry
template:

```markdown
## YYYY-MM-DD — <milestone / arm> — <one-line outcome>

- **Build under test:** boxer <commit>, ClickHouse <version>,
  JSONBench pin <commit>
- **Environment:** <CPU model, cores, memory, storage class, OS> — no
  hostnames or personal paths
- **Attempted:** <what this run set out to do>
- **Findings:** <gaps, frictions, elegance notes — each classified
  competence × ISO 25010 characteristic>
- **Solution size:** <artifacts touched: files, lines — when applicable>
- **Results:** <facts run ids / applet pointer / "none this run">
- **Run dir:** <./runs/YYYY-MM-DD-slug/ — evidence backing this entry>
```

No runs yet.
