---
type: explanation
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-14
---

# Boxer's quality temperament

The durable companion to the dated ISO 25010 assessment: what the project's premises tend to contribute to each quality characteristic and what they tend to cost it, stated at a level meant to survive reimplementation. This page carries no counts, no versions, no tool names, and no measured ratios — those live in the companion, where going out of date is expected and visible. What remains here are the dispositions that recur across the project, and the direction each one leans.

## Properties

| field | value |
| --- | --- |
| subject | the boxer repository — its premises and dispositions, not its current state |
| companion | [iso25010-assessment.md](./iso25010-assessment.md) — the dated, quantified assessment this page was derived from |
| durability | written to lose precision over time rather than validity; see the aging contract below |
| written | derived once from a dated assessment; deliberately not stamped with figures that would date it further |

## The aging contract

This page makes only one kind of claim: *a disposition the project has chosen tends to push a quality characteristic one way or the other, by its nature rather than by its current implementation.* Such claims survive a rewrite of the code, a change of tooling, a new release — which is why nothing here is counted, versioned, or measured. Whether a given check passes today, how large a cost is this quarter, which analyzer is enabled: none of that is knowable from this page, on purpose.

The page fails in exactly one way: when a premise is abandoned or a new one adopted. Then a column of the matrix below is wrong — not out of date, wrong — and the page should be revised or retracted, not annotated. Short of that, reality can only drift in ways this page does not see: an implementation can lag its premise, a check can be switched off, a measurement can stop being taken. The companion document exists to catch that kind of drift; this one cannot, and does not claim to.

A reader in some later year can still test whether the dispositions described here govern, without any of the numbers: look for new work that bypasses the shared data model, owned code that grows without consuming decisions, conventions that are urged rather than checked, and claims that are asserted rather than measured. Where those appear, the temperament described here no longer governs, whatever any document says — including this one.

## The dispositions

Architecture, process, and prose in this repository keep expressing the same small set of leanings. They are stated here without their current mechanisms, because the mechanisms are replaceable and the leanings, so far, have not been.

**D1 — Ownership over convenience**

Every dependency the system cannot function without is treated as a liability the project must be able to carry alone: referenced while trust stays cheap, vendored, ported, or reimplemented when it does not. The build is meant to be possible without a network and auditable from source.

**D2 — Description over implementation**

The durable text is a small description — a schema, an interface definition, a pass vocabulary — and the running code is a projection generated from it. Editing happens at the description; regeneration carries the change everywhere at once.

**D3 — One model, many projections**

Everything durable lands in one machine-readable data model, and every shape a value takes is a projection of that model. A new kind of fact costs vocabulary, not a new data layer.

**D4 — The system watches itself**

The toolkit is its own first workload: it observes, stores, and renders its own behaviour with the machinery it offers, and treats its own code — its volume, its provenance, its review state — as data to be measured continuously.

**D5 — With the machine's grain**

Efficiency is a default property of the design — data laid out the way the machine reads it — rather than an optimization applied afterwards. The target is good efficiency as a resting state, not peak performance as an exception.

**D6 — One mind, many checks**

One architect, assisted by machines, carries the work. Correctness is held by machine-checkable verification and a written record rather than by reviewer headcount, and intent is written down where a second reader would otherwise have been asked.

**D7 — Effort where it pays**

Interfaces are invested in unevenly on purpose: polish where discoverability pays, disposable assemblies where speed of construction pays, machine-readable surfaces where no fixed interface can span the task.

**D8 — Safety inside, the exception beside**

The first-party tree stays memory-safe. What cannot be made safe is either sandboxed or run whole as a separate process — kept apart, never linked in.

**D9 — An owned interface**

The interface is drawn by the project's own stack rather than borrowed from a web or desktop platform. Consistency, budgets, and access rules can therefore be enforced at the source; the platform's ecosystem is forgone along with its defaults.

**D10 — Decisions leave a record**

Design precedes code; alternatives are weighed in writing; the record accretes rather than rewrites. Superseded decisions remain, withdrawn ones say why, and claims are scoped rather than rounded up — including the claims about security and about the project's own limits.

**D11 — Checks over convention**

A convention that matters is held by a check that fails the build, not by convention alone. Contracts default to denial: silence reads as failure, absence as incompleteness, and refusal states its reason.

**D12 — Claims are measured and priced**

Performance, portability, and even the project's own defects are treated as claims to be measured under stated conditions and recorded with their dates. The costs of the project's own choices are measured with the same instruments as the benefits, and published beside them.

**D13 — Endings are clean**

