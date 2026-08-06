---
type: reference
audience: end-user
status: draft
title: Source volume
icon: "📏"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Source volume

How many lines of Go this module's dependency closure actually compiles,
split by who wrote them. This is the *source* lens on the code-volume
question (ADR-0173); `keelson('go_symbols')` answers the same question in
machine-code bytes, and the two disagree sharply — dead-code elimination
removes most of what is never called.

Three numbers deserve attention before the headline ratio:

- **Generated code.** A large share of this repository's own compiled lines
  are machine-written. A first-party total that hides that overstates what
  anyone typed, so the generated split is reported beside it.
- **Non-Go lines.** C, C++, assembly and headers compiled with cgo packages
  are invisible to a Go-only count and are not small.
- **`class`** is the toolchain's own answer, not a heuristic: `internal` is
  this module, `external` is every other module, `stdlib` is the standard
  library.

Reading these columns runs a line-counting pass over the closure the first
time — a couple of seconds. Queries that do not select them do not pay for
it.

```sql
WITH
  p AS (
    SELECT class, code_lines, comment_lines, blank_lines,
           generated_code, other_lang_lines, num_go_files
    FROM keelson('go_packages')
  ),
  n AS (
    SELECT
      (SELECT count() FROM p)                                    AS pkgs,
      (SELECT sum(num_go_files) FROM p)                          AS files,
      (SELECT sum(code_lines) FROM p)                            AS code_all,
      (SELECT sumIf(code_lines, class = 'internal') FROM p)      AS code_first,
      (SELECT sumIf(code_lines, class = 'external') FROM p)      AS code_third,
      (SELECT sumIf(code_lines, class = 'stdlib') FROM p)        AS code_std,
      (SELECT sumIf(generated_code, class = 'internal') FROM p)  AS gen_first,
      (SELECT sumIf(generated_code, class = 'external') FROM p)  AS gen_third,
      (SELECT sum(comment_lines) FROM p)                         AS comments,
      (SELECT sum(blank_lines) FROM p)                           AS blanks,
      (SELECT sum(other_lang_lines) FROM p)                      AS otherlang
  )
SELECT tupleElement(t, 1) AS metric, tupleElement(t, 2) AS value
FROM (
  SELECT arrayJoin([
    ('packages in closure',   toString(pkgs)),
    ('go files',              toString(files)),
    ('code lines total',      toString(code_all)),
    ('  first-party',         toString(code_first)),
    ('  third-party',         toString(code_third)),
    ('  standard library',    toString(code_std)),
    ('third : first ratio',   toString(round(code_third / nullIf(code_first, 0), 2))),
    ('first-party generated', concat(toString(gen_first), '  (',
                                     toString(round(100 * gen_first / nullIf(code_first, 0), 1)), '% of first-party)')),
    ('first-party hand-written', toString(code_first - gen_first)),
    ('third : hand-written ratio', toString(round(code_third / nullIf(code_first - gen_first, 0), 2))),
    ('third-party generated', toString(gen_third)),
    ('comment lines',         toString(comments)),
    ('blank lines',           toString(blanks)),
    ('non-Go lines (cgo)',    toString(otherlang))
  ]) AS t
  FROM n
)
```

## Reading it honestly

- **This counts what the compiler sees for this platform and these build
  tags** — files excluded by a tag, and `_test.go` files, are not here. A
  different `GOOS` or tag set is a different answer.
- **Lines are not effort and not risk.** A dependency's line count says
  nothing about how much of it runs; `keelson('go_symbols')` says what
  shipped and the coverage tables say what executed. The three disagree on
  purpose.
- **A large third-party count is partly data.** Generated lookup tables —
  compression dictionaries, Unicode tables, protobuf descriptors — are code
  lines by this measure and logic by no reasonable one. The
  `third-party generated` row is the first-order correction.
- **The closure is this module's, not this binary's.** Every package any
  target reaches is counted, which is a larger set than any one binary
  links.
