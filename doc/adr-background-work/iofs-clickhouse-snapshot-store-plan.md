---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> An implementation plan for [ADR-0198](../adr/0198-fs-snapshot-store.md),
> written for an implementing agent. The ADR is the decision; the
> [compact page](./iofs-clickhouse-snapshot-store-compact.md) is the design;
> the [full note](./iofs-clickhouse-snapshot-store.md) is the reasoning. Where
> this plan and the ADR disagree, the ADR wins; where both are silent, stop
> and ask (see *Stop points*).

# Implementation plan for ADR-0198 — the fs snapshot store (August 2026)

## 0. Read first, in this order

1. [AGENTS.md](../../AGENTS.md) — build tags, `go mod tidy --diff`, the
   integration lane, commit-by-path, design-before-code, privacy.
2. [ADR-0198](../adr/0198-fs-snapshot-store.md) — the decisions (SD1–SD11)
   and the Surfaces table.
3. [The compact page](./iofs-clickhouse-snapshot-store-compact.md) — the
   logical schema, engine clauses, macros, write protocol, adapter queries,
   operations, rclone topology.
4. The skills `leeway-beginner` and `leeway-components`, then
   [facts-bound record stores](../explanation/facts-bound-record-stores.md)
   and [the leeway SQL read surface](../explanation/leeway-sql-read-surface.md).
5. The worked examples to copy from: the facts-bound store
   [`sysmfacts`](../../public/keelson/runtime/sysmfacts/doc.go) (DTO files, a
   vocabulary beside it, a gen-test, an id-landing test), the own-table store
   [`persiststore`](../../public/keelson/runtime/persist/persiststore/schema.go),
   the generic generator [`recordstore/gen`](../../public/storage/recordstore/gen/gen.go)
   (`Input.DDL`, `SharedRA`, `ExternallyProvisioned`), and the executors under
   [`recordstore`](../../public/storage/recordstore/recordstore.go) (`ExecutorI`:
   `Exec`, `QueryArrow`, `InsertArrow`).

## 1. Rules that apply to every milestone

- Every `go build` / `test` / `vet` carries `-tags="$(cat ./tags)"`. Check
  module drift with `go mod tidy --diff`, never `tidy` + `git diff`.
- Tests that need a live ClickHouse or the `rclone` binary go in the
  integration lane (`//go:build integration`, run by
  `scripts/ci/gotest-integration.sh`); the default lane stays hermetic.
- Hash with `lukechampine.com/blake3` only — `crypto/sha256` is banned (CS009).
  Ids: the store **never** claims a tag, mints an id, reads an id's body or
  assumes a tag width (ADR-0198 SD3). The *vocabulary* claims a membership tag
  like every vocabulary — that is a different thing and is required.
- Keep every commit buildable, small, single-concern, scoped by explicit path
  (`git add <paths>`, `git commit -- <paths>`; never `git add -A`), with the
  house trailer. Commit only when the user asks, unless told otherwise.
- No personal paths, sibling repo names or data-volume counts in committed
  files; grep the diff before committing.
- Docs you touch must pass `go run -tags="$(cat ./tags)" ./public/app gov doclint <path>`.
- Lint: `scripts/ci/lint.sh` green before declaring a milestone done.
- `recordstore/gen` refuses the facts `TableDesc` under a foreign table name:
  copy it and set `DictionaryEntry.Name` to the store's table (M0, G1). The
  `Database` must match `[a-z][a-z0-9]*` (M0, G2).
- **Stop points** (ask the user, do not guess): adding a dependency
  (`pkg/sftp` is expected, anything else is not); any change to the
  `boxer.facts` `TableDesc`; any new capability subject or env var; package
  and macro names if the proposals below collide with existing conventions;
  any SD11 decision that M0's evidence does not settle.

