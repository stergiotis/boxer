---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ecsdemo — Entity-Component-System, and how this example realizes it

`ecsdemo` is a small, didactic Entity-Component-System used as a two-stage
example under `anchor/`: stage 1 (the `stage1/` subpackage) serializes the model
with `encoding/json/v2`; stage 2 (the `stage2/` subpackage) expresses the same
model through a bespoke leeway `TableDesc`, a `marshallgen` codec, and a real
`clickhouse-local` roundtrip. The mechanics of the two stages live in the package
doc comments of `stage1/` and `stage2/`. This file supplies the *why* behind
them: what ECS is, and how the types embody it. It assumes a technical reader who
has not necessarily met ECS before.

## Background

The following ECS primer is reproduced, with light copy-editing, from Sander
Mertens' **ECS FAQ** (<https://github.com/SanderMertens/ecs-faq>); see that
repository for its license. It is general background, not specific to this
package — the mapping onto `ecsdemo` follows in *How it works*.

### What is ECS?

ECS ("Entity Component System") describes a design approach which promotes code
reusability by separating data from behavior. Data is often stored in
cache-friendly ways which benefits performance. An ECS has the following
characteristics:

- It has entities, which are unique identifiers
- It has components, which are plain datatypes without behavior
- Entities can contain zero or more components
- Entities can change components dynamically
- It has systems, which are functions matched with entities that have a certain
  set of components.

The ECS design pattern is often enabled by a framework. The term "Entity
Component System" is often used to indicate a specific implementation of the
design pattern.

### When is something an ECS?

The most rigid interpretation of an ECS is something that has entities,
components and systems, according to the definitions in the previous question.

In practice ECS is used a bit more liberally. Some ECS frameworks do not have
systems, and only provide methods for querying entities. Other frameworks may
allow for adding things to entities than are not components. These
implementations are still considered ECS by many people.

A framework that lets you add "things" to entities, with a way to query for
entities that have some things but not other things, is generally considered to
be an ECS.

### How is ECS different from OOP?

ECS is often described as an alternative to Object Oriented Programming. While
ECS and OOP overlap, there are differences that impact how applications are
designed:

- Inheritance is a 1st class citizen in OOP, composition is a 1st class citizen
  in ECS.
- OOP encourages encapsulation of data, ECS encourages exposed POD (plain old
  data) objects.
- OOP colocates data with behavior, ECS separates data from behavior.
- OOP Object instances are of a single static type, ECS entities can have
  multiple, dynamically changing components.

