---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-26 as background to
> a decision not yet proposed: how the competence corpus
> ([ADR-0168](../adr/0168-capmap-business-capability-corpus.md)) is made
> precise enough to assess, deep enough to join against the tree, and broad
> enough to describe the toolkit as it stands. Nothing here is a decision;
> §8 maps the findings onto what this tree has and §9 lists the forks a
> design dialogue has to settle. §2 says where the tree stood on the compile
> date and will go stale as it moves; the ADRs it cites are authoritative.
>
> **Provenance.** Claims about this tree are **measured** (a command was run
> on the compile date) or **read** (source or a document was read that day).
> Claims about the literature are **read** where a page was fetched and
> **recalled** where it was not: two of the primary sources — The Open
> Group's TOGAF business-capability guide and the ArchiMate specification —
> sit behind a login, and the ISO/IEC 33020 preview carried no readable
> text, so what this page says about them is stated from memory and marked
> *(recalled)*. A recalled claim is not quoted and not relied on for a
> number; §10 lists the pages consulted and the incidents. No source code of
> any surveyed product was read; the tree's own code was read freely.

# Competence modelling — the design space, and what it meets in this tree

## 1 Question and scope

ADR-0168 gave boxer a *competence* corpus: a vault of Obsidian-shaped
markdown describing what the toolkit can do, parsed into two grains
(competence, relation), served as three keelson tables and loaded into
`boxer.facts`, with a treemap browser, a lint lens and a similarity ranker
over it. It also recorded, on 2026-08-15, that nothing in that design
produces an assessment, and declared the tool a *structure-and-prose* one.
The question here is what it takes to lift that scope line — to make a
competence's maturity a claim somebody can defend, to tie a competence to
the packages, decisions and measurements that realise it so the corpus can
be checked against the tree rather than only against itself, and to widen
the catalog from the toolbelt of August to the tree of October. Three
sub-questions:

1. **Precision** — what an assessment of a competence is, which scales the
   literature uses, and what makes a rating a claim rather than an opinion
   (§§3–5).
2. **Depth** — how a competence is bound to the things that realise it, so
   that "which app implements which competence" is the join ADR-0168 argued
   for and never had an edge for (§§3, 8).
3. **Breadth** — what the catalog has to cover now, derived from the tree's
   own self-descriptions rather than remembered (§11).

Out of scope: the write path and triage UI (settled as *CLI only* in
ADR-0168 §Deferrals), the storage encoding beyond what new fields need, and
the two non-boxer catalogs the prototype carried (§2.2).

## 2 Where the tree stands — 2026-10-26

### 2.1 What ADR-0168 built

**Read.** Five packages and a book: `public/gov/capmapcorpus` (vault →
model, model → vault), `capmapvocab` (twenty-four memberships under one
claimed tag value), `capmapfacts` (the facts encoding, since 2026-10-13
through generated component codecs), `capmapsimilarity` (normalised
compression distance, written back as `similar:` frontmatter), the
`boxer capmap` command (`parse`, `load`, `dump`, `similar`), three keelson
tables (`competence`, `competencesection`, `competencerelation`) and the
`bookcapmap` applet book (`comp-overview`, `comp-browser`, `comp-lint`).

The model a note carries, **read** from the parser's frontmatter struct:
`name`, `abbrev`, `synopsis`, `level` (1 macro, 2 meso, 3 micro, 4 building
block), `parent_ids`, `domain`, `catalog`, `owner`, `maturity` and `pain`
(0..5, or the `255` not-assessed sentinel), `tags`, `similar` (ref + score)
and sixteen flat `lifecycle_<phase>_{by,at}` keys over eight phases
(identified … retiring). The body is h1-delimited prose kept verbatim;
wikilinks in it become relations carrying the heading they sat under and
one of four resolution states (direct, dirref, external, unresolved).

Two properties of the parser matter for anything additive. **Read:** the
frontmatter is unmarshalled into a struct, so a key the struct does not
declare is *ignored on parse* — a note carrying new keys still parses today
— and the renderer writes only the keys it knows, so a `dump` *drops* them.
`UpsertSimilar` edits the YAML as a node tree and leaves unknown keys in
place. And membership ids are registration ordinals: a new membership
appends after the twenty-fourth, and the whole name-to-id table is pinned by
a test.

### 2.2 The vault

