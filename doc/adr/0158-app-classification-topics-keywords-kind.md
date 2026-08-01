---
type: adr
status: accepted
date: 2026-08-01
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-01
---

# ADR-0158: App classification — topics, keywords, and kind

## Context

`app.Manifest` carries one classification slot, `Category string`: free-form,
single-valued, unvalidated. The launcher groups by it and searches a
case-insensitive substring over `Display` and `Category` only. Three
structural problems follow, each independent of how many apps happen to be
registered.

**The axis is provenance, not subject.** The largest bucket, `Applets`, does
not say what its members are for; it says they were built from markdown rather
than Go. So the Go dependency explorer, the repo code-exploration demo, and the
Go-dependency applets are views of one subject that no launcher view groups and
no query returns together. `Tools` has meanwhile become "a Go app that is
neither a demo nor runtime plumbing", holding a SQL IDE, a process monitor, a
splash screen, and a regex tester. Some buckets hold a single app — what a
scheme looks like when its axis does not fit its population. And because the
provenance bucket grows per committed markdown document rather than per Go
file, it grows fastest.

**Grouping information already exists and is discarded.** `sqlapplet.manifestFor`
mints one manifest per document and hardcodes `Category: "Applets"`, dropping
`AppletDef.BookID` — the book that contributed the document. That grouping is
authored and committed, then thrown away, because the one slot is already
spent.

**One field is being asked to do three jobs.** *Placement* (where an entry sits
when browsing) wants few buckets. *Retrieval* (what you can type to reach an
entry you already have in mind) wants many aliases per entry and no structure
at all. *Ranking* (what comes first when several match) wants observed
behaviour, not authored metadata. `Category` does the first adequately, is
pressed into service for the second, and does nothing for the third.

Two things bound the change. The inventory is serialised twice — by
`keelson('apps')` and by the carousel's `listSchema`, with drifted spellings —
so every field costs two edits and gets more expensive per consumer. And
breaking existing semantics is permitted here: the manifest field may be
removed and renamed rather than extended, and the launcher's grouping model may
change shape. That licence is load-bearing. An earlier revision of this ADR kept
`Category` additive so registration sites would keep compiling, and that
constraint, not the design, produced a two-field split (see §Alternatives).

Measurements behind the above — bucket sizes, field multiplicity, the drifted
column sets — are in
[app-organization-and-launching.md](../adr-background-work/app-organization-and-launching.md)
and are deliberately not restated here; they date, and none of the reasoning
turns on their exact values.

## Prior art

The shape below is not invented for this repository. Each decision has an
established analogue, and the alignment is the main argument for it.

- **Multi-valued subject classification from a registered vocabulary** is the
  freedesktop Desktop Entry `Categories` field: a *list* of strings from a
  registered set, with an `X-` prefix as the extension escape hatch, and the
  standing guidance to list every category that clearly applies and none that
  only vaguely applies. Entries in a real desktop corpus routinely carry
  several. An ecosystem forced to pick one would have had to make exactly the
  choice this repository's `Category` field currently forces.

- **A separate, ungoverned keyword field** is freedesktop `Keywords`, added
  after the fact because matching on the display name alone was not enough and
  the description fields were the wrong shape. The principle it encodes is the
  one worth borrowing: **governance is owed only on terms rendered as
  structure**. Categories are navigated, so they are registered; keywords are
  only matched against, so they are free — and in practice they proliferate
  freely without harming anything.

- **An entry appearing under every category it carries** is plain desktop-menu
  behaviour. Duplication across sections is how the menus have worked for two
  decades and is what lets the classification stay a single field.

- **Filter-first rather than tree-first** is the convergent shape of the
  modern launcher school — Spotlight, Alfred, Raycast, and the editor command
  palettes. A single input is the primary surface, results are ranked rather
  than navigated, and category survives as a scope or filter, never as the main
  path. This repository currently inverts it: the only surface with a search
  field is the empty-state pane, which disappears the moment a window opens.

- **Shallow independent facets over one deepened hierarchy** is the settled
  position in the faceted-classification literature: each facet considers one
  aspect and need not anticipate combinations, so navigation stays shallow and
  the user picks the axis instead of the designer. It is the direct argument
  against nesting subject beneath provenance.

