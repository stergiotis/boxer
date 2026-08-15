---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-08-11 during the human
> review of the `leeway-components` skill, to seed the design dialogue for a
> future (unnumbered) ADR; revised on the compile
> date in several passes: the complexity-driver analysis (§3); the
> benchmark scenario (§1.1) and a full re-check — demoting the DDL
> fingerprint (§6), surfacing I4 and the worked-example skew (§4); the
> kind-attribute classification (§3.3); the type-discipline and system/DI
> statements (§3.1, the latter corrected against `ecsdemo/EXPLANATION.md`,
> which already defines System and Query); a consistency pass (I4
> re-attributed to fact 3); the S4 drift-causation diagnosis
> (§3.2); and the late-arity premise — monotone contract evolution
> (fact 2, R7). Nothing here is a
> decision; severity rankings are one reviewer's starting point, marked as
> such. Provenance: claims about code behaviour were verified against the
> working tree on the compile date (at commit `c65e2eb1`) by reading the
> named files and running the named tests; claims about what an ADR decided
> are readings of the ADR text; line counts come from `wc -l`. Items that
> could not be verified today are marked **unpinned**.
>
> Updated 2026-08-12: the R4 and R7 pins landed as tests. The results —
> `Projection` alone takes the first match (confirmed), and value-count
> narrowing silently zero-fills (a new finding) — are folded into §4 and
> §5, and the components skill's arity sentence was corrected.
>
> Updated 2026-08-14: the value-count finding is **closed** — ADR-0183 M0
> made a unit-shaped read refuse a multi-element value on all three read
> paths. R7's row and §5's statement of it are updated; the reading of
> what it cost is in ADR-0183 D5.
>
> Later the same day, a seam survey grounded §6's costings: API-1 is
> ~150 LOC against exactly two production `MapLookup` sites (with a
> naming-style hazard at the snapshot seam); API-3 as drafted missed
> that the facts encoders bypass plans entirely and that the typed
> lift's hidden scope is the entity-bag shape; and
> `marshallreflect/doc.go`'s registry claim turned out aspirational.
> The decisions — including a recut API-3 — now live in
> [ADR-0183](../adr/0183-leeway-component-consumer-simplification.md);
> this page remains the analysis record.

# What stands between the leeway component model and its consumers?

## 1. The question, and why it came up

The component layer — flat DTOs claiming `(section, membership)` slots,
detected and decoded off shared wire rows — was
reviewed in 2026-08 as it approaches its first consumers. The review's
verdict: the model is strong and the hard problems (overlap under fusion,
presence without global vocabulary, one contract across three read paths)
are genuinely solved — but the accumulated sharp edges push the cost of
*consuming* the layer beyond what is reasonable, and the knowledge needed
to avoid them is spread across more than a dozen documents.

Three questions follow, and this page collects the material for all of
them:

1. **Where do the edges come from** — which complexity is essential to the
   problem, and where does the accidental part concentrate (§3)?
2. **Which edges are worth fixing, and how** — API change, executable
   example, prose, or a recorded descope (§5, §6)?
3. **Where should the knowledge live** — today's landscape has grown by
   accretion; the same ADR that decides (2) may also unify it (§4, §7).

### 1.1 The benchmark scenario

Every remedy in this page is evaluated against the scenario the layer
exists for — not against the small examples it grew up in. Three premises
define it:

- **B1 — data-centric.** Operational metadata lives in the data, as facts
  ([why-boxer P3](../explanation/why-boxer.md), the data-centricity
  invariant). A mechanism that records layer metadata outside the data
  spine (in DDL comments, sidecar files, process memory) must justify the
  exception.
- **B2 — federated mesh.** On the order of thousands of components,
  **domain-owned**, written by independent stages and deployments into a
  single shared facts table. No global coordination point at write time;
  no process holds the whole vocabulary (ADR-0146's context, made
  quantitative).
- **B3 — late-formulating components.** A component is typically
  formulated *after* the data exists, and must project over rows written
  without any knowledge of it (§3.1 fact 2). The premise extends to
  *re*-formulation: a component's contract may later widen (arity) and
  must keep reading the narrower history.

An earlier draft of this page proposed a remedy (the DDL assignment
fingerprint) that is sound in the closed world and useless under B1/B2 —
the benchmark exists so that class of error is caught at proposal time.

## 2. The anti-goal: another layer of documentation

The failure mode this work must avoid is *adding* a documentation layer on
top of the problem. The landscape below already carries ~2,500 lines of
prose plus thirteen ADRs for this one layer; a fourteenth explainer would
be part of the disease. Two working rules are therefore assumed by every
remedy proposed here (the ADR can overrule them):

- **Net-negative prose.** A remedy that adds prose must name the prose it
  deletes or absorbs. New text that duplicates an existing statement is a
  future drift pair.
