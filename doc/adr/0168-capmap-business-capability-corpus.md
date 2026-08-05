---
type: adr
status: proposed
date: 2026-08-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0168: capmap — a business-capability corpus as `boxer.facts`

## Context

boxer has a governance ecosystem for one corpus — decisions. `adrcorpus` parses
`doc/adr/`, `boxer adr` emits queryable tables, keelson providers serve the same
tables in-process, and applet books carry the canned lenses. Nothing comparable
exists for *capability management*: what the toolbelt can do, how mature each
part is, where the pain is, and what depends on what.

A standalone prototype answers that question already — vault markdown, a
ClickHouse read model, an HTMX front end — and its corpus includes a catalog of
boxer's own capabilities. Bringing it in makes boxer's self-description a
first-class, queryable artifact rather than a document in another checkout.

The measurements, the survey of what boxer already provides, and the reasoning
behind each fork live in the
[background survey](../adr-background-work/capmap-port.md). The load-bearing
findings: the prototype builds and vets clean against this tree (two test
assertions drift), so compilation is not the work; `boxer.facts` already carries
`u64h` and `f64h` array sections, so no leeway table-description extension is
needed; and `text` versus `string` in that schema is currently a distinction
with no encoded difference.

## Design space (QOC)

**Question.** Where do capability and relation facts live?

**Options.**

- **O1** — `boxer.facts` itself, with new kind memberships under a new TagValue.
- **O2** — A new leeway table of the same shape, with its own schema package, vocabulary and generated artifacts.
- **O3** — A composite mapping extending `LoadRuntimeFactsMapping` into a separate physical table.

**Criteria.**

- **C1** — Join reach: can a query relate capabilities to what the runtime knows about itself?
- **C2** — Scope fidelity: does the table stay honest about what it claims to hold?
- **C3** — Codegen and maintenance cost.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −  | −  |
| C2 | −  | ++ | +  |
| C3 | ++ | −− | −  |

O1 wins on the criterion that motivated the port. The boxer catalog describes
boxer's own toolbelt, and the runtime already publishes what apps, packages and
environment it has; putting both in one table makes "which app implements which
business capability" a join rather than a data-integration exercise. C2 is the
real cost and is paid explicitly in SD1.

## Decision

We will model the capability corpus as facts in `boxer.facts`, keep the vault
authoritative, and expose the result through the same five-piece shape the ADR
ecosystem uses. The subsystem is named `capmap`; it does not use the word
*capability* for its Go packages.

- **SD1 — Two fact kinds in `boxer.facts`.** A capability fact and a relation
  fact, under a new `capmap*` vocabulary. This widens the table past its stated
  scope of app state, grants and audit records, recorded as a dated Update on
  [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD6. Rejected
  alternative: a private table, which buys scope purity and loses the join that
  motivated the port.

- **SD2 — Relations are their own facts, carried on `foreignKey`.** A relation
  fact holds both endpoints on the `foreignKey` section using
  [ADR-0109](./0109-leeway-marshall-multi-membership-ref-tuples.md)
  multi-membership under distinct role memberships, its type (`parent`,
  `similar`, `wikilink`) on `symbol`, and a similarity score on `f64Array`.
  Level-4 multi-parenting stops being a special case — it is more rows.
  Rejected: parent ids as a `u64Array` column on the capability fact, which
  makes an edge a property of one endpoint and has no place to put the
  relation's own attributes.

- **SD3 — The vault stays authoritative; facts are derived.** Ingest rebuilds
  facts from markdown and is repeatable from scratch. Editing stays in the
  vault, so the corpus remains reviewable as a diff. Rejected: facts as the
  source of truth, which would make the corpus unreviewable and collide with
  the record-store-versus-tree question.

- **SD4 — `text` and `string` separated by aspects.** `textArray` gains
  `AspectHumanReadable` and `AspectHeavyGeneralCompression`; `stringArray`
  gains `AspectMachineReadable`. Today the two generate byte-identical columns,
  so the schema's own naming is unbacked. Rejected for now: adopting `anchor`'s
  richer `text` co-section, which is the better shape but adds columns for a
  consumer that does not exist yet (§Deferrals).

- **SD5 — Prose stays prose; the AST is extracted, not decomposed.** The six
  description sections are stored as labelled text — the shape
  `anchor/codecdemo/labeledtextdoc.go` already establishes — while wikilinks
  are harvested into relation facts carrying the section they occurred in.
  Rejected: storing a parsed AST in separate attributes, which breaks vault
  round-tripping (a goldmark AST is not losslessly re-renderable) and multiplies
  rows to answer questions nobody asks.

- **SD6 — A `capmap` vocabulary with its own TagValue.** Mirrors
  `public/keelson/runtime/vocab`: `capmap*`-prefixed natural keys in
  `LowerSpinalCase`. Because nothing in the tree records which TagValues are
  taken, this ADR also states the allocation rule: **TagValues are allocated
  here, in the ADR that introduces the vocabulary, and the allocating ADR is
  named in the vocabulary package's doc comment.** Rejected: registering into
  the runtime vocabulary, which would put corpus names in a namespace whose
  prefix promises process state.

- **SD7 — The corpus lives in-tree but git-ignored.** `doc/capabilities/`,
  symmetric with `doc/adr/`, holding the boxer catalog only, with a committed
  `README.md` and everything else ignored. Two catalogs are excluded: one
  derived from a private checkout, whose name must not enter a public tree, and
  a public process-framework catalog whose licence has not been checked against
  the repo's gate. Rejected: committing the corpus now, which would decide both
  questions by accident.

- **SD8 — The read path is keelson providers, not raw facts SQL.** Providers
  decode memberships into flat columns, on the precedent set by the workingsets
  provider — reading these otherwise means raw `boxer.facts` SQL plus knowledge
  of the membership encoding. Rejected: letting applets query the physical
  leeway columns, which would spread the encoding across every book.

- **SD9 — No HTTP surface; the views are applets.** The prototype's four
  webapps are replaced by an applet book
  ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) over the provider
  tables. Rejected: porting the HTMX server, which would add a second UI stack
  and duplicate implementations boxer already has.

