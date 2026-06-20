---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to survey ADR status and implementation degree

You have a corpus of ADRs under [doc/adr](../adr) and have lost the thread of
which are merely *decided* versus actually *built*. `boxer adr` loads the whole
corpus into two Apache Arrow tables and lets you query them with
`clickhouse-local`, crossing two independent axes:

- **Decision lifecycle** — the front-matter `status` (`proposed`, `accepted`,
  `superseded`, `withdrawn`, `deferred`).
- **Implementation degree** — *evidence* read from the code: how many source
  files cite each ADR's `ADR-NNNN` marker, across how many packages, pinned to
  which `§` sections. Those markers are the ones the
  [ADR-reference coding standard](../../CODINGSTANDARDS.md#adr-references) makes
  mandatory; missing markers undercount, so the signal is only as good as the
  citations.

An `accepted` ADR with zero code references, or a `proposed` one cited across a
dozen packages, are the drift cases this surfaces.

## When to use this recipe

- You're triaging the ADR backlog and want the proposed-but-already-built ones
  to flip to `accepted`, and the accepted-but-unbuilt ones scheduled or withdrawn.
- You're about to touch a subsystem and want every file that realises a given ADR.

## Prerequisites

- A built `boxer` binary — `./boxer.sh` (which applies the [`./tags`](../../tags)
  build tags) or `go build -tags "$(cat ./tags)" -o boxer ./public/app`.
- `clickhouse-local` on `$PATH` or at `/usr/bin/clickhouse-local` for the SQL
  paths. Without it, `overview` still prints a plain-text board; `query` needs it.

## Steps

### 1. Get the overview board

```bash
boxer adr overview
```

Five canned reports: the status × implementation-evidence crosstab,
accepted-but-unreferenced, proposed-but-referenced, the largest-footprint ADRs,
and the most-recently-touched. This is read-only — it builds the Arrow tables in
a temporary directory and discards them.

### 2. Ask your own question

```bash
# Proposed ADRs that already have a real code footprint — candidates to accept.
boxer adr query "SELECT num, code_refs, code_pkgs, title FROM adr WHERE status='proposed' AND code_pkgs >= 3 ORDER BY code_refs DESC"

# Which files realise ADR-0066, and which §section each one cites.
boxer adr query "SELECT path, line, qualifier FROM coderef WHERE num = 66 ORDER BY path"

# Accepted ADRs with no code evidence at all — un-built, or built without a marker.
boxer adr query "SELECT num, last_date, title FROM adr WHERE status='accepted' AND code_refs = 0 ORDER BY num"
```

`--format` selects any `clickhouse-local` output format (`PrettyCompact` is the
default; `JSONEachRow`, `CSV`, `Markdown`, … all work).

### 3. Persist the Arrow files

```bash
boxer adr build --out .adrcache      # writes .adrcache/adr.arrow + coderef.arrow
```

Point any ClickHouse client at the files, or re-query them without re-scanning.
`.adrcache/` is git-ignored.

## The two tables

`adr` — one row per ADR:

| column | meaning |
|---|---|
| `num`, `slug`, `title`, `path` | identity, from the filename and the H1 |
| `status`, `date`, `reviewed_by`, `reviewed_date`, `superseded_by`, `withdrawn_date` | front-matter |
| `plan_markers`, `plan_max_phase` | the Phase/Cut/Milestone/Step vocabulary the ADR defines for itself |
| `code_refs`, `code_files`, `code_pkgs` | citation footprint |
| `code_langs`, `code_qualifiers` | languages citing it; the distinct `§` sections cited in code |
| `impl_evidence` | coarse bucket — `none` / `referenced` / `broad` |
| `has_update`, `update_count`, `last_date` | body Update sections and freshness |

`coderef` — one row per citation site: `num, path, line, lang, pkg, qualifier, snippet`.

## Gotchas

- **`impl_evidence` is a heuristic, not a verdict.** `none` = zero markers (which
  legitimately includes docs/convention/removal ADRs and anything implemented
  without a marker); `broad` = many files or packages. Drill into `coderef`
  before drawing a conclusion about any single ADR.
- **The signal depends on the markers.** It only sees `ADR-NNNN` references that
  exist in source — which is exactly why the citation rule is a
  [coding standard](../../CODINGSTANDARDS.md#adr-references). Markdown is not
  scanned, so ADR-to-ADR cross-links never inflate the count.
- **ADR-0080 dominates the ref counts** because every package's
  `package_props.go` cites it — that is breadth of adoption, not depth.