- **Executable beats written.** Where a semantic can be pinned by a test
  or a worked example, that is its primary home; prose points at it. The
  repo already proves the pattern works: the front-end parity corpus
  (`marshallreflect_test/parity_corpus_test.go`) gates the two DTO
  front-ends and records each deliberate accept-set asymmetry, and
  `recordstore/sharedsection/roundtrip_test.go` pins both the lifted
  gate's post-condition and the write-path limit that survives it.

A corollary preferred throughout §6: a remedy that makes an edge *stop
being true* at the consumer surface beats any amount of documenting it.

## 3. Complexity drivers — the essential core, and where the accidental sits

Most of §5's edges are symptoms. Sorting them by driver keeps the future
ADR's decision list short and prevents the classic mistake of polishing
symptoms while the source keeps generating new ones.

### 3.1 The essential core — four facts

1. **The wire carries no component identity.** Components are projections
   over `(section, membership)` attributes. Presence vs conformance,
   `Detect`'s three verdicts, archetypes as derived sets, one-sided
   detection — all essential machinery for that fact, and the place the
   layer genuinely differentiates. Complexity spent here buys something.
   Under B2 this is also what makes the model *scale*: a reader binds
   only its own components, however many thousands exist beside them.
2. **Late binding is the point** (= B3). A freshly introduced component
   works over existing data that was written without any knowledge of the
   component. This is a headline feature, not an accident: the stable
   contract is the wire vocabulary (sections, membership names and their
   ids) — never the component set. Three consequences are load-bearing:
   rows predating a component must read as *absent, never as errors*
   (which forces the one-sided detection semantics of fact 1); id
   assignments must be **timeless** — stable across artifact generations
   and process lifetimes — or late binding is unsound (the principle
   ADR-0182's "timeless vocabulary" applied to the encoding-aspect
   vocabulary); and **contract evolution is monotone** — a slot's arity
   may *widen* late (required → optional → many; the admits-set only
   grows), and the widened definition must keep reading rows written
   under the narrower one — in the type terms below, every row that
   satisfied the narrow contract satisfies the wide one. Under B2 there
   is no flag-day, so wide readers and narrow writers coexist
   indefinitely; narrowing is the breaking direction — D4 makes
   attribute-count narrowing loud, and the value-count rung — found to
   zero-fill silently (R7's pinned finding) — is loud too since ADR-0183
   M0. The data-oriented-design rule of thumb **"where
   there is one, there are many"** (Mike Acton's CppCon 2014 principles,
   with the companion advice to look on the time axis) is this
   consequence's cultural form: today's arity is a snapshot on the time
   axis, not a law.
3. **Writers are decentralized** (= B2). Together with fact 2 this
   defines the agreement requirement across *space* (independent domains
   and deployments) and *time* (data outliving and predating component
   definitions): agreement must come from a shared vocabulary registry,
   not from coordination — and at mesh scale the registry is
   load-bearing infrastructure, not a convenience.
4. **The substrate is append-only columnar.** Frame discipline —
   somewhere — is essential; a section whose offsets are written cannot
   be revisited without buffering above it.

Stated in type-discipline terms (a formulation from the review dialogue,
and the cheapest way to shed the wrong prior): **components are
structurally typed; memberships are nominally typed.** A membership's
identity is its registry name (nominal atoms — why I4's naming authority
is load-bearing); a component is a structural composition over those
atoms, satisfied by a row the way a Go interface is satisfied by a type —
by shape, retroactively, with no declaration on the satisfying side.
Fact 2 is retroactive interface satisfaction; deliberate slot sharing
(ADR-0146 D5) is two interfaces requiring the same method; the written
kind attribute (§3.3) is the `impl`/`implements` declaration — asserted
conformance, demoted to a hint. Two precisions keep the statement honest:
declared-conformance regimes (Rust traits, Java `implements`) map to the
assertion lane, not to detection; and satisfaction is checked per row at
read time — dynamic structural conformance against a static contract.
There is no subtyping hierarchy anywhere: `Approximate`/`Exact` are
conformance degrees, and an archetype is "the set of interfaces this
value satisfies", derived, never declared. The inheritance-hierarchy
prior — identity as a class tag on the row, is-a chains — is precisely
the model §3.3 demotes.

