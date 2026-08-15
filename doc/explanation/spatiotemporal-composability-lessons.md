---
type: explanation
audience: platform designer reasoning about app composition, lifecycle, and the effect/dependency contracts between keelson apps
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> **Provenance.** Compiled 2026-08-15 from a full read of Shi, Zhang and
> Cui, *A Programming Paradigm for Spatiotemporal Composability* (Peking
> University / DeepSeek-AI, 2026, 88 pp.; the copy read carried no DOI or
> preprint identifier). Claims about this repository were verified against
> the working tree on the compile date; symbols and files are named exactly
> so a search either resolves them or shows they are gone. Neither the
> paper's TypeScript framework (Cordis) nor its case-study ecosystem
> (Koishi) was read — statements about them repeat what the paper says.
> Observations of the form "N of M apps do X" are dated; they decay.

# Lessons from "A Programming Paradigm for Spatiotemporal Composability"

The paper gives dynamic composition — loading, unloading, and rewiring
components in a running system — a formal foundation, and proves that a
runtime built on it converges to the state a static assembly would have
produced. This page reads that theory against boxer's premises
([why-boxer](./why-boxer.md)) and against the parts of the tree that
compose things at runtime today: keelson apps, applets, ad-hoc datasets,
play's query graph, the bus and its brokers, and the facts plane. It records
what the two share, where they part, what transfers, and what does not.

The one-line reading, argued below: the paper and boxer hold the same
meta-principle — correctness that would rest on an author's diligence should
become a structural property — and realise it at different binding times.
The paper reifies effects and dependencies at **runtime** so a runtime can
track, revert, and re-resolve them; boxer reifies contracts at **build time**
(declared registries, generators, lint gates, goldens) and in the **data
plane** (facts, introspection tables), and declines dynamic code loading on
purpose. Where boxer *does* compose at runtime — apps, datasets, parameters,
capabilities — the paper's constructs name several gaps precisely, and a few
of its mechanisms are cheap to adopt without importing a component
framework.

## 1. The paper, in boxer vocabulary

**Two dimensions.** *Temporal composability*: removing a component reverts
exactly the modifications it made to the shared environment. *Spatial
composability*: components declare what they read from and provide to the
environment, and the runtime re-resolves those dependencies as providers
appear, vanish, or change identity, activating and deactivating dependents
accordingly. Statically both reduce to old ideas (lexical scoping / RAII;
module import resolution); dynamically neither has a lexical scope to lean
on. Operating systems and container orchestrators supply both at process and
service granularity — the paper's "coarse-grained workaround", whose cost is
that every restart discards process-local state.

**Revertible effects (§3.1).** An effect is a function `Γ → Γ × (Γ → Γ)`:
applied to a context it returns the new context *and the inverse of what it
just did*. The runtime composes inverses into an accumulator; unloading a
component applies the accumulator. Reverting in last-in-first-out order
needs no assumption; reverting in any other order — one component among many
— needs *independence* (the effects commute), which the paper discharges by
routing every shared location through a declared key whose operations
commute (a registration table is commutative; an ordered middleware chain is
not). The developer supplies one inverse per atomic operation; the inverse of
any composite follows by composition, so a component's teardown is *derived
from its loading*, not written beside it. Equalities are read up to an
observational equivalence — the heap after `free` need not equal the heap
before `malloc`.

**Reactive coeffects (§3.2).** The context carries a typed partial table
`Σ : (k : K) ⇀ V_k`. A component declares the key set `d ⊆ K` it needs;
satisfaction `σ ⊨ d` is decidable; every table change is classified against
`d` as *activating*, *deactivating*, or *neutral*. Because provisions are
themselves effects (`set(k, v)` returns the restriction as its inverse),
withdrawal is automatically observed. Two extensions: *isolation realms*
(the same key resolves to different values in different contexts — tests,
multi-tenancy, sandboxes) and *interception* (metadata attached to how a
key is accessed, merged right-biased so an enclosing context can constrain a
component without editing it — access policy is the example).