What is retired is removed all the way through, with its reasons kept in the record: no compatibility layers, no inert stubs. A smaller surface is preferred to a misleading one, and consumers periodically pay for that in migrations.

**D14 — The gate sits with the author**

Checks run where the work happens rather than at a boundary the work must cross. Nothing outside the author's own discipline has to agree before a change lands: what would elsewhere be a barrier is here a habit, and its enforcement is available rather than compulsory. The record accounts for a change after the fact; nothing stands between the author and the trunk.

> **D14 was added on 2026-09-14.** The companion assessment grades fourteen practices; this page carried thirteen dispositions, and the one it lacked was the placement of verification — the assessment's lowest-scoring column and one of the two findings it says an assessor would press hardest. It survived here only as a clause inside D6, which under-reported the cost on functional suitability and reliability and attributed the security cost to single authorship rather than to where the gate sits. Stating it as its own disposition makes the two pages congruent.

## The tendency matrix

Dispositions against the quality characteristics of the ISO 25010 product-quality model (its current revision; the essences below outlast renames). A mark says only which way a disposition leans a characteristic *by its nature* — no strengths, no counts; the sentence behind each mark is in *The leanings, in words*. The last column states the row's trade in a sentence.

`▲` contributes · `▽` works against · `·` largely unrelated.

| characteristic ↓ · disposition → | D1 | D2 | D3 | D4 | D5 | D6 | D7 | D8 | D9 | D10 | D11 | D12 | D13 | D14 | the trade |
| --- | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | :-: | --- |
| **Functional suitability** | · | ▲ | ▲ | ▲ | · | · | ▲ | · | · | ▲ | ▲ | ▲ | ▽ | ▽ | Several dispositions guard correctness and fitness; clean endings and the unguarded trunk each cost it something. |
| **Performance efficiency** | · | ▲ | ▽ | ▲ | ▲ | · | · | ▲ | ▲ | · | · | ▲ | · | · | Efficiency is designed in; the shared data shape costs performance in some regimes. |
| **Compatibility** | ▽ | · | ▲ | · | · | · | ▲ | ▲ | ▽ | · | · | ▲ | ▽ | · | Close to a balance: data interchange is cultivated while ecosystem membership is traded away. |
| **Interaction capability** | · | ▽ | ▽ | ▲ | · | ▽ | ▲ | · | ▲ | ▲ | ▲ | ▲ | ▽ | · | Strong contributions and strong costs at once; a newcomer meets the costs first. |
| **Reliability** | ▲ | · | ▲ | ▲ | · | ▽ | · | ▲ | · | · | ▲ | ▲ | · | ▽ | Failure is made visible by default; the single author and the unguarded trunk are where it gives way. |
| **Security** | ▲ | · | ▲ | ▲ | · | · | · | ▲ | ▲ | ▲ | ▲ | · | · | ▽ | Broad contributions; the concentration of write authority is the one cost. |
| **Maintainability** | ▲ | ▲ | ▲ | ▲ | ▽ | ▲ | · | · | · | ▲ | ▲ | ▲ | ▲ | · | The broadest contribution of any row; only ergonomics work against it. |
| **Flexibility** | ▲ | ▲ | ▲ | · | ▲ | · | · | ▲ | ▲ | · | ▲ | ▲ | · | · | Contributions from many directions and none against. |
| **Safety** | · | · | · | · | · | · | · | · | · | ▲ | ▲ | ▲ | · | · | A few dispositions apply, none against; the characteristic itself barely applies. |

Safety row: boxer is not a safety-related system; the marks record only where a disposition happens to implement the characteristic's vocabulary — fail-safe states, hazard warnings, named risks — by temperament rather than by requirement.

### The leanings, in words

One line per mark that carries a reason. This content was hover text in the page this document replaces.

#### Functional suitability

| disposition | | why it leans that way |
| --- | :-: | --- |
| D2 description | ▲ | One description is the single source of truth; a projection cannot disagree with its siblings. |
| D3 one model | ▲ | One model keeps memory, wire, and storage saying the same thing. |
| D4 self-watching | ▲ | The pipeline is exercised on the project's own workload before anyone else's. |
| D6 one mind | · | Machine-checked verification carries correctness while one mind's throughput bounds completeness. |
| D7 effort where it pays | ▲ | Function is matched to each task's complexity rather than uniformly polished. |
| D10 the record | ▲ | Fitness is argued in writing before code exists. |
| D11 checks | ▲ | Wrong behaviour is caught by checks and reference results rather than noticed in use. |
| D12 measured claims | ▲ | Correctness is checked against reference results under stated conditions. |
| D13 clean endings | ▽ | What stops earning its keep is removed; a smaller function set is preferred to a misleading one. |
| D14 the gate sits with the author | ▽ | Nothing has to agree before a change lands; fitness and correctness rest on the author's discipline at the moment of writing. |