The consumption-side dual, tested in the same dialogue: the style the
layer promotes for data-intensive programs is the
**dependency-injection stance** — a consumer *declares* the components
it works with and receives satisfying projections; it never navigates to
concrete objects or producers. That is what "system" means in ECS: logic
defined by its declared component signature, fed by an archetype query.
The injection sites already exist (`Bind[T]`/`Detect`; the generated
`Scan<Component>`, whose ADR-0066 `Filter` is the declaration compiled
into a query — a *Query* in ecsdemo's own vocabulary). The name exists
too, but only in one demo: `ecsdemo/EXPLANATION.md` defines **System**
("matched with all entities that have a certain set of components") and
**Query** — and nothing outside that demo adopts either, so the
consumer-facing docs and the API leave the consuming unit unnamed.
Adoption, not invention, is the move. Two calibrations keep the analogy
honest. Absence is first-class: a DI container fails fast on an
unresolvable binding, while here `present=false` is a legal outcome the
system branches on — every dependency is optional by default, with
`Exact` and the arity gates as opt-in strictness. And the decoupling is
deeper than DI's: a DI interface predates both provider and consumer,
while here only the atoms (the vocabulary) predate — the composite
contract may be formulated after the data (fact 2), by a consumer the
producers never heard of.

### 3.2 The accidental concentrations — four sites

**S1 — id regimes that are not timeless, offered as the default.** Given
fact 2, lateness is only sound over a timeless name→id vocabulary. The
accident is not that binding is late — it is that the *default* id source
(`NoOpWrapper`, per-plan declaration-order 1..N) is generation-relative:
reordering or inserting a DTO field silently renumbers the memberships,
re-keying the wire mapping — which breaks exactly the property the design
exists for. Around that default has grown: a store-local disjointness
gate consumers must understand; a second, registry-stable regime
(`FixedIdsWrapper`) that is opt-in rather than default; pure convention
on the reflect path (`MapLookup` — nothing ties it to a registry); and no
mechanism by which a reader can compare its resolved assignment against
the vocabulary of record. Under B2, positional ids are strictly a
**closed-world** device — the same generated artifacts writing and
reading a table they own; in the benchmark scenario they have no place at
all, and the accident is that nothing marks that boundary. Generates:
I1–I3, W4, the gate, and a share of §4's doc burden. (I4 sits beside
them in §5.1 but is not S1's product: name governance is the standing
cost of nominal atoms — fact 3 — present under any id regime.)

**S2 — the write path exposes the substrate's grain instead of absorbing
it.** The DML's sealed-section state machine is fact 4 showing through,
and at that altitude it is fine. The accident is that the typed store
verbs wrap it 1:1, so the constraint reaches consumers as a trichotomy of
write spellings plus errors displaced to Commit — while `RowComposer`
already *demonstrates* the absorption (buffer contributions, flush one
frame per section at commit). The layer proves its own fix and does not
apply it at the surface consumers touch. Benchmark note: under B2 the
multi-component writers are the *fusion and enrichment stages* — and the
sanctioned composition surface for them has zero production consumers
(W5), so the first mesh fusion stage is the one who will hit this site.
Generates: W1, W2, W3, W5.

**S3 — the semantic model is entangled with one skin.** `mappingplan.Plan`
is the real abstraction — every consumer takes `*Plan`, and three
authoring front-ends exist (go/ast, reflect, and the `mappingplanview`
playground, which is struct-free) — yet `Plan` carries `GoType()`,
`KindVar()` and Add-method descriptors, and the docs define a component
as a struct. Cheap today; it is why the abstraction cannot yet be stated
cleanly, and a clean statement is itself a complexity reducer. Benchmark
note: B2's domains will not all be Go forever — a polyglot mesh needs
the plan statable without the Go skin, which upgrades A1 from tidiness
to a named cost. Generates: A1, A2, R1's flattening (the borderline
call below), and part of the doc sprawl.

**S4 — knowledge management as a complexity source in its own right.**
Stale-by-design snapshot tables, five homes for one statement, unpinned
claims, review stamps predating edits (§4). The consumer's real task
includes "determine which document is true" — pure process accident.
The causal structure, read off the drift instances this review
catalogued: statements about the layer are **replicas** — one truth
cached in ADRs, skills, comments, tests and review notes — and nothing
invalidates a replica when the truth moves. The asymmetry is exact:
where the code got single-source seams (goplan's shared `PlanBuilder`
ended front-end drift; one wrapper feeding codec, filters and map ended
artifact drift; the parity corpus pins the residue), drift *stopped*;
prose has no such mesh, and AI-rate text generation amplifies the gap —
assertions are cheap, verification is not (this page's own same-day
revisions produced four drift points, one an unverified claim). The
premises were never volatile: every correction on record moved *toward*
them — which is discovery converging, not goals wandering — and §1.1
exists so derivations stop lagging the premises.

Two borderline calls, classified honestly rather than defended:

- *Present with zero-valued missing fields* (R1) is **skin cost**, not
  model cost: `Detect` reports the richer truth; the flat-struct decode
  flattens it because Go fields lack per-field optionality without
  `Option`-everywhere. A deliberate ergonomic trade to keep — classified
  under S3, stated once. Benchmark check: under B2/B3 partial and
  enriched rows are the *norm*, so the `Detect`/`Exact` split is pulling
  its weight; no change.