**Component and calculus (§4).** A component is a triple `(d, p, e)`:
declared dependencies, declared provisions, and a witnessed effect function.
Provisions of distinct components are disjoint — one provider per key. An
instance ("fiber") is Inactive → Reloading → Active → Unloading → Inactive.
Three orchestrator rules (insert, retire, remove) are the only external
inputs; lifecycle rules fire on their own whenever a fiber's *committed view*
(which provider it activated against) differs from its *target view*
(which provider it should now be reading) — a target records the *provider's
identity*, not the value, so a replaced provider is detected even when it
provides an equal value. A *guard* holds a provider's withdrawal until every
dependent that resolved to it has finished its own teardown; a provider
*leaves* (stops being visible to new resolution) before it *unloads* (runs
its accumulator). Failure is recorded on the failing fiber, never propagated
to siblings.

**Metatheory (§4.4).** Preservation; recovery exactness (an accumulator
withdraws its own fiber's contribution and nothing else, given
independence); ordering (dependents activate after providers and drain
before them); resolution coherence (a transition runs against one resolution
throughout); progress (no deadlock; termination in a bound); and
*confluence*: the quiescent state is, up to renaming, the one produced by
loading each surviving component once, in dependency order — "dynamic
history leaves no trace". That last theorem is what licenses reasoning about
a running system as if it had been assembled statically.

**Implementation and discussion (§5–§6).** Cordis, in TypeScript: a single
`ctx.effect(callback)` primitive through which every mutation flows; a
declarative loader that reconciles a configuration tree incrementally; hot
module replacement with no developer-annotated boundaries. The discussion
draws a *system boundary*: locations the system modifies exclusively are
inside it and revertible; a write to a file other programs read, a network
send, a database insert are *emissions* that cross it and can only be
withheld or compensated. It frames dependency declarations as
capability requests reviewable at load time, notes that language-level
access control is not a sandbox, treats cycles as decomposition problems,
and names dependency *typing and versioning* — nominal keys, interface
drift, key collision — as an open problem.

| Paper | Nearest thing in this tree |
|---|---|
| component `(d, p, e)` | `app.Manifest` (`Caps`, `LaunchKind`, applet `datasets:`) + `AppI.Mount`; no `p` |
| fiber, registry | a window / embed instance; `windowhost` + `app.Registry` |
| coeffect context Σ, key `k` | bus subject taxonomy; `keelson('<alias>')` handles; play's signal table |
| accumulator, `ctx.effect` | none; `defer`, `IdScope` push/pop, hand-held `unsubscribe func()` |
| target vs committed view | play `nodeLane.wantKey` / `servedKey`; nothing at app level |
| isolation realm | per-instance id salt; ADR-0155 SD3 identity split; `bindDataset` alias→handle |
| interception metadata | gloss annotations; capability `Reason`; audit sink |
| orchestrator review of `d` | ADR-0026 §SD10 capslock gate (build time); declared caps are minted into the bus client at open unreviewed; `capbroker` prompts only for caps requested afterwards |
| system boundary / compensation | facts tombstones (`HAVING argMax(is_tomb, …) = 0`); ClickHouse outside the boundary |
| confluence / canonical form | static-explicit registries; `passreg` ordering; from-scratch re-run |
| declarative loader | the app-composition survey's "composition record" — not built |

## 2. Where the premises meet

**The shared thesis is P6.** The paper's own summary of its contribution is
that "correctness that would otherwise rest on developer discipline becomes a
structural property of the paradigm" (§3.3.3), and its evidence for the need
is that plugin authors forget cleanup (§1.2.1). [why-boxer](./why-boxer.md)
P6 states the same economics from the other side: when authorship is cheap,
verification and recorded intent are the scarce assets, so correctness is
carried by a machine-checkable mesh rather than by review capacity. The two
differ in *where* the structure binds. The paper binds at runtime — a
runtime that holds inverses and re-resolves keys. Boxer binds at build time
and in data: an environment variable is a registered `env.Spec` and raw
`os.Getenv` is a codelint error (CS011); an external binary is an
`extbin.Program` and raw `os/exec` is CS012; a declared capability set is
cross-checked against what the code can actually reach
([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) §SD10);
durable ids are declared ordinals with duplicate refusal and a committed
golden ([ADR-0183](../adr/0183-leeway-component-consumer-simplification.md) D0). The
paper would call each of these a coeffect made structural; boxer made them
structural one binding time earlier.