### Milestones

- **M1 — `factsschema` aspects (SD4).** Regenerate the four artifacts.
- **M2 — `public/gov/capmapcorpus`.** Vault parsing as a pure library; blake3
  natural keys; strip the retired build tags; fix the two drifted assertions.
- **M3 — the `capmap` vocabulary (SD6).**
- **M4 — ingest.** `boxer capmap ingest --vault`, writing through the facts
  store interface so it works with or without a live ClickHouse.
- **M5 — providers (SD8).**
- **M6 — the applet book (SD9).**
- **M7 — corpus and ignore rule (SD7).**

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `boxer.facts` column encoding (`factsschema`) | Reshaped — two sections gain value aspects, one gains a heavier codec | The four generated artifacts under `factsschema/`: `ddl/`, `dml/`, `dml_cbor/`, `ra/`, all via `boxer runtimecodegen` |
| `boxer.facts` row vocabulary | Added — two fact kinds and their memberships | The new `capmap` vocabulary package; the providers that decode them |
| TagValue allocation | Added — a second allocation, and the rule for making further ones | This ADR is the register; the vocabulary package's doc comment cites it |
| Environment-variable registry ([ADR-0009](./0009-environment-variable-registry.md)) | Added — the corpus location | `doc/env-vars.md`; the package must be reachable from the `public/app` link graph or the spec stays invisible |
| keelson table-name namespace ([ADR-0094](./0094-keelson-introspection-tables.md)) | Added — the capability and relation tables | The provider registration; the applet book's queries |
| Exported Go API under `public/` | Added — `public/gov/capmapcorpus` and the vocabulary | Nothing yet; no downstream module compiles against them |

## Alternatives

- **Bespoke Arrow tables, as `boxer adr` does.** Rejected: it would model a
  corpus of relations as flat tables with parallel array columns, when the facts
  model already carries memberships and a relation channel built for exactly
  this.
- **Lift the HTMX application in whole.** Rejected: fastest to value, but adds a
  second UI stack and duplicate treemap, colour-scale and predicate-validation
  implementations alongside the ones boxer has.
- **A new leeway table rather than `boxer.facts`.** Rejected in the QOC above —
  scope purity at the cost of the join that motivates the port.
- **Facts authoritative, vault exported.** Rejected: the corpus stops being
  reviewable as a diff.
- **A parsed markdown AST in separate attributes.** Rejected in SD5: breaks
  round-tripping and multiplies rows without a query to serve.

## Consequences

### Positive

- Capability data joins runtime self-knowledge in one table.
- No leeway table-description extension is needed; the array sections and the
  relation channel already exist.
- The schema's `text`/`string` naming becomes true, and prose gets a codec
  suited to prose.
- The corpus lint becomes a query over relation facts rather than a scan in Go.
- Roughly 4,650 lines of the prototype are replaced by facilities boxer already
  has, rather than carried.

### Negative

- `boxer.facts` now holds content that is not process state, which is a real
  widening of its meaning and is why ADR-0026 §SD6 gets an Update rather than a
  silent reinterpretation.
- SD4 renames physical columns, so the table must be rebuilt (§Migration).
- Ingest against a live store needs ClickHouse, pushing part of the test surface
  into the integration lane.
- The prototype's four webapps have no equivalent on day one; the treemap in
  particular has no play panel, and that gap is real until its own decision is
  taken.

### Neutral

- `capmap` joins `capslock` and `capabilitygrant` in a namespace where "cap"
  now abbreviates two unrelated things. SD6's prefix rule keeps the membership
  names apart; the package names carry the rest.
