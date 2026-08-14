---
type: how-to
audience: end-user
status: draft
title: Querying keelson introspection tables
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Querying keelson introspection tables

The running shell exposes its own state — env vars, apps, demos, build info, the
SBOM, open windows, stored workingsets — as ClickHouse tables (the keelson
introspection facility). You can browse them from `play` without a separate
ClickHouse server.

## Point play at the /query endpoint

The shell serves a `/query` endpoint that speaks enough of the ClickHouse HTTP
interface for `play`: it POSTs a statement and reads ArrowStream back. Find the
endpoint's address in the startup log,

```
introspect: table source listening  addr=127.0.0.1:54123
```

or pin it with `KEELSON_INTROSPECT_HTTP_LISTEN=127.0.0.1:8097` before starting
the shell. Then set play's ClickHouse endpoint — the `--clickHouseUrl` flag,
which defaults to `http://localhost:8123/` — to that endpoint's `/query` path:

```
--clickHouseUrl http://127.0.0.1:8097/query
```

## Name tables with keelson('...')

In `play`, refer to a table with the `keelson(...)` macro. As elsewhere in
`play`, do not add a `FORMAT` clause — the app appends one.

See what is available:

```sql
SELECT name, freshness, column_count FROM keelson('tables') ORDER BY name
```

Inspect the environment-variable registry (sensitive values arrive redacted):

```sql
SELECT name, category, value, is_set
FROM keelson('env')
WHERE category = 'database'
ORDER BY name
```

List the registered apps and their declared capabilities:

```sql
SELECT id, surface, has_help, caps FROM keelson('apps') ORDER BY id
```

See which windows are open right now — this table is live, so it changes as you
open and close windows:

```sql
SELECT key, app_id, title, surface FROM keelson('windows') ORDER BY key
```

Ask where each open window's content came from — `plain` means nobody supplied
one, `caller` means another app opened it with arguments, and `restore` means
the shell handed back the state that window's app was left in:

```sql
SELECT key, app_id, launch_reason, config_kind, config_bytes
FROM keelson('windows') ORDER BY key
```

Which apps accept arguments, and which of them keep their working state across a
close (`launch_kind` names the config an opener must send; `workingset` implies
one):

```sql
SELECT id, launch_kind, workingset, persisted_keys
FROM keelson('apps') WHERE launch_kind != '' ORDER BY id
```

And what is actually *stored* — the records a plain open would restore from, one
row per stored record rather than one per save. `config_bytes` is the payload's
size; the payload itself is the app's own DTO and stays out of the table.
`reason` says why the window that wrote the record closed:

```sql
SELECT app_id, name, kind, config_bytes, reason, saved_at
FROM keelson('workingsets') ORDER BY app_id
```

Because `/query` runs the statement through `clickhouse-local`, ordinary SQL
works too — aggregate, filter, or join a keelson table against a generated one:

```sql
SELECT category, count() AS n
FROM keelson('env') GROUP BY category ORDER BY n DESC
```

List the membership registry — the vocabulary behind the `uint64` tags a
leeway ref channel carries, so one join turns an id from a result column back
into its name (ADR-0171 §SD4):

```sql
SELECT name, id, virtual, parents
FROM keelson('memberships')
ORDER BY name
```

The `name` column holds the folded spelling (`naturalKey` publishes as
`natural-key`), so join predicates must use that form — `LW_GET` accepts
either. A `virtual` row groups related memberships and never appears on a
lane, so matching data against one returns nothing by design.

## Caveats

- The endpoint lives inside the running shell, so the shell must be up and its
  `clickhouse-local` available; otherwise `/query` returns `503`.
- An unknown `keelson('…')` name is rejected with a clear error before the query
  runs.
- play's read-rows / bytes readout stays at zero here — the endpoint does not
  emit the `X-ClickHouse-Summary` header a full server would.
- `keelson('workingsets')` reads through whichever facts store the shell got at
  start. With ClickHouse down that is the in-memory one, so the table then shows
  only what this process saved, and the records go with the process.
- This is a local diagnostic surface bound to loopback; it is not a substitute
  for the ClickHouse server `play` normally talks to.