**Registry-as-data is P3/P4 meeting the paper's orchestrator.** The paper's
orchestrator can enumerate fibers and their declared capabilities and review
them before load (§6.3). Boxer's equivalent is queryable SQL:
`keelson('apps')` renders every manifest's caps, persisted keys, launch kind
and registration mode; `keelson('windows')` the live instances;
`keelson('adhoc')` the dataset catalog; `keelson('env')`, `keelson('extbin')`
(with a blake3 digest of the resolved binary), and the
[ADR-0126](../adr/0126-appliance-topology-as-data.md) topology tables with
their declared-vs-observed origin column
([introspect/providers](../../public/keelson/runtime/introspect/providers/providers.go)).
This is a step the paper does not take: its registry is an in-process
object; boxer's is a table a second app can join. What boxer lacks is the *live
effect graph* — there is no table of bus subscriptions, granted caps, held
handles, or running tasks per instance (2026-08-15) — which is exactly the
information a runtime would need in order to re-resolve dependents.

**Scope diverges on P1/P6.** The paper's value case is an open ecosystem:
four thousand community plugins whose authors "coordinate on nothing beyond
the coeffect that connects them" (§5.3). Boxer accepts no third-party
contributions and loads no third-party code; its coordination runs through
one architect and the ADR corpus. That removes the paper's *social* premise.
It leaves the technical one intact for the components boxer does compose at
runtime, and it relocates the "independently authored code" the paper
worries about: under P6 the independent authors are assisted sessions,
whose forgetting is the failure the paper documents for plugin authors.
Under `apps/`, seven of the fourteen `Unmount` implementations were empty on
2026-08-15. Several of those hold nothing to release, which is the point:
the runtime cannot tell an empty teardown that is complete from one that is
missing. `apps/capdemo` releases its file-watch subscription on its Stop and
Close buttons but not in `Unmount`, so a window closed mid-watch leaves the
subscription and its `fs.handle.*` cap in place. The paper's diagnosis of
VSCode's `deactivate` hook — disposal separated from creation, completeness
unverifiable — describes this shape exactly.

**P7 and agent harnesses.** The paper's second motivating example is a
self-evolving agent harness that replaces its own components while serving
requests (§1.2.2, §8). Boxer's agentic tier operates the toolkit through
machine-readable surfaces (P7) but the harness itself is external, and P6
answers self-modification with verification and recorded intent rather than
with runtime replacement. Nothing in the paper argues against that stance;
its guarantees are for systems that have chosen dynamic replacement.

**Standing commitments line up.** ADR-0026 rejected process isolation per
app (its O4) as a boundary its hygiene threat model does not require, and
declines "isolation theatre" in so many words; the paper reaches the same
split — capability declaration is access control, not a sandbox, and a
sandbox needs an execution boundary beyond the language (§6.3).
ClickHouse sits deliberately outside boxer's memory-safety boundary and, in
the paper's terms, outside its revertibility boundary: writes to it are
emissions.