- *Empty container unrepresentable* (R2) is **essential given the
  encoding** (no membership = absent; the wire has no empty-list marker).
  Lifting it is a wire change — descope material, with the why recorded.

### 3.3 Asserted kinds vs derived identity

The facts schema *writes* kind: "fact 'kind' is a membership … the same
row can carry several kind memberships" (`factsschema.go`), vocabulary in
a process-held Go package. That is not a contradiction of fact 1 — under
this formulation a written kind attribute is an **assertion, not
identity**: an ordinary claimed slot, a tiny component of its own saying
"the writer classified this row as X at write time". Identity stays
derived from slot presence — which the shipped machinery already honors:
`Filter`, `Detect`, `ArchetypePresence` and the generated stores'
`Archetype()` all derive from slots and ignore kind markers entirely.

Three consequences worth one statement each in the unification:

- **Assertions accelerate, slots decide.** A kind membership is a
  legitimate indexed prefilter; presence is the truth condition. A read
  path that *gates* on kind memberships is writer-cooperative — it sees
  only rows whose writers asserted that kind — and therefore silently
  forfeits B3: a late-formulated component can never be named by old
  rows' kind attributes. Fine for co-designed state kinds; never the
  component model's detection path.
- **Asserted-vs-derived divergence is information, not error.** A row
  asserting X without X's slots (writer drift), or carrying X's slots
  without asserting X (the *expected* late-binding case) — the delta is
  a queryable view, and it belongs to API-2's reconciliation family:
  auditing writer claims against derived reality with SQL.
- **Kind names are vocabulary.** They need the same id agreement and I4
  name governance as membership names, and under B1 the kind vocabulary
  belongs in the published vocabulary-as-facts beside membership ids.

## 4. The documentation landscape today

Prose artifacts a consumer of the component layer meets, with sizes and
observed risks:

| Artifact | Lines | Standing | Role today | Observed risk |
| --- | --- | --- | --- | --- |
| `doc/skills/leeway-beginner` | 245 | — | backbone vs payload fundamentals | below this layer; low overlap |
| `doc/skills/leeway-advanced` | 641 | — | memberships, channels, co-sections, roles | shares membership/role matter with the components skill and the how-to |
| `doc/skills/leeway-components` | 210 | draft, in review | the component/ECS layer | newest; this review is its gate |
| `doc/skills/leeway-streamreadaccess` | 197 | — | sink-side read path | adjacent, small overlap (roles) |
| `doc/howto/leeway-marshalling.md` | 729 | stable, reviewed 2026-07-19 | the single-DTO recipe | **reviewed-date predates later edits** (e.g. `bff7a421` added component-overlap statements); the stable stamp overstates its verified state |
| `public/semistructured/leeway/EXPLANATION.md` | 238 | — | architecture orientation | predates the component read contract; needs a pass |
| `anchor/ecsdemo/EXPLANATION.md` | 198 | — | the overlap worked example, explained | healthy pattern: prose bound to running code |

Plus the decision record: thirteen ADRs bear on this layer (0066, 0070,
0071, 0072, 0073, 0075, 0100, 0101, 0103, 0105, 0109, 0113, 0146), several
amended by dated updates that reverse statements in their own bodies
(0070 D3 retracted; 0100 SD6 rescoped; 0146 D5/D6 recut on acceptance
day). ADRs are the primary record of *why* and are not candidates for
deletion — but nothing currently tells a consumer which of the thirteen
are load-bearing for using the layer and which are historical.

Three structural findings, independent of any single document:

