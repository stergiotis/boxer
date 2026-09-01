---
type: adr
status: proposed
date: 2026-08-31
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0214: The launcher as an app — summary, detail, and ranked recall

## Context

[ADR-0158](./0158-app-classification-topics-keywords-kind.md) fixed how apps
are *classified* and left two things open by name: how much an entry tells you
about itself, and how several matching entries are ordered (§SD10). Both are
now the presenting complaint — the launcher has too many entries and too little
per entry to choose between them.

The corpus is 72 registrations (49 applets, 16 apps, 7 demos) against the 34
ADR-0158 was designed at, one month earlier. Growth is applet-driven, so it
tracks authoring speed rather than Go-package count, and any shape decided here
has to hold well past 72.

Three facts about the present surfaces bound the decision.

**The row carries identity and nothing else.** A launcher entry is
`Manifest.WindowTitle()` — an icon plus a title averaging 15.6 characters —
with an em-dashed topic suffix in the flattened search view. Identity works:
all 72 apps declare an icon, they resolve to 61 distinct glyphs, and no two
apps share a `Display` label. Discrimination does not work at all, because
nothing in the row says what any entry does. `keelson('apps')` meanwhile
serialises eighteen columns per manifest and the launcher renders two.

**The searchable surface is the one that disappears.** The search box lives in
the empty-state pane, which is rendered only while no window is open; the
`Apps ▾` menu has no search affordance. ADR-0158 recorded this at 34 apps as an
inversion of the convention it borrowed from. At 72 it means the corpus is
unsearchable for the whole time the runtime is being used.

**The information the launcher would need mostly exists already.** 87 declared
`SubjectFilter`s carry reader-facing `Reason` prose. 54 registrations ship a
`Help fs.FS`. Every open and close already writes a timestamped app-lifecycle
row to `boxer.facts` under [ADR-0026](./0026-app-runtime-and-capability-subjects.md)'s
audit trail — so ADR-0158 §SD10's stated blocker for ranking ("needs a launch
record in `boxer.facts`") has dissolved; what is missing is a cross-run
aggregate read, which the existing run-anchored reader deliberately declines to
offer.

The measurements behind the above, and the survey of how launchers elsewhere
answer this, are in
[app-launcher-information-design.md](../adr-background-work/app-launcher-information-design.md)
and are not restated here.

## Prior art

ADR-0158 §Prior art recorded the *filter-first* school (a single ranked input
as the primary path, categories demoted to scopes) and frecency's mechanics.
This decision leans on three further conventions from the same corpus of
launchers, summarised in the background page §4:

- **Three slots per row** — identity, one line of description, a type marker.
  freedesktop's Desktop Entry spec, whose `Categories` and `Keywords` this
  repository already borrowed, carries `Name`, `GenericName` and `Comment`;
  GNOME and KDE render `Comment` as the row subtitle. Two of those three
  descriptive fields were adopted and the third was not, which is the row's
  present poverty in one sentence.
