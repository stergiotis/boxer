---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> **Provenance.** Compiled 2026-09-09 from Kubernetes' public documentation,
> one archived design document, and the community API conventions (see §10
> Sources). No Kubernetes source code was read; every statement about internal
> mechanism is a reconstruction from documented behaviour. Kubernetes is
> Apache-2.0-licensed, so this clean-room posture is not an IP firewall — it is
> a disclosure of how far the claims below were verified.

# Lessons from Kubernetes' architecture

**Kubernetes in data-engineering terms.** Kubernetes runs programs on a fleet
of machines, but its shape is closer to a data system than to a scheduler. At
its centre is one database of typed records, each written as a small YAML or
JSON document with a `spec` that says what should exist — "three copies of
this container image, with this much memory" — and a `status` that says what
does. Users and programs alike write those records through one REST API;
nothing else is an interface. Around the database run many small workers,
called controllers, each subscribed to a change stream on one record type,
each doing the same thing forever: read the current records, compare desired
to actual, take a step that closes the gap, repeat. Starting a container,
assigning it to a machine, or opening a network port is what a controller
does *after* reading a record, never a command anyone sends directly. The
consequence for a data engineer is that a whole cluster is a table you can
query, a change stream you can subscribe to, and a set of idempotent
consumers you can add to — and that "deploy" means "insert a row".

