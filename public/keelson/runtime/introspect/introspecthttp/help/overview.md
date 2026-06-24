---
type: explanation
audience: end-user
status: draft
title: Introspection tables
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Introspection tables

keelson exposes parts of its own running state as tables you can query with
ClickHouse — environment variables, registered apps and demos, build metadata,
the build SBOM, and the open windows. The tables live in a `keelson` namespace
and are read-only snapshots taken at the moment you query them.

## What is available

| Table | Holds | Freshness |
|-------|-------|-----------|
| `env` | the environment-variable registry with live values | live; sensitive values redacted |
| `apps` | the registered app manifests | static |
| `demos` | the imzero2 demo catalogue | static |
| `build` | this process's run id, host, Go version, VCS revision (one row) | static |
| `sbom` | the build's CycloneDX components, when a path is configured | static |
| `windows` | the currently open windows | live |
| `tables` | one row per introspection table — the catalogue | static |
| `columns` | one row per (table, column) | static |

`tables` and `columns` are the self-describing catalogue, the keelson
equivalent of ClickHouse's `system.tables` / `system.columns`. Query `tables`
first to see what a given build exposes — the set can grow between versions.

## Two ways to query

The same tables are reachable two ways:

- **Over HTTP, from any ClickHouse.** A loopback HTTP endpoint serves each
  table as Arrow. A `clickhouse-local` or a `clickhouse-server` reads it with
  the `url` table function and can JOIN it against other data. The companion
  how-to walks through this.
- **In-process.** The running shell can answer a query itself, feeding the
  Arrow straight into `clickhouse-local` without an HTTP round-trip.

## The keelson() macro

Whichever path you use, you can name a table with the `keelson(...)`
table-function macro instead of spelling out a `url(...)` reference:

```sql
SELECT name, value FROM keelson('env') WHERE category = 'database'
```

The macro expands to the concrete source at query time, so a query never
hard-codes the endpoint's address or the transport. An unknown table name is
rejected before the query runs.

## A note on exposure

The endpoint binds a loopback address by default and refuses a non-loopback
bind — authenticated remote access (with TLS) is a separate, not-yet-built
decision. Values marked sensitive in the env registry are redacted before they
leave the process. The data is whatever the running build holds; treat it as
you would any local diagnostic surface.
