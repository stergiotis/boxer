---
type: reference
audience: contributor
status: draft
vizeval:
  size: 1600x1000
  intent: "See what kinds of records the batch holds, which records break their kind's pattern, and how their values compare."
  sinks: [lens, card, unicode, topo]
  questions:
    - id: kinds
      prompt: "How many distinct kinds of record, by which attributes they carry, does the batch hold?"
      answer: "SELECT uniqExact(kind) FROM base"
    - id: missing-disk
      prompt: "Which host lacks an attribute every other host has?"
      answer: "SELECT name FROM base WHERE kind = 'host' AND NOT has(num_names, 'disk')"
    - id: odd-service
      prompt: "Which service carries an attribute no other service carries?"
      answer: "SELECT name FROM base WHERE kind = 'service' AND has(num_names, 'cpu')"
    - id: busiest-host
      prompt: "Which host has the highest cpu value?"
      answer: "SELECT name FROM base WHERE kind = 'host' ORDER BY num_vals[indexOf(num_names, 'cpu')] DESC LIMIT 1"
    - id: failed-jobs
      prompt: "Which jobs are in state failed?"
      answer: "SELECT name FROM base WHERE kind = 'job' AND sym_vals[indexOf(sym_names, 'state')] = 'failed' ORDER BY name"
      compare: set
  gates:
    text.overlap_pairs: {max: 0}
    text.clipped: {max: 0}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Mixed record kinds

Forty-eight records of four kinds — hosts, services, jobs and alerts — in one
batch, laid out the way a facts table lays them out: two tagged sections by
value type, `num` for numbers and `sym` for labels, and each attribute's name
in its membership. Nothing in the batch names the kind; it shows only in
which memberships a record carries. One host lacks the `disk` measurement all
other hosts have, and one service carries a `cpu` measurement no other service
does. The reader wants the kinds first, then the records that break their
kind's pattern, then the values.

```sql base
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
```

```sql
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