- The corpus is present but unversioned, so a contributor's working tree and CI
  see different amounts of data. Providers are empty rather than erroring when
  the corpus is absent, which is the behaviour the ADR providers already
  established.

## Migration — Tier 1

- **Breaks.** SD4 changes the value-aspect and encoding-aspect segments of two
  physical column names in `boxer.facts` — the aspect bitmask is encoded in the
  name — so the existing table's columns no longer match the generated DDL.
  Go-level section accessors are unchanged, but **hand-written read-back SQL
  does break**: `chstore` and `queryrunfacts` spell physical column names out as
  string constants rather than deriving them, and no generator touches those.
  Six constants across `chstore/recentlogs.go`, `chstore/workingsets.go`,
  `chstore/runsessions.go` and `queryrunfacts/readback.go` were stale after the
  M1 regeneration; 106 such hardcoded names exist under `keelson/runtime`, so
  any future aspect change must expect the same.
- **Path.** Regenerate; fix the hand-written constants the guard test names;
  then drop and recreate `boxer.facts` from the emitted DDL and re-ingest.
  Existing rows are development-stage data, and
  [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md) §SD8
  already set the precedent that in-repo encodings are re-ingested rather than
  dual-decoded.
- **Regeneration.** `boxer runtimecodegen` for all four artifacts under
  `factsschema/`. No FFI boundary is involved, so nothing on the Rust side
  rebuilds. When `public/app` does not build, the four `codegen.Generate*`
  entry points can be driven directly — they take an output path and import
  nothing from the app.
- **Old shape.** Removed outright at M1. There is no dual-decode path and none
  is planned.

## Verification plan — Tier 1

- **Lane.** Default `go test` for corpus parsing, membership encode/decode
  round-trip, and the provider schema-parity test. The `//go:build integration`
  lane for anything needing a live ClickHouse — ingest and the end-to-end read
  path. The applet corpus gate for the book.
- **What would fail.** Parity between the ingested membership encoding and the
  providers' decoded columns is the load-bearing observable: if the vocabulary
  and the decoder drift, the provider returns wrong or empty columns, and the
  parity test goes red. Vault round-tripping is pinned by a parse-render test,
  which is what would catch SD5 being violated by a future AST decomposition.
  The default lane must stay green with no corpus and no ClickHouse present.
  For SD4's blast radius specifically,
  `TestHandwrittenColumnsMatchGeneratedSchema` (added by M1, in
  `factsschema/ddl`) scans `keelson/runtime` for hardcoded physical column
  literals and asserts each still exists in the generated block. It was
  necessary because the tests that would otherwise catch the drift call
  `t.Skipf` when ClickHouse is unreachable — so before it, a rename shipped
  green on any machine without a live server and failed at runtime against a
  re-created table. The guard needs no ClickHouse and fails vacuously-passing
  by asserting it found something to check.
- **Gap.** The serverless read path — facts-shaped Arrow through `file()` — has
  been reasoned about but not run; proving it is a step inside M4 rather than a
  standing lane. Nothing verifies that the corpus content is *correct*, only
  that it parses, encodes and decodes; capability maturity and pain scores are
  human judgements with no oracle.

## Deferrals

Each carries a trigger rather than a date.

- **`bool` → `boolArray`.** Trigger: a facts writer with a genuine array of
  booleans.
- **`anchor`'s `text` co-section, and the `symbolArray` low-cardinality
  disagreement between the two schemas.** One question, since both are "make the
  two schemas agree". Trigger: a consumer that wants `wordBag` — the
  compression-similarity ranker recomputes exactly that at query time.
- **Graph value-aspects on `foreignKey`.** Trigger: a reader that dispatches on
  them. `useaspects.AspectLinking` already states the linking intent.
- **The triage/culling workflow.** A UI that mutates repo files is a distinct
  security posture and needs its own decision. Trigger: the read path proving
  the corpus is worth curating at that rate.
- **A treemap panel for play.** Not deferred by this ADR — a play Treemap panel
  is being decided separately in ADR-0166, in flight at the time of writing.
  This corpus is a candidate consumer: a size-and-maturity hierarchy where path
  order does not matter is the motivation
  [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) said a treemap would
  need. If that panel lands, SD9's book gains a hierarchy lens; if it does not,
  the book is unaffected.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [Background survey](../adr-background-work/capmap-port.md) — measurements and the reasoning behind each fork.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD6 — the `boxer.facts` table this widens.
- [ADR-0092](./0092-adr-overview-tool.md) — the corpus-as-tables shape being copied.
- [ADR-0109](./0109-leeway-marshall-multi-membership-ref-tuples.md) — multi-membership ref tuples, the relation carrier.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — applet books.
- [ADR-0148](./0148-app-workingsets.md) — the data-centricity invariant and the provider precedent.
