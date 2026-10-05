---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Positioning statements, per subsystem

The product-level [positioning statement](../positioning-statement.md) is
Geoffrey Moore's template — *for* a target customer *who* has a need, *the
product* is a category *that* delivers a benefit; *unlike* the alternative,
it differs in one primary way — with every clause tied to a premise in
[why-boxer](../why-boxer.md) or to an ADR. The pages in this folder apply
the same template one level down, to the subsystems whose customer, or
whose foil, differs from the product's.

A subsystem earns a page when three tests hold: it has a customer distinct
from boxer's; it has a nameable *external* alternative a real team would
pick instead; and the ADR record carries enough decisions to fill the
rests-on table. A part that fails the first test is a clause in its
parent's page. A part that fails the second is a description, not a
position, and belongs in an explanation doc. A part that fails the third
waits.

Each page follows [the template](../../templates/POSITIONING.md.tmpl): a
short form, the full six-slot form ending in the trade, a rests-on table,
footnote glosses for the house terms (readable as tooltips where the
renderer shows footnotes on hover, as plain notes elsewhere), and a
boundary section naming what the page folds into a parent or leaves
to a sibling. The open questions each draft carried — customer, foil, scope
— were resolved by the owner on 2026-09-23 and removed; the checklist below
is what remains before a page flips to `stable`.

## The set

| Page | Customer | Foil | Parent or siblings |
| --- | --- | --- | --- |
| [keelson](./keelson.md) | an app author | a process per app on a broker, with state in the broker or a second store | — |
| [leeway](./leeway.md) | a SQL practitioner reading semi-structured data, with writers and readers generated from the same description | hand-written DDL, SQL and codecs per kind; a schema-less JSON or variant column | [recordstore](./recordstore.md), [identity](./identity.md) |
| [recordstore](./recordstore.md) | a library or app author needing an append-only record store | a bespoke table per kind; a flat payload column | child of [leeway](./leeway.md) |
| [identity](./identity.md) | vocabulary authors, SQL practitioners, pipeline maintainers | a fixed-width tag with out-of-band width; an opaque or hashed id | child of [leeway](./leeway.md) |
| [nanopass](./nanopass.md) | an app running user-typed SQL, and the authors of its rewrites | rewriting through an abstract syntax tree — the server's or a transpiler's | — |
| [imzero2](./imzero2.md) | an app author who needs one source on every host | a web frontend beside a backend; a remote-desktop stack | [widgets](./widgets.md) |
| [widgets](./widgets.md) | an app author who needs scientific display | one crate or JavaScript library per need | child of [imzero2](./imzero2.md) |
| [play](./play.md) | whoever writes ClickHouse SQL at the keyboard | a dashboard layer with a global filter bus; a modelling language between author and engine | — |
| [watchbill](./watchbill.md) | a developer giving an app or headless binary durable work | a job queue on a second substrate — relational store, file or broker | — |
| [lading](./lading.md) | an operator with file trees to find, diff and account for | a filesystem indexer or a backup catalog | — |
| [stevedore](./stevedore.md) | an application team owning an ingestion handler under a framework | per-processor framing and landing code; a standalone consumer service | — |
| [pushout](./pushout.md) | a team versioning records that must merge across peers and be forgotten on demand | a snapshot version-control system with erasure by history rewrite | — |
| [gov](./gov.md) | the maintainer of boxer or of a repository that consumes it | a gate assembled from separately configured tools | — |
| [observability](./observability.md) | an operator of the running toolkit | a metrics and error stack beside the data | — |

Deferred, with the trigger that would open them: **llm** (model inference
as a capability, [ADR-0254](../../adr/0254-model-inference-as-a-keelson-capability.md))
once its call record and egress path land and a live check exists;
**sqlapplet** if it gains a consumer outside play — until then it is a
clause of [play](./play.md).

## Recurring foil

Several pages share the product's foil one level down: a stack assembled
from best-of-breed parts, each with its own process, model and log. Where
a page's foil is that shape — [widgets](./widgets.md), [gov](./gov.md),
[observability](./observability.md), [keelson](./keelson.md) — it says so
and links here rather than re-deriving it. The pages whose foil is
genuinely different — [pushout](./pushout.md), [identity](./identity.md),
[nanopass](./nanopass.md), [play](./play.md) — carry the stronger
statements, because the difference is specific.