#### Performance efficiency

| disposition | | why it leans that way |
| --- | :-: | --- |
| D2 description | ▲ | Generated projections do at build time what reflection would do at run time. |
| D3 one model | ▽ | A single general shape carries a generality cost somewhere; that cost is measured rather than ignored. The disposition leans against this characteristic by its nature; where the cost actually falls, and how large it is, is the companion's business — and it has turned out smaller, and located elsewhere, than the disposition alone implies. This is the one cell where the two pages read differently, and the aging contract says which to trust for what. |
| D4 self-watching | ▲ | The cost of running is continuously observed by the system itself. |
| D5 machine's grain | ▲ | Efficiency is a property of the layout, not a campaign. |
| D8 safety inside | ▲ | The heavy lifting is delegated whole to a specialist engine kept beside the process. |
| D9 owned interface | ▲ | An owned interface can carry a frame budget and be held to it. |
| D12 measured claims | ▲ | Performance claims exist only as measurements. |

#### Compatibility

| disposition | | why it leans that way |
| --- | :-: | --- |
| D1 ownership | ▽ | Ecosystem breadth and a stable surface are traded for ownership. |
| D2 description | · | House languages, but they emit standard interchange targets. |
| D3 one model | ▲ | The one model is made to project into standard interchange forms. |
| D7 effort where it pays | ▲ | The most complex tasks are exposed through machine-readable surfaces, so software can operate them. |
| D8 safety inside | ▲ | The engine beside the process speaks standard protocols. |
| D9 owned interface | ▽ | The browser and the desktop shell are transport targets, not the interface. |
| D12 measured claims | ▲ | Neutrality toward other substrates is treated as something to test. |
| D13 clean endings | ▽ | Retirements cut cleanly rather than bridging; consumers move or stay behind. |

#### Interaction capability

| disposition | | why it leans that way |
| --- | :-: | --- |
| D2 description | ▽ | House languages must be learned here; little transfers from elsewhere. |
| D3 one model | ▽ | The shared model comes before every task; its concepts come before the leverage. |
| D4 self-watching | ▲ | A system that watches itself can explain itself. |
| D6 one mind | ▽ | A take-it-or-leave-it surface: assistance is not part of the offer. |
| D7 effort where it pays | ▲ | Interface effort is a policy, spent where each task class needs it. |
| D9 owned interface | ▲ | An owned interface can enforce its own consistency and inclusivity. |
| D10 the record | ▲ | The documentation discipline serves the same users as the interface. |
| D11 checks | ▲ | Interface conventions are held by checks, not taste. |
| D12 measured claims | ▲ | Friction is filed as a finding, not worked around. |
| D13 clean endings | ▽ | The surface may break rather than simulate stability. |

#### Reliability

| disposition | | why it leans that way |
| --- | :-: | --- |
| D1 ownership | ▲ | Nothing rented can disappear out from under the system. |
| D2 description | · | A wrong generator is wrong everywhere at once; a central fix propagates the same way. |
| D3 one model | ▲ | Durable state accretes append-only in one place, with completion made explicit. |
| D4 self-watching | ▲ | Defects in the shared model surface on the project's own screens first. |
| D6 one mind | ▽ | One mind is a single point of unavailability; the record mitigates, not removes. |
| D8 safety inside | ▲ | Memory safety inside; what is not safe fails apart from the process. |
| D11 checks | ▲ | Contracts default to denial: partial results read as partial without anyone's cooperation. |
| D12 measured claims | ▲ | Behaviour under load is a measured claim. |
| D14 the gate sits with the author | ▽ | A change can land broken and stay broken until someone looks; nothing stands at the door. |

#### Security

| disposition | | why it leans that way |
| --- | :-: | --- |
| D1 ownership | ▲ | A trust surface small enough to audit, and audited. |
| D3 one model | ▲ | What happened is written down in a form that can be queried. |
| D4 self-watching | ▲ | Accountability uses the same machinery the product ships. |
| D5 machine's grain | · | What cannot be safe carries its justification with it. |
| D6 one mind | · | One mind writes and one mind reviews; the record carries what a second reader would have said. |
| D7 effort where it pays | · | Machine-operable surfaces inherit their operators' failure modes — stated, and checked. |
| D8 safety inside | ▲ | Memory safety inside; the exception kept outside the boundary. |
| D9 owned interface | ▲ | Pixels and meshes travel; scripts and secrets do not. |
| D10 the record | ▲ | Security claims are scoped, not rounded up. |
| D11 checks | ▲ | Denied by default; refusal states its reason. |
| D14 the gate sits with the author | ▽ | No second pair of eyes and no barrier before the trunk: what one credential can reach, it can change. |

