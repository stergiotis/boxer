---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ClickHouse primitives for watchbill: what was measured

Background for [ADR-0223](../adr/0223-watchbill-durable-work-on-facts.md)
(proposed). The ADR's §SD3 assumed the store has no compare-and-set and
designed a claim as "write, settle, re-read, earliest wins". This note
records what ClickHouse offers instead, measured rather than read, so the
ADR can decide on evidence.

**Method.** Every result below is from 2026-09-09 against ClickHouse
26.8.1.1939 (official build): the workstation's server on `:8123`, a
throwaway server of the same binary started with its embedded Keeper and
`keeper_map_path_prefix` set, and `clickhouse local`. Races are twenty
parallel HTTP requests against one key, repeated three to five times. The
scratch server, its database and the scratch database on the workstation's
server were removed afterwards.

## What a durable queue on this store needs

| Need | Question |
| --- | --- |
| claim | can exactly one of N workers take a job, and know it did? |
| queue read | can "next job" be one ordered query? |
| current state | is the newest transition per job cheap to read? |
| history | do transitions stay as rows? |
| doorbell | can the store tell the bus a row landed, in order? |
| sweep | can the store run the rescuer without a worker? |
| retention | can finished work expire on its own? |
| local lane | does it work under `clickhouse local` for the default tests? |

## Findings by primitive

### KeeperMap with `keeper_map_strict_mode` — a real compare-and-set

A `KeeperMap` table keeps its rows as Keeper znodes and, in strict mode,
turns an insert of an existing key into a Keeper transaction failure
(`Transaction failed (Node exists)`, code 999). Twenty parallel strict
inserts of one fresh key: **one 200, nineteen 500, four rounds of four.**
The winner is the one whose request succeeded; nothing to re-read.

Two conditions, both found the hard way:

- **`async_insert` must be off for the claim.** It is on by default at
  26.8 (`async_insert = 1`, tier Production), and with it the twenty
  parallel inserts were batched into one Keeper transaction: twenty 200s
  and one surviving row. Strict mode is per batch, not per statement. The
  claim insert sets `async_insert = 0`; everything else may batch.
- **The server must enable it.** `keeper_map_path_prefix` is unset by
  default and the engine refuses to create a table without it. The
  workstation's server refused; the throwaway server with the setting
  worked. `clickhouse local` has no Keeper, so the local lane cannot
  exercise it at all.