- **List plus detail** — when the row cannot hold enough to commit, every
  surveyed launcher adds a second region rather than a taller row (Spotlight's
  preview, Raycast's per-item detail with a metadata panel, Windows' best-match
  pane, GNOME Software's app page). It costs nothing per row, which is what
  makes it the right answer for a growing corpus.
- **A curated landing view** — no surveyed launcher defaults to the whole
  inventory in alphabetical sections. Recents and suggestions are the default;
  browse-everything is a deliberate second view.

The internal precedent matters more than any of them: `runtime/helphost` is
already a registered app, opened by a global key through `OpenOrRaise`, laid
out as a left nav with a search box beside a central reader. A launcher is that
object with a different corpus, and reusing its shape is most of this ADR's
implementation.

## Design space (QOC)

**Question.** What should the launcher be, and what should it show, so that a
person can tell 72-and-growing entries apart and pick one?

**Options.**

- **O1** — Enrich the empty-state pane in place: summaries and a detail region,
  no new surface.
- **O2** — Promote the launcher to a registered app on a global key, with the
  pane rendering the same component.
- **O3** — An overlay palette drawn by the host chrome rather than a window —
  the Spotlight shape, outside the app model.
- **O4** — Leave the surfaces alone and invest only in ranking, on the theory
  that a well-ordered short list needs no descriptions.

**Criteria.**

- **C1 — Searchable while apps are open.** The corpus is unsearchable today
  exactly when it is being used.
- **C2 — Cost per row is bounded as the corpus grows.** Growth is at authoring
  speed.
- **C3 — One code path per decision.** Two surfaces that must be kept
  agreeing have already drifted once.
- **C4 — Reuses the app model rather than escaping it.** A second window
  mechanism would owe its own lifecycle, audit and focus story.
- **C5 — Reader-facing register.** "Not too nerdy" is half the requirement.

O4 fails C1 and does not answer the presenting complaint. O3 satisfies C1 but
fails C4 — an overlay is a third host surface with no manifest, no audit row
and no place in `keelson('windows')`. O1 fails C1 by construction. **O2 is
selected**, and the rest of this decision is its consequences.

## Decision

- **SD1 — The launcher is a registered app.** A new `runtime/launcher` package
  registers one `SurfaceWindowed` app (topic `runtime`, kind `app`), opened by
  a global key through `OpenOrRaise` so a repeated press raises the existing
  window instead of stacking another. This is `helphost`'s shape and F1's
  binding discipline, deliberately: the launcher gains window chrome,
  geometry memory, an audit row and a `keelson('windows')` entry for free, and
  the host needs no new concept.

- **SD2 — One component, two mount points.** `windowhost`'s empty-state pane
  renders the same launcher component the window renders. The `Apps ▾` menu
  stops trying to be a browser and keeps only recent entries plus a "Browse all
  apps…" item that opens the launcher. C3 is satisfied structurally rather
  than by keeping two render paths agreeing through a shared predicate
  function, which is the arrangement that drifted before.

- **SD3 — The launcher does not import `windowhost`.** It declares a narrow
  host interface — open-or-raise, open-with-config, and which apps currently
  have windows — that `windowhost.Inst` satisfies, and `hostboot` injects the
  host at boot. The direction is forced: SD2 makes `windowhost` a consumer of
  the launcher component, so the launcher must not consume `windowhost`. It is
  also the arrangement `windowhost` already uses for its own dependencies,
  which arrive as `*app.Registry` and `factsstore.FactsStoreI`.

- **SD4 — `Manifest.Summary`, authored, one line, required for windowed apps.**
  A new manifest field carrying what the app does, validated non-empty for
  `SurfaceWindowed` exactly as `Topics` is. Applet documents carry it as a
  required frontmatter `summary:` key, read by the minter beside `topics:` and
  `keywords:`.

  **Authored rather than derived**, though a derivable source exists (54
  registrations ship help corpora, and every one of their 289 documents has a
  lead paragraph). Three reasons. A lead paragraph is written to *open a
  document* and a summary is written to *distinguish a row from its siblings* —
  they read alike and do different jobs, and the job that matters here is
  telling 19 `data` entries apart. Deriving would leave the 18 apps with no
  help corpus with no summary at all. And a derived line would need a
  convention for which document speaks for a multi-document corpus, which is a
  second contract to maintain for a field that is one string.

  The cost is accepted deliberately: 72 authored strings, a compile break on
  `app.Manifest`, and 49 frontmatter edits. ADR-0158 already established that a
  one-commit break to `Manifest` is the better trade than a second field nobody
  can keep consistent.

  **No length cap in `Validate`.** A rejected manifest is dropped with a Warn
  (ADR-0158 §SD9), so a cap would silently remove an app from the launcher over
  a style matter. The style budget — roughly 60 characters, verb-first, never
  repeating the name — is enforced by the tree-wide test instead, where a
  violation is a red build rather than a missing app.

- **SD5 — The row is three slots, and the list virtualises.** Icon, then
  `Display` over a dimmed `Summary`, then right-aligned badges. A badge appears
  only when it says something: provenance for applets and demos and not for the
  ordinary case, and an open-window marker. The list renders through the
  virtualising table with a visible-range query, so per-frame cost is a function
  of window height rather than corpus size — `fsbrowser` is the working
  precedent for exactly this list shape, keys included. C2 is met by
  construction, which is what lets the corpus keep growing without revisiting
  this decision.

- **SD6 — The detail pane renders what the manifest already declares.** For the
  selected entry: the summary; the metadata `keelson('apps')` already
  serialises (topics, keywords, kind, launch kind, workingset, registration,
  version); **what it touches**, rendered from declared `Caps` and their
  `Reason` prose; the app's help contents; and an actions row — open, raise,
  open help. Beyond SD4 this needs no new data, which is the point: the
  eighteen columns already exist and have had nowhere to go.

  The help section is the book's **contents list**, not its rendered prose.
  This paragraph originally said "the help book's lead document through the
  markdown widget", and that was descoped rather than gated: `markdown.Doc`'s
  ids come from a per-Render sequence whose generation the caller must key an
  IdScope on, so a launcher embedding one has to be revised whenever the
  markdown renderer learns a segment kind. That is a bad trade for prose the
  Help center is one click away from, and the click is in the actions row.
  Reopening it wants a rendering entry point that owns its own id scope.

- **SD7 — Launch history is an optional store capability.** A new reader
  interface, type-asserted by consumers, implemented only by the
  ClickHouse-backed store: per app, a decayed launch score and a last-opened
  time, aggregated across runs over the app-lifecycle `started` rows. This is
  the [ADR-0155](./0155-app-embed-seam.md) §SD1 pattern that
  `RunEventReaderI` already follows, and it is what makes the in-memory
  fallback correct rather than special-cased: no capability means no history,
  and the launcher shows its authored-metadata view. The existing run-anchored
  lifecycle reader is left alone — its refusal to scan globally was a
  deliberate choice for a different consumer.

- **SD8 — Frecency orders the landing view and biases the typed one, bounded.**
  The score is the usual exponentially-decayed sum over launch events, with a
  half-life registered as an env var (ADR-0009) so it can be tuned without a
  rebuild.

  With no query, frecency *is* the order, and the landing view shows recent and
  frequent entries rather than the whole sectioned corpus — the surveyed
  default, and the half of the complaint that says "too many apps".

  With a query, the ADR-0164 pattern battery's score dominates and frecency
  adds a **bounded bonus, capped below one field-weight tier step**. The cap is
  the load-bearing part: an unbounded blend makes "why is this first"
  unanswerable, and lets history lift a weak keyword match above a display-name
  match, which is the failure mode that makes learned ordering feel
  capricious. Bounded, frecency reorders within a relevance band and never
  across one.

- **SD9 — Two IDL additions, both generalising something that exists.**
  `TextEdit` gains `CaptureKeys(mask)`, the mask form of the `CaptureTab` it
  already has and the form `Frame` already carries; and a global key fetcher is
  added beside `fetchF1KeyPressed`. The second is what
  [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) §Context
  anticipated in as many words — "future runtime-level shortcuts (debugger,
  command palette) would each add their own fetcher to keep the consumed-event
  ownership explicit per binding".

  The keyboard contract they buy: the field takes focus when the launcher opens
  (`RequestFocus` exists), typing filters, ↑/↓ move the list cursor while the
  caret stays in the field, Enter opens the cursor row, Escape surrenders focus
  and closes. Without the `TextEdit` half this collapses to mouse-plus-Ctrl+Enter,
  which fails C5 — a launcher you cannot type into and arrow through is a
  dialog.

  The binding is **F2**, beside F1's help: unused anywhere in the tree, and
  symmetric with the one global key the runtime already owns. A bare function
  key is chosen over the conventional Ctrl+K palette binding because the
  fetcher consumes globally — a bare key steals nothing from a focused
  `TextEdit` in any app, and a global consuming Ctrl+K would take a keystroke
  every text field has a claim on.

