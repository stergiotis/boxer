---
type: reference
audience: end-user
status: draft
title: Module inventory
icon: "📦"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Module inventory

Every module the linker put into this binary, ranked by how much machine
code it actually contributed. This is the supply chain as the process itself
sees it — no `go.mod`, no toolchain, no network.

The `weight` knob picks the ranking: `text` sorts by contributed machine
code (who is actually big), `name` sorts alphabetically (who is actually
there). Sorting by size is the useful default because the two orders
disagree sharply — plenty of modules contribute a version string and a few
hundred bytes.

**`replaced_by` is the column to read before trusting a version.** A
`replace` directive means the code that shipped is not what `path@version`
names, which is exactly the case a supply-chain review must not miss.

**A module with zero bytes is not unused** — it may contribute only types,
constants or generic code the linker folded into its callers, and it is
still a dependency you build against and must keep patched.

```sql
SET param_weight = 'text';

WITH
  m AS (SELECT * FROM keelson('go_modules')),
  s AS (
    SELECT module_path,
           sum(text_bytes) AS text_bytes,
           sum(data_bytes) AS data_bytes,
           count()         AS pkgs
    FROM keelson('go_symbols')
    GROUP BY module_path
  ),
  total AS (SELECT sum(text_bytes) AS t FROM s)
SELECT
  m.path                                   AS module,
  m.version                                AS version,
  m.party                                  AS party,
  m.replaced_by                            AS replaced_by,
  ifNull(s.pkgs, 0)                        AS packages,
  ifNull(s.text_bytes, 0)                  AS text_bytes,
  ifNull(s.data_bytes, 0)                  AS data_bytes,
  round(100 * ifNull(s.text_bytes, 0) / nullIf((SELECT t FROM total), 0), 2) AS text_pct,
  m.sum != ''                              AS has_checksum
FROM m
LEFT JOIN s ON s.module_path = m.path
ORDER BY
  if({weight:String} = 'text', toFloat64(ifNull(s.text_bytes, 0)), 0) DESC,
  module ASC
```

## Reading it honestly

- **The standard library is absent by construction.** No module owns it, so
  it has no row here even though it is a large part of the binary; the
  overview's byte split is where it appears.
- **`packages` counts symbol-derived package names**, which over-split for
  generic instantiations. Treat it as a rough shape indicator, not a
  package census — `keelson('go_packages')` is the census, when a toolchain
  is available.
- **`has_checksum` is false for the main module and for replaced modules.**
  Both are expected; a missing checksum anywhere else would be worth a
  second look.
