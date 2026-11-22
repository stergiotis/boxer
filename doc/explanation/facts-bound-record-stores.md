---
type: explanation
audience: contributor adding a new fact kind to boxer.facts
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** This page describes what exists today.
> Where it and an ADR disagree, the ADR is the record.

# Facts-bound record stores

If you are about to persist a new kind of fact to `boxer.facts`, this page is
the one to read first. It exists because the obvious search lands in the wrong
place: `keelson/runtime/factsstore` is the *hand-rolled* interface and its
ClickHouse implementation, and its name gives no hint that a generated
alternative exists or that the generated one is the direction of travel.

## Two ways to write the table, and which one to pick

**`factsstore.FactsStoreI` / `chstore`** is the older lane. It carries one
hand-written verb per kind — `WriteGrant`, `WriteLog`, and the run-lifecycle
family (the bus-request `WriteAudit` retired onto the trail's `AuditEvent`
on 2026-10-30, ADR-0296 Updates) — each hand-encoding the leeway DML, and
hand-composed SQL for the read-backs. It is production-wired (the imzero2 host
app, `apps/capinspector`) and it is not going away on a schedule. (App state
used to be verbs here too — persist values, workingsets and column widths. It
left for its own generated store, `boxer.persiststate`, and the verbs were
removed — ADR-0105 D3a and its 2026-08-15 Update. A kind that is state rather
than trail belongs there, not on this table.)

**A facts-bound record store** is generated: you write a DTO, register your
memberships in a vocabulary, and a generator emits the ingest, scan and decode
code. [ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md) §D5 is
the standing policy — *"the next kind or schema change lands as a generated
store behind the existing `chstore.Store` facade"*. So for a **new** kind, the
generated store is the default answer, and `chstore` is where you look only if
the generated lane refuses your shape.

It can refuse. `marshallgen.ReadRowSupported` declines two shapes, and they are
the ones the largest existing kinds use: **carrier channels** (the log kind
passes the field name as a runtime membership parameter) and
**dynamic-membership tuples** (the run-anchored lifecycle rows). The refusal is
not a missing method — `recordstore/gen` turns it into an error for the whole
store, so one unsupported kind stops the others generating.

