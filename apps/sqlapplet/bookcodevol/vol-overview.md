---
type: reference
audience: end-user
status: draft
title: Code volume overview
icon: "⚖"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Code volume overview

How much of this running process is its own code, and how much is somebody
else's — read out of the binary itself (ADR-0173). Two facts the linker
recorded and one derived split: the modules that went in, and the machine
code that survived dead-code elimination, attributed to the module that
owns it.

**Supply-chain surface and code surface are different quantities.** The
module count answers "how many third parties do I depend on"; the byte
split answers "how much of me is theirs". They disagree by a lot, and both
are worth knowing — a module contributing a handful of bytes still has to
be reviewed, patched and trusted.

**Bytes here are machine code only.** `text` is instructions; the data
symbols are reported separately and deliberately not folded in, because a
single zero-filled buffer in the standard library's FIPS module is tens of
megabytes and would swamp every real package.

```sql
WITH
  m AS (SELECT * FROM keelson('go_modules')),
  s AS (SELECT * FROM keelson('go_symbols')),
  n AS (
    SELECT
      (SELECT count() FROM m)                                AS mods,
      (SELECT countIf(party = 'third') FROM m)                AS third_mods,
      (SELECT countIf(replaced_by != '') FROM m)              AS replaced,
      (SELECT anyOrNull(module_attribution) FROM s)           AS attribution,
      (SELECT count() FROM s)                                 AS pkgs,
      (SELECT sum(text_bytes) FROM s)                         AS text_all,
      (SELECT sumIf(text_bytes, party = 'first') FROM s)      AS text_first,
      (SELECT sumIf(text_bytes, party = 'third') FROM s)      AS text_third,
      (SELECT sumIf(text_bytes, party = 'stdlib') FROM s)     AS text_std,
      (SELECT sum(data_bytes) FROM s)                         AS data_all
  )
SELECT tupleElement(t, 1) AS metric, tupleElement(t, 2) AS value
FROM (
  SELECT arrayJoin([
    ('modules linked',        toString(mods)),
    ('third-party modules',   toString(third_mods)),
    ('modules replaced',      toString(replaced)),
    ('module attribution',    ifNull(attribution, 'no symbol table')),
    ('packages with symbols', toString(pkgs)),
    ('text bytes total',      toString(text_all)),
    ('  first-party',         concat(toString(text_first), '  (',
                                     toString(round(100 * text_first / nullIf(text_all, 0), 1)), '%)')),
    ('  third-party',         concat(toString(text_third), '  (',
                                     toString(round(100 * text_third / nullIf(text_all, 0), 1)), '%)')),
    ('  standard library',    concat(toString(text_std), '  (',
                                     toString(round(100 * text_std / nullIf(text_all, 0), 1)), '%)')),
    ('third : first ratio',   toString(round(text_third / nullIf(text_first, 0), 2))),
    ('data bytes total',      toString(data_all))
  ]) AS t
  FROM n
)
```

## Reading it honestly

- **This is what shipped, not what was written.** Dead-code elimination
  already removed what no entry point reaches, so a dependency contributing
  a thousand source lines may contribute a hundred bytes here. The source
  side of the question lives on `keelson('go_packages')`, which needs the Go
  toolchain and the source tree — this table needs neither.
- **`module attribution` says how much to trust the split.** `exact` means
  every package was resolved by longest-prefix match against the module list
  this same binary declares. Package *names* are always approximate (they
  are derived from symbol names), but the party split does not depend on
  that precision.
- **An empty result means a stripped binary**, or a platform whose
  executables are not ELF. The module count still answers; the boot log
  carries the reason.
- **A `go test` binary reports one module.** The toolchain omits the
  dependency list from test binaries, so these numbers are only meaningful
  in a `go build` binary.
