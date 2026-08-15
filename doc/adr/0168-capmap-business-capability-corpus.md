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
exists for *business-capability management*: what the toolbelt can do, how
mature each part is, where the pain is, and what depends on what. (boxer calls
the unit a **competence** — §SD6 says why the industry's word cannot be used
here.)

A standalone prototype answers that question already — vault markdown, a
ClickHouse read model, an HTMX front end — and its corpus includes a catalog of
boxer's own competences. Bringing it in makes boxer's self-description a
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

**Question.** Where do competence and relation facts live?

**Options.**

- **O1** — `boxer.facts` itself, with new kind memberships under a new TagValue.
- **O2** — A new leeway table of the same shape, with its own schema package, vocabulary and generated artifacts.
- **O3** — A composite mapping extending `LoadRuntimeFactsMapping` into a separate physical table.

**Criteria.**

- **C1** — Join reach: can a query relate competences to what the runtime knows about itself?
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

We will model the corpus as facts in `boxer.facts`, keep the vault
authoritative, and expose the result through the same five-piece shape the ADR
ecosystem uses. The subsystem is named `capmap`, after the artefact it builds —
a capability map is what the literature calls the deliverable. The **unit** it
holds is a *competence*, not a *capability*: §SD6 has the rule and why.

- **SD1 — Two fact kinds in `boxer.facts`.** A competence fact and a relation
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
  Rejected: parent ids as a `u64Array` column on the competence fact, which
  makes an edge a property of one endpoint and has no place to put the
  relation's own attributes.

  A relation carries how its target resolved, which is what retires the
  precomputed lint columns: a broken link is a relation, not a finding. M2
  found that this needs four states rather than two. Roughly a quarter of body
  links are citations — `Jouppi-1990`, `GDPR-Art-17` — that no competence slug
  could ever match, so they name something outside the corpus and are not
  defects; and the two spellings of a link to a directory-backed competence
  differ only in whether the link also resolves inside Obsidian. Collapsing
  either distinction into "broken" turns 3,257 apparent defects on the
  reference corpus into 1,698 real candidates.

  **`unresolved` is an upper bound, not the defect count**, and M6 measured by
  how much. Of those 1,698, only 217 name nothing that exists anywhere; 1,481
  point into the vault's sibling reference trees — `standards/`,
  `technologies/` — which §"Point it at a competence tree" deliberately keeps
  out of the read, because their names collide with competence names. A
  fifth state would need the parser to be told where those trees are, so it is
  not encoded; the applet book states the bound instead, and offers a
  `cited_by` fan-in count as the usable signal, since a missing target named
  once is a typo and one named a dozen times is a note that was never written.

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

