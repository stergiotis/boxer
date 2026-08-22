---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-23
---

> Where this page and [why-boxer](./why-boxer.md) or an ADR disagree, those
> are the record.

# Positioning statement

What boxer is for, in one paragraph — and the longer form that paragraph is
cut from. The form is Geoffrey Moore's positioning template: *for* a target
customer *who* has a need, *the product* is a category *that* delivers a key
benefit; *unlike* the primary alternative, *the product* differs in one
primary way. The short form leads the README's [Why](../../README.md#why)
section. This page carries the full six-slot form and the premise or
decision each clause rests on. Since 2026-08-23 the category and benefit
clauses also carry what [doc/ARCHITECTURE.md](../ARCHITECTURE.md) evidences —
the processes, the hosts an app runs on, and the one table shape everything
durable lands in — merged from the positioning that page's §5 reads off the
architecture alone.

## Short form

For a small but ambitious data team ready to bet on the compounding of a
composable stack it owns — and needing air-gapped operability and
auditability from source — boxer is a sovereign, data-centric
data-engineering toolkit and app stack over ClickHouse (the query engine and
database): one Go host process carrying the apps, the bus and the data paths,
a Rust client beside it that only renders, and ClickHouse as the one place
anything durable lives.

Rather than assembling best-of-breed parts with fragmented models and logs,
boxer generates tables, codecs, ingestion and readers from small,
problem-oriented languages; turns a query into a table, chart, map or board —
or, in a markdown file, an app; runs that app unchanged on a desktop, in a
browser, or from a roughly 110 MB appliance image; and lands every query,
error, grant and app state — even file trees — in one queryable table shape,
operable by people and agents alike — trading ecosystem breadth and a stable
API for a stack a small team can read from source, end to end.

## Full form

**For** a small but ambitious data team that is ready to bet on the
compounding of a composable stack it owns for the long term, and needs
air-gapped operability and auditability from source,

**boxer is** a sovereign, data-centric data-engineering toolkit and app stack
over ClickHouse, the query engine and database: one Go host process carrying
the apps, the bus, the data paths and the network policy, a Rust client
beside it that only renders, helpers (`clickhouse-local` workers, `ffmpeg`,
`rclone`) spawned over pipes, and ClickHouse as the one place anything durable
lives,

**that** lets a few people carry the whole stack: small problem-oriented
languages generate the ClickHouse tables, Arrow codecs, ingestion path,
statically typed readers and per-kind SQL functions for extraction and
transformation; a SQL query becomes a table, chart, map or board on screen —
or, in a markdown file, an app; that app runs unchanged on a desktop window,
in a browser over one WebSocket, or from a roughly 110 MB appliance image
with no GPU stack; and every durable fact — queries, errors, grants,
launches, app state, metrics, even file trees — lands in one queryable table
shape, so a new requirement costs a projection, not a new data layer.

**Unlike** a stack assembled from best-of-breed parts — each a process, a
deployment, a model and a log of its own,