#### Maintainability

| disposition | | why it leans that way |
| --- | :-: | --- |
| D1 ownership | ▲ | Everything the system depends on is in the tree, readable and changeable. |
| D2 description | ▲ | The editing surface is the description; the bulk regenerates. |
| D3 one model | ▲ | A new need is vocabulary and projection, not a migration. |
| D4 self-watching | ▲ | The state of the code is itself observable data. |
| D5 machine's grain | ▽ | Machine-shaped code asks more of the person changing it. |
| D6 one mind | ▲ | Intent survives in the record instead of in memory. |
| D9 owned interface | · | An owned interface is owned maintenance, and also testable at its source. |
| D10 the record | ▲ | The why is written where the next reader will look. |
| D11 checks | ▲ | Conventions are machine-checked, so drift is caught instead of accumulating. |
| D12 measured claims | ▲ | Quantitative prose is kept in a form something can contradict. |
| D13 clean endings | ▲ | Removal goes all the way through; the tree carries no inert remains. |
| D14 the gate sits with the author | · | Small single-concern changes help the next reader; the absent barrier lets a regression sit until someone releases. |

#### Flexibility

| disposition | | why it leans that way |
| --- | :-: | --- |
| D1 ownership | ▲ | What builds without a network installs without one. |
| D2 description | ▲ | A new target is a new projection of the same description. |
| D3 one model | ▲ | The model, not any one substrate, is the durable investment. |
| D5 machine's grain | ▲ | Shapes chosen for the machine's grain scale with it. |
| D8 safety inside | ▲ | The engine sits beside the system, and is in principle replaceable. |
| D9 owned interface | ▲ | One application source is meant to run wherever a host can stand. |
| D11 checks | ▲ | Portability is a checked property, not an aspiration. |
| D12 measured claims | ▲ | Replaceability is tested rather than asserted. |

#### Safety

| disposition | | why it leans that way |
| --- | :-: | --- |
| D10 the record | ▲ | Risks and failure modes are named by their authors before others find them. |
| D11 checks | ▲ | Failure lands in refusal, not in an undefined state; hazards are documented where the hand touches them. |
| D12 measured claims | ▲ | Even the deliberately unsafe variant is modelled, to confirm the check would catch it. |

## The trades

Each characteristic read as a whole — what the dispositions give it and what they take from it, said once and without figures.

### Maintainability

**What wins:** legibility to whoever maintains the system next. The record explains why; the descriptions shrink what must be understood; self-measurement keeps the state of the code observable; checks keep conventions from drifting; clean endings keep the tree free of inert remains. **What loses:** ease of routine change — code shaped for the machine asks more of the person changing it — and ownership converts the ecosystem's maintenance into the project's own. Both costs are acknowledged in the project's documents.

### Flexibility

**What wins:** nearly everything, which is less expected for a stack this opinionated. Self-contained installation follows from ownership; adaptation to new targets follows from regenerable descriptions; the same application is meant to run wherever a host can be stood up; and the durable investment is the model rather than any one substrate, so even the engine beside the system is in principle replaceable — a property treated as something to test rather than assert. **What loses:** nothing by disposition; the costs of this row are paid elsewhere, in compatibility.

### Security

**What wins:** a trust surface kept small enough to audit, contracts that fail closed, actions that can be queried afterwards, and claims that are declined rather than rounded up when they cannot be kept. **What loses:** the separation of duties. The one standing exposure follows from where the gate sits: nothing stands between an author and the trunk, so what one credential can reach, it can change — and with a single author there is no second pair of eyes to compensate. The record and the machinery account for actions after the fact; they do not bar them beforehand.

### Functional suitability

**What wins:** correctness and fitness — single sources of truth, denial by default, reference results rather than opinion, and suitability argued in the record before code exists. **What loses:** completeness, and correctness at the moment of writing. A temperament that prefers removing a capability to bridging it will periodically make the function set smaller than its users hoped, and says so when it does; and because nothing outside the author's own discipline has to agree before a change lands, a wrong one can land.

### Performance efficiency

**What wins:** efficiency as a resting state — designed in rather than optimized in — with the heavy lifting delegated whole to a specialist engine. **What loses:** the workloads where one general data shape is slower than the special-purpose shape it replaced. The project does not avoid that cost; it measures it with its own instruments and publishes the price next to the benefit.

### Reliability

