---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified end to end; do not cite as
> authoritative.

# How to watch live code coverage of a running boxer process

ADR-0169's continuous-coverage lane samples which Go code a *running*
process has executed and serves it as live `keelson.coverage_*` tables plus
three canned applets. This page is the operating recipe; the design and the
measured overhead live in
[ADR-0169](../adr/0169-continuous-coverage-keelson.md).

## 1. Build instrumented (the one prerequisite)

Runtime counter snapshots exist only in binaries built with
`-cover -covermode=atomic` — `set`/`count` modes refuse them, and an
uninstrumented binary just logs one line and leaves the tables empty.

For the desktop GUI (the host `hmi.sh` runs):

```sh
cd rust/imzero2
CGO_ENABLED=0 go build -tags "$(cat ../../tags | tr -d '\n'),binary_log" \
  -cover -covermode=atomic -o main_go ../../public/thestack/cmd/imzero2/
HMI_BUILD=0 ./hmi.sh            # HMI_BUILD=0, or hmi.sh rebuilds uninstrumented
```

`./build_go.sh` restores the plain binary afterwards. For the CLI hosts and
size/overhead numbers, `scripts/dev/cover-build.sh` builds baseline and
instrumented pairs side by side (`BOXER_COVERLANE_DIR` for the output,
`BOXER_COVERLANE_COVERPKG` to narrow — see §5).

Expectations, measured on one dev box: binary ≈ +5–6 %; full-tree
instrumentation ≈ 3× Go frame time (fps p50 60 → 27 under a continuously
animating app). Usable for a diagnosis session, not a daily driver.

## 2. Nothing to start — drive the app

On an instrumented build the carousel starts the sampler itself
(`covscrape: coverage sampler started` in the log): every
`IMZERO2_COVERAGE_INTERVAL` (default `5s`; `0` disables) it snapshots the
counters and folds them into the cumulative covered set. Coverage is
monotone and process-scoped — it grows as you exercise features and resets
at the next launch. There is no durable history yet (the ADR-0169 §SD6
facts tee is deferred), so what you see is this run.

## 3. The applets

Apps menu → *Observability*, or launch one directly:

```sh
./hmi.sh --launch "subject_alias = 'cov-map'"        # also: cov-overview, cov-uncovered
```

- **Coverage overview** — the cumulative totals: statement coverage %,
  covered/total at unit, function and package grain, samples folded.
- **Coverage map** — the package tree as a treemap, area = statements,
  colour = coverage bracket. `size_by = 'uncovered'` re-sizes by *untested*
  statements: the biggest rectangle is the biggest gap.
- **Uncovered functions** — the work list. Paste a package path from the
  map into `pkg` (ILIKE pattern); `show` picks `uncovered` / `partial` /
  `all`.

The loop that works: launch instrumented → drive the feature you care
about → watch the map light up → zoom with the browser.

## 4. Ad-hoc SQL and external tools

In the SQL Playground, switch Endpoint → *Keelson introspection*, then:

```sql
SELECT * FROM keelson('coverage_status');
SELECT pkg_path, covered_stmts, total_stmts,
       round(100 * covered_stmts / nullIf(total_stmts, 0), 1) AS pct
FROM keelson('coverage_pkgs') ORDER BY total_stmts - covered_stmts DESC LIMIT 20;
SELECT func, src_file, total_stmts FROM keelson('coverage_funcs')
WHERE pkg_path ILIKE '%play%' AND covered_units = 0 ORDER BY total_stmts DESC;
```

From outside the process, the boot log's
`introspecthost: table source listening addr=…` names the loopback
endpoint; scripts can `curl -X POST --data-binary "<sql> FORMAT
TabSeparated" http://<addr>/query`, and an external
clickhouse-local/-server joins via `url('<addr>/table/coverage_pkgs',
'ArrowStream')`.

## 5. Narrowing, when 3× is too much

Instrumentation costs strictly per instrumented package — uninstrumented
packages run at exactly baseline speed. To watch one subsystem smoothly,
build with `-coverpkg` (the lane script's `BOXER_COVERLANE_COVERPKG`):

```sh
BOXER_COVERLANE_COVERPKG='./apps/play/...,./public/keelson/...' scripts/dev/cover-build.sh
```

Totals then refer to the narrowed set only.

## 6. The older file-dump paths

Still there and orthogonal: `--coverageTrapDir <dir>` dumps meta+counter
files on SIGUSR1, and `GOCOVERDIR=<dir>` makes any instrumented run emit
covdata files at exit for `go tool covdata` / `scripts/dev/coveragehtml.sh`.