- **SD6 — The unit is a *competence*.**
  And the vocabulary has its own tag-value base.

  **The word first.** boxer already spends "capability" on the runtime's
  security capabilities — ADR-0026's subjects, what `capslock` audits and a
  `capabilitygrant` records — and the literature spends it on the thing this
  corpus holds. One word for two unrelated ideas in one tree makes every
  mention ambiguous, and the flat `keelson('…')` namespace is where that bites
  hardest: a table called `capability` sitting beside `apps`, `env` and `procs`
  reads as the runtime's, not the corpus's. So **everything boxer names says
  competence** — the `Competence` model, the `capmapCompetence*` memberships,
  the `competence` / `competencesection` / `competencerelation` tables.

  **The vault does not.** It is authored in Obsidian against the
  business-capability literature: its marker file is `capability.md` and its
  links are `[[slug/capability]]`. Renaming those would fork the convention
  every other vault of this kind follows, and buy nothing — the collision is
  inside boxer, not in the vault. `capmapcorpus` is the translation point, and
  says so in its package doc. Rejected: renaming the subsystem too, which would
  churn five landed commits and an allocated tag-value base to relabel an
  artefact whose industry name really is "capability map".

  **The vocabulary.** Mirrors `public/keelson/runtime/vocab`:
  `capmap*`-prefixed natural keys in `LowerSpinalCase`, at
  `public/gov/capmapvocab`. Rejected: registering into the runtime vocabulary,
  which would put corpus names in a namespace whose prefix promises process
  state.

  Because nothing in the tree records which tag values are taken, this ADR is
  the register, and the allocation is named in the vocabulary package's doc
  comment where the next author will be standing. **Allocation is by base, not
  by single value**: a registry mints `base + tv` for any even `tv`, so a base
  reserves an open-ended range and "the next free integer" would place a new
  vocabulary inside an existing one's growth path. The rule is therefore **the
  next unused multiple of 16, owning the even offsets up to the following
  multiple**. capmap takes **16**; two bases predate the rule and are
  grandfathered at their present size, 1 (keelson vdd) and 2 (keelson runtime);
  the next vocabulary takes 32.

  Nothing but a test stands behind this. A collision does not fail to compile
  or to write — it makes two unrelated facts wear one membership id and
  quietly falsifies every query over either — so
  `TestTagValuesAreDisjointFromOtherVocabularies` compares capmap's minted ids
  against both other vocabularies, and is verified to fail when the base is
  moved onto one of theirs.

  **Superseded by [ADR-0183](./0183-leeway-component-consumer-simplification.md)
  D0 (2026-08-15).** The base rule and the register-by-ADR convention are gone:
  every vocabulary now claims one tag value from the width-32 class through
  `identity/tagmint`, which refuses a value already claimed, and capmap's is
  2178311. A width-32 tag holds ~4.3·10⁹ ids, so a vocabulary's growth happens
  inside its tag rather than in reserved room beside it. This ADR's own
  observation — that nothing but a test stood behind the allocation — is why:
  the fourth vocabulary was allocated under this rule and a fifth, in `apps/`,
  had already duplicated the runtime's value without anyone noticing.

  **A membership's id is its registration ordinal**, which SD10 made concrete:
  the registry composes the id from the count registered so far, so a new
  membership declared anywhere but at the *end of the block* renumbers every one
  after it — silently, since nothing about the change fails to compile or to
  write. The package comment used to say "at the end of its group", which is
  precisely the mistake. The whole name-to-id table is now written down in a
  test, so an insertion fails there rather than in somebody's ingested corpus.

- **SD7 — The corpus lives in-tree but git-ignored.** `doc/competences/`,
  symmetric with `doc/adr/`, holding the boxer catalog only, with a committed
  `README.md` and everything else ignored. Two catalogs are excluded: one
  derived from a private checkout, whose name must not enter a public tree, and
  a public process-framework catalog whose licence has not been checked against
  the repo's gate. Rejected: committing the corpus now, which would decide both
  questions by accident.

- **SD8 — The read path is keelson providers over the vault.**
  Not over the facts table. Three tables — `competence`, `competencesection`,
  `competencerelation` —
  reading the corpus live on the `adr` providers' precedent (ADR-0122 §SD4):
  registered statically, `FreshnessLive`, and empty rather than erroring
  off-repo. Rejected: letting applets query physical leeway columns, which
  would spread the encoding across every book.

  This inverts what this ADR first said, which was that providers would decode
  memberships out of `boxer.facts`. Two costs settled it. An applet whose table
  only exists once an ingest has been run is the failure mode the pprof
  datasets hit, and recovering from it took a rebinder that watches for the
  data to appear. And decoding from SQL means hand-written leeway column
  names, the coupling M1 found already spread across a hundred sites with
  nothing guarding it. A live read has neither, and costs ~150 ms for a
  ~1,700-competence tree against `capmapcorpus`'s existing snapshot window.
  The ingest keeps its own job — history, and joins on the ClickHouse side —
  and `competence.fact_id` carries the id it wrote, so the two surfaces can be
  joined without knowing how the id is derived.

  `competencesection` is split from `competence` for the reason `adrcontent` is split
  from `adr`: measured on the reference corpus the prose is 2.8× the metadata,
  and a query about maturity should not pay for it.

- **SD9 — No HTTP surface; the views are applets.** The prototype's four
  webapps are replaced by an applet book
  ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) over the provider
  tables. Rejected: porting the HTMX server, which would add a second UI stack
  and duplicate implementations boxer already has.

  **What an applet cannot be.** Measured against the prototype's two working
  screens, the reading one is an applet and the triage one is not: a Culler
  needs a cursor over an ordered result, a card layout, a way for a gesture to
  reach host code, and a write path — none of which a SQL document has, and the
  last of which is §Deferrals' open decision. The reading screen needs only
  things every book would use: a tab's placement declared in `tabs:`,
  enumerated params, a reset, and a predicate input. The
  [background survey §12](../adr-background-work/capmap-port.md) has the gap
  list and what each is worth. Nothing here changes: SD9 says the *views* are
  applets, and a triage surface is not a view.

