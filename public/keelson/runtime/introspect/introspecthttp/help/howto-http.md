---
type: how-to
audience: end-user
status: draft
title: Querying the introspection tables over HTTP
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Querying the introspection tables over HTTP

The running shell serves the introspection tables over a small loopback HTTP
endpoint, so any ClickHouse on the same host can read them.

## Finding the endpoint

The endpoint binds a loopback address. By default the port is chosen at random
and logged at startup:

```
introspect: table source listening  addr=127.0.0.1:54123
```

For a stable port, set the bind address before starting the shell:

```bash
export KEELSON_INTROSPECT_HTTP_LISTEN=127.0.0.1:8097
```

The examples below assume `127.0.0.1:8097`.

## Listing the tables

```bash
curl -s http://127.0.0.1:8097/tables
```

Each line is a table name. `GET /table/<name>` returns that table as an Arrow
stream; you rarely fetch it by hand — point ClickHouse at it instead.

## From clickhouse-local or clickhouse-server

Read a table with the `url` table function, asking for the `ArrowStream`
format:

```sql
SELECT name, value
FROM url('http://127.0.0.1:8097/table/env', 'ArrowStream')
WHERE category = 'database';
```

Because it is an ordinary table source, a long-running `clickhouse-server` can
JOIN it against its own data, and one query can read several keelson tables. To
discover what a build exposes, read the catalogue:

```sql
SELECT name, freshness, column_count
FROM url('http://127.0.0.1:8097/table/tables', 'ArrowStream') ORDER BY name;
```

### Fetching fewer columns

`url()` reads the whole table and ClickHouse then discards unused columns. To
avoid materialising a column you do not need, ask the endpoint for a subset
with the `cols` query parameter:

```sql
SELECT * FROM url('http://127.0.0.1:8097/table/env?cols=name,value', 'ArrowStream');
```

## The /query endpoint

`POST /query` runs a statement through `clickhouse-local` for you and lets you
name tables with the `keelson(...)` macro instead of a full `url(...)`:

```bash
curl -s http://127.0.0.1:8097/query --data \
  "SELECT name, value FROM keelson('env') WHERE category = 'llm' FORMAT JSONEachRow"
```

The macro expands to a `url(...)` reference against this same endpoint, so the
address stays out of your query. Any `clickhouse-local` SQL works — you can join
a keelson table against `numbers()` or a `system` function — and an unknown
`keelson('...')` name is rejected before the query runs. Append your own
`FORMAT` clause to choose the output (`ArrowStream`, `JSONEachRow`,
`CSVWithNames`, `PrettyCompact`, …); without one you get `clickhouse-local`'s
default.

`/query` answers `503` when the shell has no `clickhouse-local` available, since
there is then nothing to run the statement.
