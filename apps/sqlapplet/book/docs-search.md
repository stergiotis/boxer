---
type: reference
audience: end-user
status: draft
title: Documentation search
summary: "Search help, decisions and SQL passes at section grain"
icon: "🔎"
endpoint: introspection
tabs: [table, detail]
topics: [about]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Documentation search

One search over three documentation corpora at section grain (ADR-0164):
the registered help books (`keelson('helpsections')`), this repository's
decision corpus (`keelson('adrsections')`), and the executing engine's own
`system.documentation` — help sections, ADR §s and ClickHouse functions in
one ranked result. Runs against the in-process introspection endpoint; the
`chdoc` rows therefore describe the bundled engine's ClickHouse version,
which is the version your queries here actually run on.

The `docsearch('…')` argument is a pattern battery, compiled exactly as the
Help center's search box compiles it: whitespace-separated case-insensitive
RE2 patterns, all of which must hit a section for it to qualify; a token
that is not a valid regex matches literally, and one naming a ClickHouse
function alias or a launcher keyword also matches its canonical spelling
(`lcase` finds `lower`). Scoring mirrors the GUI tiers (title 8, heading 4,
body 1 per pattern). The macro needs its query as a plain quoted string —
it expands before parameters bind — so there is no params-strip widget
here: edit the string inside `docsearch('…')` in the buffer and Run again.

```md preamble
Every pattern must hit. `ref` is the canonical reference — `help://…`,
`adr://…`, `chdoc://…` — and the **Detail** tab shows the full row of a
selected hit, `context` included.
```

```sql
SELECT source, ref, title, heading, score, context
FROM docsearch('deduplicate argMax')
ORDER BY score DESC
LIMIT 50
```
