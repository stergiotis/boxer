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
hand-written verb per kind — `WriteGrant`, `WriteAudit`, `WriteLog`,
`WriteWorkingset`, `WriteColumnWidth`, and the run-lifecycle family — each
hand-encoding the leeway DML, and hand-composed SQL for the read-backs. It is
production-wired (the imzero2 host app, `apps/capinspector`) and it is not
going away on a schedule. (App persist state used to be a verb here too; it
left for its own generated store, `boxer.persiststate`, and the verbs were
removed — ADR-0105 D3a and its 2026-08-15 Update.)

**A facts-bound record store** is generated: you write a DTO, register your
memberships in a vocabulary, and a generator emits the ingest, scan and decode
code. [ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md) §D5 is
the standing policy — *"the next kind or schema change lands as a generated
store behind the existing `chstore.Store` facade"*. So for a **new** kind, the
generated store is the default answer, and `chstore` is where you look only if
the generated lane refuses your shape.

It can refuse. `marshallgen.ReadRowSupported` still declines two shapes, and
they are the ones the largest existing kinds use: **carrier channels** (the log
kind passes the field name as a runtime membership parameter) and
**dynamic-membership tuples** (the run-anchored lifecycle rows). A DTO built
from plain scalar and `unit` shapes avoids both by construction. That is worth
knowing before you design the DTO rather than after.

The generated lane also cannot currently take over two things `chstore` does,
for reasons that are properties of the facts schema rather than of the
generator:

- **Keyed point lookup.** The store keys on the leading `EntityId`, which in
  `boxer.facts` is `id` — a per-process counter. The access identity (the
  blake3 `naturalKey`) is the *second* `EntityId`, and explicit role election
  is an open [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md)
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
| DDL | the `TableDesc` | identical; `chstore` owns applying it |
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
  The `.sql` file is still written: it is the physical schema the store decodes
  positionally, and reviewing it is how you see what the store expects.
  `VerifySchema` matters more here than elsewhere for the same reason — nothing
  in the store guarantees the live table's shape.

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
[`recordstore/sharedsection`](../../public/storage/recordstore/sharedsection)
and [`recordstore/example`](../../public/storage/recordstore/example) are
driven. `keelson/runtime/sysmfacts` is the one facts-bound store in the tree
and the worked example to copy: a DTO file per kind, a membership vocabulary
beside it, a `gen_test.go` calling `storegen.Input{...}.Generate()`, and a test
file asserting the ids landed.

The one seam worth understanding before you start is
`factsschema/storegen.MembershipIds`. A store bakes membership ids into its
scan filter SQL at *generation* time, so they must come from the same
vocabulary the writers use; an id baked from any other source matches nothing
on read, silently. Registries spell natural keys lower-spinal and DTO tags
spell them lowerCamel, and that function is the single place the two are
reconciled.

## Where the decisions live

- **ADR-0100** — the generator itself: what a store emits, the `internal/lowlevel`
  layout, and the frame-control wall.
- **ADR-0105** — why keelson adopts generated stores by milestone rather than by
  refactor, and why app persist state left `boxer.facts`.
- **ADR-0184** — the first facts-bound store, and why it cannot run
  its own DDL.
- **ADR-0183** — the component-authoring surface these DTOs are
  written against.