Names below were proposals under `public/keelson/runtime/`. They were settled
during M1: the store does not belong under keelson — its only tie there is the
row shape it borrows (`factsschema`) and the executor it writes through — so
it lives at **`public/fs/lading/`**, beside the tree watcher, as `ladingvocab`,
`ladingschema`, `ladingmeta`, `ladingdata`, `ladingpolicy` and the `lading`
package itself, with `ladingingest`, `ladingadapter` and `ladingsftp` to come.
`lading` because a bill of lading is issued once for one voyage, lists exactly
what was loaded, and is never amended — only superseded by the next one. The
memberships carry the same prefix. Read `fsvocab` / `fsstore` / `fsingest` /
`fsadapter` / `fssftp` below as the older spelling; the *tables* keep their
`fs` names (`boxer.fsmeta`, `boxer.fsdata`, `boxer.fssnap`), because they say
what they hold rather than who owns them.

## 2. Milestones

Each milestone ends with: the acceptance criteria met, the verification
commands green, a short note in the ADR's `## Updates` (dated) of what shipped
and what was corrected, and — if the user asked for commits — one or a few
commits by explicit path.

### M0 — verify and decide (no production code) — **done 2026-08-19**

Ran as [the M0 trial](../trials/fs-snapshot-store-m0/); the decisions and the
corrections are ADR-0198's `## Updates` entry for that date. Read both before
starting M1 — four of the eleven checks corrected something below.

**Goal.** Replace the note's assumptions with measured facts on a live
ClickHouse, and settle ADR-0198 SD11, before any generated code exists.

**Deliverables.** A trial under `doc/trials/fs-snapshot-store-m0/` (README +
logbook, per [doc/trials/](../trials/README.md) conventions), throwaway SQL
and Go under the trial or the integration lane, and a dated `## Updates`
entry on ADR-0198 recording the decisions.

**Checks (the full note §14, restated as pass/fail).**

1. One block = one compressed block: a facts-shaped table with
   `index_granularity = 1` and a 1 MiB `blobArray` value per row — confirm
   via `system.parts_columns` marks/compressed-block counts; repeat for 256 KiB.
2. `ttl_only_drop_parts = 1`: whether regular merges still remove expired
   rows, and the drop latency after a partition expires.
3. `now()` in a `ROW POLICY` condition; `PARTITION BY toYYYYMMDD(<DateTime64
   plain>)` with `TTL` on the same plain.
4. `MATERIALIZED` columns over the facts plains added by `ALTER` after
   `EnsureTable`: hidden from `SELECT *`; a dependent `MATERIALIZED` (`dir`
   uses `name`); `VerifySchema` of the generated store still passes; a bloom
   skip index on the materialised `dir` is used (`EXPLAIN indexes = 1`).
5. `recordstore/gen` with the facts `TableDesc`, `SharedRA` bound to
   `factsschema/ra`, `ExternallyProvisioned = false`, `Input.DDL` clauses —
   generates, `EnsureTable` creates the table with our clauses, `Scan` with
   `ExtraPredicate` works, and many rows per `(id, ts)` (one per path) decode
   correctly. If the generator refuses the combination, record what it needs.
6. The materialised view into `fssnap` under one batched insert carrying many
   root rows.
7. `FULL OUTER JOIN` default-filling behind the diff idiom.
8. **Insert throughput** of a facts-shaped `fsdata` against a bespoke one at
   1 MiB blocks, and of a facts-shaped `fsmeta` at many small mounts per
   batch — the number that decides the `fsdata` shape.
9. Compression ratio on one representative mount at `ZSTD(1)`/`ZSTD(3)` and
   256 KiB/1 MiB blocks (the `system.parts_columns` query of the compact page §9).
10. `lukechampine.com/blake3`: whether subtree chaining values at block
    offsets are reachable (or Bao with 1 MiB chunk groups); `BLAKE3()`
    throughput in ClickHouse for an audit.