- **SD10 — Triage state is a tag in the note's frontmatter.** A `tags:` list,
  normalised without the leading `#`, carried on the competence and encoded as
  one symbol attribute per tag under `capmapCompetenceTag`.

  Scores answer "how good is this"; a tag answers "what did somebody decide to
  do about it" — `needs-owner`, `merge-candidate` — and the two are wanted at
  once, so a tag is not a sixth maturity value. Frontmatter rather than an
  inline `#tag` in the body, which is where the prototype's culler wrote them:
  the body round-trips verbatim (SD5), so a body tag would be carried twice and
  a write would have to edit prose. Measured before choosing: the reference
  vault has **zero** notes with frontmatter tags and one with an inline one, so
  there is no installed base to keep faith with.

  The character rules are Obsidian's, restated for a whole-string check rather
  than the inline scanner's prefix scan — `needs!owner` is a malformed tag here
  and the tag `needs` there — and the two are pinned to each other by a test.

- **SD11 — The store is a round trip, not a write-only sink.** `boxer capmap
  load` writes the corpus into `boxer.facts` and `boxer capmap dump` reads it
  back and renders a vault, so a corpus can survive the vault being lost and an
  edit made anywhere can be brought back into diffable form. SD3 is unchanged:
  the vault is still authoritative, and `dump` is how a stored corpus returns to
  the form that is reviewed, not a second editing surface.

  **The read is written in column handles, not physical names.** The first cut
  spelled 28 physical column names out in one file and leaned on the guard that
  fails when a regeneration invalidates one. That was the wrong half of the
  bargain — the repository provides a read surface precisely so a query does not
  carry names that can go stale
  ([leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md);
  ADR-0116 handles), and the jsonbench-on-facts trial measured what not finding
  it costs. The query now names `section:column` and a client-side pass
  resolves it against the schema the DML artifact generates, so the names come
  from the same source the writer writes through and a test asserts every
  handle resolves with no server in reach.

  **The scalar reads are `LW_GET`.** Each names a section and a membership id;
  the expansion pass resolves the lanes and emits the read-back call
  (ADR-0181 §SD3). Nothing about the flattened layout is written here any more.

  **The plural reads are `LW_SEL`.** This encoding writes several attributes
  under one membership on purpose — a tag each, a section each, a lifecycle
  entry each — which is the question `LW_GET` cannot ask, and until the
  selector landed it was the reason this package still filtered an identity
  lane by hand. `LW_SEL_ATTRS` returns the attribute indices a membership
  occupies and `LW_SEL` the membership-lane positions; the value lane and the
  mixed channel's parameter lane — the section heading, the lifecycle phase
  (§SD5) — project through them and stay aligned because the two selectors are
  co-indexed. The gather on an array-valued section goes through
  `LW_RAGGED_ELEM`, since there an attribute index is not a value index.

  Nothing in this package computes a lane position any more.

  **This gives the dump a precondition: a provisioned server.** The read-back
  family is installed by `boxer leeway sqlsurface install`, not by writing to
  the table, so a store that has only ever been written to cannot be read back
  until an operator provisions it. `ReadCorpus` therefore checks
  `LW_SURFACE_VERSION()` first and refuses with that sentence, because the
  alternative — `Unknown function LW_VALUE_BY_TAG_EQUAL` — is no help to
  somebody recovering a corpus. A version that does not match what this build
  emits is refused rather than attempted: a silently different surface is the
  failure mode that returns wrong rows instead of an error.

  Two things the read-back had to get right, neither of which a unit test can
  reach. A section's memberships are stored flattened across attributes with a
  per-attribute cardinality beside them, so a membership's position in that
  array is the attribute's index only while every earlier attribute contributed
  exactly one — true of what the encoder writes today, and it would stop being
  true the day a mixed-membership attribute is written first. The decode goes
  through the cardinality column instead. And ids are derived, so a re-load
  restates entities rather than minting new ones and the table holds one row per
  competence per load; the newest wins.

  Not carried back: whether a frontmatter link was written `[[slug]]` or
  `[[slug/capability]]`. The encoding stores the resolved target, not the
  spelling. Both name the same competence and the frontmatter kinds are exempt
  from the dirref rule, so a re-read of a dumped vault yields the same corpus —
  the difference is textual, against an original that used the qualified form.

