---
type: reference
audience: contributor
status: draft
vizeval:
  size: 1600x1000
  intent: "Read long part names and long attribute names in full, and find the part with the highest supply voltage."
  sinks: [card, unicode, json]
  questions:
    - id: highest-voltage
      prompt: "Which part has the highest nominal supply voltage?"
      answer: "SELECT part FROM base ORDER BY volts DESC LIMIT 1"
    - id: part-4-name
      prompt: "What is the full name of part 4?"
      answer: "SELECT part FROM base WHERE n = 4"
  gates:
    text.overlap_pairs: {max: 0}
    text.clipped: {max: 0}
    text.elided: {max: 0}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Long labels

Six parts whose names run to eighty characters or more, each with a tagged
section `spec` whose membership names are long too. The data is small; the
difficulty is width. A rendering that fits by cutting the names off has lost
the thing a reader needs to tell two parts apart, which is why elision is gated
here and not in the host scenario.

```sql base
SELECT
  number AS n,
  concat('Industrial replacement assembly ', toString(number),
         ' — long-lead item, see supplier catalogue revision ',
         toString(cityHash64(number, 7) % 90 + 10)) AS part,
  round(12 + (cityHash64(number, 8) % 2300) / 10, 1) AS volts,
  round(-40 + (cityHash64(number, 9) % 1650) / 10, 1) AS temp,
  round((cityHash64(number, 10) % 5000) / 100, 2) AS mass
FROM numbers(6)
```

```sql
SELECT
  LW_PLAIN(toUInt64(n), 'id', 'u64', 'item:id'),
  LW_PLAIN(part, 'natural-key', 's', 'item:id'),
  LW_TV([volts, temp, mass], 'spec', 'value', 'f64'),
  LW_TV_MEMB(['nominal-supply-voltage-volts', 'maximum-operating-temperature-celsius', 'shipping-mass-kilograms'], 'spec', 'low-card-verbatim'),
  LW_TV_SUPPORT([toUInt64(1), 1, 1], 'spec', 'lvcard')
FROM base
ORDER BY n
```