**Measured.** `doc/competences/` on this working tree holds one file, its
README. The ignore rule (`/doc/competences/*` except the README,
ADR-0168 §SD7) means the 79-competence boxer catalog the ADR and its survey
describe was never versioned, and it is not on this disk. Whether a copy
survives elsewhere — another checkout, a `boxer.facts` store it was loaded
into (§SD11's recovery path) — is not knowable from here. The consequence
for this page: the catalog is treated as **lost from this tree**, and §11
derives a new one from the tree's own documents. If a copy turns up, it is
the better seed for prose; the structure below would still be the target.

What is known about it, **read** from ADR-0168 and the port survey: one
catalog (`boxer`), one domain (`boxer-toolbelt`), one level-1 root,
thirteen level-2 branches, every competence single-parented, every one at
`255`/`255`, no lifecycle, no owner, no tags. The prose sections sampled
were *Vision and Scope*, *Activities*, *Standards*, *Obligations* and
*Business Capabilities*.

### 2.3 What has grown since

**Measured.** ADR-0168 was proposed at number 0168 on 2026-08-05; the
newest record on this tree is 0336 (two files share the number 0335, which
the next author should notice). Of the 336, 288 are accepted, 17 proposed
and the rest retired in one form or another. The areas with no counterpart
in the August catalog are visible from the titles alone: an *interactive
single-person cell* for agents — operations, task grants, journals, a
moderator loop, exits, labels, tamper-evident records, harness profiles —
across roughly 0269–0335; watchbill as a shipped subsystem; lading and
tally; stevedore; model inference and chat on facts; vector fields and a
GRIB reader; tab mode; the evidence-profile record store.

### 2.4 The tree's other self-descriptions

The corpus is not the only place the tree says what it can do. Each of
these is a source the catalog can be *derived* from, and several are a
source an assessment can be *checked* against. **Read:**

| Self-description | Form | What it carries that a competence model wants |
| --- | --- | --- |
| [Positioning pages](../explanation/positioning/README.md), fourteen of them plus the product statement | Moore's six-slot template, one page per subsystem that has a customer and a foil of its own | the customer, the mechanisms, the trade, and a **rests-on table** in which every clause names a premise or an ADR — a clause with no row is cut or marked *proposed* |
| [why-boxer](../explanation/why-boxer.md) | seven premises P1–P7 | the intent behind a subsystem: which bet it enacts |
| [ISO 25010 assessment](../explanation/iso25010-assessment.md) | a graded matrix of practices × quality characteristics, dated, with rationales and load-bearing caveats | a worked example, in this tree, of *dimensioned* assessment: a judgement per characteristic, each with a reason, and a temperament page that keeps the verdicts after the figures age out |
| [ARCHITECTURE §6](../ARCHITECTURE.md) | an index of ADRs by area, with accepted / proposed / unbuilt / withdrawn marked | delivery state per decision, as of its own date |
| ADR corpus as tables (`adr`, `adrsections`, `subtask`; [ADR-0092](../adr/0092-adr-overview-tool.md)) | keelson providers over `doc/adr` | status, dates, milestone check marks |
| [Trials](../trials/README.md), seven of them | a §0 *citable claim* per trial, conditions inside the sentence | measured evidence a maturity claim can cite without re-quoting a figure |
| App manifests ([ADR-0214](../adr/0214-launcher-as-app-summary-detail-recall.md), [ADR-0158](../adr/0158-app-classification-topics-keywords-kind.md)) | one authored `Summary` line per app; multi-valued `Topics` from a registered vocabulary; `Keywords`; `Kind` | the app layer's own subject taxonomy — nine topics, already governed, already a filter in the launcher |
| App operations ([ADR-0269](../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)) | `app_operations` table: name, class, effect, what each reads and writes | what an app *can be asked to do*, per app, machine-readable |
| Package properties ([ADR-0080](../adr/0080-packageprops-per-package-declarations.md)) | a curated `Props` per package: WASM amenability per target, `Kind` | a per-package declaration registry with a verify step — the shape a per-package competence anchor would join |
| Coverage ([ADR-0169](../adr/0169-continuous-coverage-keelson.md)) | `coverage_status`, `coverage_pkgs`, `coverage_funcs` providers | covered units per package, when a snapshot is loaded |
| Governance corpora | `go_modules`, `go_symbols`, `coderef`, `sbom`, `env`, `extbin`, `components`, `memberships`, `sql_passes`, `panel_shapes`, `helpsections`, the commit digest, the changelog index | the tree's inventory of itself, each served as a table with a name roster test |

Three observations from the table. First, the catalog's breadth is
*derivable*: level 2 of the toolbelt is the positioning set, and level 3 is
the rests-on tables' ADRs grouped by what a customer could want on its own.
Second, two of the tree's own practices already enforce the rule the
literature's appraisal methods enforce — **no claim without a basis** — the
positioning checklist's "a clause with no row is cut" and the trials'
"quote §0, never a `runs/` table". The competence model has no such rule,
which is one reason its scores stayed empty. Third, most of what would
count as *evidence* for a competence is already a table somebody can join,
so an evidence layer is mostly a set of joins over tables that exist.

## 3 Capability modelling in the enterprise-architecture literature

### 3.1 TOGAF — the business capability and the map

*(recalled; the guide is behind The Open Group's login.)* TOGAF's Business
Architecture guidance defines a business capability as a particular ability
an enterprise may possess or exchange to achieve a specific purpose, and
the capability map as the one-page structured view of them. The
*capability* is the stable "what"; its *instance* — the people, process,
information and technology that realise it in a given place — is the
"how", and heat-mapping colours the map by an attribute of the instance:
maturity, business value, strategic importance, cost, or a gap between
current and target. A secondary source read for this page describes the
usual decomposition as a one-to-three-level hierarchy, and capability-based
planning as *map, assess, plan* — assess being the gap between present and
target maturity. The split between the stable capability and its measured
instance is the one idea here that the ADR-0168 model lacks: it has the
capability and a scalar, and nothing in between to say *what* was assessed
or *on what evidence*.

### 3.2 BIZBOK — what a capability is not

*(read from secondary sources; the Guild's own text was not reached.)*
The design-principle literature agrees on four rules: a capability is a
*what*, not a *how* — if the node sounds like a department, a workflow
step, an application module, a product, a role or an initiative, it is not
one; capabilities change only when the business model does, while
processes, applications and teams change under them; siblings are mutually
exclusive and together exhaustive; names are nouns or gerunds in the
language the audience uses, and one source warns against a rigid naming
convention. Two of these bear on §11: a competence named after a *package*
("capmapcorpus") or a *decision* ("ADR-0168") fails the first rule, and a
level-2 set that overlaps — a subsystem and the panel inside it both at
level 2 — fails the third.

### 3.3 ArchiMate — the strategy layer's vocabulary

*(recalled; the specification is behind a login, and a vendor's concept
sheet returned 403.)* ArchiMate 3 models strategy with four elements:
**capability** — an ability an active structure element possesses;
**resource** — an asset owned or controlled; **course of action** — an
approach or plan for configuring capabilities and resources to achieve a
goal, what an enterprise has decided to do; and **value stream** — a
sequence of value-adding stages. Capabilities are *realised by* core
elements (behaviour such as processes and functions, structure such as
applications and systems), are *served* by other capabilities, and
*realise* courses of action; outcomes *influence* goals rather than realise
them. The relationships are what matter here: the model has a named edge
from a capability to the concrete thing that realises it, and a named edge
from a capability to the decision (course of action) it serves. ADR-0168's
relations are *parent*, *similar* and *wikilink*; the realisation edge is
missing, and the decision edge is an undifferentiated wikilink to an ADR
citation key.

### 3.4 LeanIX — assessment dimensions as practised

**Read** (the vendor's wiki page on capability assessment). The page
defines seven assessment dimensions with scales:

- **Strategic importance** — *commodity* ("a basic operational capability
  required to run the business but not a source of differentiation"),
  *differentiation* ("strengthens competitiveness or efficiency compared to
  peers"), *innovation* ("enables new value, growth, or business models and
  requires continuous evolution").
- **Current maturity**, five levels — *ad hoc* ("work is unstructured,
  reactive, and inconsistent"), *repeatable* ("performed consistently but
  relies heavily on individual knowledge"), *defined* ("processes, roles,
  and outcomes are documented and standardized"), *managed* ("performance
  is measured, monitored, and continuously controlled"), *optimized*.
- **Target maturity** — the same five labels with future-state wording.
- **Lifecycle status** — *plan*, *phase in*, *active*, *phase out*, *end of
  life*.
- **AI potential** — four values, from *no potential* to *process
  transformation*.
- **Enterprise domain** — corporate, customer, products and services,
  supply.
- **Quality seal** — *broken*, *rejected*, *draft*, *approved*: the
  governance state of the capability *definition* itself.

Who assesses: capability owners, domain leaders, strategy representatives,
with "enterprise architects usually act[ing] as facilitators"; how often:
"annually or during major transformation initiatives"; how shown: heat
maps are "the most common way", with radar charts and quadrants as
alternatives. A customer case on the same site describes sorting
capabilities into *innovative, differential, or foundational* with
"standardized interviews and scoring templates" so that assessments are
consistent across regions.

Two things to take. The maturity ladder is CMMI's, applied to a capability
rather than a process (§4.1). And the dimensions are **separate
attributes**, each with its own small vocabulary — importance, maturity,
target, lifecycle, definition quality — rather than one score; the ADR-0168
model folded importance and maturity into a `maturity` that nobody could
agree on and a `pain` that nobody filled.

### 3.5 Wardley — evolution as a dimension

**Read** (Simon Wardley's evolutionary-characteristics cheat sheet as
reproduced under CC BY-SA on learnwardleymapping.com). Every component of
a value chain moves through four stages, and the sheet gives fifteen
characteristics per stage. The ones that discriminate for a software
toolkit's parts:

| | Genesis | Custom-built | Product (+rental) | Commodity (+utility) |
| --- | --- | --- | --- | --- |
| Ubiquity | "Rare" | "Slowly increasing consumption" | "Rapidly increasing consumption" | "Widespread and stabilising" |
| Certainty | "Poorly understood" | "Rapid increases in learning" | "Rapid increases in use / fit for purpose" | "Commonly understood (in terms of use)" |
| Publication types | "Normally describe the wonder of the thing" | "Build / construct / awareness and learning" | "Maintenance / operations / installation / features" | "Focused on use" |
| Failure | "High / tolerated / assumed" | "Moderate / unsurprising but disappointed" | "Not tolerated, focus on constant improvement" | "Operational efficiency and surprised by failure" |
| Efficiency | "Reducing the cost of change (experimentation)" | "Reducing cost of waste (Learning)" | "Reducing cost of waste (Learning)" | "Reducing cost of deviation (Volume)" |
| Decision drivers | "Heritage / culture" | "Analysis & synthesis" | "Analysis & synthesis" | "Previous experience" |

Evolution is not maturity. A commodity component can be immature in a
given implementation, and a genesis one can be the best-built thing in the
tree. For this repository the stage answers a different question —
*what kind of thing is this in the world* — and that question drives a
posture the premises already name: P1's reference-while-cheap, port-when-
not applies to product and commodity parts; genesis parts have nothing to
reference. The "publication types" row is also a usable test for which
stage a part is at, from what its documents are about.

### 3.6 Gartner's pace layers

*(read from secondary sources and a 2012 note hosted by a university.)*
Applications sort into *systems of record*, *of differentiation* and *of
innovation* by rate of change and distinctiveness, with different
governance and funding per layer, and the same application may sit in a
different layer in a different organisation. The idea overlaps LeanIX's
strategic-importance triple almost exactly and adds nothing a competence
model needs beyond it; it is recorded because the *rate of change* framing
is the one a maintainer can measure from the commit history.

## 4 Maturity and readiness scales

### 4.1 CMMI — maturity of an organisation, capability of a practice area

**Read** (the CMMI Institute's appraisal-levels page). Maturity levels, for
an organisational unit across practice areas: 0 *Incomplete* ("ad hoc and
unknown"), 1 *Initial* ("unpredictable and reactive"), 2 *Managed*
("managed on the project level"), 3 *Defined* ("proactive, rather than
reactive"), 4 *Quantitatively Managed* ("measured and controlled"),
5 *Optimizing* ("stable and flexible"). Capability levels, for one practice
area: 0 *Incomplete* ("inconsistent performance"), 1 *Initial* ("addresses
performance issues"), 2 *Managed*, 3 *Defined*. The distinction worth
keeping is the **scope** of a rating: a maturity level is a statement about
the whole; a capability level is a statement about one area, and the
continuous representation lets an organisation pick which areas it rates.
A competence catalog is the continuous case.

### 4.2 ISO/IEC 33020 — attribute ratings, then a level

*(recalled; the preview carried no readable text, and the search confirmed
only the clause structure: six capability levels, a rating scale in
clause 5.3, a rating method in 5.4.)* The 33000 family rates each *process
attribute* on a four-point ordinal scale — N not achieved, P partially,
L largely, F fully — with the bands in its 15504 ancestor at roughly
0–15 %, >15–50 %, >50–85 % and >85–100 % of the evidence, optionally refined
to P−/P+/L−/L+; a capability level is then *derived* — level *n* needs its
own attributes at L or F and every lower level's at F. Two ideas survive
even without the exact figures: a rating is given to an **attribute**, and
a level is a **rule over ratings**, never a number typed directly. That is
the opposite of `maturity: 3`.

### 4.3 TRL — readiness of a technology, with a software column

**Read** (NASA's TRL definitions table, software column). The nine levels
describe what has been *built and tested*, not how polished it is:

| TRL | Software description (abridged, quoted fragments) |
| --- | --- |
| 1 | "Scientific knowledge generated underpinning basic properties of software architecture and mathematical formulation." |
| 2 | "Basic principles coded. Experiments performed with synthetic data." |
| 3 | "Development of limited functionality to validate critical properties and predictions using non-integrated software components." |
| 4 | "Key, functionally critical, software components are integrated, and functionally validated, to establish interoperability and begin architecture development." |
| 5 | "End-to-end software elements implemented and interfaced with existing systems/simulations conforming to target environment. … Prototype implementations developed." |
| 6 | "Prototype implementations of the software demonstrated on full-scale realistic problems. Partially integrate with existing hardware/software systems. Limited documentation available." |
| 7 | "Prototype software exists having all key functionality available for demonstration and test. Well integrated with operational hardware/software systems demonstrating operational feasibility. Most software bugs removed." |
| 8 | "All software has been thoroughly debugged and fully integrated … All user documentation, training documentation, and maintenance documentation completed. … Verification and Validation (V&V) completed." |
| 9 | "… System has been successfully operated in the operational environment." |

Each level also carries an *exit criterion* that is a document: "peer
reviewed publication", "documented description of the application/concept
that addresses feasibility and benefit", "documented test performance
demonstrating agreement with analytical predictions", "documented mission
operational results". A readiness level is therefore **anchored to an
artefact** by construction, which is what makes it checkable. Agencies
differ in wording (ESA uses ISO 16290; the EU's TRL 3 is "experimental
proof of concept"), so a scale adapted for this tree should name its own
rungs rather than cite one agency's.

### 4.4 The tree's own quality model

**Read.** [iso25010-assessment](../explanation/iso25010-assessment.md)
grades the toolkit's practices against the nine ISO 25010:2023 product
characteristics in a matrix with per-cell rationales, load-bearing caveats
as footnotes, marginals and a dated verdict, and
[iso25010-temperament](../explanation/iso25010-temperament.md) keeps the
verdicts without the figures. It is an assessment *of the whole*
(CMMI's staged scope) and shows that dimensioned, reasoned grading is a form
this repository already writes and reviews. What it does not do is grade
per part — which is the continuous scope a competence catalog is for.

### 4.5 Comparison

| Scale | Rates | Scope | Rungs | How a rating is justified |
| --- | --- | --- | --- | --- |
| CMMI maturity | an organisation's process | whole unit | 0–5 | appraisal evidence (§5.1) |
| CMMI capability | one practice area | per area | 0–3 | same |
| ISO/IEC 33020 | process attributes, then a level | per process | N/P/L/F per attribute; level 0–5 derived | assessment indicators; the level is a rule |
| TRL | a technology's demonstrated state | per technology | 1–9 | a named artefact per rung |
| LeanIX maturity | a business capability's execution | per capability | 1–5 (CMMI wording) | owner interviews and templates |
| Wardley evolution | what kind of thing a component is | per component | 4 stages | observable characteristics |
| ISO 25010 (this tree) | product quality | whole product | graded matrix | per-cell rationale with caveats |
| ADR-0168 `maturity` / `pain` | unstated | per competence | 0–5 | none; never filled |

The row that has no "how justified" is the one that stayed empty.

## 5 Evidence — what makes a rating a claim

### 5.1 CMMI appraisals: artefacts and affirmations

**Read** (CMMI Institute quality tips; an arXiv comparison of appraisal
methods). SCAMPI appraisals rest on three kinds of objective evidence —
direct artefacts, indirect artefacts and affirmations — where affirmations
are gathered in interviews and artefacts are reviewed. The adequacy and
sufficiency rules live in the Method Definition Document; the tips quote
two: all evidence "must result from work performed by the sampled projects"
and "must be produced by people with a defined process role". Affirmations
cannot stand alone — "a statement about future intent does not suffice";
both kinds "must align with completed work"; evidence not from the
appraised organisation is inadmissible; everything must be "directly
traceable" to the unit appraised. The less rigorous appraisal classes
differ "mostly in the level of corroboration required".

Translated: a rating with no artefact behind it is an affirmation, and an
affirmation alone is the weakest class of appraisal. In this tree the
artefacts are the tree — a test in a named lane, a trial's §0, an accepted
ADR with its milestones ticked, a howto, a scene that runs — and
*traceable* means a link that resolves.

### 5.2 SFIA — a skill at a level, evidenced

**Read** (SFIA 9's responsibility pages). SFIA is the framework that uses
the word *competence* in the sense ADR-0168 chose not to mean: a
professional skill held at one of seven levels of responsibility (Follow,
Assist, Apply, Enable, Ensure/advise, Initiate/influence, Set strategy),
each level defined by autonomy, influence, complexity and business skills,
and an individual's competence being the skills they can evidence at a
level. The structural lesson is the same as TRL's: the unit of assessment
is *(thing, level, evidence)*, never a bare level. The terminological note
is for §6.

### 5.3 This tree's two precedents

**Read.** The positioning checklist: "every clause of the full form has a
row in the rests-on table, and every row links a premise or an ADR by
number"; "a clause that rests on a proposed, deferred or unbuilt ADR says
*proposed*". The trials rule: "quote the trial README's §0 citable claim …
no figure travels without the pair of arms it compares". Both are
basis-required rules enforced by review and by a link checker. An
assessment stanza that carried a `basis:` list of resolvable anchors would
be the third instance of a rule the repository already follows, not a new
discipline.

## 6 Competence, capability, skill — the word, once more

ADR-0168 §SD6 chose *competence* because *capability* is the runtime's
word. The literature read for this page uses the three words as follows:
*business capability* (TOGAF, BIZBOK, ArchiMate, LeanIX) is the stable
"what an enterprise can do"; *capability level* (CMMI, ISO 33020) is a
rating of a process; *competence* (SFIA, the European e-CF) is a person's
evidenced skill at a level. boxer's *competence* is the first of these
wearing the third's name, which is harmless as long as the vault keeps the
literature's word (it does) and nobody expects the SFIA sense. No change is
proposed; the note is here so the next reader does not re-derive it.

## 7 What did not work in the existing attempt — a diagnosis

Each item is a property of ADR-0168's chosen shape, not a defect in its
execution; the ADR's own §Verification plan and §Deferrals name most of
them.

- **a. The scores had no rubric, no basis, no assessor and no date.**
  `maturity: 3` says nothing about *what* is mature or *why*, and nothing
  in the model holds a reason. Every scale in §4 that gets filled in
  practice attaches at least one of those. The ADR made the emptiness a
  scope line rather than fixing the shape.
- **b. No realisation edge.** The join that won ADR-0168's QOC — competences
  beside what the runtime knows about itself — has no data to run on,
  because a competence does not say which package, app, table, decision or
  measurement realises it. The ArchiMate realisation relation (§3.3) and
  TRL's artefact-per-rung (§4.3) both put that edge at the centre.
- **c. Level semantics were inherited, not defined.** "Macro, meso, micro,
  building block" came from the prototype; nothing says what a level-2
  node *is* in this tree, which is why the thirteen branches cannot be
  reconstructed from the tree today.
- **d. The catalog was unversioned, and it is gone from this tree.** §SD7
  kept it ignored to avoid deciding the other two catalogs' licence and
  privacy questions by accident; the boxer catalog has neither question,
  and the cost of ignoring it was paid in full — CI, doclint and the
  applets' corpus gate never saw it, and a working tree can lose it
  silently, which this one did.
- **e. Breadth froze in August.** Half the ADR record postdates the catalog.
- **f. Lifecycle duplicates the ADR corpus.** Eight organisational phases
  with who/when pairs, parallel to a decision corpus that already carries
  proposed/accepted/superseded and milestone ticks, and nothing in-tree
  that would write them.
- **g. Prose headings were unconstrained by design** (§SD5), which is right
  for round-tripping and wrong for comparing two notes; nothing suggests a
  shape a new note should take.

## 8 Mapping onto this tree

For each need, what exists, what is missing, and what the smallest addition
is. Nothing here is decided.

### 8.1 Anchors — a `realised_by` relation resolved against the tree

**What exists.** Relations are rows with a kind string, a target text and a
resolution state; the parser's relation kinds are `parent`, `similar`,
`wikilink`; `external` already means "names something outside the corpus".
The tree's corpora the anchors would resolve against are each a keelson
table with a roster test (§2.4), and three are pure libraries reachable
without a process: `adrcorpus` (decision ids and statuses), the
environment-variable registry (ADR-0009), and the Go package list.

**Missing.** A typed reference syntax, a fourth relation kind, and a
resolver per anchor kind. A plausible syntax, in the vault's own idiom, is
a frontmatter list of `kind:key` strings:

```yaml
realised_by:
  - adr:0223            # resolved against doc/adr; carries the ADR's status
  - pkg:public/streaming/watchbill
  - app:watchbill
  - table:boxer.facts   # or keelson/<name> for a provider table
  - howto:watchbill-jobs
  - trial:jsonbench-on-facts
  - env:BOXER_WATCHBILL_QUEUE
```

A resolver that cannot reach its corpus (no checkout, no process) reports
*unchecked* rather than *unresolved*, which is one more resolution state
and follows the providers' empty-rather-than-erroring convention. An
anchor that resolves to a *retired* decision is its own finding, since the
competence may be describing something the tree no longer does.

**What it buys.** The join; a lint over the corpus that the tree can
falsify; a derivation path for §8.3; and a second similarity signal
(two competences realised by the same packages) that explains itself where
compression distance does not.

### 8.2 Assessment — dimensioned claims with a basis

**What exists.** Two `uint8` scores with a sentinel; the facts encoding
carries them as two memberships; the overview lens reports their coverage.

**Missing.** A stanza shaped like the scales that get filled:

```yaml
assessment:
  - dimension: readiness      # a 9-rung ladder adapted from TRL's software column, §4.3
    rating: 7
    basis: [adr:0223, adr:0234, howto:watchbill-jobs, scene:watchbill-smoke]
    by: "<handle>"
    at: 2026-10-26
  - dimension: evolution      # genesis | custom | product | commodity, §3.5
    rating: custom
    basis: [adr:0223]
    by: "<handle>"
    at: 2026-10-26
  - dimension: importance     # foundational | differentiating | innovating, §3.4
    rating: differentiating
    basis: [positioning:watchbill]
    by: "<handle>"
    at: 2026-10-26
  - dimension: pain           # 0..5 with named rungs
    rating: 2
    basis: [adr:0237]
    by: "<handle>"
    at: 2026-10-26
```

Rules worth considering: a claim with an empty `basis` is a lint finding
(an affirmation, §5.1); a basis anchor must resolve (§8.1); the absence of a
dimension is *unassessed*, so the `255` sentinel is no longer needed for
new notes and stays only for reading old ones; `maturity:` and `pain:` as
bare keys are read for compatibility and not written. The dimension
vocabulary is small and registered — four to start — and adding one is a
registry addition, not a decision, on the ADR-0158 precedent.

**The readiness ladder, adapted.** TRL's rungs are about demonstration in
an operational environment; this tree's equivalent artefacts are concrete
and nameable, which is the point of adapting rather than citing:

| Rung | The competence is … | Typical artefact the basis cites |
| --- | --- | --- |
| 1 | named in a decision's Context or a survey | a background-work page, a proposed ADR |
| 2 | decided — an accepted ADR with milestones unticked | the ADR |
| 3 | built in part — a package exists and tests in the default lane pass | `pkg:`, the ADR's ticked milestones |
| 4 | built end to end behind a surface — a command, a table, a widget | `app:`, `table:`, `applet:` |
| 5 | exercised against the real engine — integration lane, a scene | an integration test, `scene:` |
| 6 | documented for a consumer — a howto or tutorial a stranger can follow | `howto:` |
| 7 | positioned — a customer and a foil named, every clause resting on a record | `positioning:` |
| 8 | measured — a trial's §0 states its cost or benefit under conditions | `trial:` |
| 9 | operated — the toolkit's own workload runs on it and its facts show it (P4) | a `boxer.facts` query, a dashboard |

The ladder is a *rule over anchors* in the ISO 33020 sense (§4.2): most
rungs can be checked, and a claimed rung above what the anchors support is
visible. Rungs 6–8 are not strictly ordered in practice — a measured part
can lack a howto — so the ladder may need to be a set of achieved
indicators with a level derived as "the highest rung all of whose lower
rungs hold", which is exactly 33020's derivation rule. That is a fork
(§9, F3).

### 8.3 Evidence signals — a derived table beside the claims

**What exists.** Every input is a keelson table or a library (§2.4). The
providers run in-process over the vault with live freshness.

**Missing.** A fourth provider, `competenceevidence`, one row per
competence per signal, computed from the anchors: ADR status and milestone
ratio (adrcorpus), howto/explanation/positioning presence (the doc tree),
trial presence (`doc/trials`), package existence and test-file presence by
lane (`_test.go` with and without the integration tag), coverage when a
snapshot is loaded, app registration and operation count (`apps`,
`app_operations`), commit recency and churn (commitdigest). Signals are
*what the tree carries*, never a verdict; the lens shows them beside the
claims so a disagreement is a row. The table is cheap because the vault is
small (the August catalog was 79 notes) and the joins are over tables that
already exist; the one new reader is the test-lane probe, which is a file
walk.

### 8.4 Levels — definitions for this tree, and three roots

**What exists.** `level` 1–4 with the prototype's words. The
positioning README's three tests for when a subsystem earns a page
(a customer of its own, a nameable external alternative, enough decisions
to fill the rests-on table) are a usable definition of a level-2 node.

**Candidate definitions.** Level 1: a *root* with a customer type of its
own — who the catalog is for. Level 2: a *subsystem* in the positioning
sense; the fourteen pages plus the two the README defers. Level 3: a
*competence* a customer could want without the rest of its subsystem,
usually one to a few decisions. Level 4: a *building block* shared by
several level-3 competences, which is where multi-parenting lives.

**Three roots.** The August catalog had one, the toolbelt. The tree now
describes two more customers in its own words: the *maintainer* of the
repository, whose competences are the practices ENGINEERING_PRACTICES and
the gov positioning page describe (decision records, trials, clean-room
surveys, the license gate, reproducible builds, the test lanes), and the
*person operating agents inside the cell* ([ADR-0307](../adr/0307-threat-model-for-an-interactive-single-person-cell.md)'s
single-person cell), whose competences are what an agent may be granted,
what is journaled, what is moderated and what may leave. The second is not
a subsystem — it cuts across keelson, play, chat and the record stores —
which is why it is a root rather than a branch. §11 lays all three out.

### 8.5 Committing the boxer catalog

**What exists.** The ignore rule, the README's §SD7 rationale, doclint's
skip of git-ignored trees, the applet corpus gate reading a fixture vault
*because* the real one is ignored.

**What changes if committed.** doclint would walk the vault and apply its
frontmatter rule (`type`, `audience`, `status`), which competence notes do
not carry; either the doclint path filter excludes the vault, or notes
gain `type: competence` and doclint learns it. The corpus gate could read
the real catalog. The README's §SD7 paragraph changes. The two excluded
catalogs stay excluded by a narrower ignore rule. Nothing in the encoding
changes.

### 8.6 Lifecycle and sections

Lifecycle: the eight phases could be *derived* — from the readiness claim
and the anchored ADR's status — and the sixteen keys kept read-only for
compatibility; nothing writes them today and the boxer catalog need not
author them. Sections: a recommended heading set for new notes, free-form
parsing unchanged. A set that mirrors what the anchors and the positioning
template already ask for: *Scope* (what, in the customer's words),
*Mechanisms* (how it is realised, prose around the anchors), *Evidence*
(what shows it works), *Boundaries* (what it leaves to siblings),
*Standards* (citations, as before).

## 9 Forks for the design dialogue

Each with the recommendation this survey would make.

- **F1 — Anchor syntax.** `kind:key` strings in a frontmatter list, or
  wikilinks with a prefix (`[[adr:0223]]`)? *Recommend strings:* they
  are not Obsidian links and should not look like ones, and the resolver
  is per kind.
- **F2 — Which anchor kinds in the first cut.** *Recommend* `adr`, `pkg`,
  `app`, `table`, `howto`, `explanation`, `positioning`, `trial`, `env`,
  `applet`, `scene`, `skill`; each resolves against something that exists
  today.
- **F3 — Readiness as a typed rung or as a derived level.** *Recommend
  typed, with the lens computing the supported rung from anchors beside
  it:* derivation makes the claim unwritable by hand, and the point is a
  defended human claim, not an oracle.
- **F4 — Dimension set.** Readiness, evolution, importance, pain — or also
  a target rung (LeanIX's *target maturity*) and the ISO 25010
  characteristics per competence? *Recommend the four;* a target is a
  course of action and belongs in a tag or an ADR, and per-competence
  quality characteristics are the whole-product assessment's job.
- **F5 — Who may assess.** `by:` as a handle, with the P6 reading that one
  architect with AI assistance assesses and the basis is what makes it
  reviewable. *Recommend recording `by:` and `at:` and nothing more.*
- **F6 — Commit the boxer catalog.** *Recommend yes,* narrowing the ignore
  rule to the other catalogs; the reasons §SD7 gave do not apply to it and
  the cost of not committing has already been paid once.
- **F7 — doclint.** Path-filter the vault, or teach doclint a note type?
  *Recommend the filter;* the vault has its own lint (`capmap parse`) and
  that is the gate that should run over it.
- **F8 — Roots.** Three (toolbelt, practice, cell) or one with the two as
  branches? *Recommend three:* they have different customers, which is the
  level-1 definition.
- **F9 — Encoding.** New memberships appended for the anchor relation
  kind, the dimension, rating, basis, by and at — or the assessment as a
  tuple kind on the facts row under ADR-0326? *Recommend appended
  memberships first;* the tuple shape is the better one and the component
  codecs now admit it, but it is a second decision.
- **F10 — The old catalog.** If a copy of the 79-note vault turns up, port
  its prose onto the new skeleton; if not, write from the tree. *Recommend
  not waiting:* the skeleton is derivable either way.

## 10 Pages consulted, and incidents under the protocol

Read in full or in part on 2026-10-26:

- learnwardleymapping.com, *Landscape* — the evolution characteristics
  sheet (CC BY-SA 4.0).
- leanix.net, *Business capability assessment* (wiki) and the Metis
  Strategy and Kao pages (blog, customer story).
- cmmiinstitute.com, *Appraisal levels*; two CMMI Institute quality tips on
  evidence sufficiency; an arXiv 2026 comparison of documentless
  assessment methods.
- NASA, *Technology Readiness Level Definitions* (the 458490main table,
  hosted by ENCQOR Québec), software column.
- sfia-online.org, SFIA 9 *Levels of responsibility* and the alternative
  presentation of behavioural factors.
- Secondary pages on TOGAF capability mapping and capability-based
  planning (Berrisford; a University of Twente thesis; study guides),
  on BIZBOK design principles (LeanIX wiki, Bizzdesign blog, IF4IT), and on
  Gartner pace layers (a 2012 note hosted by the University of Florida;
  vendor blogs).

Incidents: pubs.opengroup.org (TOGAF business-capabilities guide; ArchiMate
strategy chapter) redirected to a login and was not followed; a vendor's
ArchiMate 3.2 concept sheet returned 403; the ISO/IEC 33020 preview PDF
carried no extractable text. Everything attributed to those three sources
above is marked *recalled* and carries no quoted figure.

## 11 The catalog skeleton, derived from the tree

The breadth deliverable at design stage: the nodes a re-derived boxer
catalog would carry, with the anchors each would cite, so the structure
can be reviewed before any prose is written. **Derivation rule.** Level 2
of the toolbelt is the positioning set (§2.4) plus the two the README
defers; level 3 groups each page's cited decisions by what a customer could
want on its own; the cell root is read off ADR-0269–0335's titles and
ADR-0307's threat model; the practice root is read off ENGINEERING_PRACTICES'
sections and the gov page. Decision numbers are anchors, not names — a
competence is never named after a decision (§3.2). Numbers in *italics*
were **proposed** on the compile date; struck ones are retired and are
listed so a note does not anchor to them by mistake. Status was read from
each file's front matter that day and will drift.

### 11.1 Root: toolbelt — for a consumer of the toolkit

| Level 2 | Level 3 candidates (anchors) |
| --- | --- |
| **keelson** — app runtime | app mounting and manifests (0026, 0135, 0158, 0214, 0260, *0335*); the bus and its codec (0036, 0143, *0290*); capability subjects and grants (0026, 0253, 0262); introspection tables (0094); background tasks (0038); app state and working sets (0148, 0185, 0105); the embed seam (0155); instance attribution and effects (0188, 0191, 0322); trust boundaries (*0207*); host bootstrap (0211); external-process chokepoint (0118); pre-spawned workers (0285); launchable-app limits (0272); one render goroutine (0261) |
| **leeway** — data mapping | table description and DDL generation; marshalling (0074, 0101, 0103, 0109, 0113, ~~0008~~); membership roles and single-membership declaration (0072, 0073, 0213, ~~0007~~); components and read contracts (0075, 0146, 0183, 0189, 0326); the SQL read surface (0116, 0171, 0181, 0226, 0289); canonical record form and wire (0201, 0210, 0325); aspects (0182); params codec (0233); co-ragged function pack (0162); data contracts (~~0060~~); the LWQ language (~~0022~~, ~~0023~~, ~~0010~~) — retired, listed so nothing anchors to them |
| **recordstore** | generated stores on facts-shaped tables (0100, 0105, 0089); call identity and write observers (0295); the evidence profile — chain, vault, marks, seals, anchors (0327); audit events as a trail component (0296); the shared audit recorder (*0330*) |
| **identity** | fibonacci-tagged ids (0106); technology-neutral leased generation (0111) and leases through one row (0319); tag minting (0183 D0); call identity |
| **nanopass** | pass discipline and environment (0002, 0006); rewrite combinators (0098); the pass registry (0108); parser bounding — DFA cache, two-stage SLL, exact qualifiers, LL islands, memoisation (0084, 0196, 0304, 0305, 0306); cost profiling (0192); passthrough classification (0117); text-to-DSL (~~0139~~, deferred) |
| **imzero2** — UI stack | the framed render pipeline (0049, 0062, 0261, 0128); widget contracts (0012, 0013, 0267, ~~0014~~); the design system and policy as code (0029–0032, 0034, 0040, 0044, 0065, 0156, 0258, *0273*, ~~0033~~, ~~0037~~); hosts — desktop, browser, headless accessibility, CPU-rasterized, appliance, native windows, tab mode (0024, 0154, 0205, 0206, 0243, 0263, 0278); remote sessions and viewers (0082, 0086, 0088, 0242, ~~0081~~); the headless driver and scenes (0154, 0248); interaction record and replay (0127); window arrangement, captures and trees (0275, 0276, 0281, 0301); keyboard capture (0177, 0279); the id stack (0321); deferred-block memoisation (0049); hidden desktop host (*0332*) |
| **widgets** | plots (0149); maps and geo (0204, 0096, 0114, 0244, ~~0056~~, ~~0203~~, ~~0165~~); graph views and the painter lane (0224, 0225, 0228, 0232, 0329 painter-lane); flows, icicle, treemap, sankey (0159, 0160, 0166); layered graphs and entity diagrams (0069, 0129, 0167, 0331); vector fields and particles (0249, 0250, *0291*); record tables (0151, *0336*); the native tree (0176); the SQL editor and completion (0147, 0190); markdown editing and blocks (0178, 0334, ~~0216~~); chat view (0239); timeline and time strip (0043, *0251*); gauge, spectrum, audio (0068, 0091, 0208); value inspectors (0046, 0067); FSM and pipeline views (0045, 0119); scrolling texture (0058); footnote glosses (0255); button kinds (*0273*) |
| **play** — SQL workbench | the reactive query graph (0097); panels — chart, distribution, kanban, cards, treemap, geo raster, choropleth, sankey, layered graph, graphview, flow graph, chat, time series, vector field (0172, 0161, 0122, 0245, 0166, 0096, 0114, 0129, 0227, 0153, 0239, 0163, 0250, 0231); parameters and expression slots (0124, 0187); content-typed detail (0123); column handles (0116); endpoint dispatch and engine adapters (0141, 0144, 0133, 0142); gloss catalog and rules (0186); the vocabulary panel (0174); canonical record identity (0219); operations catalog and agent limits (0270); the natural-language ask panel (~~0120~~) |
| **sqlapplet** — deferred page; a clause of play today | SQL-defined applets (0132); agent-operable applet windows (*0274*); loading from an origin (*0299*); ad-hoc datasets and bundles (0240, 0288, ~~0134~~); the creator app |
| **watchbill** | durable work on facts (0223); the client protocol and worker presence (0234, 0237); the management app (0236) |
| **lading** | the snapshot store (0198); the tally browser (0200); the composition surface (0222); rclone mounts |
| **stevedore** | framework-driven ingestion — routing, failure and state handling (0252); persisted streams from Connect (0005) |
| **pushout** | the patch model and wire (0079, 0209); forgetting and antiquing (0025, ~~0027~~, ~~0039~~ deferred); storage capabilities, retention, batch verbs (0220, 0221) |
| **observability** | the sysmetrics collector, data plane and tee (0019, 0090, 0184); query observability (0115, ~~0051~~, ~~0050~~); continuous coverage (0169); resource monitors and replay (0020, 0061, 0197); error shredding (0041); the split pprof listener (0212); code-volume self-inspection (0173); interaction tracing |
| **gov** | the license gate (0004, 0246); code and doc lint (0011); the downstream gate and skeleton (0179); the decision corpus as tables (0092); the competence corpus (0168); the data catalog (0170); dispatch classification (0107); package properties (0080); file naming (0048); provenance trailers (0083); adversarial review (0131) |
| **llm** — deferred page | model inference as a capability (0254); retained conversations and the chat app (0264, 0265); streamed answers (*0286*); metering and moderator rules (*0300*); the completion client |
| **analytics and science** | subsequence anomaly detection (0150); compression-distance similarity (0175); the graph analytics engine and force model (0229, 0230); explaining clusterings (0235); projection feature sets (0238); smoothing (0152); distributions and quantile grids; the H3 bridge (0003); Swiss coordinates (0244); the GRIB reader (*0292*); road routing (0256); line-of-sight (0099); units; scored renderings (~~0266~~) |
| **distribution and hosts** | the air-gapped bundle (0095); the appliance image (0206); reproducible builds (0215); release signing; toolchain adoption (0199); the tab host digest (0278) |
| **sync and markdown** | guided ClickHouse-to-ClickHouse sync (0259, 0271); the markdown ingestor and Obsidian rendering (0218, 0180); markdown to play (0217) |

### 11.2 Root: cell — for a person operating agents

| Level 2 | Level 3 candidates (anchors) |
| --- | --- |
| **operations** | the command/query contract under a task grant (0269); harness-exposed inference and coordinator tools (0315, 0318); agent-readable schema (0313); imztop and play operation catalogs (0329, 0270) |
| **grants and consent** | the person's decisions queue (0324); status and what opens (0328); a ceiling scored on a ladder (0280); unattended mode within bounds (*0298*) |
| **journal** | a run journal written before each act (~~0303~~ superseded, 0308, 0325); context projection as SQL (0309); compaction as an entry (0316); bodies in a vault under per-entry keys (0320) |
| **moderation** | the built-in moderator loop and escalation (*0302*); attention levels (0323); metered calls and rules (*0300*) |
| **exits and labels** | exits, sinks, approval tiers and budgets (0312); copy by reference through one choke point (0317); labels, integrity levels, TLP tiers (0311) |
| **evidence and audit** | one audit trail for model calls and agent work (0277); tamper-evident records (0314); the evidence profile (0327); the shared recorder (*0330*) |
| **profiles and surfaces** | harness profile and registry composition (0310, *0335*); windows agents read and arrange (0276); captures under one policy point (0281, 0284, 0287, 0301); the overlay (*0297*); the threat model (0307) |

### 11.3 Root: practice — for the maintainer

| Level 2 | Level 3 candidates (anchors) |
| --- | --- |
| **deciding** | decision records and their tiers (0001, the documentation standard); the QOC form; background-work surveys; the clean-room bar (0119 §SD6) |
| **measuring** | trials and the §0 citation rule; benchmarks in the default lane; cost profiling (0192) |
| **verifying** | the default and integration lanes; conformance suites; golden files; headless scenes (0248); continuous coverage (0169); the screenshot tour (0057) |
| **governing** | the gate and skeleton (0179); code and doc lint (0011); the license gate (0004, 0246); build-tag discipline (0106, 0212); file and package naming (0048); error conventions (0293); one CLI framework (0294) |
| **owning dependencies** | the P1 posture — reference, vendor, port, retire (0149, 0204, 0194, 0195, 0202, 0215, 0268); the air-gapped bundle (0095) |
| **recording provenance** | git trailers (0083); adversarial review (0131); AI-assisted documentation (the documentation standard §5) |
| **describing itself** | positioning pages; the ISO 25010 assessment; the architecture page; the changelog and its index; the decision, competence and data catalogs as tables (0092, 0168, 0170) |

Counting the cells: three roots, roughly thirty level-2 nodes and on the
order of a hundred and sixty level-3 candidates before any are merged or
split in review — about twice the August catalog's size, which is the
breadth the two new roots and two and a half months of decisions add.