**boxer** records everything it does in that one model, queryable and
renderable by its own tools and operable by people and by agents through the
same machine-readable surfaces, inside one memory-safe Go + Rust boundary a
small team can read from source, end to end, with the engine beside it as a
separate self-hosted process — trading ecosystem breadth and a stable API for
exactly that.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | a small team ready to bet on the compounding of a composable stack it owns | [why-boxer](./why-boxer.md) "The bet, named" and "Who this is for" (shares P1, wants the compounding of P2–P5 rather than a parts bin); the R5RS lineage cited under P2 |
| For | air-gapped operability, auditability from source | P1; the airgapped bundle ([ADR-0095](../adr/0095-airgapped-build-bundle.md)); the license gate ([ADR-0004](../adr/0004-license-gate-cyclonedx.md)) |
| is a | sovereign, data-centric, over ClickHouse | "The bet, named" (sovereignty defined operationally); P3 (the data-centric wager); the standing commitments (ClickHouse as the query-execution engine and database; Go and Rust; egui over a framed FFI) |
| is a | one Go host process, a Rust client that only renders, helpers on pipes, ClickHouse as the one place anything durable lives | [ARCHITECTURE §1](../ARCHITECTURE.md) (the four process kinds and their boundaries); FFFI2 over the client's pipes ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md) §Context); the `clickhouse-local` pool ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md)); durable facts on the server ([ADR-0026 §SD6](../adr/0026-app-runtime-and-capability-subjects.md), [ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md)) |
| that | a few people carry the whole stack | P6, and the P2→P6 chain in "The bet, named" |
| that | problem-oriented languages generate tables, codecs, ingestion, readers, per-kind SQL functions | P2; the leeway generators ([ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md), [ADR-0089](../adr/0089-rowdml-serialization-clickhouse-native-ingestion.md), [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md), [ADR-0189](../adr/0189-component-sql-authoring-surface.md)) |
| that | a query becomes a table, chart, map or board; a markdown file becomes an app | the play query graph and its panels ([ADR-0097](../adr/0097-play-reactive-query-graph.md), [ADR-0122](../adr/0122-play-kanban-panel.md), [ADR-0129](../adr/0129-play-layered-graph-panel.md)); SQL-defined applets ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md), build-time in v1) |
| that | that app runs unchanged on a desktop, in a browser, or from a roughly 110 MB appliance image | "placement is the host's call, not the app's" ([ADR-0026 §SD8](../adr/0026-app-runtime-and-capability-subjects.md)); the headless carrier and browser viewer ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md)); the CPU-rasterized host and the gokrazy images ([ADR-0205](../adr/0205-imzero2-cpu-rasterized-pixel-host.md), [ADR-0206](../adr/0206-gokrazy-appliance-image.md) (proposed)); [ARCHITECTURE §2](../ARCHITECTURE.md) |
| that | every durable fact, even file trees, lands in one queryable table shape | the facts table and the generated record stores ([ADR-0026 §SD6](../adr/0026-app-runtime-and-capability-subjects.md), [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md), [ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md)); metrics as facts ([ADR-0184](../adr/0184-sysmetrics-persistence-tee.md)); the fs snapshot store ([ADR-0198](../adr/0198-fs-snapshot-store.md)); [ARCHITECTURE §3.2–3.3](../ARCHITECTURE.md) |
| that | a requirement costs a projection, not a data layer | P3 (what it buys) |
| Unlike | best-of-breed parts — each a process, a deployment, a model and a log of its own | why-boxer's opening and "Who this is for — and not"; P3's failure mode (a collection of independent engines); the contrast [ARCHITECTURE §1](../ARCHITECTURE.md) draws (four process kinds, the boundaries between them) |
| boxer | records everything it does in the same data model | P4; errors ([ADR-0041](../adr/0041-rowmarshall-error-shredding.md)), query runs ([ADR-0115](../adr/0115-query-observability-data-plane-strategy.md)), grants and audit ([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)), launches ([ADR-0135](../adr/0135-app-launch-requests.md)), app state ([ADR-0148](../adr/0148-app-workingsets.md), including its data-centricity update) |
| boxer | operable by people and by agents through machine-readable surfaces | P7 (the agentic tier over P3's surfaces); the introspection tables ([ADR-0094](../adr/0094-keelson-introspection-tables.md)); the natural-language surfaces are proposed, not shipped ([ADR-0120](../adr/0120-play-natural-language-ask-panel.md), [ADR-0139](../adr/0139-semantic-layer-text2dsl.md)) |
| boxer | one memory-safe Go + Rust boundary, the engine beside it | the standing commitments in why-boxer |
| boxer | trading ecosystem breadth and a stable API | P1 and P6 costs; "What this costs you" |

## Further reading

- [why-boxer](./why-boxer.md) — the premises the statement compresses.
- [README](../../README.md) — the short form in place.
- [doc/ARCHITECTURE.md](../ARCHITECTURE.md) — the drawing behind the process and host clauses; its §5 keeps the architecture-only reading as a check on what the architecture evidences by itself.