- **The worked examples skew closed-world (B2 check).** Every
  pedagogical artifact — `anchor/ecsdemo` (shared hand-built `MapLookup`),
  `recordstore/example` (positional ids), `recordstore/sharedsection`
  (caller-snapshot `FixedIdsWrapper`) — lives in the schema-agnostic
  closed world. The benchmark scenario's path — registry-resolved ids
  over the shared facts table — is exercised by the runtime itself but
  has **no worked example and the least documentation**, while being the
  main scenario. Related generator gap: the facts-target wrapper does not
  implement `MembershipIdSourceI` (its ids resolve at runtime init), so
  the registry-snapshot resolver that would let generated artifacts bake
  registry ids (ADR-0105 D3b's storegen) is unbuilt. (S4 + S1.)
- **Snapshot tables go stale by design.** ADR-0146's Context carries the
  measured pre-D4 collision behaviour (scalar errors, `Option` silently
  absent, containers concatenate). D4 then made arity uniform — the table
  is now a historical measurement that reads as current behaviour unless
  the reader also finds the decision that obsoleted it. Accepted-ADR edit
  policy (dated updates only) makes this pattern recurrent: the *current*
  semantics need an executable home, with ADR prose as history.
- **Unpinned claims — resolved 2026-08-12.** The components skill stated
  that surplus-attribute arity errors uniformly on every path *including*
  the SQL `Projection` artefact. Pinning it (the readback
  projection-surplus test, executed against clickhouse-local) falsified
  the wording: the CH lane enforces in the **Validator/Filter** — the
  surplus row is excluded from any `Scan` — while `Projection` alone
  still takes the first match. The skill's sentence was corrected the
  same day. The episode is S4's thesis in one move: the claim stood in
  prose for weeks; the test decided it in minutes.

## 5. Sharp-edge inventory (symptoms, keyed to their driver)

Severity vocabulary, worst first: **silent** (wrong answer, no error),
**displaced** (error far from its cause), **asymmetry** (front-ends or
paths disagree), **loud** (clear error, still a stumble), **surprise**
(documented but contrary to expectation); **latent** marks a hazard no
consumer has hit yet, and rows that are not consumer-facing defects carry
plain labels (**contained**, **framing**). Remedy classes are defined in
§6; each edge lists candidates, not decisions.

### 5.1 Id agreement (driver S1)

| # | Edge | Severity | Where the truth lives | Remedy candidates |
| --- | --- | --- | --- | --- |
| I1 | A wrong lookup id is observationally identical to honest absence for `Option`/container slots — decode succeeds, component absent | **silent** | ADR-0146 update (`InspectLookup`/`Suspect()` as diagnosis) | API-1; API-2 turns the diagnosis into a reconciliation query; X (worked failure example) |
| I2 | Cross-writer / cross-generation id drift on a shared table: a reader under one assignment sees rows written under another as all-absent, no error — the lateness-breaker (§3.2 S1). Benchmark form: **registry skew** — a deployment's runtime-resolved snapshot diverging from the vocabulary of record | **silent** | ADR-0100 SD6 (as corrected); `<Store>MembershipIds` doc text; `VerifySchema`'s own doc names the hole | API-2 (vocabulary-as-facts reconciliation — the mesh mechanism); the closed-world DDL fingerprint survives only as an optional belt (§6); X (diagnosis example) |
| I3 | The reflect path's `MapLookup` is pure convention — nothing ties it to any registry; the generated-store equivalent was closed by `FixedIdsWrapper` (one id source feeding codec + filters + map) | **silent** enabler | `marshallreflect` docs; ADR-0105 D2 for the generated side | API-1 (registry-backed lookup constructor). Under B2, convention-based lookups are untenable, not merely risky |
| I4 | **Name governance (new, from the B2 check):** two domains coin the same membership *name* for different semantics — the registry hands both the same id, and the collision is semantic, invisible to every id-level check below the vocabulary layer | **silent** | nowhere — surfaced by this review | registry-layer policy: domain namespacing / coinage refusal; a vocabulary-as-facts registry (API-2) makes claims queryable and auditable; out of scope for any table- or store-level mechanism |

### 5.2 Write path (driver S2)

| # | Edge | Severity | Where the truth lives | Remedy candidates |
| --- | --- | --- | --- | --- |
| W1 | Three write spellings — typed Add verbs, `Raw()` section surface, reflect `RowComposer` — differ in capability and error surface; nothing guides the choice | surprise | components skill; ADR-0146 D6; `sharedsection` | API-3 (dissolves it); else X (choice matrix bound to one example) |
| W2 | Sections seal after first use within an entity frame; the second-visit error surfaces at Commit, not at the Add that caused it | **displaced** | DML state machine (`lw_dml_generator.go`); pinned by `sharedsection` | API-3 (dissolves it at the typed surface); API (error attribution); S at the DML altitude (fact 4) |
| W3 | `RowComposer` defers emit errors to `CommitRow` by design (buffered shared frames) | **displaced** | `marshallreflect/stack.go` doc comment | S (inherent buffering cost; state once, centrally) |
| W4 | A membership *name* is unique per generated store (the `kind<Name>` const), so two kinds cannot share a membership inside one store even under unique ids | loud | gate error text (`store_emit.go`) | D (Q&A row); S. Benchmark note: under B2 names are registry-governed anyway (I4), so the store-local rule adds no mesh friction |
| W5 | `RowComposer` — the sanctioned overlap spelling — has zero production consumers; its potholes are undiscovered. Benchmark note: B2's fusion/enrichment stages are precisely the multi-component writers, so the mesh's *main composition path* is the untrodden one | latent | test suite only | API-3 makes it an implementation detail of the typed path; else adopt-or-descope. API-3's scope statement must cover the facts write path, not only recordstore stores |

### 5.3 Read semantics (facts 1–2, with S3/S4 accidents attached)

| # | Edge | Severity | Where the truth lives | Remedy candidates |
| --- | --- | --- | --- | --- |
| R1 | Present ≠ conforming: a partial row decodes `present=true` with missing fields zero-valued; only `Detect`'s `Exact` / the Scan filter check conformance | surprise (skin cost, §3.2) | ADR-0100 SD6 tail; skill | X + D (one statement, one example; delete duplicates) |
| R2 | Empty container is unrepresentable: writes no membership, reads back absent | surprise (essential, §3.2) | skill; ADR-0100 | S (wire fact) + X (round-trip asymmetry example) |
| R3 | All-optional kinds get presence-by-disjunction (presence = "necessary for carrying", not conformance) | surprise | ADR-0066 dated update | D (fold into the same statement as R1) |
| R4 | Arity behaviour history: pre-D4 split (measured) vs post-D4 uniform (decided). Projection-alone **pinned 2026-08-12**: it takes the first match; the Validator/Filter is the CH enforcement point (readback projection-surplus test; the skill's sentence corrected) | asymmetry (pinned) | the readback test; ADR-0146 Context (still a stale snapshot) | D (mark the Context table historical via dated update) — the X item landed |
| R5 | A fat DTO spanning several components can only answer "is all of this here" — component DTOs must claim only their own slots for `Detect` to work | surprise | ADR-0146 update; `ecsdemo` | D + X (already half-covered by ecsdemo; consolidate). Benchmark note: B2's domain-owned narrow DTOs make the fat DTO an anti-pattern the mesh discourages structurally |
| R6 | `string` fields are `CopyNone` — they alias the Arrow buffer; a DTO outliving released readers is a lifetime footgun that type-checks fine | **silent** (use-after-release) | scattered; where stated durably is **to be confirmed** during unification | X (a test demonstrating retain-record discipline) + D |
| R7 | Arity widening over live data (fact 2's third consequence), **pinned 2026-08-12** (`arity_evolution_test.go`): required → optional and unit → container both read narrow-written rows with their values, wide readers and narrow writers coexist in one table, and widening is admits-superset at the contract layer. The pin also surfaced a **new silent defect**: the reverse value-count direction — a container-written multi-element value under a unit definition — neither errored nor truncated; it decoded present with the field **zero-filled** (attribute-count narrowing was already loud, D4). **Closed 2026-08-14** by ADR-0183 M0: the refusal landed on all three read paths, `arity_evolution_test.go`'s assertions moved to it, and the two Go front-ends are pinned to refuse the same wire. The R3 disjunction interaction is pinned for the Option-widened kind in the readback suite | **resolved** (narrowing rung loud on every path) | `arity_evolution_test.go`; `arity_parity_test.go`; the readback projection-surplus and unit-refusal tests | D (state the monotone-evolution invariant once); the tuple rung of the corpus remains open |

### 5.4 Front-end and policy asymmetries (S2/S3 fringe)

| # | Edge | Severity | Where the truth lives | Remedy candidates |
| --- | --- | --- | --- | --- |
| F1 | `DefaultClassifier` marks primary by `/` prefix, so ordinary DTO memberships classify *secondary*; nil (all-primary) is the only symmetric default, and the name invites the wrong choice | asymmetry | ADR-0073; ADR-0146 M4 | API (rename to what it is, e.g. path-prefix classifier; nil stays default); D |
| F2 | Role filtering exists only in the reflect front-end; generated codecs resolve memberships at init and take no per-read policy | asymmetry | ADR-0146 M4 | S (descope generated-side roles until a consumer needs them — consistent with the 0073 adoption gap) or API (build it) |
| F3 | Go-type → `FieldShape` derivation is per-front-end — the one place the two parsers can drift | contained | parity corpus gates it | none — maintain the corpus; noted so the unification names it as the mechanism |

### 5.5 Abstraction residue (driver S3)

| # | Edge | Severity | Where the truth lives | Remedy candidates |
| --- | --- | --- | --- | --- |
| A1 | `mappingplan.Plan` carries Go residue — `GoType()`, `KindVar()`, Add-method descriptors — though every consumer takes `*Plan` and one authoring front-end is already struct-free | latent → **named mesh cost** (B2: polyglot domains need the plan statable without the Go skin) | this review | S now, recorded as a constraint any future non-Go plan source inherits; the vocabulary-as-facts direction (API-2) is where a language-neutral plan statement would naturally land |
| A2 | The skill (and some package docs) present the DTO struct as the definition of a component; under §3 the plan is the definition and the struct one authoring syntax | framing | components skill opening | D (reframe during unification — changes emphasis, not facts; §3.1's type-discipline statement is the candidate opening line) |

## 6. Remedy classes, costed

Ordering principle from §3: attack sites, not symptoms, prefer
**dissolving** moves, and evaluate everything against §1.1 — a remedy
that only works in the closed world must say so in its first sentence.

- **API-1 — one id-source seam everywhere (S1).** The reflect analogue of
  `FixedIdsWrapper`: a lookup *constructed from* a registry snapshot,
  subsuming today's by-hand `MapLookup`; registry-stable ids become the
  recommended default and positional ids the explicitly-marked
  closed-world escape hatch. Dissolves I3, defangs I1, shrinks the gate
  to a footnote. Benchmark check: **passes** — this is the mesh
  requirement made structural; at B2 scale convention is untenable.
- **API-2 — the vocabulary as facts, reconciliation as queries (S1, B1).**
  The membership vocabulary of record — name → id, owning domain, and
  optionally each kind's slot claims — is published as rows in the same
  substrate, domain-owned like everything else. Drift detection then
  needs no new mechanism class: a reader's resolved assignment versus the
  vocabulary facts is a JOIN; `InspectLookup`'s heuristic ("what does
  this section actually carry vs. what did I resolve") becomes a
  queryable view; registry skew (I2's benchmark form) is detectable by
  anyone with SQL, continuously — not only at a store's startup. I4's
  governance (namespacing, coinage) gets an auditable substrate. Open
  questions: does the registry publish, or do writer deployments assert
  usage claims (or both — the outer join is the interesting view)?
  reconciliation cadence (continuous vs attach-time)? where does name
  governance live? does the kind vocabulary (§3.3) publish the same way?
  do consuming systems (§3.1) publish their declared signatures too — the
  dual join being impact analysis (who consumes kind X)?
  *Demoted from this slot:* an earlier draft proposed a DDL/table-comment
  assignment fingerprint checked by `VerifySchema`. Kill-reason under the
  benchmark: it records vocabulary outside the data spine (fails B1), and
  its monotone-union comment is a write-time coordination point
  contended by every domain's deployments (fails B2) — a bandaid that
  works only where there is no registry. It survives, if at all, as an
  optional belt for closed-world positional stores, deciding nothing for
  the mesh.
- **API-3 — lift `RowComposer`'s buffering into the typed composition
  surface (S2).** Buffer per-kind contributions, flush shared frames at
  Commit. Dissolves the write-spelling trichotomy — W1/W2/W5 stop being
  true at the surface consumers touch, and `RowComposer` becomes an
  implementation detail instead of a third spelling. Benchmark check:
  scope must include the **facts write path** used by fusion/enrichment
  stages (B2's actual multi-component writers), not only recordstore
  stores.
- **D — consolidate prose (S4).** Move each fact to exactly one home,
  delete the duplicates, leave pointers. Net line count must go down
  (§2). Cheap; the risk is breaking links (doclint hard-fails on deleted
  linked files, which is the safety net). A mechanizable extension this
  review's drift diagnosis (§3.2 S4) suggests: a doclint rule flagging a
  `status: stable` document edited after its `reviewed-date` — the
  how-to's stamp drift (§4) is exactly the class it would catch.
- **X — executable documentation (S4, and the current-state home §4
  demands).** A failure-mode suite: one small test or worked example per
  edge above (I1, I2-diagnosis, R1, R2, R6, W1-matrix if API-3 does not
  land; the R4 pin and R7's option/container rungs landed 2026-08-12 —
  the tuple rung and the mesh centerpiece remain), in the pattern of
  `sharedsection` and the parity corpus. Benchmark addition: the suite's centerpiece should be the
  **missing main-scenario example** (§4) — registry-resolved ids over a
  shared table, one domain formulating a component late over rows written
  earlier by another. That example currently cannot be generated
  end-to-end (the storegen resolver is unbuilt, ADR-0105 D3b), which
  makes it a forcing function, not a nice-to-have. Also the seed corpus
  for the educational app (§8).
- **API (small) —** F1 rename; W2 error attribution if API-3 is
  descoped. Each API item above is Tier-appropriate ADR material on its
  own; none is decided here.
- **S — recorded descopes.** Some edges are inherent (R2's wire fact,
  W3's buffering cost) — the remedy is one clear statement of *why* they
  stand, in the one home D-class assigns.
- **App — educational.** See §8; deliberately separated so the doc/API
  work is not gated on it.

## 7. Unification options for the documentation landscape

For the dialogue; kill-reasons to be filled where options die.

- **U1 — the components skill becomes the consumer entry.** The skill
  (smallest, newest, currently under review) absorbs the single-statement
  homes for §5's edges and §3's driver framing; `leeway-advanced` and the
  how-to shed their duplicated component matter and keep their own scopes
  (mechanics of memberships/channels; the single-DTO recipe);
  EXPLANATION files become orientation pointers; the future ADR carries a
  one-line map of which ADRs are load-bearing for consumers. Fits the
  net-negative rule; the skill's draft status makes it the cheapest
  artifact to reshape.
- **U2 — the how-to becomes the hub.** It is the only artifact with a
  review stamp and the natural home of runnable steps. Costs: it is
  already the largest prose artifact (729 lines), its stamp is currently
  stale relative to its own edits (§4), and growing it works against the
  recipe form that earned the stamp.
- **U3 — an index page over the existing landscape.** Rejected on sight
  unless the dialogue revives it: it is precisely the "new layer on top"
  the anti-goal names, and it adds a page that must itself be maintained.

Under any option: re-review the how-to (its stamp), pass over
`EXPLANATION.md`, mark ADR-0146's Context table as a historical
measurement by dated update — and rebalance the examples toward the
benchmark scenario (§4's closed-world skew): whichever artifact becomes
the entry must *lead* with the registry-backed mesh path and present the
schema-agnostic closed world as the special case, which is the reverse of
every current document's ordering.

## 8. The educational app (deferred decision)

The reviewer's suggestion: an interactive visual representation of the
concept stack. The seeds exist in-tree — `mappingplanview`'s playground
already builds a plan field-by-field with no struct, and `componentview`
demonstrates `Detect`/archetypes against live wire data. An explorer
chaining them (author a plan → inspect slots/`ReadContract` → simulate
rows, including §6-X's failure corpus → watch verdicts change) would make
the failure-mode suite *visible*, in the demo-registry pattern
(ADR-0057). The benchmark scenario supplies the demo that actually sells
the layer: rows written first, a component formulated *afterwards* in the
playground, projecting over data that never heard of it — B3 made
visible. Deliberately not costed here: it should consume the X-class
corpus, not replace it, and the doc/API decisions must not wait on it.

## 9. What this feeds

A future ADR (unnumbered) deciding at the driver level, then per residual
edge:

1. **S1:** adopt API-1 (id-source unification, registry-stable by
   default, positional marked closed-world)? Adopt API-2
   (vocabulary-as-facts: publisher, reconciliation cadence, name
   governance for I4)? Keep or kill the closed-world DDL-fingerprint
   belt?
2. **S2:** adopt API-3 (typed-path buffering, scoped to include the
   facts write path), or sanction `Raw()` with guidance and record the
   deferral?
3. **S3:** the plan-vs-skin reframing in the docs now; `Plan`'s Go
   residue recorded as a constraint for any future non-Go plan source
   (B2 polyglot).
4. **S4:** the unification option from §7 including the example
   rebalance; the X-class corpus scope (centerpiece: the mesh worked
   example, gated on storegen); the descope list from §6-S.
5. `RowComposer` (W5): production adopter before beta, subsumed by API-3,
   or a recorded deferral.
6. The contract-evolution invariant (fact 2, R7): adopt monotone
   widening as a stated rule; decide the sanctioned widening ladder
   (required → optional → container/tuple) and which shape crossings
   are supported; scope the arity-evolution corpus.
7. The educational app's go/no-go or deferral (§8).

## 10. Sources

- [why-boxer](../explanation/why-boxer.md) — P3 is §1.1's B1.
- Mike Acton, ["Data-Oriented Design and C++" (CppCon 2014)](https://github.com/CppCon/CppCon2014/blob/master/Presentations/Data-Oriented%20Design%20and%20C%2B%2B/Data-Oriented%20Design%20and%20C%2B%2B%20-%20Mike%20Acton%20-%20CppCon%202014.pptx)
  — the "where there is one, there are many" rule of thumb fact 2's
  evolution consequence cites (verified 2026-08-12).
- ADRs 0066, 0070, 0071, 0072, 0073, 0075, 0100, 0101, 0103, 0105, 0109,
  0113, 0146 under `doc/adr/` — the decision record this page reads.
  ADR-0182 for the "timeless vocabulary" precedent §3 cites; ADR-0105
  D3b for the unbuilt storegen resolver §4 and §6-X depend on.
- `public/semistructured/leeway/mappingplan/` (`Plan`, `ReadContract`),
  `marshall/go/goplan/` (`PlanBuilder`, shared by both front-ends),
  `marshall/go/marshallreflect/` (`RowComposer`, `InspectLookup`),
  `marshall/go/marshallgen/` (`wrapper.go`: `MembershipIdSourceI`,
  `FixedIdsWrapper`), `public/storage/recordstore/gen/store_emit.go`
  (the three-tier gate), verified 2026-08-11.
- Executable-documentation precedents:
  `marshallreflect_test/parity_corpus_test.go`,
  `recordstore/sharedsection/roundtrip_test.go`,
  `recordstore/example/gen_validation_test.go`.