**Immediate mode is a third answer the paper does not discuss.** The paper
offers two ways to be rid of a component's effects: an inverse (inside the
boundary) or compensation (outside it). imzero2 mostly uses a third — a
*lease*. `StateManager.Sync()` is the one drain point per frame; data
bindings are reset every frame and a consumer that stops re-registering
simply stops being bound; the id stack is pushed to a sentinel at frame
start and asserted balanced at frame end. Effects decay by non-renewal
rather than being undone, and confluence holds at frame granularity by
construction. Where state *is* retained the tree names the paper's premise
as an option it could not take: [ADR-0012](../adr/0012-imzero2-collapsible-retained-bodies.md)
lists the Rust-side caches that survive frames, weighs eviction policies
E1–E9, and rejects explicit-release-alone because "Go's authoring model has
no reliable 'widget destroyed' event" — choosing TTL plus optional release
instead. [ADR-0013](../adr/0013-imzero2-stateful-widget-contract.md) records
what a retained effect with no inverse looks like here (a `responseFlags`
entry that latched `PrimaryClicked` until overwritten) and fixed it at the
contract level with a per-frame reset. Window keys are monotonic and never
reused, so a closed window's egui memory and Rust-side cache entries become
unreachable rather than reclaimed — correctness bought with a monotone leak,
bounded by TTL where a TTL exists and unbounded for the caches ADR-0012
lists as immortal. Push/pop pairs (`IdScope`, capture scopes, generated
`defer End()`) are the statically scoped reversal the paper calls
complementary, and they are enforced by panic or frame-end assertion.

## 3. Construct by construct

The table lists each construct of the paper, its nearest realisation here,
and a verdict. "Partial" means the construct exists for a subset of cases
or without the property the paper proves for it.