- **SD10 — The register is reader-facing where it is visible.** The search
  placeholder names what the box searches and not its syntax; regex
  diagnostics appear when a token actually fails to compile rather than
  standing as a permanent hint. Vocabulary tokens get display labels for
  chips and section headings, which is a rendering function over the closed
  vocabulary and not a change to it. The default row action becomes
  open-or-raise, so clicking an app that already has a window raises it
  instead of silently opening a second — today's behaviour, and a bug by any
  reading.

- **SD11 — Recorded deferrals.** **Previews** are not built: the ADR-0057
  driver can already render any app, but where images live, how staleness is
  handled, and whether a launcher may raster per row are three undecided
  questions, and 49 of 72 entries are applets that would mostly resolve to the
  same table. **Pins** are not built — frecency covers the demonstrated need,
  and an authored pin list is a second ordering input to reconcile with it.
  **Free-form tags** stay refused on ADR-0158's reasoning with its reopening
  trigger intact, and **the SQL-predicate launcher** (ADR-0158 O6) stays open;
  this decision blocks neither.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Manifest` (exported, `public/`) | **added:** `Summary` | every `app_register.go` site, the applet minter, any downstream module compiling against the struct |
| `Manifest.Validate` | non-empty `Summary` for `SurfaceWindowed` | registration-time behaviour: a missing summary drops the app with a Warn (ADR-0158 §SD9) |
| sqlapplet frontmatter | new **required** `summary:` key | the committed applet documents, the authoring how-to, the ADR-0132 "O4" store validation gate |
| `keelson('apps')` (ADR-0094 provider) | `summary` column added | provider tests, the `runtime-apps` applet |
| carousel `listSchema` | `summary` column **appended** — positional stability preserved | list-format goldens (the column count) |
| `runtime/launcher` (new package) | added | `hostboot` wiring, the carousel's blank-import set |
| `windowhost` | empty-state pane delegates; `Apps ▾` reduced; default action becomes open-or-raise | `renderEmptyState`, `RenderAppsMenu`, `renderEmptyStateEntry`, `renderAppsMenuEntry`, the launcher-surface tests |
| `factsstore` | new optional reader capability | the CH store implements it; in-memory and fakes do not (type-asserted) |
| egui2 IDL | `TextEdit.CaptureKeys`, one global key fetcher | generated bindings, `interpreter.rs`'s generated region, the keys demo |
| env registry (ADR-0009) | frecency half-life | `doc/env-vars.md` |

## Alternatives

- **Enrich the pane only (O1).** Cheapest, and it delivers the summary and the
  detail pane. Rejected on C1: the surface it improves is the one that vanishes
  at the first open, so the corpus stays unsearchable in use. It is, however,
  the natural first commit of SD2 — the component mounts in the pane before the
  app exists.
- **An overlay palette (O3).** The Spotlight shape and the most familiar one.
  Rejected on C4: it would be a host surface with no manifest, no lifecycle, no
  audit row and no window record, duplicating machinery the app model already
  has. SD1 buys the same interaction inside the model.
- **Ranking only (O4).** Rejected: it answers "too many apps" and not "what
  does this contain", and the second is the harder half — no ordering tells a
  reader what `Socket owners` does.
- **A derived summary, no manifest field.** Rejected in SD4 on three grounds
  (wrong job, 18 apps uncovered, a second convention to maintain). Its merit
  was real — 49 of 72 summaries are nearly written already — and it remains the
  fallback if authoring 72 strings stalls.
- **Per-row focus for keyboard navigation.** Rejected by ADR-0177 §SD9's
  reasoning rather than freshly: under virtualisation only visible rows exist,
  so egui's focus order becomes a function of scroll position. One capture
  frame with an internal cursor is the shape that survives SD5.
- **Unbounded frecency blending in typed queries.** Rejected in SD8 — it makes
  result order unexplainable and lets history outrank relevance.

## Consequences

### Positive

- The corpus becomes searchable while apps are open, which is C1 and the
  complaint ADR-0158 recorded and could not fix within its scope.
- Every entry gains a sentence, and the eighteen already-serialised manifest
  columns gain somewhere to be read.
- Declared capabilities become user-facing: "what it touches" is prose the
  authors already wrote for a different purpose.
- Ranking closes ADR-0158 §SD10's first deferral on a substrate that turned out
  to be already present.
- Per-frame cost stops tracking corpus size (SD5), so the growth rate that
  prompted this stops being an architectural problem.
- Two small IDL generalisations land where the IDL's own comments said they
  would, and both are reusable beyond the launcher.

### Negative

- A compile break on `app.Manifest` plus 72 authored strings and 49 frontmatter
  edits. The break is announced, mechanical, and caught by the compiler for Go
  sites — but the applet half is caught only at registration, as a Warn.
- The launcher becomes a window that can be closed, which the pane could not
  be. A user who closes it and does not know the key has a worse position than
  today's always-present pane; the `Apps ▾` fallback and the empty-state mount
  exist for that, and the key is discoverable from the menu.
- Frecency makes the launcher's order depend on invisible history, and the
  landing view stops being the same for everyone. Bounding the typed-query
  contribution (SD8) limits how surprising this can get; it does not eliminate
  it.
- A summary is metadata that can go stale, and nothing detects a summary that
  has stopped describing its app.
- The IDL additions mean this decision cannot land Go-only; it needs a
  regeneration and a Rust-side pass.

### Neutral

- The launcher appearing in its own list is harmless and left in — it is a
  registered app, and hiding it would be a special case.
- `Summary` is a fourth descriptive field beside `Display`, `Title` and `Icon`.
  That is the freedesktop shape, not drift.
- Nothing here changes the topic vocabulary or its axis; SD10's display labels
  are a rendering function over it.

## Migration

1. Add `Summary` to `Manifest` and to both inventory serialisations; leave
   `Validate` permissive for one commit so the tree keeps building.
2. Author the 72 summaries — 23 Go sites, 49 frontmatter keys — then flip
   `Validate` and the minter to required. Splitting it this way keeps every
   commit buildable, which trunk-based development requires.
3. Land the launcher component mounted in the empty-state pane only (the O1
   increment), with the row, the detail pane, and battery search. Reviewable
   against the current pane on like-for-like behaviour.
4. Land the IDL additions and regenerate; wire the keyboard contract.
5. Register the launcher app, wire the key in the host chrome, reduce
   `Apps ▾`, and switch the default action to open-or-raise.
6. Add the history capability and the frecency ordering last — it is the only
   layer that needs a live ClickHouse to show anything, so it is the one whose
   absence must be invisible.

Breaking-change note for downstream consumers: `app.Manifest` gains a required
field for windowed apps, and both inventory schemas gain a column. The
`listSchema` column goes on the end rather than beside `display`/`title`, so a
`--launch` predicate addressing columns positionally keeps working; only the
column count changes.

## Verification plan

- The ADR-0158 tree-wide classification test extends to `Summary`: every
  windowed registration has one, and it satisfies the style budget (length,
  does not begin with the app's own `Display`). This is the gate that catches
  the applet half of the migration, which the compiler cannot see.
- Launcher unit tests over the ordering rules: the battery-dominates property
  in SD8 stated as an assertion (a maximally-frecent non-display match never
  outranks a display-name match), and the landing view's order under a
  synthetic history.
- A facts-store test for the aggregate read, plus the absent-capability path
  asserting an empty history rather than an error.
- Cursor-movement unit tests for SD9's Go half — heading skipping, end
  clamping, the empty list — plus a mask-shape test pinning why Escape is in it
  and Tab and Space are not. The Rust half (the pre-widget consume) is covered
  by the crate's `cargo check` / `clippy` gate and by use; a headless key trace
  through the whole path, following the existing focus/keys demo traces, is
  owed and not written.
- A screenshot of the launcher in both mount points, via the ADR-0057 driver.
- Not verified: whether the authored summaries are *good*, and whether the
  half-life is well-tuned. Both are review and use, not test. Nor does
  anything detect a summary that has drifted from its app — the same
  acknowledged gap ADR-0158 has for topics.

## Status

Proposed (2026-08-31). Selected shape agreed in design dialogue; implemented
along §Migration's order in the same session, which is why several decisions
above carry a note about what the implementation changed. Awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — topics,
  keywords, kind; §SD10's deferrals are what this ADR takes up.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the app contract,
  declared caps, and the audit trail SD7 reads.
- [ADR-0135](./0135-app-launch-requests.md),
  [ADR-0148](./0148-app-workingsets.md) — the launch and workingset verbs SD6
  surfaces as actions.
- [ADR-0164](./0164-documentation-regex-search.md) — the pattern battery SD8
  ranks on top of.
- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — the capture
  mechanism SD9 extends, and §SD9's argument against per-row focus.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — the screenshot driver
  behind SD11's deferred previews.
- [ADR-0094](./0094-keelson-introspection-tables.md) — the `keelson('apps')`
  provider whose columns SD6 renders.
- [app-launcher-information-design.md](../adr-background-work/app-launcher-information-design.md)
  — the measurements and the launcher survey behind §Context and §Prior art.