11. rclone contracts (can be deferred to M5/M6 if no rclone is at hand): the
    `sftp` backend's `ssh = "<cmd>"` appends `-s sftp`; `rclone serve sftp
    --stdio` exists and honours filters; `rclone serve s3` lists without
    delimiter over an SFTP remote.

**Decisions to record (SD11).** The block ordinal encoding; `fsdata`
facts-shaped or bespoke; which hot attributes to materialise; the macro
spellings; whether the fleet profile's `by_path` projection is default.

**Acceptance.** Every check has a pass/fail line with the server version and
date; every SD11 item has a decision or an explicit "still open" with what
evidence would settle it.

### M1 — vocabulary, generated stores, provisioning — **done 2026-08-19**

Shipped as `public/fs/lading{,/ladingvocab,/ladingschema,/ladingmeta,/ladingdata,/ladingpolicy}`;
what changed against the sketch below is ADR-0198's `## Updates` entry for
that date. Three things to carry into M2: the packages and memberships are
spelled `lading*` and live under `public/fs/`, not under keelson; the root row
is two components (`ladingEntry` + `ladingSnapshot`) on one row; and
`recordstore/gen`'s emitted `VerifySchema` now skips `MATERIALIZED` and
`ALIAS` columns, which is the decision M0 left open.

**Deliverables.**

- `ladingvocab`: the membership registry — `ladingMode`, `ladingSize`,
  `ladingMtime`, `ladingLinkTarget`, `ladingContentHash`, `ladingContent`,
  `ladingBlockSize`, `ladingBlocks`, `ladingText`, `ladingNodeKind`,
  `ladingErr`, `ladingSnapEntries`, `ladingSnapBytes`, `ladingTtlClass`,
  `ladingTextRule`, `ladingInlineMax`, `ladingData`, `ladingBlockHash`,
  `ladingLine0`, the kind markers `ladingKindEntry` / `ladingKindSnapshot` /
  `ladingKindBlock` / `ladingKindMount`, the policy kind's memberships
  (`ladingMountName`, `ladingMountStore`, `ladingMountTtlClass`,
  `ladingMountTextRule`, `ladingMountInlineMax`) — claimed and registered
  exactly as `sysmvocab` does, with the committed `(ordinal, name, id)` table
  and the id-landing test.
- `ladingmeta` / `ladingdata` / `ladingpolicy`: DTO files for the entry kind, the block kind and the
  policy kind (`lw:` tags; plains `id`/`naturalKey`/`ts`/`expiresAt`; every
  attribute scalar or `unit` — except `ladingText`, which is
  `lw:"ladingText,bool"`:
  the `bool` section has no `unit` cardinality and the `,unit` spelling emits
  code that does not compile, M0); gen-tests generating (a) the entry store over
  `boxer.fsmeta` and (b) the block store over `boxer.fsdata` through
  `recordstore/gen` with the facts `TableDesc`, `SharedRA`, `Input.DDL` from
  the compact page §3 and the M0 decisions; (c) the policy kind as a
  facts-bound store (`storegen.Input`) or, if the generator allows, as a third
  kind in the same package — whichever the generator accepts; registry-stable
  ids (`FixedIdsWrapper` over the registry snapshot) so sections may be shared.
- A provisioning function: `EnsureTable` for `fsmeta`/`fsdata`/`fssnap`, the
  `ALTER … ADD COLUMN IF NOT EXISTS` / `ADD CONSTRAINT` / `ADD INDEX` set, the
  `fssnap` materialised view — idempotent; and `VerifySchema` at start.

**Acceptance.** `go generate`/gen-tests are reproducible (re-running changes
nothing); `scripts/dev/generate.sh` runs clean; the repo-wide id-disjointness
check passes; on a live server (integration lane) provisioning is idempotent
and an insert of one entry row through
`Begin(id, ts, Envelope{NaturalKey, ExpiresAt}).Add<Kind>(row).Commit()`
round-trips through `Scan` — **not** through the generated `Ingest<Kind>`,
which refuses two rows sharing a key and drops the envelope (M0, G3).
`VerifySchema` after the `ALTER`s is the open decision of M0's finding G4,
not an acceptance criterion this milestone can meet as things stand.

### M2 — the ingest library (the walker) — **done 2026-08-20**

Shipped as `public/fs/lading/ladingingest`; the corrections are ADR-0198's
`## Updates` entry for that date. Two to carry into M3 and M4: a per-block
hash is a **standalone** BLAKE3 digest (a subtree chaining value would fail
the SQL audit the column exists for), and `text = true` on an entry row is a
guarantee that no line straddles a block boundary — a file with an over-long
line is stored `text = false`. The rclone-stdio source moved to M6 with the
rest of the `pkg/sftp` work.