- **A controlled vocabulary with a free layer underneath** is the recurring
  remedy for folksonomy failure — synonymy, word-form variation, single-use
  tags, and the absence of any way to hold consistency over time. Topics and
  Keywords are that hybrid, which is why free-form tags are refused as a third
  mechanism (§Alternatives).

- **Frecency** — frequency and recency folded into one exponentially decaying
  score — is the ranking layer the same launcher school relies on, with
  well-understood mechanics (a half-life, tuned short for fast-moving work and
  long for reference material). It is deferred here (§SD10), not dismissed:
  it is the recognised next step, and it is the one layer that needs no
  authored metadata at all.

## Design space (QOC)

**Question.** What classification should `app.Manifest` carry so that a growing
app-and-applet corpus stays browsable and findable?

**Options.**

- **O1** — Status quo: one free-form `Category string`.
- **O2** — Richer single axis: nested categories (`Applets/Go`, `Tools/Go`).
- **O3** — Free-form tags: an ungoverned `Tags []string`, folksonomy-style.
- **O4** — One multi-valued `Topics []string` from a registered vocabulary,
  replacing `Category`, with the browse view showing an entry under every
  topic it carries.
- **O5** — Two subject fields: `Category` (placement, one value) beside
  `Topics` (facets, many), both from one vocabulary.
- **O6** — SQL-native launcher: the GUI filter becomes a predicate over the
  same relation `--launch` already queries.

**Criteria.**

- **C1 — Axis collision** — does one subject stop being split across buckets?
- **C2 — One place to declare a subject** — can an author state "this is about
  code" exactly once, without a consumer having to union two fields or a
  reviewer having to check they agree?
- **C3 — Governance owed** — how much vocabulary must be maintained, and what
  notices drift?
- **C4 — Migration cost** — registration sites, applet documents, the two
  serialisation schemas, the two launcher surfaces.
- **C5 — Growth-proof** — does it still hold as the corpus multiplies?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 | O6 |
|----|----|----|----|----|----|----|
| C1 | −− | −  | +  | ++ | ++ | ++ |
| C2 | ++ | ++ | ++ | ++ | −  | ++ |
| C3 | ++ | +  | −− | −  | −  | ++ |
| C4 | ++ | +  | +  | −− | −  | −− |
| C5 | −− | −  | −  | ++ | ++ | ++ |

O4 and O5 separate from the field on C1/C5, and O4 beats O5 on C2: two fields
drawing from one vocabulary make "is this about code?" a two-place question,
with nothing preventing them from disagreeing. O4 pays for that on C4 — it is
the only option that breaks compilation at every registration site — which the
Context licences and §Migration bounds.

