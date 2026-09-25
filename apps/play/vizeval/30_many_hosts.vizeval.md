---
type: reference
audience: contributor
status: draft
vizeval:
  size: 1600x1000
  intent: "Find the busiest of twenty-four hosts by cpu load."
  sinks: [card, unicode, json, chart]
  questions:
    - id: busiest-cpu
      prompt: "Which host has the highest cpu load?"
      answer: "SELECT host FROM base ORDER BY cpu DESC LIMIT 1"
    - id: disk-over-80
      prompt: "Which hosts have a disk load above 80?"
      answer: "SELECT host FROM base WHERE disk > 80 ORDER BY host"
      compare: set
    - id: host-03-mem
      prompt: "What is the memory load of host-03?"
      answer: "SELECT mem FROM base WHERE host = 'host-03'"
      compare: approx
      tol: 0.05
  gates:
    text.overlap_pairs: {max: 0}
    text.clipped: {max: 0}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Many hosts

Twenty-four hosts in the shape of the host-metrics scenario. More rows than a
screen of card rows holds, and more than the box-drawn and JSON sinks' row caps:
the scenario measures what a rendering does when the data does not fit, and
records the sinks whose cap it exceeds as inadmissible rather than scoring a
picture of part of the data.

```sql base
SELECT
  number AS n,
  concat('host-', leftPad(toString(number), 2, '0')) AS host,
  round((cityHash64(number, 1) % 1000) / 10, 1) AS cpu,
  round((cityHash64(number, 2) % 1000) / 10, 1) AS mem,
  round((cityHash64(number, 3) % 1000) / 10, 1) AS disk
FROM numbers(24)
```

```sql
SELECT
  LW_PLAIN(toUInt64(n), 'id', 'u64', 'item:id'),
  LW_PLAIN(host, 'natural-key', 's', 'item:id'),
  LW_TV([cpu, mem, disk], 'load', 'value', 'f64'),
  LW_TV_MEMB(['cpu', 'mem', 'disk'], 'load', 'low-card-verbatim'),
  LW_TV_SUPPORT([toUInt64(1), 1, 1], 'load', 'lvcard')
FROM base
ORDER BY n
```