### Milestones

- **M1 — `factsschema` aspects (SD4).** Regenerate the four artifacts.
- **M2 — `public/gov/capmapcorpus`.** Vault parsing as a pure library; blake3
  natural keys. The retired build tags and the drifted test assertions turned
  out not to belong here: both sit in packages this port replaces rather than
  carries, so neither is ported and neither needs fixing.
- **M3 — the `capmap` vocabulary (SD6).**
- **M4 — ingest.** `boxer capmap ingest --vault` (M9 renames the verb `load`
  and keeps `ingest` as an alias), plus a `parse` verb that
  reports the corpus and needs no database. **Not** through `FactsStoreI` as
  first planned: that interface is a closed per-kind surface — twenty methods,
  `WriteGrant` through `WriteColumnWidth` — with no generic write, so using it
  would mean adding `WriteCompetence` to a keelson-runtime contract for a gov
  concern and obliging every implementer to carry methods for a tool it does
  not use. Encoding lives in `public/gov/capmapfacts` instead and hands
  finished Arrow batches to a one-method sink that `chclient.Client` already
  satisfies, which is also what lets the encoding be tested with no ClickHouse
  in reach.
- **M5 — providers (SD8).**
- **M6 — the applet book (SD9).** Four lenses under `TopicCode`:
  `comp-overview`, `comp-browser`, `comp-map`, `comp-lint`. The treemap lens is
  the one §Deferrals left conditional, and ADR-0166 landing is what made it
  buildable. Its `color` channel is the level-2 ancestor rather than `domain`,
  measured: the shipped catalog has one domain, so colouring by it produces a
  single-colour picture, while the branch gives thirteen. Building it also
  found that an applet ignored its document's `tabs:` order and opened on the
  dock's own first tab, so a treemap applet landed on a table — fixed in
  [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) (Update 2026-08-05),
  which corrects three other books' landing tabs with it.
- **M7 — corpus and ignore rule (SD7).** The committed `README.md` §SD7 asks
  for turned out to parse *as a competence* — `readme` is a well-formed slug,
  so nothing else caught it, and the first read reported 80 competences and a
  relation the corpus never declared. `ParseDir` now skips it and reports it
  as a skipped file.
- **M8 — tags (SD10).** `Competence.Tags`, the `tags:` frontmatter reader, the
  `capmapCompetenceTag` membership, a `tags` provider column, and the tag knob
  and coverage rows in the applet book. It also turned up what SD6 now records
  about ordinals, since adding a membership is what made the hazard concrete.
- **M9 — load and dump (SD11).** `capmapcorpus.WriteVault` renders a corpus
  back to markdown, `capmapfacts.ReadCorpus` reads one out of the table, and
  `boxer capmap dump` joins them; `ingest` becomes `load` and keeps its old name
  as an alias. Writing the renderer is also what supplied the parse-render test
  §Verification plan had claimed for two milestones and did not have.
- **M10 — the read-back moves onto the SQL read surface (SD11).** It landed
  spelling 28 physical column names out and hand-writing the flattened-layout
  arithmetic, which is the thing
  [leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md) exists to
  stop. Now: column handles resolved against the generated schema, `LW_GET` for
  the scalars, `LW_SEL` for the plural reads, and a version check so an
  unprovisioned server says so. It took two passes — the first left the plural
  and mixed-channel reads hand-written because the surface could not express
  them, and `LW_SEL` plus the mixed channels landed days later. Eleven column
  handles remain where there were twenty-eight, and every one of them is a
  value or parameter lane a selector projects through; the identity and
  cardinality lanes are the expansion's business now.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `boxer.facts` column encoding (`factsschema`) | Reshaped — two sections gain value aspects, one gains a heavier codec | The four generated artifacts under `factsschema/`: `ddl/`, `dml/`, `dml_cbor/`, `ra/`, all via `boxer runtimecodegen` |
