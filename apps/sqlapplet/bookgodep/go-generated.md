---
type: reference
audience: end-user
status: draft
title: Generated vs hand-written
icon: "🖊"
endpoint: introspection
tabs: [chart, table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Generated vs hand-written

Two bars per directory of this module: the lines a generator wrote, and the
lines someone typed. Around 40% of this repository's own compiled Go is
machine-written, and it is not spread evenly — a few subtrees carry nearly
all of it, and a total that hides that overstates how much code anyone
maintains by hand.

Both lanes are line counts, so they share an axis honestly. That is the
whole reason this is a chart and the shipped-vs-executed comparison next
door is not: bytes and statements on one axis would invent a comparison the
data does not support.

**The knobs.** `depth` is how many path segments make a group — `1` is the
handful of top-level directories, which in a repository whose code all lives
under one of them is a single dominant bar and not much of a picture; `2`
(the default) is where the real structure usually is. `top` is how many
bars, ranked by total compiled lines.

Reading these columns runs a line-counting pass over the closure the first
time — a couple of seconds. It is cached for the life of the process.

```sql
SET param_top = '12';
SET param_depth = '2';

WITH p AS (
  -- import paths relative to the module, so the groups are the repository's
  -- own directories rather than three hostname levels
  SELECT multiIf(import_path = module_path, '.',
                 startsWith(import_path, concat(module_path, '/')),
                 substring(import_path, length(module_path) + 2),
                 import_path) AS rel,
         code_lines, generated_code
  FROM keelson('go_packages')
  WHERE class = 'internal'
)
SELECT arrayStringConcat(arraySlice(splitByChar('/', rel), 1, {depth:UInt8}), '/') AS x,
       sum(generated_code)              AS generated,
       sum(code_lines - generated_code) AS hand_written
FROM p
GROUP BY x
HAVING sum(code_lines) > 0
ORDER BY sum(code_lines) DESC
LIMIT {top:UInt32}
```

## Reading it honestly

- **"Generated" is the conventional marker**, `Code generated … DO NOT EDIT.`
  on its own line. A file that is machine-produced but unmarked counts as
  hand-written, and a hand-edited file carrying the marker counts as
  generated. The marker is a claim, not a proof.
- **Bars are grouped, not stacked** — the chart panel does not stack in v1,
  so the total for a directory is the two bars added, not the taller one.
- **Directory is not ownership.** Grouping by path prefix is a coarse
  arrangement chosen because it fits on an axis; the package table is where
  the real grain is, and `depth` is the dial between them.
- **This is the source lens.** Generated code that ships nothing still
  counts here; `keelson('go_symbols')` is where its absence from the binary
  would show.