An earlier criterion, *unambiguous placement* ("does every entry have exactly
one browse home"), scored O4 negative and decided the earlier revision in
favour of O5. It is dropped: it presumed the static per-category tree that
§SD6 replaces, and the prior art is unanimous that multi-placement is normal.

## Decision

We will **replace `Category` with a multi-valued `Topics`** drawn from one
registered vocabulary, add an ungoverned `Keywords` field for retrieval, move
provenance off the subject axis into a `Kind` filter, and reshape the launcher
from a category tree into a facet-filtered list.

- **SD1 — One registered topic vocabulary.** `public/keelson/runtime/app`
  gains a registered set of topic tokens: lowercase, `[a-z0-9-]`, declared in
  one place, following the ADR-0009 environment-registry precedent. All tokens
  are equal — there is no placement-eligible subset, because §SD3 removes the
  notion of a single placement. Unregistered tokens are refused at
  registration. The initial vocabulary: `runtime`, `code`, `topology`,
  `observability`, `data`, `sql`, `ui`, `geo`, `about`. This is a starting
  point, not a ceiling — adding a member to a registry is explicitly not a
  decision (CODINGSTANDARDS § What triggers an ADR), so a later topic needs no
  ADR. Changing what the vocabulary *means* — its axis — would.

- **SD2 — `Topics []TopicT` replaces `Category`.** The field is removed, not
  deprecated. `Topics` is multi-valued, unordered, drawn from §SD1, and
  required non-empty for `SurfaceWindowed` apps. The authoring rule is
  freedesktop's: list every topic that clearly applies, none that only vaguely
  applies. The element is a **named type with exported constants**, not a bare
  string, following the `SurfaceE` / `CapDirectionE` precedent in the same
  package: it makes an in-tree typo a compile error rather than a
  registration-time one, which matters given §SD9. Runtime-supplied topics
  (applet frontmatter) parse into the same type and are checked against the
  registry.

- **SD3 — An entry browses under every topic it carries.** There is no primary
  topic and no single home. The no-query browse view lists each entry once per
  topic, so an applet about Go dependencies is reachable whether the reader
  thinks "code" or "topology". Duplication across sections is the point, not a
  defect. Alphabetical-by-`Display` ordering within a section is unchanged.

- **SD4 — `Keywords []string` is free retrieval text.** Ungoverned: no
  vocabulary, no validation beyond non-empty, duplicates and near-synonyms
  expected and harmless. It is **never rendered as structure** — it exists only
  to be matched against — and that is precisely why it owes no governance. This
  is the field that makes "processes", "cpu", "htop", and "deps" find the right
  app.

- **SD5 — `Kind` takes provenance off the subject axis.** A small enum on the
  manifest — `KindApp` (zero value), `KindApplet`, `KindDemo` — set by the
  registrar: the applet minter stamps `KindApplet`, demo apps declare
  `KindDemo`. It is a **filter and a column, not a section**: the launcher may
  offer "show demos" or "show applets" as a toggle, but neither is a place an
  entry lives. Without this field, retiring the `Applets` and `Demos` buckets
  would delete two views that are actually used; with it, those views survive
  as filters over a subject-organised spine.

- **SD6 — The launcher becomes facet-filtered, and both surfaces share it.**
  `preferredCategoryOrder`, `groupByCategory`, and the `"Other"` bucket are
  removed. In their place: one filter state (topic chips + `Kind` toggles + a
  query string) resolved by one function, consumed by both the `Apps ▾` menu
  and the empty-state pane. With no filter, the view sections by topic per
  §SD3; with a filter, it flattens to a ranked list. Matching becomes
  subsequence/fuzzy over `Display`, `Topics`, and `Keywords`; `Id` stays
  excluded (every entry matches "github"). The menu's lack of an in-bar search
  field is a separate, recorded egui constraint (`menu_button` closes on any
  outside click) and is **not** resolved here — the menu gets chips, not a text
  field.

- **SD7 — Applet books stop discarding their grouping.** `manifestFor` maps
  `AppletDef.BookID` to a default topic set, and the frontmatter gains an
  optional `topics:` list that overrides it plus a `keywords:` list. The
  existing `type:`/`audience:` keys stay help metadata and are not
  reinterpreted.

- **SD8 — Both inventory relations follow the field.** In `keelson('apps')`
  the `category` column becomes `topics` (a string list) and `keywords` /
  `kind` are added. The carousel `listSchema` takes the same change; its
  documented positional stability is **deliberately broken**, because the
  column it stabilised is the one being removed. Unifying the two schemas
  remains desirable and is *not* attempted here (§SD10) — but the
  compatibility argument that kept it deferred no longer applies, so the
  deferral is now a scope choice.

- **SD9 — Validation is a hard gate from the first commit.** Because §SD2 is a
  rename, the compiler finds every in-tree registration site in the same
  commit; there is no window in which a site is half-converted, so the
  warn-then-error staging an additive change would have needed is unnecessary.
  §SD2's named type narrows what can go wrong further: an in-tree topic that
  is not a declared constant no longer compiles. Two hazards survive it. A
  *registered but wrong* topic — apt-looking, misfiled — is invisible to both
  the compiler and `Validate`, and stays a review matter. And an unregistered
  topic arriving at runtime from applet frontmatter fails `Validate`, which
  `RegisterFactory` handles by dropping the app with a Warn — so a bad
  frontmatter topic removes an applet from the UI without an error. That drop
  behaviour is pre-existing and not fixed here; §Verification plan names the
  tree-wide test that contains it.

- **SD10 — Recorded deferrals.** Three things this ADR deliberately does not
  decide. **Ranking** — frecency, recents, pins — needs a launch record in
  `boxer.facts`, which is Tier-1 on its own, and the ordering problem is not
  yet demonstrated at this corpus size. **Free-form user tags** are rejected
  for now, with the kill reason in §Alternatives and the reopening trigger
  named: ADR-0132 "O4" runtime-authored applets growing the corpus outside
  review. **The SQL-predicate launcher** (O6) and the unification of the two
  inventory schemas stay open; this ADR is compatible with both and blocks
  neither.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Manifest` (exported, `public/`) | **removed:** `Category`; **added:** `Topics`, `Keywords`, `Kind` | every `app_register.go` site, `sqlapplet.manifestFor`, any downstream module compiling against the struct |
| Topic vocabulary (new named registry) | added | `Manifest.Validate`, the launcher's sectioning, anything asserting on category strings |
| `Manifest.Validate` | new vocabulary + non-empty checks | registration-time behaviour: unregistered topic → drop-with-Warn (§SD9) |
| `keelson('apps')` (ADR-0094 provider) | `category` → `topics`; `keywords`, `kind` added | provider tests, the `runtime-apps` applet that queries it |
| carousel `listSchema` | `category` → `topics`; positional stability broken | `--launch` predicates naming `category`, the `demo` command's help text, list-format goldens |
| `windowhost` launcher | grouping model replaced | `preferredCategoryOrder`, `groupByCategory`, `uncategorisedBucket`, `filterManifests`, both render paths, `windowhost_categories_test.go` |
| sqlapplet frontmatter | new optional `topics:` / `keywords:` keys | the committed applet documents, the authoring how-to, the ADR-0132 "O4" store validation gate |

## Alternatives

- **Status quo (O1).** Rejected by measurement, not taste: the split Go-tooling
  views, the single-app buckets, and the discarded `BookID` are three
  independent symptoms of one field carrying three axes.
- **Nested categories (O2).** `Applets/Go`, `Tools/Go` nests provenance above
  subject and so re-commits the diagnosed error one level down — against the
  faceted-classification position in §Prior art that shallow independent
  dimensions navigate better than one deepened tree.
- **Free-form tags (O3).** Rejected *for now*. They buy what §SD4's keywords
  already buy, while adding the folksonomy failure modes and a governance
  surface. A single-curator corpus removes the *disagreement* risk the
  literature emphasises but not the *drift* risk (`geo` in spring,
  `geospatial` in autumn), and the thing that contains drift is a registered
  vocabulary — which is §SD1 either way. Reopening trigger in §SD10.
- **Two subject fields, `Category` beside `Topics` (O5).** The decision in the
  previous revision of this ADR; it lost when the additive constraint was
  lifted. Its merit was migration: `Category` kept its name and type, so
  registration sites kept compiling and only values moved. Its cost is C2 —
  one vocabulary read through two fields means every consumer unions them, an
  author can put `code` in one and forget the other, and nothing detects the
  disagreement. Once a breaking change is permitted, a one-commit compile
  break to delete that class of inconsistency is the better trade.
- **SQL-predicate launcher (O6).** Not rejected — deferred (§SD10). It is
  where the repository's data-centricity invariant points, and it is what the
  headless `--launch` path already does; but a filter box cannot shell out to
  `clickhouse-local` per keystroke, so it needs either the in-process
  introspection engine on the keystroke path or a narrower predicate language.
  That is its own decision, and this ADR does not foreclose it.
- **Keywords only, leaving `Category` untouched.** The lightest cut: it fixes
  retrieval (§SD4) and nothing else, leaving the axis collision in place.

## Consequences

### Positive

- One subject stops being split by implementation technique: a `code` filter
  returns the dependency explorer, the code-exploration demo, and the
  Go-dependency applets together.
- A subject is declared in exactly one place. There is no primary-versus-rest
  distinction to get wrong and no union at the consumer.
- The corpus can grow without the browse view degrading — new applets land
  under a subject rather than swelling a provenance bucket, so ADR-0132 §SD2's
  gallery trigger recedes rather than approaches.
- Retrieval stops depending on guessing the display name; keywords cost one
  line per manifest and need no vocabulary decision.
- Authored grouping that exists today (`BookID`, and the frontmatter an author
  can now write) reaches the launcher instead of being discarded.
- The two launcher surfaces converge on one filter state, so a fix to matching
  applies to both.

### Negative

- Every registration site breaks at compile time, and every downstream module
  compiling against `Manifest` breaks with them. This is the price of C2 and
  is only acceptable because the Context licences it.
- `--launch` predicates naming `category` stop working, including any a reader
  has in a script or a runbook. `topics` is a list, so the equivalent predicate
  changes shape, not just spelling (`has(topics, 'code')`).
- An entry now appears in several browse sections, so the no-query view is
  longer than the registration count. That is the accepted cost of §SD3 and
  the first thing to re-measure as the corpus multiplies.
- A vocabulary is now a thing to maintain, and a wrong-but-registered topic is
  a mistake nothing catches — validation checks membership, not aptness.
- `Applets` and `Demos` stop being browse sections. They survive as §SD5
  filters, but anyone who navigates by them today has to learn a new gesture.
- Two inventory schemas now drift in three more columns rather than none;
  unifying them stays deferred.

### Neutral

- No change to how apps are *started*: `Open`, `windowhost.open`, `LaunchKind`,
  and the `--launch` *mechanism* are untouched. This decision is about
  finding, not launching — only the column a predicate names moves.
- Ranking stays absent, so ordering within a section remains alphabetical by
  `Display`. Whether that needs to change is §SD10's deferred question.
- The vocabulary is Go constants rather than facts-backed. That is the ADR-0009
  precedent and the cheap cut; a facts-backed vocabulary would be more
  data-centric and much heavier, and nothing here forecloses it.

## Migration

- **Breaks.** `Manifest.Category` is removed: every registration site fails to
  compile until converted, which is the intended mechanism — the compiler
  enumerates the work. `keelson('apps').category` and `listSchema.category` are
  removed in favour of `topics`, so `--launch "category = 'tools'"` and any
  saved query naming that column break and must become a list predicate.
  `listSchema`'s documented positional stability is broken (§SD8). Applet
  documents keep parsing — the new frontmatter keys are optional.
- **Path.** (1) Land the vocabulary, the `Topics`/`Keywords`/`Kind` fields, and
  `Validate`, deleting `Category` in the same commit. (2) Convert the
  registration sites the compiler names; set `Kind` on the demos. (3) Convert
  `sqlapplet.manifestFor` per §SD7 and add optional frontmatter keys to the
  applet documents. (4) Replace the launcher grouping with the §SD6 filter and
  add the chips and `Kind` toggles. (5) Move both relations to the new columns.
  Steps 1–2 are one commit because the tree does not build between them; 3–5
  are separate and each leaves the tree green.
- **Regeneration.** None. No codegen input, no leeway encoding, no FFFI2
  boundary — neither side of an FFI boundary needs rebuilding. `go generate
  ./...` output is unaffected.
- **Old shape.** `Category` is removed outright, not deprecated-then-removed:
  there is one consumer tree, the compiler finds all of it, and a deprecation
  window would only let the two-vocabulary inconsistency exist for longer. The
  one prose consumer is the `demo` command's own help text
  (`imzero2_demo_cli.go`), which advertises `--launch "category = 'tools'"` as
  a worked example and needs rewriting in the same change. That help text also
  points at `doc/howto/launch-apps-non-interactively.md`, which does not exist
  — a pre-existing dangling reference, worth fixing while the paragraph is open
  but not caused by this decision.

## Verification plan

- **Lane.** Default `go test` — no integration lane, no golden image, no
  screenshot scene is required. Four places carry it: a table test over
  `app.AllManifests()` asserting every topic in the tree is registered and
  every windowed app declares at least one; `Manifest.Validate` unit tests for
  the accept/reject cases; `windowhost_categories_test.go` rewritten for §SD6's
  filter and §SD3's multi-section browse; the sqlapplet mint tests for §SD7's
  `BookID` mapping and frontmatter parsing.
- **What would fail.** A new app registering with a typo'd or unregistered
  topic reddens the tree-wide table test — which is the containment for the
  §SD9 drop-with-Warn hazard, since the app would otherwise vanish from the UI
  silently. A regression that reintroduces provenance as a browse section shows
  up as a launcher test asserting a section named for how something was built.
- **Gap.** Nothing verifies that the chosen topics are *apt* — that an applet
  tagged `data` really is about data. Membership is checkable, judgement is
  not; that stays review, as it does for the env-var registry. Nothing verifies
  search *quality* either: the fuzzy matcher gets unit tests for specific
  queries, not a relevance measure. And nothing catches a `--launch` predicate
  that lives outside the repository. All three are acceptable: the first two
  are cosmetic misfilings rather than broken contracts, and the third is what
  §Migration's break note exists to announce.

## Status

Accepted (2026-08-01). Implementation follows the §Migration path.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-02 — implemented; four things the decision did not anticipate

§Migration steps 1–5 are done and the tree is green. Four refinements the
implementation forced, none of which change the decision:

- **Multi-placement collides widget ids.** §SD3 renders one manifest under
  every topic it declares, so the app id alone stopped identifying a launcher
  row: two sections derived the same button id, and a duplicate id resolves to
  one shared response — clicking either row would open whichever the id stack
  landed on. Both entry renderers now take a section key
  (`"open-" + section + "-" + id`). This compiles and vets clean either way,
  so it would have surfaced only as a mis-dispatched click.

- **The kind toggles cannot be written the obvious way.** `SendRespVal` is
  deferred to `StateManager.Sync`, after the frame body — so the natural
  `before := …; Checkbox(…).SendRespVal(&local); if local != before` never
  observes a change and the toggle is a silent no-op. The toggles bind
  straight to a persistent `[]bool` and the mask is derived per frame.
  `SelectableLabel` and `Button` report clicks in-frame and are unaffected,
  which is why the topic chips need no such mirror.

- **The two filter axes store opposite sets.** Kind stores what is *hidden*,
  topic stores what is *selected*. Both make the zero value inert, which is
  the property that matters; the difference follows the gesture — three kinds
  you normally want all of ("hide the demos") against nine topics you normally
  want one of ("show me only code"). A shown-set for kinds could not tell
  "everything hidden" from "nothing configured".

- **The menu carries no topic chips.** §SD6 says the menu gets chips; in
  practice its per-topic submenus already *are* the topic axis, so a chip row
  there would be two controls for one thing. The menu does carry the kind
  filter, as a `Show` submenu of state-reporting Buttons rather than
  checkboxes — the menu is the only launcher surface once a window is open, so
  a kind filter reachable only from the empty-state pane could not be undone
  without closing everything. Both surfaces share one `launcherFilter` value,
  so they cannot disagree about what is on screen.

Also worth recording: `registry.Demo.Category` (ADR-0057's screenshot-tour
struct) is a *different* field and was left alone. §Surfaces did not
distinguish the two, and a reader grepping `Category` will find both.

## References

- [app-organization-and-launching.md](../adr-background-work/app-organization-and-launching.md)
  — the status-quo measurement and design-space survey behind this decision.
  It carries the bucket sizes, the field-multiplicity measurements, and the
  sourced external prior art §Prior art summarises; this ADR deliberately does
  not restate them.
- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — `AppI`/`Manifest`/`Registry`, the contract this reshapes.
- [ADR-0132 — sqlapplet, SQL-defined applets](./0132-sqlapplet-sql-defined-applets.md)
  — §SD2 mints under `Category: "Applets"` and records the gallery fallback
  this decision is an alternative answer to; "O4" is §SD10's reopening trigger.
- [ADR-0094 — keelson introspection tables](./0094-keelson-introspection-tables.md)
  — `keelson('apps')`, the inventory-as-relation whose columns move here.
- [ADR-0009 — environment variable registry](./0009-environment-variable-registry.md)
  — the named-registry precedent §SD1 follows.
- [ADR-0135 — app launch requests](./0135-app-launch-requests.md) and
  [ADR-0148 — app workingsets](./0148-app-workingsets.md)
  — the launch and state paths this decision leaves untouched; ADR-0148's
  per-app record write path is what §SD10's deferred ranking layer would use.