A third refusal fires **earlier**, and is the one to check first because it is
about a shape the hand-written DML has always allowed. A plan holds **one
membership channel per section** (ADR-0008 D3's uniformity invariant), so a
section carrying a mixed-channel attribute *beside* plain ones has no plan at
all: `marshallreflect.PlanFor` fails with *"section mixes membership channels —
pick one channel per section"* before any read gate is consulted. Nothing stops
an encoder writing that shape, and a hand-written read reads it back, so the
constraint only surfaces when someone tries to describe those rows as a
component. `gov/capmapfacts` writes exactly it — its `symbol` section carries
the kind marker, slug and tags on the plain channel and a lifecycle membership
on the mixed one — and that is why its write side is still hand-rolled.

A DTO built from plain scalar and `unit` shapes avoids all three by
construction, and a container (`[]string` under one membership on a scalar
section) is fine. That is worth knowing before you design the DTO rather than
after.

### Two layers refuse, and they are not the same refusal

Worth separating, because "this kind cannot be a component" and "this kind
cannot be a generated store" are different claims and only the first is about
expressiveness:

- **`marshallreflect.PlanFor` refuses** ⇒ there is no plan, so the shape is not
  a component on any path. The channel-uniformity rule above is here.
- **`marshallgen.ReadRowSupported` refuses** ⇒ the plan is fine and the reflect
  path reads it; what is unavailable is the *generated* read — `<Kind>ReadRow`,
  and therefore a store. Carrier channels and dynamic-membership tuples are
  here.

So a flat DTO carrying a mixed channel alone on its section is a perfectly good
component today. It just cannot be a store, and that is the ADR-0100 deferral
rather than a modelling verdict.

### Isn't there a typed shape for dynamic memberships?

There is — the dynamic-membership tuple (ADR-0103, extended by ADR-0109): a
slice-of-struct field whose elements each carry their own membership through an
`@membership` field. It is the right reach for "one attribute per label", and
`anchor/codecdemo/labeledtextdoc.go` is the worked example.

Two properties decide whether it helps you, and both bite before the store gate
does:

- **`@membership` takes only the four simple channels.** A carrier channel is
  refused outright — *"`@membership` cannot use a carrier / parametrized
  channel — its identity is per-row carrier data, not an element field"*. So a
  tuple cannot express what `AddMembershipMixedLowCardRef` writes. It is an
  alternative *encoding* of the same intent (the label becomes a verbatim
  membership name, or a ref id), not a way to describe existing mixed rows.
- **A tuple monopolises its section** — *"section is mapped by a tuple field —
  no other field may target it"*. This is the section-scoping property
  ADR-0183 M3 recorded, showing up at plan-build time. If the section also
  carries static memberships, they have to move or be given up: `chstore`'s log
  fan-out shares `stringArray` with `runtimeLogMessage` and `runtimeLogError`,
  so a tuple there is not available without re-siting those two.

And the tuple is refused by `ReadRowSupported` in any case, so it buys a
component on the reflect path, never a store.

### Know which question the mixed channel is answering

The mixed channel is what costs a kind the generated lane, so it is worth being
precise about what it buys. A `mixedLowCardRef` carrier is the `(Id, Params)`
pair, so the parameter is part of the **membership's identity** for that
occurrence — `(runtimeApp, "data.play")` and `(runtimeApp, "other.app")` are
two memberships, not one membership seen twice. That is a real modelling
statement, and it is never merely decorative.

What differs across `boxer.facts` is whether the attribute's **value** carries
something else:

- **The parameter discriminates, the value carries data.** A log field's
  *name* is the parameter and its *value* is the attribute
  (`runtimeLogField`, across seven sections); likewise a ProfileEvents
  counter's name against its count, and capmap's authored section heading
  against its prose. Both lanes are load-bearing. There is no other spelling,
  and these are why the carrier deferral exists.
- **The parameter and the value hold the same bytes.** `runtimeApp`,
  `runtimeRun` and `runtimeLaunchCaller` are written
  `BeginAttribute(X).AddMembershipMixedLowCardRef(memb, []byte(X))` by all nine
  `chstore` kinds and by `queryrunfacts`, so `X` sits in the value lane *and*
  in `mrhp`. Nothing is lost by carrying it in one lane only — so here you have
  a genuine choice, and it is a modelling one.

The markdown frontmatter row (ADR-0218) is the first case taken
deliberately in a package that also has a generated store: the leaves ride the
mixed channel on a row of their own, written through the entity builder's
`Raw()`, beside the document row the store writes — because `Raw()` and
`Add<Kind>()` are exclusive per entity, a hand-written channel cannot share a
row with a component. That split is the shape to copy when a kind has both a
schematised half and an open-world half (2026-09-02).

If you are adding a kind, that second case is the decision to make
deliberately: **is this identity a membership discriminator, or an attribute
value?** Discriminator means attribution is part of the row's membership
structure, and costs you the component lane until the carrier deferral lifts.
Attribute value on the plain channel keeps the kind expressible today, and
`LW_GET('symbol', '<name>', 'chan:low-card-ref')` reads it back cheaply. Decide
it on the modelling question; do not arrive at the mixed channel by copying a
neighbouring kind.

The generated lane also cannot currently take over two things `chstore` does,
for reasons that are properties of the facts schema rather than of the
generator:

- **Keyed point lookup.** The store keys on the leading `EntityId`, which in
  `boxer.facts` is `id`. Each writer chooses it: `chstore` leases one per row
  from a tagged stream ([ADR-0397](../adr/0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)
  §SD2), the trail hashes its key, and a row written while no block could be
  leased carries an id unique only within its process. A key a store reads
  by is therefore its own writers' convention, not the table's. The access
  identity (the blake3 `naturalKey`) is the *second* `EntityId`, and explicit
  role election is an open [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md)
  deferral.
- **Latest-wins reads.** The generated state view needs a `u8` lifecycle
  column; the facts table's lifecycle lane is `DateTime64`. This is why
  ADR-0105 §D3a moved app persist state off `boxer.facts` onto its own table
  rather than adding the column.

Append-shaped kinds — the ones that only ever ingest and scan — are the ones
that fit today.

## What a store is made of

The split that matters, because it decides what can be shared and what cannot:

| Emitted artifact | Derived from | Consequence |
| --- | --- | --- |
| DML / read-access scaffolding | the `TableDesc` and row config **alone** | identical for every store over the table |
| DDL | the `TableDesc`, and the facts layout's engine clause | identical; `chstore` owns applying it |
| Per-kind codec (`<kind>_dto.out.go`) | one DTO plus its membership ids | your domain's content |
| The store (`<store>_store.out.go`) | the whole component set | *is* the composition |

A store is nothing but its components: one `option.Option[T]` entity field,
one `Ingest<Kind>` and one `Scan<Kind>` per kind, over one baked membership-id
map. Nothing in it is table-generic. So **two domains cannot share a store** —
they can only become one store, by generating over both DTO sets, which makes
them one entity type. Whether that is right is a modelling question about your
data, not a question about the generator.

The corollary is that each domain gets its own package, and that is fine:
because the scaffolding is table-derived, it is the part that can be shared
instead of copied.

## What is shared, and what is not

`boxer.facts` is unusual among leeway tables in that its scaffolding already
exists independently — `factsschema/dml`, `factsschema/ra` and
`factsschema/ddl` predate the record-store generator and serve the bus codecs
and the hand-written writers. A store binding this table would otherwise emit
a second copy of all three.

- **Read access is shared.** `gen.Input.SharedRA` binds `factsschema/ra` — the
  same package `codec/factswrapper` emits into every keelson wire codec, so a
  store and a codec decoding the same row agree on what its columns are by
  construction rather than by regeneration discipline.
- **The DML is not shared.** Its entity-frame control surface is walled by the
  `internal/lowlevel` import barrier (ADR-0100 §SD6): the store owns the frame,
  and a holder of `Raw()` can touch attributes but cannot open, commit, drain
  or re-key it. `factsschema/dml` exports that surface, because the
  hand-written writers drive frames directly. Binding it would drop the wall,
  which is a decision for ADR-0100 rather than a default.
- **The DDL is emitted but never applied.** `chstore.SetupTable` is the sole
  DDL author for this table, so a facts-bound store carries no `EnsureTable`,
  no embedded DDL string and no DDL tail — not a discouraged method, an absent
  one ([ADR-0184](../adr/0184-sysmetrics-persistence-tee.md) §SD2).
  The `.sql` file is still written: it is the physical schema the store reads,
  and reviewing it is how you see what the store expects. Reads select those
  columns by name and bind the decoders to the result by name
  ([ADR-0387](../adr/0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md)),
  so a facts table carrying columns another store added reads fine, and one
  lacking a column this store reads fails naming it. `VerifySchema` (the
  verdict of `JudgeSchema`, as an error) matters more here than elsewhere for
  the same reason — nothing in the store guarantees the live table's shape.

## Running a store over a fake or an in-memory executor

Two properties of the executor seam catch code that runs a store over
anything but a ClickHouse server
([ADR-0387](../adr/0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md)):

- Judging a table reads `system.tables` and `system.columns`. An executor that
  answers no catalog read makes the table look absent, so `VerifySchema`, and
  any check that requires an ok verdict before it opens a store, refuses.
  `recordstoretest.CatalogExecutor` remembers the `CREATE TABLE` statements
  it is sent and answers the two reads from them.
- A store inserts through `recordstore.InsertArrowWith`, which prefers an
  executor's own `InsertArrowWith`. A wrapper that embeds a concrete executor
  and overrides only `InsertArrow` is bypassed by the promoted
  `InsertArrowWith`. `recordstoretest.HookExecutor` forwards every seam to an
  inner executor and routes both insert seams through whichever insert hook
  is set.

## Two regeneration lanes, one schema

`factsschema`'s own artifacts regenerate **only** via `boxer runtimecodegen
all`; there is no `//go:generate` for them. A store's artifacts regenerate from
a gen-test, which plain `go test ./...` and `go generate ./...` both sweep.

So a leeway aspect-vocabulary change — which renames physical columns — moves
one lane and not the other unless you run `scripts/dev/generate.sh`, which
invokes both in the right order. Both lanes compile either way. Run the script,
not `go generate` alone.

## How to add one

There is no CLI. The lane is a gen-test in the target package, the way
[`recordstore/sharedsection`](../../public/storage/recordstore/internal/sharedsection)
and [`recordstore/example`](../../public/storage/recordstore/internal/example) are
driven. `keelson/runtime/sysmfacts` was the first facts-bound store and is
the worked example to copy — the same lane drives `watchbill/watchbillpresence`,
`runtime/trail`, `lading/ladingpolicy`, `markdown/mddocfacts` and
`stevedore/stevedorefacts`: a DTO file per kind, a membership vocabulary
beside it, a `gen_test.go` calling `storegen.Input{...}.Generate()`, and a test
file asserting the ids landed.

Every row on the table says who wrote it through one shared component,
`audit.Origin` — run, app and instance on the runtime vocabulary's
memberships ([ADR-0277](../adr/0277-one-audit-trail-for-model-calls-and-agent-work.md),
Update of 2026-10-30). A store binds it beside its own DTOs through an
alias, as those packages do, adding its ids with `storegen.AddOriginIds`
when its vocabulary is not the runtime one; a hand-written lane writes the
same three slots, `""` and `0` when unattributed.

A deployment may read facts from a relation other than the table written to
— a view over its own storage — named by `BOXER_FACTS_READ_TABLE`
([ADR-0402](../adr/0402-facts-stored-in-a-deployments-own-table-a-write-transform-at-the-insert-seam-and-one-read-table-setting.md)).
A store generated against the facts table looks the setting up in its
constructor when its config names no `ReadTable`, so its scans and its
schema check follow it; a `ReadTable` the builder does pass wins, and a
store pointed at another table reads that table. The setting is validated
there, and a value that cannot be used panics — a binary that would rather
return an error calls `factsschema.ReadTable` first. The writes need
nothing: the executor applies an insert transform the binary registered.

The one seam worth understanding before you start is
`factsschema/storegen.MembershipIds`. A store bakes membership ids into its
scan filter SQL at *generation* time, so they must come from the same
vocabulary the writers use; an id baked from any other source matches nothing
on read, silently. Registries spell natural keys lower-spinal and DTO tags
spell them lowerCamel, and that function is the single place the two are
reconciled.

## What the `id` column holds

A row of the facts table carries an id minted from the table's stream:
one per entity where the writer's rows share an entity, one per row where
they do not
([ADR-0405](../adr/0405-every-id-in-the-facts-table-is-minted-and-a-natural-key-resolves-to-one-id-on-the-server.md)).
Two writers keep an id of their own, and rows written before the decision
keep what they had. Read this before you choose a key
for a new store, and before you treat an `id` as the name of a row.
Checked against the tree on 2026-11-22.

| Writer | What it puts in `id` now | Before ADR-0405 |
| --- | --- | --- |
| the hand-written facts store (`chstore`) | an id per row: leased from `factsRow`, or from `factsRowLocal` while no block can be leased (ADR-0397 §SD2) | the same |
| system metrics | an id per (host, domain) series, resolved from the series' natural key | xxh3 of host and domain |
| the watchbill presence | an id per run, resolved from the run id | a hash of the run id |
| dead letters | an id per message position, resolved from the position's digest | a digest of the position |
| the trail, markdown documents | an id per row, the next of the stream | a hash of the row's key, or of content and ingest time |
| the capability map | an id per competence and per relation, resolved from the natural key | the leading bytes of the natural key's digest |
| query runs | a digest with the top bit set (`queryrunfacts.IdBand`), which no minted id carries; kept by decision (ADR-0405 §SD8) | the same |
| the lading mount policy | the mount's tagged id, minted elsewhere under a tag the app declares; one per mount, a row per version | the same |

What follows from it:

- **"Resolved from the natural key" is a lookup, not arithmetic.** The id
  directory, a table beside the lease table, holds which key has which id;
  the same key gives the same id from every process. A writer is handed
  the keyed generator for its scope (`chstore.KeyedIdsOf`, or
  `chstore.NewKeyedIdGenerator` for a process without a facts store), a
  reader the lookup (`chstore.NewKeyedIdLookup`), which gives no key an
  id.
- **A store over the facts table is handed its generator.** Regenerated,
  it has `Keys` in its configuration, and `MintKeyFor` resolves a row's id
  from the row's natural key through it. Hand it the keyed generator and
  the rows of an entity share an id; hand it the sequential one
  (`chstore.RowIdsOf`, `chstore.NewRowIdGenerator`) and every row has its
  own. A store built without one refuses to write.
- **Choose by what a row is.** If two rows can be about the same thing and
  a reader will want them together, the store is keyed and needs a scope
  of its own. If every row is an event, it is sequential, and costs the
  directory nothing.
- **The directory is state.** It is backed up and moved with the facts
  table. Without it the facts are all there and no key resolves to the id
  its older rows carry.
- **Rows from before are still there.** Their ids are hashes that can, by
  the odds of 64 bits, equal another id. A reader of an entity takes the
  hash beside the resolved id, as the metrics replay does.
- **The tag of an id means something only for a minted one.** A hash
  parses as *some* tag too. Tell rows apart by kind first.

## Where the decisions live

- **ADR-0100** — the generator itself: what a store emits, the `internal/lowlevel`
  layout, and the frame-control wall.
- **ADR-0105** — why keelson adopts generated stores by milestone rather than by
  refactor, and why app persist state left `boxer.facts`.
- **ADR-0184** — the first facts-bound store, and why it cannot run
  its own DDL.
- **ADR-0183** — the component-authoring surface these DTOs are
  written against.
- **ADR-0402** — facts stored in a deployment's own table: the
  insert transform and the read table.
