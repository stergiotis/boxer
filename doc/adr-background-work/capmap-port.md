---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Measurements below were taken
> 2026-08-05 against the tree at that date; nothing here has been reviewed.

# Porting a business-capability map into boxer

Background for [ADR-0168](../adr/0168-capmap-business-capability-corpus.md).
This page carries the measurements, the survey of what boxer already provides,
and the reasoning behind each fork; the ADR records only the outcomes.

Provenance markers: **measured** claims were produced by running the stated
command against this tree on 2026-08-05. **Read** claims come from reading
source at that date. Everything else is an estimate and says so.

## 1. What is being ported

A standalone business-capability management prototype: a hierarchical
capability model with maturity and pain scoring, lifecycle tracking, an
Obsidian vault as the editing surface, ClickHouse as the query store, and a
server-rendered HTMX front end.

**Measured** (`find`/`wc` over the prototype's checked-in files):

| | |
| --- | --- |
| Go | 7,413 lines, 16 packages, plus one vendored MIT fork (a squarified-treemap layout, 248 lines) |
| Web | 1,221 lines of HTML/JS/CSS across four webapps — browser, culler, cull-configer, lint — plus a landing page and a vendored HTMX |
| SQL | `schema.sql` + `views.sql`, ~340 lines, `ReplacingMergeTree` |
| Corpus | 2,625 markdown files, 11 MB, three catalogs — a boxer catalog (79 capabilities), a public process-framework catalog (82), and one from a private checkout (27) |

Its own architecture note already describes the write path as CQRS: vault
markdown is the write model, ClickHouse the eventually-consistent read model.
That framing survives the port intact and is the reason §5's fork was cheap to
settle.

## 2. API drift against boxer is near zero — measured

The prototype pins a boxer version from 2026-04-06. To measure the real delta,
its checked-in files were copied to a scratch directory, a
`replace github.com/stergiotis/boxer => <this tree>` appended to `go.mod`, and
the build run under both tag sets with `GOWORK=off`:

- `go build ./...` — **exit 0**
- `go vet ./...` — **exit 0**
- `go test ./...` — **two failures**, both in one file's assertions:
  `astbuilder` now emits quoted identifiers, so a test asserting the substring
  `id = 12345` sees `"id" = 12345`. Behaviour is unchanged; the fix is two
  assertions.

All fourteen boxer import paths it uses still exist. Every external dependency
it needs is already in boxer's `go.mod`; `blackfriday` is present only as an
indirect, and `public/semistructured/markdown/obsidian` can replace it outright.

One blocker is policy rather than API: two files still carry
`//go:build llm_generated_opus4*`, retired by
[ADR-0083](../adr/0083-retire-llm-generated-build-tags.md).

**Conclusion.** Compilation is not the work. Placement and shape are.

## 3. The model to copy: boxer's ADR ecosystem

**Read.** The decision corpus is exposed by five cooperating pieces, and
markdown is the only source of truth in the chain:

| Piece | Where |
| --- | --- |
| Corpus parser (pure library over markdown) | `public/gov/adrcorpus` |
| CLI emitting Arrow, queried via `clickhouse-local` | `public/app/commands/adr` |
| The same tables in-process for the GUI | `providers/adr.go` under `public/keelson/runtime/introspect` |
| Canned SQL lenses | `apps/sqlapplet/book*` ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)) |
| The committed corpus itself | `doc/adr/` |

Two properties of that chain are worth naming because the port inherits both.
A schema-parity test pins the CLI's Arrow schemas equal to the providers'
schemas, so a query written against one runs verbatim against the other. And
the providers are **empty rather than erroring** off-repo — a shipped binary
with no checkout around it has no corpus, which is a fact about the process
rather than a failure.

## 4. Naming: "capability" is already taken