## The short forms, together

The coherence check: every short form on one page, in one vocabulary.
Each is the opening of its page and is kept in step with it. The house
terms are glossed on the pages by footnote and gathered below.

**keelson.** For an app author who wants to write a data app as four methods
and let the host decide where it runs and what it may touch, keelson is
boxer's app runtime: one Go process that mounts apps, passes every request
over a message bus whose addresses double as permissions, and records
grants, audit, logs, launches and app state as rows in the facts table.
Unlike a host that gives each app its own process on a message broker, with
state kept in the broker or in a second store, keelson keeps the runtime's
whole record in one queryable place, so the SQL workbench doubles as the
runtime's debugger. The price is a hygiene boundary rather than a security
one, and an indirection on every call.

**leeway.** For a SQL practitioner who reads semi-structured data in
ClickHouse — values under several tags at once, ragged arrays, notes
attached to single values — and wants a table layout plain SQL can read
without a lookup, leeway is a data-mapping engine: describe the schema once,
and the ClickHouse tables, the ingestion path, the Go readers and writers
and a versioned set of SQL functions are all generated from it. Unlike hand-
written DDL, SQL and codecs for every kind of record, or a schema-less JSON
column whose structure is implied rather than declared, leeway keeps one
description as the only thing to maintain. The cost is a vocabulary that
exists nowhere else, a more involved write path, and generators that fail
the way compilers do: everywhere at once.

**recordstore.** For a library or app author who needs an append-only record
store over ClickHouse — an event store, a version-control backend, a metrics
log — and does not want to own a table, a writer, a reader and a cache for
it, recordstore generates all four from one description, over a table in the
facts shape that the rest of the toolkit can read. Unlike a hand-built table
per kind of record, with its own schema, insert path and reader that shared
tooling cannot see, recordstore makes a new kind cost a data type and a few
vocabulary entries, never a schema change. The cost is no control over
indexes and retention, about twice the insert time of a hand-built table,
and a generator that inherits drift from the four it drives.

**identity.** For anyone minting identifiers that both Go and SQL must split
into a kind and a number without a lookup, identity is a tagged-id scheme: a
variable-width tag coded so no tag is a prefix of another, a number behind
it, one split routine kept identical in Go and ClickHouse by a shared test,
and one authority that hands out tag values. Unlike an opaque id whose kind
lives in a column beside it, or a fixed-width tag every reader must know the
width of, identity puts the kind in the id itself, so a test for one kind
becomes a range the primary key can prune. The cost is ordering across kinds
that follows code bits, a slower split for unknown tags, and a one-time
breaking migration.

**nanopass.** For an app that runs SQL typed by a user — a workbench, an
applet, an introspection endpoint — and the people who write the rewrites it
applies, who need the rewritten query to still be the user's query, comments
and positions intact, nanopass is a pass framework for ClickHouse SELECT
statements: small stateless passes over the parsed text, declared and
checked pass properties, and a registry that applies the active passes
before execution and lists them as a table. Unlike rewriting through an
abstract syntax tree — the server's exported tree or a transpiler's — which
drops comments, whitespace and positions and returns a different query than
the one typed, nanopass edits spans of the original text. The cost is a re-
parse per pass, a parser that accepts more than the canonical grammar, and
output that is normalised rather than byte-identical.

**imzero2.** For an app author who writes a data app in Go and wants it on a
desktop, in a browser and on a headless appliance from one source, imzero2
is an immediate-mode UI stack over a framed FFI: Go owns every piece of
state, the Rust side is a stateless renderer of a per-frame command stream,
and the same renderer serves a desktop window, a video stream to a browser,
a headless accessibility tree for a driver, and a CPU-rasterized appliance
with no GPU. Unlike a web frontend beside a backend, or a remote-desktop
stack over a desktop toolkit, imzero2 leaves the app unable to tell which
host it is on. The cost is two binary targets, one frame of input latency,
browsers that must ship the video codec, and the web platform's assistive
technology.

**widgets.** For an app author who needs scientific and data displays —
plots, distributions, spectra, maps, flows, graphs, flame graphs — inside a
Go app, widgets is a catalog drawn by Go code with Go-owned state: a ported
plot kernel with one interaction idiom, a ported map kernel, a shared camera
and hosting protocol for the graph and map views, one colour-map
configuration, and a design system a lint enforces. Unlike one crate or one
JavaScript library per need — a plot crate, a map crate, a graph crate —
each with its own state, interaction model and a bridge per feature, widgets
keeps the state in Go where the data already is. The cost is ported code
that must track its upstreams, tessellation as the performance ceiling, and
finishing touches such as context menus arriving later.