| `boxer.facts` row vocabulary | Added — two fact kinds and their memberships, named for the *competence* unit (§SD6) | The new `capmap` vocabulary package; the providers that decode them |
| TagValue allocation | Added — a second allocation, and the rule for making further ones | This ADR is the register; the vocabulary package's doc comment cites it |
| Environment-variable registry ([ADR-0009](./0009-environment-variable-registry.md)) | Added — the corpus location | `doc/env-vars.md`; the package must be reachable from the `public/app` link graph or the spec stays invisible |
| keelson table-name namespace ([ADR-0094](./0094-keelson-introspection-tables.md)) | Added — `competence`, `competencesection`, `competencerelation` | `RegisterStatic` and the name roster it is pinned by; the applet book's queries |
| Applet id namespace ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) | Added — a fifth book, `capmap`, minting `cap-overview`, `cap-browser`, `cap-map`, `cap-lint` | The cross-book slug-collision test, which counts every minted applet |
| `boxer.facts` row vocabulary (again, M8) | Added — `capmapCompetenceTag`, appended last because ids are ordinals (§SD6) | The golden name-to-id test; the encoder; the `tags` provider column |
| keelson `competence` table columns | Added — `tags` | The applet book's `tag` knob and its coverage rows |
| `boxer capmap` verb names | Renamed — `ingest` is `load`, which keeps `ingest` as an alias; `dump` is new | The command's own help; the prose in `doc/competences/README.md`, the providers and the book |
| Exported Go API under `public/` | Added — `capmapcorpus`, `capmapvocab`, `capmapfacts` under `public/gov/`, and the `boxer capmap` command. M8/M9 add `NormalizeTag`, `ParseResolution`, `RenderCompetence`, `WriteVault`, `SortCorpus` and `capmapfacts.ReadCorpus` | Nothing yet; no downstream module compiles against them |

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
- Of the prototype's four webapps, two are replaced (browser, lint) and two are
  deferred with the triage workflow (culler, cull-configer). The treemap gap
  closed when ADR-0166 landed, and `cap-map` reads its nodes contract. The
  replacement is not feature-parity: the browser's enumerated filters, its
  reset, its free `WHERE` bar and its pane placement are play and sqlapplet
  features that do not exist yet, and the lenses are knobs and tabs until they
  do (background survey §12).
- The store holds a corpus that can be recovered from it (§SD11), which is what
  makes `boxer.facts` a place to keep one rather than only a place to query
  one. It also means two copies exist and can disagree; SD3 says which wins.

### Neutral

- `capmap` joins `capslock` and `capabilitygrant` in a namespace where "cap"
  abbreviates two unrelated things. That is as far as the overlap goes: §SD6
  gives the unit its own word, so nothing boxer names is called a capability
  twice. The subsystem prefix is the one place the abbreviation is shared, and
  it is shared with the artefact's industry name rather than with the runtime's
  concept.
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
- **The §SD6 rename re-keys every row, and that was free exactly once.** The
  natural-key domain separator went from `capmap.capability` to
  `capmap.competence` and every membership natural key changed with it, so an
  id derived before the rename does not match one derived after. It cost
  nothing because no corpus had been ingested anywhere — the encoding had only
  ever been exercised by tests and one integration run against a scratch
  database. Doing it later would have meant a re-ingest of every store holding
  the corpus, which is why the word was settled before the first real write
  rather than after.

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

  **That last sentence was not true when it was written.** There was no
  renderer, so there was no such test, and this plan asserted a guard the tree
  did not have from M2 until M9 built one. It now says what it always claimed:
  a fixture vault is parsed, written back with `WriteVault`, parsed again, and
  the two corpora compared — competences, relations, tags, lifecycle and prose.
  The comparison is over the model rather than the bytes, because YAML emission
  style is not what the corpus means. The lesson is the general one about
  verification plans written before the code: a claim in this section is a
  promise, and the only thing that keeps it is a test somebody can name.

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
  M4 landed that integration test:
  `TestIngestRoundTripsThroughClickHouse` writes a corpus and reads it back as
  SQL, asserting the three shapes a unit test cannot reach — a scalar decoded
  by membership id, a section heading carried as a mixed membership's
  high-card parameter (§SD5), and a relation joined to both endpoints through
  `foreignKey` (§SD2). It builds its own scratch database and never touches
  `boxer.facts`, since a test that dropped the runtime's table would destroy
  real state to check an encoding.
  M6 added the book's own half of the gate. The corpus test pins the four
  documents' shape; `TestCapmapBookQueriesExecute` then runs every buffer
  verbatim — SET prelude included — through the introspect engine over the live
  provider tables, which is what a parse cannot reach: a buffer that parses,
  classifies and mints can still name a column that does not exist. It reads a
  fixture vault rather than `doc/competences`, because that directory is
  git-ignored and a test that only passed on a populated working tree would be
  green for the wrong reason. The fixture carries the shapes the buffers reason
  about — a directory-backed competence, a multi-parent level-4 leaf, an
  interior node with prose of its own, and one relation of each resolution.
  M9 added the read-back's half, in the same lane and for the same reason —
  the decode is SQL over physical columns, so none of it can be exercised
  without a server. Three tests: what was written comes back as what was
  written; a second load of a changed corpus reads back as the newer one and
  not as two; and a fixture vault loaded and dumped parses equal to itself,
  which is the only test that exercises the two verbs against each other rather
  than each against a fixture.