**Deliverables.** `ladingingest`: `Snapshot(ctx, fsys fs.FS, mountID
identifier.TaggedId, policy Policy, stores…) (Result, error)` — fixes `snap`
and `expiresAt`; `fs.WalkDir`; an entry row per node via `Begin`/`Add<Kind>`/`Commit`
(errors into `fsErr`, walk continues); blocks for files under the inline threshold via the
block store (`content` mode, `block_size`, text rule with newline cuts and
`line0`; BLAKE3 file hash streamed in the same pass; per-block `hash` where
the profile has it); `Flush` through `ExecutorI.InsertArrow`; the root row
**last**, with totals and the applied policy; batching across mounts with the
root-row batch after the others; the policy record written through the
policy store. A `fstest.MapFS`-driven default-lane test and an integration
test against a live server.

**Acceptance.** Default lane: a `MapFS` tree (files, directories, symlinks,
an unreadable entry, a text file spanning blocks, a binary file, an empty
directory) produces exactly the expected rows (an in-memory or recording
`ExecutorI`): counts, `content` modes, hashes (compare against
`blake3.Sum256` of the source), `line0` sequence, root row last; a walk aborted
before the root row leaves no complete snapshot. Integration lane: the same
tree through a live server; `fssnap` shows the snapshot once the root row
lands and not before.

### M3 — the `io/fs` adapter — **done 2026-08-20**

Shipped as `public/fs/lading/ladingadapter`; the corrections are ADR-0198's
`## Updates` entry for that date. For M4: a predicate over `fsdata` must
escape the NUL in a block's natural key (`\0`), or an executor that passes
SQL as a process argument fails before ClickHouse sees it; and `Glob` stayed
in Go rather than becoming `match()`, so the SQL surface offers a regular
expression directly instead of translating a glob.

**Deliverables.** `ladingadapter`: `Open(store, mountID, snap)` →
`fs.FS` implementing `StatFS`, `ReadDirFS`, `ReadFileFS`, `GlobFS`,
`ReadLinkFS`, `SubFS`; `File` with `Read`, `ReadAt`, `Seek`, `Stat`,
`ReadDir`; `Latest(store, mountID)` and `Snapshots(store, mountID)` over
`fssnap`. Reads go through the generated `Scan<Kind>(ScanOpts{ExtraPredicate,
Limit})` with predicates over the physical columns (`dir = …`,
`startsWith(naturalKey, …)`, `naturalKey = … AND <ordinal range>`), ordering
in memory; `content = 'none'` → `Read` returns a typed error; `'ref'` → the
live-source fetch hook (interface, default "not available").

**Acceptance.** `testing/fstest.TestFS` passes against a snapshot written by
M2 (integration lane); `ReadAt` across a block boundary returns the exact
bytes; `ReadDir` order is bytewise by name; a symlink `Lstat`s as a link and
`ReadLink` returns the target; `Sub` works; a snapshot written while another
is read does not change the pinned view.

### M4 — the SQL surface — **done 2026-08-20**

Shipped as `public/fs/lading/ladingsql`; the corrections are ADR-0198's
`## Updates` entry for that date. Two for M5: `PREWHERE` does not survive a
macro (the expansion is a subquery, and ClickHouse allows PREWHERE only
against a table), and a third relation `fssnap()` carries the commit
record's totals. The capability check is a seam (`MountVisibilityI`,
default-deny) and binding it to the capability broker is still the stop
point the rules section names.

**Deliverables.** (The how-to this section made conditional was written after
M6, as [doc/howto/lading-snapshot-store.md](../howto/lading-snapshot-store.md),
so it could cover Go, SQL and rclone at once.) The `fs()` / `fsdata()` nanopass rewrites — parenthesised
subqueries per the compact page §4, projection from the entry kind's generated
Projection, the completeness rule, the logical cutoff, the capability check
at expansion; entries in the security classifier allowlist and play's dispatch
policy (beside `keelson`); goldens for the expansions; the operations of the
compact page §7 (grep, history, diff, du, identical content, audit) as
executed integration tests over a seeded store; docs: a how-to for the SQL
surface if the repository's doc conventions call for one (ask).

