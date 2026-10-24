---
type: adr
status: withdrawn
date: 2026-10-24
withdrawn-date: 2026-10-24
---

# ADR-0333: Listener pools — a long-lived `clickhouse local` behind `ch.local.exec`

## Context

[ADR-0028](./0028-chlocal-low-latency-sql-cap.md) serves local SQL from
one-shot `clickhouse local` workers: each is pre-spawned, answers one
statement, and exits. It rejected long-lived workers in server mode (its O2).
ClickHouse 26.6 added `SYSTEM START LISTEN`, which lets `clickhouse local`
serve HTTP itself, so O2 became buildable. This ADR proposed it, and was
withdrawn before acceptance. It is kept as a record of what the option would
have taken.

On one host and binary (2026-10-24, 26.9.13.15, 32 cores, load average
~2.5), the two shapes compared:

| Request | Warm one-shot worker | Listening process |
| --- | --: | --: |
| `SELECT 1` | 13.4–14.1 ms | 0.43–0.70 ms |
| 100k rows as ArrowStream | 18.4–22.1 ms | 10.6–15.9 ms |

About 10 ms of each one-shot request is the process exiting after the result
has arrived. The pool has to wait for it, because a statement that fails
mid-stream still writes a complete-looking ArrowStream; the exit code is the
only failure signal.

## Decision

As proposed: a pool could be configured as a **listener pool**. It would keep
one long-lived `clickhouse local` serving HTTP on loopback, behind the same
`ch.local.exec.<pool>` subject and reply contract. One-shot would stay the
default, and the only shape that accepts writes.

Each piece below was tested on 26.9:

- **Lifetime.** Started with `--interactive --query "SYSTEM START LISTEN
  HTTP"`, the process serves until its stdin closes, so it dies with its
  parent.
- **Access.** By default the listener serves any local process as the
  passwordless `default` user. A password cannot fix that, because the
  listener's own session also logs in from loopback as `default`. The working
  gate replaces the HTTP handlers with ones that require a secret header,
  kept in a 0600 config file.
- **Isolation.** Each request gets a fresh `session_id` and `readonly=2`.
  That blocks persistent tables but allows the temporary tables input data
  needs, and a statement cannot lower it.
  `cancel_http_readonly_queries_on_client_close=1` stops a query when its
  client goes away.
- **Preludes.** HTTP rejects multi-statement bodies. Input tables become one
  request each in the session, and parameters move to `param_*` URL values.
- **Errors.** A mid-stream failure arrives as `200`, a tagged `__exception__`
  trailer, and a body left unterminated.
- **Memory.** All requests share one process budget. Under the allocator's
  default decay, freed memory stays resident for 4–8 s and counts against
  that budget meanwhile. The default was kept so both shapes manage memory
  the same way.
- **Version floor.** 26.8, the first release in which a failing connection
  handler no longer kills the whole process.

## Alternatives

- **No gate:** every local process gets in.
- **Password authentication:** locks the listener out of its own session.
- **A Unix socket:** not supported by `clickhouse local` 26.9.
- **Choosing the shape per request, or by inspecting the SQL:** puts the
  memory decision on every caller, or on a classifier that can be wrong.
- **A separate subject family:** a second permission grant for the same
  request shape.
- **Returning freed memory immediately (jemalloc decay 0):** two memory
  strategies instead of one, and heavy queries ran 30–300% slower.
- **A full `clickhouse-server`:** rejected for ADR-0028's reasons.

## Consequences

Withdrawing leaves one-shot workers as the only shape. A warm request keeps
costing about 14 ms. The cap avoids a second worker shape, an access gate
whose correctness depends on ClickHouse's handler config, a second form of
every prelude and error path, a read-only routing rule, and a memory budget
shared between requests.

## Status

Withdrawn on 2026-10-24, before acceptance. The gain, about 13 ms per small
read, has no consumer that needs it. ADR-0028's latency target is already met
by warm one-shot workers. The cost is a second worker shape with a security
boundary to maintain.

Reopen when a consumer's measured per-call cost on a warm one-shot worker is
its bottleneck; [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)
§SD8 is the likely first. Re-check each premise above on the ClickHouse
release current at that time before relying on it.

## References

- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the one-shot shape; its O2 is the option recorded here.
- [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) — the consumer most likely to reopen it.
