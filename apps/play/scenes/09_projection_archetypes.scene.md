---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_PROJECTION: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# projection archetypes

Projection over forty-eight records of four kinds — hosts, services, jobs and
alerts, each kind a set of attributes in a `num` and a `sym` section — at the
panel's defaults (structure features, min cluster 5), which find the four
kinds, the eight alerts among them (ADR-0289 §SD5). The archetypes
view then reads each cluster as what it typically holds and the rows that
break it: the host without a disk, the service with a cpu, the one failed job
(ADR-0289 §SD4).

```sql
WITH base AS (
SELECT n, kind, name, num_names, num_vals, sym_names, sym_vals FROM (
SELECT
  number AS n,
  multiIf(number < 16, 'host', number < 28, 'service', number < 40, 'job', 'alert') AS kind,
  multiIf(kind = 'host', concat('host-', leftPad(toString(number), 2, '0')),
          kind = 'service', concat('svc-', ['api','auth','billing','cart','search','mail','queue','cache','feed','media','geo','pay'][number - 15]),
          kind = 'job', concat('job-', toString(1000 + number)),
          concat('alert-', toString(number - 39))) AS name,
  multiIf(
    kind = 'host' AND number = 5, ['cpu', 'mem', 'rx', 'tx'],
    kind = 'host', ['cpu', 'mem', 'disk', 'rx', 'tx'],
    kind = 'service' AND number = 21, ['replicas', 'p99ms', 'cpu'],
    kind = 'service', ['replicas', 'p99ms'],
    kind = 'job', ['attempt', 'runtime_s'],
    ['value']) AS num_names,
  arrayMap(i -> round(multiIf(
      num_names[i] IN ('cpu', 'mem', 'disk'), (cityHash64(number, i, 1) % 1000) / 10,
      num_names[i] IN ('rx', 'tx'), (cityHash64(number, i, 2) % 90000) / 10,
      num_names[i] = 'replicas', 1 + cityHash64(number, 3) % 8,
      num_names[i] = 'p99ms', 20 + (cityHash64(number, 4) % 4000) / 10,
      num_names[i] = 'attempt', 1 + cityHash64(number, 5) % 4,
      num_names[i] = 'runtime_s', (cityHash64(number, 6) % 36000) / 10,
      (cityHash64(number, 7) % 1000) / 10), 1), arrayEnumerate(num_names)) AS num_vals,
  multiIf(
    kind = 'host', ['os', 'rack'],
    kind = 'service', ['version', 'owner', 'state'],
    kind = 'job', ['queue', 'state'],
    ['severity', 'rule', 'target']) AS sym_names,
  multiIf(
    kind = 'host', [['linux', 'linux', 'linux', 'bsd'][1 + cityHash64(number, 8) % 4], concat('r', toString(1 + cityHash64(number, 9) % 4))],
    kind = 'service', [concat('v1.', toString(cityHash64(number, 10) % 5)), ['core', 'growth', 'infra'][1 + cityHash64(number, 11) % 3], ['up', 'up', 'up', 'degraded'][1 + cityHash64(number, 12) % 4]],
    kind = 'job', [['batch', 'etl', 'ml'][1 + cityHash64(number, 13) % 3], ['done', 'done', 'running', 'failed'][1 + cityHash64(number, 14) % 4]],
    [['warn', 'crit'][1 + cityHash64(number, 15) % 2], ['cpu-high', 'disk-full', 'latency'][1 + cityHash64(number, 16) % 3], concat('host-', leftPad(toString(cityHash64(number, 17) % 16), 2, '0'))]) AS sym_vals
FROM numbers(48))
)
SELECT
  LW_PLAIN(toUInt64(n), 'id', 'u64', 'item:id'),
  LW_PLAIN(name, 'natural-key', 's', 'item:id'),
  LW_TV(num_vals, 'num', 'value', 'f64'),
  LW_TV_MEMB(num_names, 'num', 'low-card-verbatim'),
  LW_TV_SUPPORT(arrayMap(x -> toUInt64(1), num_names), 'num', 'lvcard'),
  LW_TV(sym_vals, 'sym', 'value', 's'),
  LW_TV_MEMB(sym_names, 'sym', 'low-card-verbatim'),
  LW_TV_SUPPORT(arrayMap(x -> toUInt64(1), sym_names), 'sym', 'lvcard')
FROM base
ORDER BY n
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"click","name":"Compute projection","role":"button","settleMs":600}
{"do":"read","valueContains":"cluster(s)","role":"label","pattern":"(?P<clusters>\\d+) cluster\\(s\\), (?P<noise>\\d+) noise"}
{"do":"expect","of":"clusters","eq":4}
{"do":"expect","of":"noise","eq":0}
{"do":"capture","text":"09_projection_archetypes_graph","settleMs":3000}
{"do":"click","name":"archetypes","role":"button","settleMs":800}
{"do":"capture","text":"09_projection_archetypes","settleMs":800}
{"do":"click","name":"row","role":"button","settleMs":800}
{"do":"capture","text":"09_projection_archetypes_row","settleMs":800}
```