**Acceptance.** Expansion goldens; the classifier reports `fs()` as
introspection-read; each §7 query returns the expected rows on the seeded
store; an expired row is invisible through `fs()` while still present in the
table (the cutoff).

### M5 — the rclone head (SFTP over stdio) — **done 2026-08-20**

Shipped as `public/fs/lading/ladingsftp` plus `boxer fs sftp-stdio`; the
corrections are ADR-0198's `## Updates` entry for that date. `pkg/sftp` was
audited before adding (the stop point) and is now a direct dependency.
For M6: the same library's client is what wraps an `rclone serve sftp
--stdio` remote as an `fs.FS`, so the dependency question is settled.

**Deliverables.** `ladingsftp` + a CLI subcommand (`boxer fs
sftp-stdio --store <name> …`, name per the CLI conventions): `pkg/sftp`
`RequestServer` over stdin/stdout; handlers over the adapter — `Fileread` →
`ReadAt` with a per-handle cache of decoded blocks and readahead, `Filelist`
→ `ReadDir`/`Stat`/`Lstat`/`Readlink`, `Filewrite`/`Filecmd` → permission
denied; the virtual tree `/<mount>/<snapshot>/<path>` with `latest` as a
symlink; mount naming as hex id (name alias via the policy record if cheap);
a capability manifest entry for the store read. Integration lane driving the
real `rclone` resolved through `extbin`: `rclone lsd/ls/cat/copy --metadata`
and a mount read over `:sftp,ssh="…"`.

**Acceptance.** rclone lists mounts, snapshots and trees identically to the
adapter; `rclone cat` bytes equal the source; `rclone copy --metadata`
restores mtimes and modes; a 32 KiB chunked read of a 10 MiB file issues one
block query per block, not per chunk (assert on an executor counter).

### M6 — rclone ingress — **done 2026-08-20**

Shipped as `public/fs/lading/ladingremote` — its own package rather than
inside `ladingingest`, so the walker keeps `fs.FS` as its only input; the
reasoning and the measured transport limits are ADR-0198's `## Updates`
entry for that date.

**Deliverables.** In `ladingingest`: an `fs.FS` over the `pkg/sftp` client
(`ReadDir`, `Lstat`, `Open`, `ReadLink`) and a source that spawns `rclone serve
sftp --stdio <remote>` and snapshots it; filters passed through. Integration
test: a local directory served by `rclone serve sftp --stdio`, snapshotted,
read back through the adapter, `fstest.TestFS` green.

**Acceptance.** Round trip equal to a direct `os.DirFS` snapshot of the same
directory up to SFTP's 1 s mtime precision and symlink availability.

### M7 — deferred, by measurement

A native S3 head on the runtime's HTTP mux and `fsmeta` projections — only if
M0/M5 numbers show `rclone serve s3` over the stdio head cannot carry the
listing scale or bulk throughput a consumer needs. Not started without the
user's go-ahead.

## 3. Verification commands (per milestone)

```sh
go build -tags="$(cat ./tags)" ./...
go test  -tags="$(cat ./tags)" ./public/fs/lading/... ./public/db/clickhouse/dsl/nanopass/...
go mod tidy --diff
scripts/ci/lint.sh
scripts/ci/gotest-integration.sh          # live ClickHouse (+ rclone for M5/M6)
go run -tags="$(cat ./tags)" ./public/app gov doclint doc/adr/0198-fs-snapshot-store.md doc/adr-background-work/
```

## 4. Do not

- Partition by mount; share blocks; deduplicate; use `LowCardinality` on
  `data`; put `mount` or `ordinal` into the `boxer.facts` `TableDesc` without
  an explicit decision; claim a tag or mint ids for mounts; import
  `crypto/sha256`; bind a non-loopback address for any head; write through the
  adapter; hand-write array arithmetic where the read surface provides a
  function; invoke `rclone` without `extbin` resolution.

## 5. What to report back at the end of each milestone

What shipped (packages, files), what was measured (dated numbers), which
ADR-0198 sub-decision was confirmed or corrected (and the dated `## Updates`
entry that records it), what is left open, and the exact commands that prove
the acceptance criteria.
