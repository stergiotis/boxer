---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# lading — positioning

Positions the fs snapshot store — a walk of a file tree written once into
ClickHouse and read back as a file system or as SQL — together with the
`tally` browser and the ad-hoc tree publisher, which are clauses here rather
than pages of their own. The one table shape the store lands in is
positioned by [recordstore](./recordstore.md).

## Short form

For an operator who keeps file trees — on local disks, or on any remote a
file-transfer tool reaches — and needs to find, compare, size and verify
them without walking them again, lading is a snapshot store[^snapshot] for
file trees in ClickHouse: each walk lands once as rows in the facts
shape[^facts], kept for a declared time, and is read back as a Go file
system, in a file browser, or with SQL.

Unlike a filesystem indexer or a backup catalog with a store and a query
language of its own, lading makes a snapshot a set of rows next to every
other record, so find, diff, history, disk usage and integrity are each one
SQL query. The cost is no deduplication, a full walk per snapshot, and a
store that is not a hot serving path.

## Full form

**For** an operator who keeps file trees — local, or on any remote `rclone`
reaches — and needs to find, compare, size and verify them after the fact
without walking the tree each time,

**lading is** a snapshot store[^snapshot] for file trees in ClickHouse: a
walk of a tree written once into tables the store controls, `rclone` spawned
over a pipe as the transport, a read-back as a Go file system pinned to one
snapshot, SQL functions, a file browser, and the ability to publish any in-
memory tree as a short-lived mount,

**that** answers find, diff, history, disk usage and integrity with one SQL
query each, leaves nothing visible from an interrupted walk, and lets one
store serve many owners because the caller names the identity,

**unlike** a filesystem indexer or a backup catalog, each a store of its own
with its own query language and lifecycle,

**lading** keeps a snapshot append-only and expires it by time, with no
garbage collector, no reference counting and no sweep. The cost is no
deduplication, a full walk per snapshot, and a store that is not a hot
serving path.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | an operator with file trees to find, diff and account for | the how-to's audience ([lading-snapshot-store](../../howto/lading-snapshot-store.md)); [ADR-0200](../../adr/0200-tally-lading-browser.md) Context, a browser a file-transfer power user recognises |
| For | on any remote `rclone` reaches | [ADR-0198 §SD9](../../adr/0198-fs-snapshot-store.md), ingress from any rclone remote (M6) |
| is a | a walk written once into facts-shaped tables the store controls | [ADR-0198 §SD1–SD2](../../adr/0198-fs-snapshot-store.md); the data-centricity invariant of [ADR-0148](../../adr/0148-app-workingsets.md) |
| is a | `rclone` over a pipe, "possession of the pipe is the authorisation" | [ADR-0198 §SD9](../../adr/0198-fs-snapshot-store.md); [rclone-architecture-lessons](../rclone-architecture-lessons.md) §4–6 |
| is a | `io/fs` adapter, SQL macros, the `tally` browser, ad-hoc mounts | [ADR-0198](../../adr/0198-fs-snapshot-store.md), [ADR-0200](../../adr/0200-tally-lading-browser.md), [ADR-0222 §SD5](../../adr/0222-tally-composition-surface.md) |
| that | find, diff, history, du, integrity as one query each | [ADR-0198 §SD7](../../adr/0198-fs-snapshot-store.md); [ADR-0200](../../adr/0200-tally-lading-browser.md) Context |
| that | an interrupted walk leaves nothing visible | [ADR-0198 §SD6](../../adr/0198-fs-snapshot-store.md), the root-row commit rule |
| that | per-block hashes auditable from SQL | [ADR-0198 §SD5](../../adr/0198-fs-snapshot-store.md) |
| that | the caller owns identity; one store, many owners | [ADR-0198 §SD3](../../adr/0198-fs-snapshot-store.md) |
| unlike | a filesystem indexer or a backup catalog | named as prior art, not as rejected alternatives — [iofs-clickhouse-snapshot-store](../../adr-background-work/iofs-clickhouse-snapshot-store.md) §11–12; the rejected shapes are a chunked block store (a shared block's lifetime cannot be a TTL) and a row store for metadata; the indexer and catalog foil is accepted on the prior-art citation |
| lading | append-only, unshared, TTL-retained; no collector | [ADR-0198 §SD1](../../adr/0198-fs-snapshot-store.md) |
| trade | undeduplicated storage, a full walk per snapshot, not a hot path | [ADR-0198](../../adr/0198-fs-snapshot-store.md) Consequences; the how-to's limits section |

## Boundary

- `tally` and the ad-hoc tree publisher are clauses of this page; a browser
  without ingest or export is a reader
  ([ADR-0200](../../adr/0200-tally-lading-browser.md) Consequences).
- The path filter interface (`fsmatch`) has no decision of its own and is not
  positioned.
- Designed but not built, and therefore absent from the clauses: a native S3
  head ([ADR-0198](../../adr/0198-fs-snapshot-store.md) M7, deferred), a
  capability binding for mount visibility, in-app ingest and export, and the
  per-file writer that [stevedore](./stevedore.md) would land bytes through.

## Further reading

- [why-boxer](../why-boxer.md) — the premises the clauses compress; P3 is
  the one this store enacts most directly.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [lading-snapshot-store](../../howto/lading-snapshot-store.md) — putting a tree in and reading it back.
- Decisions: [ADR-0198](../../adr/0198-fs-snapshot-store.md),
  [ADR-0200](../../adr/0200-tally-lading-browser.md),
  [ADR-0222](../../adr/0222-tally-composition-surface.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/fs/lading

[^snapshot]: One complete listing of a tree at one moment; a later walk is a new snapshot, never an update to the old one.
[^facts]: `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