**play.** For someone who writes ClickHouse SQL at the keyboard and wants a
result as a table, chart, map, board or graph they can steer by interaction,
play is a SQL workbench over a reactive query graph: the buffer is the
artifact, its common table expressions are the nodes, panels watch nodes,
parameters that panels write are the signals, and a query nothing watches
never runs. Unlike a dashboard layer that links panels through a global
filter bus and matches them by column name, or a modelling language placed
between the author and the engine, play recovers the graph from the SQL
itself, so the pasted query stays the truth. The cost is a reactive runtime
that can surprise, plain SQL's lack of define-once measures, and a desktop
app rather than a web page.

**watchbill.** For a developer giving an app or a headless binary work that
must survive the process — a download, an export, a long computation — on a
box that has a ClickHouse server and nothing else, watchbill is durable work
as records: a job table in ClickHouse, a claim that is one conditional
update plus a read-back, a lease that is the runtime's existing heartbeat,
and five verbs over named queues. Unlike a job queue on a second store — a
relational database, a file, or a broker whose state nothing can query —
watchbill keeps every job's whole life as rows next to the launches and
grants it can be joined with, readable from the SQL workbench. The cost is
throughput in the tens of jobs a minute, no periodic or workflow jobs, and a
queue that is gone when the server is.

**lading.** For an operator who keeps file trees — on local disks, or on any
remote a file-transfer tool reaches — and needs to find, compare, size and
verify them without walking them again, lading is a snapshot store for file
trees in ClickHouse: each walk lands once as rows in the facts shape, kept
for a declared time, and is read back as a Go file system, in a file
browser, or with SQL. Unlike a filesystem indexer or a backup catalog with a
store and a query language of its own, lading makes a snapshot a set of rows
next to every other record, so find, diff, history, disk usage and integrity
are each one SQL query. The cost is no deduplication, a full walk per
snapshot, and a store that is not a hot serving path.

**stevedore.** For an application team that owns an ingestion handler and
runs it under a streaming framework, and needs the framing, failure
handling, identity and landing around that handler to be the same in every
pipeline, stevedore is the host for framework-driven processors: one library
that speaks the framework's process contract, classifies failures, derives
identity from the request's origin, and lands items in ClickHouse after the
flush. Unlike each processor carrying its own framing, retry and landing
code, or a standalone service that re-implements what the framework already
provides, stevedore keeps the code that is the same in every pipeline in one
place, testable without a broker. The cost is at-least-once delivery, so
every sink must key by reference and part, and a body bounded by one reply.

**pushout.** For a team versioning records that must both merge across peers
and be forgotten on demand, pushout is a patch-based version-control engine:
patches named by a content hash over their changes and dependencies,
independent patches that commute, conflicts kept as data, and storage,
encoding and transport behind interfaces that each carry a conformance
suite. Unlike a snapshot version-control system, where every commit is
chained to its whole ancestry and erasure means rewriting history, pushout
keeps a patch's identity stable under cherry-pick and sync, and makes
retention an explicit, checked mode of the store with a purge that is
durable once the call returns. The cost is no convergence at the value level
and envelopes that cannot be grepped. Forgetting a subject without touching
the graph is designed and not yet built.

**gov.** For the maintainer of boxer, or of a repository that consumes it,
who must answer "what may this tree depend on, and does it follow its own
standards" without a review team, gov is a governance toolkit: a license
gate reading one in-source policy for Go and Rust from a bill of materials,
lints whose rule ids cite the standard they enforce, a pinned list of gate
steps, and an emitted skeleton that fails when it drifts. Unlike a gate
assembled from separately configured tools — a license scanner per language,
a lint meta-runner, a configuration language — each with a policy of its own
that drifts from the others, gov keeps one policy in Go and breaks boxer
first when it changes. The cost is an owned list of license identifiers, a
dependency cone in the gate binary, and fewer linters than a meta-runner
offers.

