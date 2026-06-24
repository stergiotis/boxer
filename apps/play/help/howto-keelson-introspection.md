---
type: how-to
audience: end-user
status: draft
title: Querying keelson introspection tables
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Querying keelson introspection tables

The running shell exposes its own state — env vars, apps, demos, build info, the
SBOM, open windows — as ClickHouse tables (the keelson introspection facility).
You can browse them from `play` without a separate ClickHouse server.

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

Because `/query` runs the statement through `clickhouse-local`, ordinary SQL
works too — aggregate, filter, or join a keelson table against a generated one:

```sql
SELECT category, count() AS n
FROM keelson('env') GROUP BY category ORDER BY n DESC
```

## Caveats

- The endpoint lives inside the running shell, so the shell must be up and its
  `clickhouse-local` available; otherwise `/query` returns `503`.
- An unknown `keelson('…')` name is rejected with a clear error before the query
  runs.
- play's read-rows / bytes readout stays at zero here — the endpoint does not
  emit the `X-ClickHouse-Summary` header a full server would.
- This is a local diagnostic surface bound to loopback; it is not a substitute
  for the ClickHouse server `play` normally talks to.
