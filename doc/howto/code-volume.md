---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to find out how much of boxer is third-party code

The answer depends on which lens you ask through, and they disagree by a
lot. [ADR-0173](../adr/0173-code-volume-self-inspection.md) has the design;
this page is the operating recipe.

## 1. The applets

Apps menu → *Code* → **Code volume overview**, or launch one directly:

```sh
./hmi.sh --launch "subject_alias = 'vol-overview'"
```

| applet | answers |
| --- | --- |
| `vol-overview` | the headline split: modules linked, machine-code bytes by party, the third-to-first ratio |
| `vol-modules` | every module ranked by contributed machine code; `replaced_by` and checksums |
| `vol-map` | the same split as a treemap — first-party / third-party / stdlib, broken into modules |
| `vol-lenses` | shipped bytes beside executed statements, per module |
| `go-volume` | the *source* lens: compiled lines by party, with the generated split (godep book) |

## 2. Which lens needs what

Each table answers only what its inputs allow, and each degrades to empty
rather than failing:

| table | needs | cost |
| --- | --- | --- |
| `go_modules` | nothing | microseconds |
| `go_symbols` | an unstripped ELF binary | ~30 ms |
| `go_packages` volume columns | Go toolchain + source tree + module cache | ~2 s, on first use |
| `coverage_pkgs` | a `-cover -covermode=atomic` build | live |

The first two read the running binary, so they work on a deploy target
where the toolchain-backed ones cannot run at all.

## 3. Ad-hoc SQL

In the SQL Playground, switch Endpoint → *Keelson introspection*:

```sql
-- what shipped, by party
SELECT party, sum(text_bytes) AS bytes, count() AS pkgs
FROM keelson('go_symbols') GROUP BY party ORDER BY bytes DESC;

-- the ten biggest dependencies in the binary
SELECT module_path, sum(text_bytes) AS bytes
FROM keelson('go_symbols') WHERE party = 'third'
GROUP BY module_path ORDER BY bytes DESC LIMIT 10;

-- source lines by class (needs the toolchain; first call takes ~2 s)
SELECT class, sum(code_lines), sum(generated_code)
FROM keelson('go_packages') GROUP BY class;

-- dependencies you build against but that shipped no code
SELECT path, version FROM keelson('go_modules')
WHERE NOT is_main
  AND path NOT IN (SELECT module_path FROM keelson('go_symbols'));
```

## 4. When a table is empty

Empty is a fact about the build, not a query failure — check which one
applies before assuming a defect:

- **`go_symbols` empty** — the binary is stripped (`-ldflags=-s`), or the
  platform is not ELF. The boot log carries the reason. Note that `go test`
  links the binary it runs *without* a symbol table, so this table is always
  empty under the default test lane; `go test -c` and `go build` binaries
  carry it.
- **`go_packages` all zeroes in the volume columns** — no toolchain, no
  source tree, or off-repo. `keelson('go_collection')` carries the reason in
  its `error` column.
- **`coverage_pkgs` empty** — the build is not instrumented. See
  [continuous coverage](continuous-coverage.md).
- **`go_modules` with one row** — a `go test` binary. The toolchain omits
  the dependency list from test binaries; a `go build` binary carries it.

## 5. Reading the numbers honestly

- **`text_bytes` is machine code after dead-code elimination**; source lines
  are what the compiler saw. A dependency large in source may be small in
  the binary, which is why the two lenses disagree rather than one being
  wrong.
- **Never sum `text_bytes` and `data_bytes`.** One zero-filled buffer in the
  standard library's FIPS module is tens of megabytes and swamps every real
  package.
- **Module attribution is exact, package attribution is not.** `pkg_path` is
  derived from symbol names and over-splits generic code; `module_path`
  resolves against the module list the binary itself declares, which is the
  grain the first-vs-third question is asked on.
- **Coverage and symbols have different scopes.** Coverage instruments
  whatever `-coverpkg` selected — by default the main module only, so a
  third-party module reading zero executed statements usually means "not
  instrumented", not "not executed".
- **Every table here describes one Go binary.** The Rust render client is a
  separate executable that no `go_*` table sees, and its answer is very
  different — a few percent first-party against the Go binary's ~30%. Nor do
  these tables separate what somebody wrote from what a generator emitted,
  beyond the `generated` flag. Both are §SD8–§SD10 of
  [ADR-0173](../adr/0173-code-volume-self-inspection.md) and are not built
  yet; until they are, read every number on this page as "of the Go binary".