**What wins:** visibility of failure. Contracts read silence as failure and absence as incompleteness, durable state accretes append-only with completion made explicit, and nothing rented can disappear out from under the system. **What loses:** continuity, in two places. One mind is a single point of unavailability, which the written record reduces without removing; and because the checks sit with the author rather than at a boundary, a change can land broken and stay broken until someone looks.

### Safety

**What wins:** where the characteristic's vocabulary applies at all — failure landing in refusal rather than in undefined states, hazards documented at the point of use, risks named by their authors before others find them. **What loses:** nothing; a data-engineering toolkit simply does not carry safety requirements, so most of this row is not in play.

### Interaction capability

**What wins:** operability, self-description, and inclusivity — invested in and held by checks rather than urged. **What loses:** the first encounter. The house languages and the shared model's concepts must be learned here, little transfers in from elsewhere, assistance is limited by construction, and the surface may break rather than simulate stability. A newcomer meets the costs before the benefits; the project's own documents describe that ordering as intended.

### Compatibility

**What wins:** the data. The one model exists to project into standard interchange forms, the engine beside the process speaks standard protocols, and neutrality toward other substrates is treated as a claim to keep testing. **What loses:** the ecosystem. House forms replace its defaults, the browser and the desktop shell are transport targets rather than the interface, and retirements cut cleanly instead of bridging. This is the one characteristic the premises work against knowingly: interoperability of data is cultivated, and membership in a surrounding ecosystem is the price paid for ownership.

## The strongest postures

Below the characteristic level, a few fine-grained qualities are held more firmly than the matrix can show, because several dispositions converge on the same property and a mechanism, not a habit, keeps each one in place. They are stated as postures — positions the project keeps returning to — rather than achievements, and without the figures that would date them.

### 1. Analysability — legibility to the next maintainer

*converging: D1 · D2 · D4 · D6 · D10 · D12*

More dispositions converge here than anywhere else. The reasons for decisions are written where the next reader will look; the code's own state is observable data; quantitative prose is kept in a form something can contradict; and everything the system depends on is in the tree to be read. What one person cannot hold in memory is written down instead.

### 2. Testability — verification that does not depend on attention

*converging: D2 · D9 · D10 · D11 · D12*

Checks are placed where they run without being remembered: conventions fail the build, generated code is compared against its source description, the interface can be driven and compared without a person watching, and a decision that matters is expected to name the check that would catch its silent regression.

### 3. Integrity and authenticity — custody of what is trusted

*converging: D1 · D6 · D10 · D11*

The set of things the project trusts is kept small, enumerated, and inspected. What enters the build is accounted for, what runs beside the process is resolved through one auditable point, and authorship is recorded at the level of individual changes.

### 4. Fault tolerance — refusal as the resting state

*converging: D3 · D8 · D11*

Where a contract can default one way or the other, it defaults to no: an incomplete result reads as incomplete, an unproven request is not served, an unsafe configuration does not start. Partial success is made unrepresentable rather than detected after the fact.

### 5. Installability — arriving whole

*converging: D1 · D2 · D8 · D9*

The system is built to be carried to where it will run: no network at build time, no runtime it did not bring with it, the same application source wherever a host can be stood up. Installation is treated as a property to design for, not a document to write.

The postures held least firmly are the same two trades the matrix shows at the coarse level: ease of first learning, and membership in a surrounding ecosystem.

## The characteristics, in essence

The ISO 25010 product-quality characteristics reduced to their essences — enough to read this page on its own. The standard revises and renames over time (usability and portability have both carried other names); the essences below are the part that persists. The companion document carries the full sub-characteristic reference for the revision it was assessed against.

- **Functional suitability** — the functions are all there, give right answers, and fit the task.
- **Performance efficiency** — good behaviour in time and resources, with limits that hold.
- **Compatibility** — lives alongside other systems and exchanges information they can actually use.
- **Interaction capability** — people can learn it, operate it, recover from mistakes, and are not excluded from it.
- **Reliability** — works without faults, is there when needed, contains failure, and recovers.
- **Security** — information is guarded, actions are attributable, and the system defends itself while doing its job.
- **Maintainability** — the people who must change it can understand it, change it safely, and test the result.
- **Flexibility** — adapts to new environments and workloads, installs cleanly, and can replace or be replaced.
- **Safety** — under hazard, it constrains itself, warns, and fails toward the safe state.

> **Provenance.** Derived from a dated, quantified assessment of the repository's documents, decision records, checks, and trials — see the [companion document](./iso25010-assessment.md) for the evidence, the figures, and the finer-grained grading. Where the two disagree, trust the companion for the state of the tree on its assessment date, and this page only for what the premises imply. Both are one assessor's reading, not the project's self-description and not the ISO normative text.