| Construct | Here | Verdict |
|---|---|---|
| Declared dependencies `d` | `Manifest.Caps` (subjects), `Manifest.LaunchKind` (the DTO accepted), applet `datasets:` aliases; play panels declare `Channels()` and tabs declare `Writes` | partial — three surfaces, none reviewed by the host at open |
| Declared provisions `p` | none in the manifest; the [app-composition survey](../adr-background-work/app-composition-survey.md) names "output ports" its one missing manifest surface (F3) | absent |
| Effect with inverse | `fsbroker` pairs `AddCap` at handle resolve with `RemoveCap` at handle close — the one runtime inverse the tree holds; everything else is `defer`, push/pop, or a closure the app must remember to call | absent as a mechanism |
| Accumulator applied at unload | `windowhost.reapWindow`: release refcount, save workingset, call `Unmount`; no unsubscribe, no cap revocation, no task cancel; `MountContextI.Cancel()` is wired to a `nil` channel, so `task.ForApp`'s auto-terminate never fires under windowhost; `natsbus.Client` has a `Close()`, `inprocbus.Client` does not | absent |
| Satisfaction and notification | `sqlapplet`'s `datasetRebinder` polls until a declared alias resolves, then stops; ad-hoc dataset republish bumps a revision that re-runs consumers | partial — appear-only; a retracted provider triggers nothing |
| Guard (drain dependents, then withdraw) | producers `Retract` in their own `Unmount`; the consumer window stays open and its next query fails at name resolution; play's per-lane `closed` flag is a hand-rolled local guard | absent at app level |
| Provider identity in the target view | none — a consumer is bound to an alias, not to the publishing instance | absent |
| Isolation realms | per-window id salt pushed by the host, `AbsoluteWidgetId` as a realm root; `bindDataset` maps a stable alias to a per-instance handle; ADR-0155 SD3 splits identity into target-for-state and composed-for-attribution; persist keys are alias-only (no instance dimension) | partial, decided case by case |
| Interception | glosses attach declared, validated metadata to a column at access; capability `Reason` strings; the audit sink | present for rendering; not for access policy |
| One provider per key | `Registry.register` refuses duplicate app ids and subject aliases; leeway record stores refuse two kinds on one section only under package-local ids, and ADR-0146 D5 explicitly declines global slot disjointness; a duplicate widget id compacts newest-wins in `Sync`, so the earlier widget silently reads no response | partial, layer-local |
| Retained UI state | Go-side `StateManager` drained once per frame, bindings reset per frame; Rust-side caches per ADR-0012 under TTL plus optional release, some immortal; window keys never reused, nothing released at close | lease, not inverse |
| Orchestration rules | `Open`/`OpenWithConfig`, `Close`, reap; `capbroker` grants caps and never revokes them | present; retire without revoke |
| Failure confined to the fiber | ADR-0155 SD4: a failed embedded Mount renders a label in its region, never an embedder crash | present |
| Observational equivalence | goldens compare bytes; workingset dirtiness is `reflect.DeepEqual` over the composed launch DTO | present, stricter than the paper's |
| Independence / commutation | `PassProperties.Reads/Writes` and `Requires/Produces` are declared but unused (documentation in v1); `passreg` orders by explicit ordinal and distrusts registration order; leeway `RowComposer` writes in call order and recovers order-independence only on read, via membership ids | partial — declared, not derived or checked |
| Confluence with static assembly | the "static-explicit contracts" rule (durable values declared, never derived from init order) makes static assembly the norm rather than a theorem | present by construction, unproven for composition records |
| System boundary and compensation | every delete is a tombstone append; `HAVING argMax(is_tomb, sk) = 0` on read; ADR-0185 SD5 forbids describing a clear as erasure; nanopass `IsDiscardOutput` withholds a body rewrite but lets the pass's environment writes stand | present; one leak |
| Acquisition record | audit rows for every `Request` (including denials), grants, launches, lifecycle; `Publish` and `Subscribe` are cap-checked but emit no record | partial |
| Service broker | `fsbroker`, `clipboardbroker`, `openservice`, `persist`, `adhocdata`, task supervisor — process-lifetime singletons behind subjects | present, static |
| Dependency typing (the paper's open problem) | leeway kinds with `kindcheck` at the launch boundary, per-subject codecs, canonical type signatures, explicit ordinals with goldens | present — boxer's answer to §6.6 |
| Language independence via metaprogramming | Go has no proxy; boxer's answer is generation (P2): the egui2 IDL and leeway generators emit typed accessors | present, by a different route |

## 4. What transfers

Each item names the paper's construct, the site here, and a cut sized to
descope-over-gate. None of them requires a component framework; each is a
tracking or declaration change to a contract that already exists.

**L1 — Declare provisions, and bind consumers to a provider identity.**
The paper's component is `(d, p, e)`; the manifest carries `d` and lacks
`p`. The composition survey reached the same conclusion from the other
direction (its F3, "output ports"), so this is a confirmation, not news: the
missing half of the interface is the declared output. The paper adds one
detail worth taking with it — the target view records *which fiber*
provides a key, not the value, so a replaced provider is detected. Applied
to ad-hoc datasets: a binding is `(alias, handle, publishing instance key,
revision)`, and a re-published handle from a different instance is a
re-resolution, not a same-value no-op.
[ADR-0134](../adr/0134-adhoc-datasets.md) records that "play knows an alias
is unbound; it cannot know what produces it" — a declared `p` is what would
let it know.

**L2 — Withdraw in two steps.** The paper's guard splits withdrawal into
*leave* (the provider stops being visible to new resolution and its
dependents are notified) and *unload* (its inverse runs once every dependent
that resolved to it has finished). Ad-hoc datasets retract in one step, in
the producer's `Unmount`, and a consumer discovers the loss on its next
query. A two-phase retract — mark withdrawing, notify bound consumers,
bounded wait, then deregister — is local to `adhocdata.Service` and the
`datasetRebinder`, and gives the survey's "propagation policy" (D12) its
deactivation half. The paper's argument that the guard cannot deadlock rests
on the provider having already left the resolution table before it waits;
keep that ordering.

**L3 — Turn `Unmount` from authored into derived.** The paper's Algorithm 1
is a disposer stack: every acquisition through the context prepends its
inverse; disposal runs them last-in-first-out and fires at most once. The
mount context could carry the same: `Subscribe` through the mount context
registers the unsubscribe; `task.ForApp` registers a cancel; a resolved
`fs.handle` registers its close; the host runs the stack at reap. Three
observations make this cheap here rather than speculative: the seam already
exists (`MountContextI`); the one dead wire is known (`windowhost` passes a
`nil` stop channel; ADR-0155 SD4 decides a real channel for embedded
instances — not yet built — and leaves the window case as a recorded
follow-up); and the bus keeps every subscription's owning app id, so a
per-client sweep at reap needs little new bookkeeping — per *client*, not
per app id, since two windows of one app share an id and a sweep by id
would take a sibling's subscriptions with it. This is tracking, not
negotiation — it does not grow the
four-method contract the survey's DN1 protects — and it is the change that
lets a lint say "an acquisition outside the mount context is an error", the
same shape as CS011 and CS012.

**L4 — Make the live effect graph data.** The introspection tables expose
declarations and instances but not effects: no table of subscriptions,
grants, handles, or tasks per instance. The paper's runtime needs that graph
to re-resolve; boxer's premises want it as facts regardless (P3/P4, and the
survey's D4 "keep every relation queryable"). Providers over the bus router's
subscription list, the broker's grant map, `fsbroker`'s handle map, and the
task registry are ordinary `introspect.ProviderI` additions, and they are
the prerequisite for the survey's descriptive composition graph (S1) to
show *effects* rather than only launches.

**L5 — Use confluence as a test oracle.** Theorem 73 says any sequence of
insertions, retirements, replacements, and reversions quiesces at the state
a from-scratch load of the final configuration produces. For the survey's
composition record, that is a property test: drive a `rapid.StateMachine`
of open / close / retarget / republish operations, then compare the
quiescent facts and bindings against a fresh load of the final record. The
same oracle applies one level down to `passreg`: any two entries whose
declared `Writes` sets are disjoint should produce byte-equal output in
either order — which would give the currently unused `Reads/Writes`
declarations a consumer, or show they should be deleted.

**L6 — Name the realm decision when it is taken.** The paper's isolation
table makes "which contexts share a binding for key `k`" an explicit,
per-key decision. Boxer takes that decision repeatedly and implicitly:
persist keys are alias-scoped with no instance dimension (two windows of one
app share `lastSql`); ADR-0151 rejected a per-instance dimension for column
widths; ADR-0155 SD3 chose target-for-state, composed-for-attribution;
ADR-0148 named workingsets are, in the paper's terms, named realms. Nothing
needs to change in code; the lesson is to record each such choice as a
realm decision (shared / per-instance / named) in the ADR that makes it, so
the next one is not made by accident.

**L7 — Record acquisitions, not only requests.** The paper's boundary
argument is that the *acquisition* (open, subscribe, grant) stays inside the
boundary and is what a runtime must record; the *emission* (write, publish)
crosses it. Boxer audits every `Request` and every grant, and deliberately
not `Publish` (a fire-and-forget emission, mirroring NATS). `Subscribe`
falls on the acquisition side of that line and is currently unrecorded,
including its denials. Recording subscribe/unsubscribe would close the gap
without touching the publish decision.

**L8 — Give `Requires`/`Produces` a check or delete them.** The paper's
satisfaction predicate is decidable because the context's domain is finite;
[ADR-0006](../adr/0006-nanopass-environment-and-first-class-pass.md)
already names the corresponding registration-time lint (`Produces ⊇` the
`Requires` of later passes in a `Sequence`) and defers it. Since that ADR
(2026-05-01) no production pass has populated the fields and nothing reads
them beyond a display in play's passes tab; a declared field with no consumer
is a claim nothing can contradict, which the documentation standard's own
rule on decaying claims argues against. Either the check ships in
`passreg.Compose`, or the fields go. Related, and smaller: `IsDiscardOutput` withholds a pass's
body but keeps its environment writes; the paper's account of withholding
would put the environment write inside the withheld effect.

## 5. What does not transfer

**Hot module replacement and dynamic code loading.** The paper's motivating
cases are plugin hosts and self-modifying harnesses; its §6.4 notes that
native code has no module registry and must link and unlink explicitly. Go's
`plugin` package cannot unload, and boxer's build is CGO-free besides. Boxer
compiles every app in, registers from `init()`, and takes the process as the
unit of temporal composability — the paper's
"coarse-grained workaround", chosen with its cost known. The cost the paper
lists (restart discards caches, connections, partial computations) is
mitigated here by design: state lives in facts, workingsets restore, launch
configs are replayable, and egui memory is deliberately ephemeral. P1 and P6
say not to load third-party code; nothing in the paper argues for doing so.

**A component model as a framework.** The survey's DN1 warns against
growing negotiation, activation, and storage protocols onto the four-method
contract. The paper's lifecycle states and rules are the formal shape of what
happens *between* the survey's participants and typed channels, not a call
to add a framework; L1–L4 above are contract-local. If a general fiber
lifecycle ever seemed necessary, that would be a Tier 1 ADR and the survey's
warning applies in full.

**Proxy-mediated context access.** Cordis intercepts property access with a
`Proxy` so a component's undeclared read raises. Go offers no such hook;
boxer's route is P2 — generate the accessor and the declaration together —
which is the compile-time alternative the paper itself lists (§6.4).

**Per-effect closures.** The paper's accumulator allocates a closure per
tracked effect; its §6.7 notes a compiler could emit one state machine
instead. Under P5 an app-scoped disposer would be a per-instance table of
typed inverse records (subscription id, task id, handle uuid) rather than a
chain of closures, and the bus sweep in L3 needs no record at all because
ownership is already kept on the subscription.

**Equivalence up to observation.** The paper's equalities hold up to an
observational equivalence chosen per key. Boxer's tests compare bytes and
goldens; where a composition test needs a coarser relation, it should name
one (as workingset dirtiness names `DeepEqual` over the launch DTO) rather
than inherit the paper's.

**Evidence weight.** The paper's own threats-to-validity: one ecosystem, one
host language, observational rather than controlled. Its theorems are
independent of that; its adoption claims are not. Each item in §4 should be
judged on its local cost and on this tree's own tests, not on Koishi's
plugin count.

## 6. Sources

- Y. Shi, W. Zhang, T. Cui, *A Programming Paradigm for Spatiotemporal
  Composability*, Peking University / DeepSeek-AI, 2026. Sections cited by
  number in the text.
- [why-boxer](./why-boxer.md) — the premises this page reads the paper
  against.
- [Composing keelson apps — a design-space survey](../adr-background-work/app-composition-survey.md)
  — the repository's own composition line; its F3 and DN1 are the two
  places this page touches most.
- [ADR-0012](../adr/0012-imzero2-collapsible-retained-bodies.md),
  [ADR-0013](../adr/0013-imzero2-stateful-widget-contract.md),
  [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md),
  [ADR-0094](../adr/0094-keelson-introspection-tables.md),
  [ADR-0126](../adr/0126-appliance-topology-as-data.md),
  [ADR-0134](../adr/0134-adhoc-datasets.md),
  [ADR-0146](../adr/0146-leeway-marshall-component-read-contract.md),
  [ADR-0148](../adr/0148-app-workingsets.md),
  [ADR-0155](../adr/0155-app-embed-seam.md),
  [ADR-0185](../adr/0185-durable-app-state-manager.md).
- Code read on the compile date:
  [runtime/app](../../public/keelson/runtime/app/app.go),
  [runtime/windowhost](../../public/keelson/runtime/windowhost/windowhost.go),
  [runtime/inprocbus](../../public/keelson/runtime/inprocbus/client.go),
  [runtime/adhocdata](../../public/keelson/runtime/adhocdata/service.go),
  [runtime/fsbroker](../../public/keelson/runtime/fsbroker/service.go),
  [runtime/task](../../public/keelson/runtime/task/api.go),
  [runtime/introspect/providers](../../public/keelson/runtime/introspect/providers/providers.go),
  [nanopass pipeline](../../public/db/clickhouse/dsl/nanopass/nanopass_pipeline.go),
  [passreg](../../public/keelson/data/passreg/passreg.go),
  [recordstore gen](../../public/storage/recordstore/gen/store_emit.go),
  [play graph lane](../../apps/play/play_graph_lane.go),
  [sqlapplet datasets](../../apps/sqlapplet/sqlapplet_datasets.go),
  [egui2 state management](../../public/thestack/imzero2/egui2/bindings/egui2_statemanagement.go),
  [egui2 id handling](../../public/thestack/imzero2/egui2/bindings/egui2_id_handling.go).