It should be noted that some have argued that ECS fits the characteristics of
Object Oriented Design (see
<https://www.gamedev.net/blogs/entry/2265481-oop-is-dead-long-live-oop/>) and
should therefore be considered a subset.

However, in practice the design process of an ECS application is sufficiently
different from that of what most people would recognize as OOP. As such it is at
least useful to approach it as a separate approach towards design.

### Can ECS be used outside of gaming?

Yes. It can be (and has been) used for projects outside of gaming.

### What are the different ways to implement an ECS?

There are many different ways in which to implement an ECS, each with different
tradeoffs. This non exhaustive list contains some of the more popular approaches:

**Archetypes (aka "Dense ECS" or "Table based ECS").** An archetype ECS stores
entities in tables, where components are columns and entities are rows. Archetype
implementations are fast to query and iterate. Examples are Flecs, Our Machinery,
Unity DOTS, Unreal Sequencer, Unreal Mass, Bevy ECS, Legion, Hecs and Ark.

**Sparse set ECS (aka "Sparse ECS").** A sparse set based ECS stores each
component in its own sparse set which has the entity id as key. Sparse set
implementations allow for fast add/remove operations. Examples are EnTT and
Shipyard.

**Bitset based ECS.** A bitset-based ECS stores components in arrays where the
entity id is used as index, and uses a bitset to indicate if an entity has a
specific component. Different flavors exist: one has an array per component with
an accompanying bitset to indicate which entities have the component; another
uses the hibitset data structure (see link). Examples are EntityX and Specs.

**Reactive ECS.** A reactive ECS uses signals resulting from entity mutations to
keep track of which entities match systems/queries. An example is Entitas.

### Glossar
* **Entity** An entity in ECS represents a single "thing" in a game and is generally represented as a unique integer value. 
* **Component** A component is a datatype that can be added to or removed from entities. Components in ECS are generally plain data types and not encapsulated. 
* **Tag** A tag is a component that has no data.
* **System** A system is an executable object that is matched with all entities that have a certain set of components.
* **Query** A query is similar to a system, but cannot be executed by itself.
* **World** A world is the container for all ECS data. ECS frameworks often allow a single application to have multiple ECS worlds.
* **Registry** Same as world.
* **Archetype** A data structure that stores entities for a specific set of components. Components are stored as columns in contiguous arrays.

## How it works — one model, two stages

Both stages serialize the *same* ECS model — entities (`EntityID`), id-free
components (`Identity`, `Battery`, `Located`, `Tasked`), an archetype = the set of
components present — and answer one question, "can this document be unserialized
into this shape?", as the same trichotomy (mirroring [ADR-0066](../../../../../doc/adr/0066-leeway-dql-clickhouse-readback-generator.md)):

| ECS concept             | stage 1 — `stage1` (json/v2)         | stage 2 — `stage2` (leeway → ClickHouse)   |
| ----------------------- | ------------------------------------ | ------------------------------------------ |
| entity = id             | `EntityID` (World key / `Entity.ID`) | `id` plain column                          |
| component = POD         | `Identity` … `Tasked` (id-free)      | a section / sub-column bundle              |
| storage                 | `World` (a map per component)        | a columnar Arrow table (bespoke `TableDesc`) |
| gather one entity       | `World.Gather` → `Entity`            | `FatRow.Extract[T]` over one row           |
| archetype = present set | `Entity.Components()`                | `FatRow.Archetype` (RA population counts)  |
| approximate check       | `Presence` / `ArchetypePresence`     | readback `presence` SQL                    |
| exact check             | `Validate` / `ArchetypeValidate`     | readback `validator` SQL                   |
| projection              | `Unmarshal`                          | readback `projection` SQL                  |

The approximate check is a *necessary, not sufficient* sub-computation of the
exact one, at both the per-component and archetype level.

Stage 2 carries this end to end: a fat `DroneEntity` is marshalled (the
`marshallgen` codec) to Arrow, the readback presence/validator/projection run in
`clickhouse-local`, and every row reads back — all five sections, including the
multi-sub-column `geoPoint` and `timeRange`. The fat row is then split back into
the four typed components via `Extract[T]`. `stage2/cross_test.go` asserts the two
stages return the *same* verdict on corresponding data, in both directions.

## Complexity — the inversion

|                  | stage 1                 | stage 2                          |
| ---------------- | ----------------------- | -------------------------------- |
| hand-written Go  | ~520 LOC                | ~320 LOC                         |
| generated Go     | none                    | ~4,100 LOC (DML / RA / codec)    |
| external deps    | stdlib + `eh`           | Arrow + ~18 leeway packages      |
| runtime          | `go test`, nothing else | also needs `clickhouse-local`    |

Stage 2 writes *less* bespoke code, yet its cognitive load is far higher: it
rides the leeway pipeline (`TableDesc` → DDL/DML/RA codegen → Arrow layout →
readback SQL), a large generated-code body you must trust, the `lw:` tag grammar
and membership ids, and an external SQL engine. Stage 1's complexity is
self-contained — every line is in those ~520, it runs anywhere under `go test`,
and the one idea is "reflect the struct, scan the json". Stage 2 trades that
transparency for production realism (columnar storage, a real SQL engine) and an
open, codegen-free read path (`marshallreflect.Unmarshal`).

## Invariants

- An entity owns no data of its own; it is only an id. All data lives in
  components.
- A component is plain data with no behavior, and never names its entity id (the
  id is the join key, held by the storage, not the component).
- An entity "has" component `C` iff its id is present in `C`'s store (stage 1:
  the map key is present, equivalently the `*C` field on the gathered `Entity` is
  non-nil).
- The approximate check is a *necessary condition* for the exact check, at both
  granularities: `Presence[C]` ⊆ `Validate[C]` and `ArchetypePresence` ⊆
  `ArchetypeValidate`. If the approximate check fails, the exact check is
  guaranteed to fail; the converse does not hold.
- `Gather` after `Scatter` reproduces the same set of present components
  (round-trip on composition).

## Further reading

- Source of the primer: ECS FAQ — <https://github.com/SanderMertens/ecs-faq>
- "OOP is dead, long live OOP" —
  <https://www.gamedev.net/blogs/entry/2265481-oop-is-dead-long-live-oop/>
- Decisions: [ADR-0066: leeway DQL ClickHouse read-back generator](../../../../../doc/adr/0066-leeway-dql-clickhouse-readback-generator.md)
  — the Presence / Validator / Projection trichotomy the unserializability checks mirror.
- Reference: the `stage1/` and `stage2/` package doc comments, and
  <https://pkg.go.dev/github.com/stergiotis/boxer/public/semistructured/leeway/anchor/ecsdemo/stage1>