- **Gap.** The serverless read path — facts-shaped Arrow through `file()` — has
  been reasoned about but not run; proving it is a step inside M4 rather than a
  standing lane. Nothing verifies that the corpus content is *correct*, only
  that it parses, encodes and decodes; competence maturity and pain scores are
  human judgements with no oracle. That gap is wider than it sounds: on the
  reference corpus **nothing has been assessed at all** — every one of the
  1,722 competences carries the `255` sentinel for both maturity and pain, and
  none carries a lifecycle record. So the scoring half of what §Context calls
  "how mature each part is, where the pain is" has a schema, an encoding and a
  query surface, and no data. `cap-overview` reports the coverage rather than
  averaging over an empty column.

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

  **2026-08-14 — the trigger is closer and the shape is known.** SD10 gives
  triage somewhere to land, so what is missing is the write, and it is a
  narrower question than "a UI that mutates repo files": measured, `fsbroker`
  writes a whole file to the one path a picker granted, and the "pick folder"
  grant it already has (`fs.dialog.bundle`) carries no path-relative operation.
  So a surface that tags one of a thousand notes per keystroke cannot go
  through the dialog path at all — the choice is a new broker operation scoped
  to a granted directory, or keeping the mutation in a CLI verb and leaving the
  UI read-only. The
  [background survey §12](../adr-background-work/capmap-port.md) has the rest
  of what such a surface needs from play, none of which is blocked on this.

  **Settled the same day: the CLI is the mutation surface.** §SD11's `load` and
  `dump` move the corpus between the vault and the store, editing stays in the
  vault, and no in-app write path is built — so this deferral stops being an
  open question about a UI and becomes a scope line. `fsbroker` gains nothing,
  and the reading surfaces stay reading surfaces.
- **A treemap panel for play.** ~~Not deferred by this ADR~~ — settled. A play
  Treemap panel was decided separately in ADR-0166, in flight at the time of
  writing, and landed; SD9's book gained the hierarchy lens as this said it
  would. It is sized by prose bytes rather than by maturity, since there is no
  maturity to size by (§Verification plan), which makes it a
  size-and-*structure* map rather than the size-and-maturity one
  [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) described.

## Status

Proposed — awaiting review.

Reconciled against the tree on 2026-08-14, which is what a proposed ADR's
in-place edits are for: M8 and M9 were added, SD10 and SD11 record decisions
taken while building them, SD6 gained the ordinal rule that adding a membership
exposed, and §Verification plan's claim about a parse-render test was corrected
— it named a guard the tree did not have. What has *not* changed is the shape:
the nine original decisions all still describe what is there. The two things a
reviewer should weigh before this is accepted are SD11, which pays SD8's
coupling on purpose for the dump path, and the §Deferrals note on triage, which
is the only open decision left.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [Background survey](../adr-background-work/capmap-port.md) — measurements and the reasoning behind each fork.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD6 — the `boxer.facts` table this widens.
- [ADR-0092](./0092-adr-overview-tool.md) — the corpus-as-tables shape being copied.
- [ADR-0109](./0109-leeway-marshall-multi-membership-ref-tuples.md) — multi-membership ref tuples, the relation carrier.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — applet books.
- [ADR-0148](./0148-app-workingsets.md) — the data-centricity invariant and the provider precedent.