**observability.** For an operator of the running toolkit — at a desktop, in
a sandboxed appliance, or acting through an agent — who needs load, runtime
behaviour, errors and query runs next to the data they concern, boxer's
observability is a set of collectors, bridges and dashboards that put what
the system does into the same facts table as what it stores, drawn with the
toolkit's own widgets. Unlike a metrics stack beside the data — a collector,
a time-series store and a dashboard of its own, each with its own model — it
turns errors into queryable rows, copies metrics into the facts table on
request, replays stored history through the live view, and captures query
runs from the engine itself. The cost is a reimplemented collector that can
drift from its model, an observer effect in the process it measures, and
growth of the facts table.

## Glossary

The terms the pages gloss by footnote, in one place. A page glosses a term
the first time it uses it; the wording here and there is the same.

- **applet** — A small app defined entirely by one markdown document holding SQL, launched like any other app.
- **bus** — The in-process message bus every app request travels over; a NATS client can stand in for it.
- **capability** — What an app may request; here a filter on bus addresses, declared in the app's manifest and checked against its code.
- **claim** — A worker takes a job by one conditional update that succeeds for exactly one claimant, then reads the row back to confirm it won.
- **cst** — A concrete syntax tree keeps every token of the source, including comments and whitespace, which an abstract syntax tree discards.
- **facts** — `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
- **fffi** — A framed foreign-function interface batches a frame's worth of UI calls into one message from Go to Rust, instead of one cross-language call per widget.
- **godrawn** — The widget's geometry is computed in Go and sent as drawing commands; the Rust side rasterizes them and keeps no widget state.
- **graph** — The sub-queries in one SQL buffer and the references between them; reactive means a change to one node re-runs only what depends on it.
- **immediate** — Immediate mode: the app redraws its whole interface every frame from its own state, instead of keeping a tree of widgets the toolkit owns.
- **introspection** — Tables named `keelson.*` that expose runtime state — grants, launches, app state — to any SQL client.
- **membership** — A membership is a tag a value carries; one value can carry several, which is what "multi-membership" means throughout the leeway docs.
- **pass** — A pass is one small rewrite with one job, such as turning CASE into a function call; passes are chained into a pipeline.
- **patch** — A patch records a change and the patches it depends on; two patches that touch unrelated things apply in either order, which is what "commute" means here.
- **plane** — The one-way channel the metrics scraper publishes on; the UI subscribes to it instead of reading the operating system itself.
- **processor** — The program a streaming framework starts to transform each message; stevedore is the part of that program that is not the transformation.
- **sbom** — A software bill of materials: the machine-readable list of every dependency in a build, here in CycloneDX form.
- **snapshot** — One complete listing of a tree at one moment; a later walk is a new snapshot, never an update to the old one.
- **sow** — Schema-on-write: the structure is declared before data is stored, so readers get typed columns instead of parsing at query time.
- **tag** — The tag is the part of the identifier that says what kind of thing it names; Fibonacci coding lets frequent kinds get short tags.

## Review checklist

A page flips from `draft` to `stable` when a reviewer confirms each line:

- Every clause of the full form has a row in the rests-on table, and every
  row links a premise or an ADR by number.
- A clause that rests on a proposed, deferred or unbuilt ADR says
  *proposed* in the clause or in the table.
- No adjective names a reaction rather than the artifact
  ([DOCUMENTATION_STANDARD §4](../../DOCUMENTATION_STANDARD.md#voice-and-tone)).
- The foil is something a real team would pick, named as a practice or a
  product class.
- Exactly one primary difference, not a list.
- The short form stays under 150 words and the full form under about 200,
  one idea per slot, with the inventories in the rests-on table.
- Every house term is glossed by footnote the first time it appears, in
  words a reader who has not opened the repo can follow, and the gloss
  matches the entry in the glossary below.
- The trade is named in the last clause.
- No counts, versions, undated time words or line numbers; nothing a
  refactor that keeps the decision would falsify
  ([§4 Claims that decay](../../DOCUMENTATION_STANDARD.md#claims-that-decay)).
- The short forms, read together above, use one vocabulary: the product
  statement's terms for the same things.

## Maintenance

Only the rests-on table ages. When an ADR is superseded, the rows that cite
it are what needs revisiting; the link checker catches a row whose target
moved. A page whose subsystem is retired moves to `superseded` and names
the page that absorbs its customer. The short form on this page is a copy
of the page's own; when one changes, the other does.