**Read.** In boxer the word means *runtime/security capability*:
`public/keelson/security/capslock`, `codec/capabilitygrant`,
`apps/capinspector`, `apps/capdemo`, the `CapId`/`CapSpec` types, and
[ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)'s capability
subjects — a named registry, and therefore Tier 1 by
[CODINGSTANDARDS](../../CODINGSTANDARDS.md#what-triggers-an-adr).

A business-capability corpus cannot claim that word. The port keeps `capmap`
as the subsystem name — a compound that does not collide — and names the
corpus package `capmapcorpus`, mirroring `adrcorpus`.

> **2026-08-05, after the fact.** This section stopped one level too high. It
> settled the *package* names and said nothing about the **unit**, so the
> implementation went on to call the model type `Capability` and the keelson
> tables `capability` / `capsection` / `caprelation` — putting the word back in
> the flattest shared namespace boxer has, which is the one place it collides
> worst. ADR-0168 §SD6 now names the unit a **competence** and states the rule
> the survey left implicit: boxer's names take the fresh word, the vault keeps
> the literature's, and `capmapcorpus` is the boundary.

## 5. The facts model already carries almost everything

The decisive question was whether the corpus should get bespoke Arrow tables or
be modelled as facts. Once modelled as facts, four things had to be checked.

### 5.1 `u64h` and `f64h` already exist

**Measured** (`grep` over the generated DDL at
`public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go`). The schema
promotes every numeric scalar with `ScalarModifierHomogenousArray` in two
loops, so the array sections are already there:

```
u64Array:value:val:u64h      f64Array:value:val:f64h
u8h u16h u32h i8h i16h i32h i64h f32h   z64h   sh   yh
u32m u64m                    ← ScalarModifierSet, also present
```

`ctabb.U64h` and `ctabb.F64h` exist as first-class generated abbreviations.
No extension to the leeway table description is required.

### 5.2 Array-valued promotion is 18 of 22 value columns done

**Measured**, same source. Only four value columns are scalar:

| Section | Type | Status |
| --- | --- | --- |
| `symbol` | `s` | Deliberate — the schema comment records that 1-element-array wrapping would defeat the inter-record low-cardinality encoding that audit and grant rows rely on |
| `u32Range` | `u32`, `u32` | Structural — co-columns (`beginIncl`/`endExcl`), a range pair rather than a value list |
| `foreignKey` | `u64` | Structural — the relation channel itself |
| `bool` | `b` | The one genuinely open candidate |

So a "promote everything" pass is mostly already applied, and the residue is
one section that no capability data needs.

### 5.3 `text` and `string` are byte-identical

**Measured.** The two sections generate the same column specification —
canonical type `sh`, encoding aspects `g`, value aspects `0`, and the same
`Array(String) CODEC(ZSTD(3))`. Only the section *name* differs. Because
leeway schemas are nominal rather than structural they remain distinct
addresses, but nothing downstream can tell them apart.

By contrast `symbol` earns its name: its value-aspect segment is `24`, base62
for 128 = `1<<7` = `AspectCanonicalizedValue`, and its low-cardinality hints
produce `Array(LowCardinality(String))`.

**Read.** The triple arrived wholesale in the upstream import commit
`884e2c9f` and has never been revisited here. The runtime barely exercises the
distinction: the ClickHouse facts store writes `stringArray` four times and
`textArray` once.

The compression mapping makes the difference actionable —
`AspectLightGeneralCompression` emits `ZSTD(3)`, `AspectHeavyGeneralCompression`
emits `ZSTD(12)`, ultra-heavy `ZSTD(19)`.

**A second, richer shape exists in `anchor`**, boxer's leeway showcase, whose
`text` section is a co-section of `text` + `wordLength` + `wordBag` — human
readability carried structurally, by derived retrieval columns, rather than by
a flag. The two schemas also disagree on whether an array of symbols is still
low-cardinality: `anchor`'s `symbolArray` drops the low-card hints,
`factsschema`'s keeps them.

| | `anchor` | `factsschema` |
| --- | --- | --- |
| `symbol` | `S` + canonicalized + low-card | same |
| `symbolArray` | `Sh` + canonicalized, light compression | `Sh` + canonicalized **+ low-card** |
| `stringArray` | `Sh`, light compression | same |
| `text` | co-section: `text` + `wordLength` + `wordBag` | `textArray`: identical to `stringArray` |

### 5.4 The relation channel exists and has never been used

**Read.** `foreignKey` is a `LowCardRef`, `u64`-valued section in its own
streaming group carrying `useaspects.AspectLinking` — the cross-fact reference
channel. [ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)
already lets one tuple element carry several memberships including ref
channels, with the ref id carried directly as `uint64`, which is an
edge-with-roles in all but name.

`valueaspects.AspectGraphVertex` (52), `AspectGraphEdge` (53) and
`AspectHyperGraphEdge` (54) exist with **zero consumers** repo-wide. They are
schema-level declarations on columns, so using them means changing
`factsschema` — a Tier-1 change with no consumer yet to serve.

## 6. The vocabulary gap

**Read.** `public/keelson/runtime/vocab` registers 73 memberships under
`identifier.TagValue(2)`, all `runtime*`-prefixed, in `LowerSpinalCase`. The
`estSize` arguments (`4`, `32`) are capacity hints for a growing container, not
ceilings — there is no analogue of the 64-aspect cap here.

The gap is allocation. A second vocabulary needs its own TagValue, and nothing
records which are taken.
[ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) §SD8
notes only that `TagValue(0)` was invalid under the fibonacci scheme and that
the runtime registry moved to a valid value. The precedent the vocab comment
cites for scope separation is a schema from a checkout that is **no longer in
this tree**, so its allocation is claimed by comment and nothing else.

Identity itself is settled by precedent rather than invention: facts rows carry
a `u64` `id` plus a `naturalKey` that is a blake3-16 digest over the
identifying tuple. A capability's identity becomes that, which also retires the
prototype's xxh3 slug hash and one dependency with it.

## 7. Store: a real tension, and the bridge

**Read.** The two obvious positions collide. The ClickHouse facts store targets
a **live server** (`http://localhost:8123/` by default) — that is how facts are
written. `chlocalpool` spawns `clickhouse-local --path <mktemp>`, pooled and
reaped: a stateless engine over external files that **cannot host** a
persistent table.

The reconciliation is that `factsschema/cborarrow` already converts facts rows
into an ArrowStream IPC payload using the generated typed Arrow builder, and
`leeway/ddl/arrow` is the Arrow mapping. So a serverless read path exists in
principle as *facts-shaped Arrow read through `file()`*, rather than as a table
of the port's own design.

**Not measured.** The pieces were read, the round-trip was not run. Proving it
is a milestone step, not an assumption.

## 8. Markdown: extract, do not decompose

The question was whether to store each capability's markdown as a parsed AST in
separate attributes.

**Read — prior art.** `anchor/codecdemo/labeledtextdoc.go` is boxer's own
answer to putting a text document into leeway. A document is a sequence of
*labelled text chunks*: the label is the membership, the prose is the prose,
and the derived structure is a word bag and word lengths. Not an AST. That maps
onto a capability body without adaptation — six labelled chunks, one per h1
section, which is exactly what the prototype's parser already produces.

**Read — corpus precedent.** The ADR ecosystem stores decision markdown as one
media-typed column (`content@text/markdown`,
[ADR-0123](../adr/0123-play-content-typed-detail-cells.md)) in its own table,
split out because the source is roughly 60× the metadata.

**Read — what the data actually contains.** Sampling the boxer catalog, the six
description sections are heterogeneous: `Vision and Scope` is genuine prose;
`Activities` and `Standards` are bullet lists of wikilinks with display labels;
`Business Capabilities` is prose with inline wikilinks. The AST's queryable
meaning is concentrated in the links — and links are relations, which are
already their own facts.

Three arguments against full decomposition:

1. **Round-trip.** The vault stays authoritative and the dump verb must
   regenerate it; the prototype has a round-trip test. A goldmark AST is not
   losslessly re-renderable — whitespace, list markers, link-title style — so
   storing AST instead of source would break the CQRS contract.
2. **Volume with no query behind it.** 2,625 capabilities × 6 sections × N
   nodes, to answer questions nobody asks. Nobody queries the third emphasis
   node in the second paragraph.
3. **The data-centricity invariant does not demand it.**
   [ADR-0148](../adr/0148-app-workingsets.md)'s rule targets app *state* and
   names blob-section opaque payloads as the anti-pattern. Prose in a
   text-typed section carrying `AspectHumanReadable` is modelled — the model is
   "this is human-readable text". A CBOR blob would not be.

## 9. The ledger

Estimated from the package line counts in §1, assuming the shape settled in
ADR-0168.

| Fate | Lines | What |
| --- | --- | --- |
| Ported | ~1,900 | vault parsing, lint, similarity |
| Deferred | 1,070 | the culler's tag-write path |
| Replaced by boxer | ~2,900 | its ClickHouse client, its WHERE-predicate validator and filter builder, its SVG treemap and layout fork, its colour scale, its table and detail renderers |
| Dropped | ~1,750 | the HTTP server, middleware, all 1,221 lines of HTML/JS/CSS, the vendored HTMX, and both SQL files |

The replacements are one-for-one with things boxer already has: `chlocalpool`
and the facts store; play's SQL editor and the nanopass passes; the imzero2
treemap widget, which carries its own squarified layout; the qualitative
palette; play's table and detail panes.

## 10. Corpus data

The vault cannot be committed as it stands. It carries a catalog derived from a
private checkout, whose name must not enter a public tree, and a public
process-framework catalog whose licence has not been checked against the repo's
gate. The boxer catalog — boxer describing its own toolbelt — is the part that
motivates having the tool here at all.

Placement is symmetric with `doc/adr/`, and there is precedent for a
git-ignored directory beneath `doc/`: doclint skips git-ignored trees, and its
own source comments describe exactly that arrangement.

## 11. Questions left open

Each is deferred with a trigger rather than left implicit.

- **`bool` → `boolArray`.** The one remaining promotion candidate. No
  capability data needs it. Trigger: a facts writer with a genuine array of
  booleans.
- **`anchor`'s `text` co-section, and the `symbolArray` low-cardinality
  disagreement.** One question, since both are "make the two schemas agree".
  Trigger: a consumer that wants `wordBag` — the compression-similarity ranker
  is the obvious candidate, since it recomputes exactly that at query time.
- **Graph value-aspects on `foreignKey`.** Using them means changing
  `factsschema` for semantics no consumer reads yet; `AspectLinking` already
  states the linking intent. Trigger: a reader that dispatches on the graph
  aspects.
- **The triage/culling workflow.** A UI that mutates repo files is a distinct
  security posture. Trigger: the read path proving the corpus is worth curating
  at that rate.
- **A treemap panel for play.** The widget exists and had never been wired to a
  play panel; one was proposed for profiles and dropped, on the reasoning that
  an icicle answered the same question while preserving path order. A
  capability map is the opposite case — a size-and-maturity hierarchy where
  path order does not matter — which is the motivation that kill note said
  would be needed. As of 2026-08-05 this is no longer open here: a play Treemap
  panel is being decided in ADR-0166, written concurrently with this survey.