Kubernetes is usually read as a container orchestrator. Read from this
repository's premises — data outlives the behaviour attached to it, logic is a
system that matches shapes in the data and emits more data
([why-boxer P3](./why-boxer.md#p3--one-machine-readable-data-spine)) — it is
something narrower and more transferable: the largest production deployment of
an entity-component-system over a single typed record store. Objects are
entities, `spec`, `status`, labels and annotations are components, controllers
are systems, and the API server is the world. The scheduling is incidental.
This note records the properties of that shape that survive being lifted out
of the container domain, the places they bear on this repository, and the
places they do not. It is a companion to
[rclone-architecture-lessons](./rclone-architecture-lessons.md), which does the
same for an interface rather than a store.

## 1. The store is the interface

Kubernetes describes itself as *"not just API-driven, but API-centric"*: the
API server implements the common machinery, and user clients and controllers
reach cluster state through the identical API — *"there are few direct
inter-component APIs, and no hidden internal resource-oriented APIs"*. Below
the API sits one backing store, and only the API server talks to it.

The transferable property is that the control plane is not a separate
subsystem. It is the set of records that systems read to decide whether and
how to run, kept in the same store, under the same verbs, as the records the
systems produce. A scheduler, a garbage collector and a human with `kubectl`
are peers over one surface.

This repository made the same call for its runtime: capability grants, audit
records and app state are rows of one facts table discriminated by kind
([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)), chosen
explicitly so that every fact is queryable from one place rather than through
per-subsystem schemas. What Kubernetes adds is the discipline of *no other
path*: a component that keeps private state outside the store has left the
architecture, however convenient the shortcut.

## 2. Intent and observation are different columns

Every resource carries `spec` — *"a complete description of the desired
state"* — and `status`, which *"summarizes the current state of the object in
the system"*. The two are written by different parties under different
authorization (status has its own subresource), and the conventions require
that status be reconstructable by observation rather than remembered. A
monotone `metadata.generation` counts spec revisions; a controller reports the
generation it acted on in `observedGeneration`, and a `Conditions` list carries
per-aspect `(type, status, reason, message, observedGeneration,
lastTransitionTime)`.

Read through CQRS this is the command/query split kept as *data*, not as
topology. The command is a record (spec). The effect is a record (status).
Whether the command has been carried out is not hidden inside a handler — it
is the join `observedGeneration == generation`, computable by any reader, and
the lag between the two is a queryable quantity rather than a system's private
state. The rule that status be reconstructable is what keeps this honest: a
status field that only a controller's memory could produce would be an effect
that never became a fact.

Two habits follow for a facts-shaped store. Intent facts and observation facts
should be distinguishable by kind, not by convention, and an observation
should name the intent revision it observed. The second is the cheaper one to
forget and the more expensive one to retrofit.

## 3. A controller is a system, and it is level-triggered

*"Controllers are control loops that watch the state of your cluster, then
make or request changes where needed."* The design document is explicit that
they are *level-based* — they act on the current record, not on the event that
changed it — and that the watch API exists to *"minimize reaction latency and
redundant work"*, an optimisation over polling rather than a source of truth.
The API conventions give the test case: if a field goes 2 → 5 → 3 in two
writes, *"the system is not required to 'touch base' at 5"*. The client-side
pattern enforces it mechanically: an informer's event handler enqueues the
object's *key*, and the worker re-reads the object's current shape from the
local cache before acting. The concept page goes further: the cluster
*"potentially never reaches a stable state"*, and that does not matter *"as
long as the controllers … are running and able to make useful changes"*.

This is the ECS system contract. A system matches on the present shape of the
data; it does not consume events. The event stream is a hint about where to
look, and any event may be lost, duplicated or coalesced without changing the
result. It is also where the idempotency requirement of classical CQRS ends
up: not on a command handler that must tolerate a replayed message, but on the
system, which must be safe to run again against the same records. The
nanopass discipline states the same contract at pass grain — a pass declares
`Idempotent` or `NeedsFixedPoint` and the declaration is corpus-checked
([ADR-0006](../adr/0006-nanopass-environment-and-first-class-pass.md)); a
controller is a fixed-point pass whose corpus is the live cluster.

The corollary is the split between a *notification* and a *record*. The
runtime bus carries the first, the facts table the second, and a consumer that
reconstructs state from bus traffic alone has built an edge-triggered system
on a substrate that promises only levels.

## 4. Versions are opaque, local, and compactable

`resourceVersion` is an opaque token per resource; clients *"must not compare
resourceVersions except for equality"*. A write carries the version it read as
a precondition and receives a conflict if it has moved — optimistic
concurrency at record grain, no cross-object transaction. A watch that resumes
from a version the server has compacted receives `410 Gone` and must list
again before watching again.

Two properties transfer and one warning does. Concurrency control needs no
lock when every record carries its own version and every writer states the
version it believed. And the history the store keeps is *bounded by policy,
not by the model*: readers are required to rebuild from a current listing
whenever the window is gone, so the log is a cache over state, not the state.
The warning is that a store with per-resource opaque versions has no global
"as of" — there is no single position of which a query across kinds is a pure
function. Kubernetes accepts that because its product is the present state;
§10 returns to why this repository cannot accept it everywhere.

## 5. Field-grain ownership makes a conflict a value

With several systems writing the same record, Kubernetes tracks ownership at
the *field*, in `managedFields`: each entry names a manager, the operation
(`Apply` or `Update`), and the set of fields it last asserted. An apply carries
a *"fully specified intent"* — *"a partial object that only includes the fields
and values for which the user has an opinion"* — and the server merges intents
rather than values. A *conflict* is *"a special status error that occurs when
an `Apply` operation tries to change a field that another manager also claims
to manage"*, and it has exactly three documented resolutions: force and become
sole manager; drop the field from the intent and give up the claim; adopt the
live value and become a shared manager. Ownership moves to whoever last
changed a value. Removing a field from an intent removes it from the object if
no other manager still claims it.

The property: when the merge of concurrent intents is not automatic, the
disagreement must be a first-class value with named resolutions, not a silent
last-writer-wins and not an exception. This is the same shape the pushout
engine gives a text conflict — a deterministic, inspectable state resolved by a
new patch that depends on both sides
([ADR-0079](../adr/0079-pushout-production-storage-codec-exchange.md),
[pushout-distributed-operation §2](./pushout-distributed-operation.md)) — at
the grain of a structured record instead of a line graph. The two differ in
what a manager's claim is anchored to (a field path versus a graph context),
and in that Kubernetes resolves the conflict at write time while pushout
persists it into the state. Both refuse to guess.

## 6. One write chokepoint: normalise, then validate

Every mutating request passes admission — *"prior to persistence of the
resource, but after the request is authenticated and authorized"* — in two
fixed phases, mutating first and validating second; reads *"bypass the
admission control layer"* entirely. Both phases are extensible, by webhook or
by an in-process expression-language policy, without touching the server.

Two things transfer. The write path has one place where canonicalisation and
invariant checks run in a fixed order, and the order is normalise-then-check
so that validators see canonical input; the nanopass rule of fixing the
canonicalize pass rather than the consumer ([AGENTS § Subsystem
notes](../../AGENTS.md#subsystem-notes-when-you-touch-them)) is this at SQL
grain. And reads skip it because a read produces no record — the referentially
transparent side of CQRS survives as "nothing to admit".

## 7. Deletion is a state, and the cascade is a named ladder

A delete does not remove a record. The server sets `deletionTimestamp`, the
object stays visible, and it disappears only once its `finalizers` list is
empty; each finalizer is a system's claim that it still has cleanup to do.
Dependents are linked to owners by `ownerReferences`, and cascading deletion is
an ordered ladder of three named modes — foreground, background, orphan —
each documented by what it stops guaranteeing.

Deletion is the canonical non-monotone operation, and this is a monotone
encoding of it: "marked for deletion" is a fact added to the record, and
physical removal is what a garbage-collection system does when the remaining
facts permit it. The retention model in the pushout engine — a tombstone
stamped at apply time, purged by a later sweep on a replica-local horizon
([pushout-sweep-and-purge-durability](./pushout-sweep-and-purge-durability.md))
— and the `expiresAt` lifecycle column on the facts table are the same move.
The named cascade ladder is rclone's degradation ladder
([rclone-architecture-lessons §3](./rclone-architecture-lessons.md#3-name-the-degradation-ladder))
applied to a destructive operation: the compromise lives in the interface as a
small ordered set, not in a default.

## 8. Extension is a new kind, not a new verb

A custom resource, whether declared by a `CustomResourceDefinition` or served
by an aggregated API server, receives CRUD, watch, discovery, RBAC,
finalizers and the `spec`/`status`/`metadata` conventions without further
work. The documentation also states the negative: prefer a stand-alone API when
*"your API does not fit the Declarative model"*, when the fixed REST path shape
or cluster/namespace scoping is a poor fit, or when the generic API features
are not wanted.

This is the payoff of one spine: a new kind costs a schema, and the generic
tooling — listing, watching, authorising, garbage-collecting — amortises over
every conforming kind. The facts table makes the same promise, that *a new
kind of durable fact is a DTO plus vocabulary entries and never a schema change*
([ARCHITECTURE §3.3](../ARCHITECTURE.md#33-why-facts-shaped-and-what-a-generated-record-store-is)).
The honest half is the documented exit: a spine buys its leverage by imposing
one model, and a thing that does not fit the model should be kept off it
rather than bent onto it.

## 9. Grown or planned

The retrospective by the original designers (Burns, Grant, Oppenheimer,
Brewer, Wilkes, 2016; §12 Sources) settles which of the properties above were
designed and which accreted. Kubernetes is the third system of a lineage. Borg
had a master that "knows the semantics of every API operation"; Omega replaced
it with a passive Paxos store, optimistic concurrency and all logic in clients
that read and wrote the store directly; Kubernetes is described as the middle
ground — Omega's store behind one API server that "hides the details of the
store implementation and provides services for object validation, defaulting,
and versioning". The paper presents §1, §2 and §3 as corrections of named Borg
mistakes: the uniform metadata/spec/status shape; labels in place of a job's
indexed task vector, into whose names "people encode this information … that
they decode using regular expressions"; reconciliation "based on observation
rather than a state diagram". Those parts were planned.

What arrived by iteration is the set of concerns that remember or compose.
Field-grain ownership (§5) came after a client-side three-way merge and a
last-applied annotation; custom resources after a weaker third-party-resource
mechanism; a shared condition type after per-kind status; strict rejection of
unknown fields after silently dropping them; the watch cache, bookmarks and the
compaction response after operational scale. Two problems the paper names as
open stayed open. On configuration it argues for "a simple, data-only format
such as JSON or YAML" with programmatic modification "in a real programming
language", and the ecosystem's templating languages are that warning realised.
On dependencies, owner references serve garbage collection, not ordering, and
the store holds no dependency graph.

A usable test falls out: a property was planned where it is a *mechanism* and
grew where it is a *convention*. Spec and status are a mechanism — a
subresource with its own authorization. Status being reconstructable by
observation is a convention, and a controller that remembers violates it
invisibly. Level-triggering is a convention enforced by a client library, not
by the server. For this repository the test says which decisions to copy and
which to take up front rather than discover: the uniform shape and set
membership are already here by analogy (§1, §8); history as the product, a
dependency graph as data, and field-grain ownership of intents are the ones to
decide before a first version, because Kubernetes shows what retrofitting each
of them costs.

## 10. What does not transfer

- **Kubernetes discards history.** `resourceVersion` is not a version
  history; compaction is routine, `410 Gone` is a normal response, and audit
  is a separate log with its own retention. A store whose *product* is
  history — the append-only facts trail, or a patch repository where *"the
  log is the system of record"* and the snapshot is an accelerator
  ([pijul EXPLANATION](../../public/algebraicarch/pushout/pijul/EXPLANATION.md))
  — has the opposite orientation, and cannot copy the store shape. It can
  copy the level-triggered systems (§3) and the field-grain conflicts (§5)
  without copying the compaction.
- **There is no global order.** Per-resource, equality-only versions rule out
  "the state as of position *p*" across kinds. A facts table ordered by a
  timestamp, or a version defined as a downward-closed patch set with a
  frontier digest
  ([pushout-distributed-operation §3](./pushout-distributed-operation.md)),
  exists to answer exactly that question. Reproducible queries need a global
  position; reconciliation does not.
- **Eventual is the only consistency.** No transaction spans two objects;
  invariants across records are maintained by controllers converging, and may
  be visibly violated in between. That is the right trade for reconciling a
  fleet and the wrong one for a mapping stage whose downstream pass assumes
  its input is whole.
- **The coordination has not gone away; it moved.** The non-monotone residue
  — leader election, the version precondition, the ordered watch — rests on a
  consensus store underneath the API server. Adopting the record model on a
  single columnar engine copies the shape and inherits that engine's
  guarantees, not etcd's.
- **A capability declaration is a liability sized to the contributor base.**
  The conventions, the conformance suite and the `Conditions` vocabulary stay
  true because a large project keeps them true. A small repository can adopt
  the conventions; it should not adopt the number of kinds that made them
  necessary.

## 11. Glossary

Terms as this note uses them. Several have looser meanings elsewhere; the
definitions here are the ones the lessons above depend on.

- **Record.** One typed document in the store — in Kubernetes an *object*
  with `metadata`, `spec` and `status`. The unit of identity, versioning,
  authorisation and watch.
- **Desired state / observed state.** What a writer declared should be true
  (`spec`), and what a system reported is true (`status`). Kept on the same
  record, written by different parties, distinguishable by kind rather than
  by convention.
- **Controller, reconciliation, control loop.** A non-terminating process
  that reads records, compares desired to observed, takes one step that
  narrows the difference, and repeats. Kubernetes' word for it is
  *controller*; the act is *reconciliation*.
- **Edge-triggered.** Logic that acts on the *change* — an event, a message,
  a transition — and therefore depends on receiving every change exactly
  once, in order. A missed, duplicated or reordered event leaves the system
  in a state nothing will correct, because nothing later re-examines the
  current values. A command bus is edge-triggered; so is a handler that
  updates a counter on each message.
- **Level-triggered.** Logic that acts on the *current value* — the records
  as they are now — and treats events only as a hint about where to look.
  Any event may be lost, duplicated, coalesced or delivered late without
  changing the result, because the next pass reads the level again. The
  price is that every pass must be safe to repeat (idempotent) and must
  tolerate never reaching a stable state (§3). The two words come from
  digital electronics, where an edge-triggered circuit responds to a signal's
  transition and a level-triggered one to its steady value.
- **Entity-component-system (ECS).** The data-oriented design in which an
  *entity* is an identity, a *component* is a typed piece of data attached
  to it, and a *system* is logic that matches entities by the components
  they carry and produces or updates components. Read this way, a Kubernetes
  object is an entity, its spec, status, labels and annotations are
  components, and each controller is a system. Data outlives the behaviour
  attached to it.
- **Control plane / data plane.** In this note, not two subsystems but two
  roles of records in one store: the records systems read to decide *whether
  and how* to run (grants, schema, desired state), and the records they
  produce and carry. The control plane is where the non-monotone operations —
  revocation, deletion, reconfiguration — are sequenced; the data plane can
  be unidirectional and replayed freely.
- **Monotone.** A computation whose output only grows as its input grows;
  more facts never retract an earlier conclusion. Monotone logic over an
  append-only store needs no coordination (the CALM result of Hellerstein
  and Alvaro), which is why level-triggered reconciliation scales. Deletion,
  revocation and "exactly one" constraints are the non-monotone residue.
- **Idempotent.** Applying an operation twice yields the same result as
  applying it once. Classical CQRS demanded it of commands so that retries
  under at-least-once delivery were safe; a level-triggered design demands
  it of the reconciliation step, because the step runs again on every pass.
- **Pure derivation / referential transparency.** Output that is a function
  of declared inputs and nothing else — no hidden state, no clock, no
  external call. A query over an append-only log at a known position is
  pure; a status a controller can only produce from its own memory is not.
- **Version, frontier.** A name for a state of the store. Kubernetes uses an
  opaque per-resource `resourceVersion` (§4). A content-addressed log uses a
  *frontier*: the set of latest entries nothing else depends on, whose
  digest names the whole downward-closed set.
- **Field manager, ownership.** The writer that last asserted a field's
  value, tracked per field in `managedFields` (§5). A *conflict* is an
  attempt by one manager to change a field another manager claims.
- **Admission.** The single point on the write path, after authorisation and
  before persistence, where mutating (normalising) and then validating
  logic runs on a record (§6). Reads never pass through it.
- **First-class conflict.** A disagreement between concurrent writers that
  is represented as a value with named resolutions — a conflict status, a
  conflict node in a graph — rather than resolved silently by last-writer-
  wins or rejected as an error.

## 12. Sources

Public documentation only, retrieved 2026-09-09:

- Cluster architecture and components — https://kubernetes.io/docs/concepts/architecture/
- Controllers — https://kubernetes.io/docs/concepts/architecture/controller/
- Kubernetes API concepts: resource versions, watch, `410 Gone`, patch types, dry run — https://kubernetes.io/docs/reference/using-api/api-concepts/
- Server-side apply and field management — https://kubernetes.io/docs/reference/using-api/server-side-apply/
- Admission controllers — https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/
- Garbage collection, owner references, finalizers, cascade modes — https://kubernetes.io/docs/concepts/architecture/garbage-collection/
- Custom resources: CRDs versus aggregation — https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/
- API conventions (spec/status, conditions, generation, idempotency) — https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md
- The Kubernetes Resource Model, design document (B. Grant, 2018-02-20; archived) — https://github.com/kubernetes/design-proposals-archive/blob/main/architecture/resource-management.md
- client-go controller pattern (sample-controller) — https://github.com/kubernetes/sample-controller/blob/master/docs/controller-client-go.md
- Burns, Grant, Oppenheimer, Brewer, Wilkes, *Borg, Omega, and Kubernetes*, ACM Queue 14(1), 2016 — https://queue.acm.org/detail.cfm?id=2898444 (PDF via https://research.google/pubs/borg-omega-and-kubernetes/)