Strict `ALTER TABLE … UPDATE` is optimistic: it reads the znode version and
fails if it moved. Twenty parallel conditional updates (`WHERE worker =
'free'`): twelve 200, eight 500, one row changed — the eleven other 200s
matched nothing. A winner must read back to know. Twenty parallel
unconditional strict updates: five 200, fifteen version conflicts. `DELETE
FROM` works. A row costs one znode (12 bytes for the probe's payload); the
table is bounded by Keeper's memory and its snapshot cadence, which is right
for thousands of open jobs and wrong for millions.

### Lightweight `UPDATE` on MergeTree — also a compare-and-set, by a different route

26.8's lightweight update (`UPDATE t SET … WHERE …`, patch parts) needs the
table to carry `enable_block_number_column = 1` and
`enable_block_offset_column = 1`, which the default table did not; the
error names the fix. With them, twenty parallel conditional updates
`SET state = 'running-wN', n = n + 1 WHERE id = 1 AND state = 'queued'`
gave **one winner and `n = 1`, five rounds of five**, and twenty parallel
unconditional `n = n + 1` gave `n = 20`: no lost update. The same held
under `update_parallel_mode = 'async'` (three rounds) and `'sync'`; the
default `'auto'` serialises updates whose read set touches another's write
set, which a claim always does. The winner learns by reading its own marker
back — one round trip, but no settle: the read after the update saw the
patched state at once. Every update leaves a patch part (seven of eight
active parts after the probes) until merges fold them.

Two caveats. Both settings are tier **Beta** at 26.8
(`allow_experimental_lightweight_update`, `enable_lightweight_update`; both
on by default). And an update is a mutation of a row rather than a new
row, so a design that wants every transition as history writes the event
row *and* updates the state row, or reads history from the patch parts,
which nothing should.

### Writes to `system.zookeeper` — not a lock

With `allow_zookeeper_write` the server accepts `INSERT INTO
system.zookeeper`, but a second insert of the same name **overwrote** the
node (version 1, value of the second writer) rather than failing, and
there is no ephemeral node. Not a create-if-absent; KeeperMap is the
sanctioned door to the same Keeper.

### The NATS table engine — a server-side doorbell, with one coupling

The engine is compiled into the official build. A materialized view from
the events table into a `NATS` table published every inserted row on
`watchbill.changed` (three of three, in insert order), and the reverse — a
`NATS` table on `watchbill.wake` with a view into MergeTree — turned a
published message into a row within the second. The publish happens inside
the insert, after the base row, so the "row before message" rule holds by
construction.

The coupling: **with the NATS server down, the insert into the events
table fails** (`CANNOT_CONNECT_NATS`, code 665) and no row lands. With
`materialized_views_ignore_errors = 1` the row lands but the client still
receives the error. So a synchronous view into a NATS table makes the
store's durability depend on the bus's liveness, which inverts ADR-0090's
split. The engine reconnected on its own once the server returned and
later inserts published. Usable as an *additional* doorbell where the bus
is known to be up; not as the only one, and not in the insert's path.

### Refreshable materialized views — the sweep as a schedule

`CREATE MATERIALIZED VIEW … REFRESH EVERY 2 SECOND APPEND TO …` ran on the
workstation's server without a flag (`allow_experimental_refreshable_
materialized_view = 1` by default) and ticked on schedule
(`system.view_refreshes` reports status and next refresh). The abandoned
sweep — running rows whose run id has no fresh heartbeat — can be a
refreshable view that appends `abandoned` events, so the rescuer needs no
worker to be alive and runs where the data is. It is a schedule, not a
trigger: latency is the refresh interval.

### ReplacingMergeTree with `is_deleted` and `FINAL` — the current state

Confirmed as expected: a `ReplacingMergeTree(ver, is_deleted)` read with
`FINAL` returned the newest row per key and dropped the deleted key. A
current-state table fed by a materialized view from the events table is
the cheap `watchbill_current`, in place of an `argMax` over events on
every poll. The events table stays the history.

### `clickhouse local`

`KeeperMap` refuses (no Keeper). `EmbeddedRocksDB` works, including
`ALTER TABLE … UPDATE`, and is single-node key-value without a version
check — fine for a local lane's stand-in, not a claim primitive.
Lightweight update, refreshable views and `ReplacingMergeTree` run under
`clickhouse local`.

## What this changes for the ADR

- **§SD3's settle-and-tie-break is not needed.** Two real compare-and-set
  primitives exist. KeeperMap strict insert is the cleaner claim — the
  winner is told by the response — and costs a server setting and a
  second engine. Lightweight conditional `UPDATE` keeps everything on one
  MergeTree table, costs a read-back, and is Beta. Either removes the
  losing worker's inert `running` row and the settle wait.
- **`async_insert` is a hazard, not a default.** On by default at 26.8; the
  claim path must turn it off explicitly, and the ADR should say so where
  it names the row-before-message rule.
- **The current state can be a table.** Events plus a `ReplacingMergeTree`
  projection through a materialized view, read with `FINAL`, instead of a
  window over events.
- **The rescuer can be a refreshable view.** Then a store with no live
  worker still abandons stale claims on schedule.
- **The doorbell stays the client's.** The NATS engine can add a
  server-side publish where the bus is known up, but a view into it must
  not sit in the insert path.
- **The local lane cannot see KeeperMap.** If the claim is KeeperMap, the
  default lane tests the worker over a stand-in claim store and the
  integration lane needs a server with Keeper, which `clickhouse local`
  is not; the lightweight-update route keeps the whole thing under
  `clickhouse local`.
