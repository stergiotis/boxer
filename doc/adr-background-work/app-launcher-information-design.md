---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

> **Provenance.** Status-quo measurement plus a survey of launcher
> information design, compiled 2026-08-31, ahead of any decision. Three
> tiers, marked throughout: (a) claims about this repository were measured
> against the working tree on the compile date by a throwaway probe that
> minted the applet corpus and walked the real registry — counts in §2 come
> from that probe, not from grepping; (b) claims about what the launcher
> renders were read off the two render paths in `windowhost`, not observed on
> screen; (c) the external prior art in §4–§5 is summarised from familiarity
> with the named products and specifications as of the compile date and was
> **not** re-verified against fresh captures for this page — treat the
> shapes as reliable and any specific limit as needing a check before it is
> quoted. Nothing here is a decision.

# Deciding what an app contains — launcher information design

> **Decision taken.** The survey below informed
> [ADR-0214](../adr/0214-launcher-as-app-summary-detail-recall.md), which is
> authoritative for what was decided and why. This page is a snapshot of the
> reasoning, not a description of current behaviour, and it is not maintained
> against the tree — §2's measurements in particular date from the compile
> date and the launcher has since changed shape.

## 1 Question and scope

[ADR-0158](../adr/0158-app-classification-topics-keywords-kind.md) settled how
apps are *classified*: a registered multi-valued `Topics` vocabulary for
placement, ungoverned `Keywords` for retrieval, `Kind` for provenance. It
explicitly did not settle how much an entry *tells you about itself*, and it
recorded ranking as deferred.

The prompting complaint is two symptoms at once:

1. **Too many apps.** The list is long enough that browsing it is work.
2. **Not enough information to decide what an app contains.** Even having
   found a plausible entry, the launcher does not say what opening it would
   get you.

These are not the same problem and do not have the same fix. (1) is about
*reducing what is on screen* — ranking, curation, scoping. (2) is about
*increasing what each surviving row says* — description, preview, metadata.
Adding information per row makes (1) worse unless (1) is addressed too, which
is why they are surveyed together.

Scope is the launcher's information design: what a user sees before committing
to open something, and how they get from 72 candidates to one. Out of scope:
the classification vocabulary (decided), window placement and lifecycle, and
what happens after an app opens.
[app-organization-and-launching.md](./app-organization-and-launching.md) is the
predecessor page — its §2 status quo is a month stale, its §5 prior art on
classification and frecency stands, and this page does not restate either.

## 2 What the launcher shows today, measured

### 2.1 The inventory has doubled

72 registrations, against the 34 measured on 2026-08-01:

| Kind | Count | Share |
| --- | --- | --- |
| applet | 49 | 68% |
| app | 16 | 22% |
| demo | 7 | 10% |

The growth is where ADR-0158 predicted: applets are committed markdown
documents, so the corpus grows at authoring speed rather than at Go-package
speed. Whatever the launcher does has to hold at a corpus that has recently
been doubling per month, not at 72.

Topic distribution, and the browse pane's row count:

| Topic | Apps |
| --- | --- |
| data | 19 |
| code | 17 |
| observability | 15 |
| runtime | 10 |
| topology | 6 |
| about | 3 |
| ui | 3 |
| sql | 2 |
| geo | 1 |

No section is unmanageably large on its own; the pane renders all nine, one
after another, in a single scroll — 76 rows for 72 apps.

### 2.2 The row is sixteen characters

A launcher row is `Manifest.WindowTitle()`: an icon glyph plus a title. In
the flattened search-hit view it gains an em-dashed topic suffix. That is the
entire per-entry information budget, and it measures 15.6 characters of
`Display` on average (longest: "Generated vs hand-written", 25).

What the icon buys is real and worth keeping: all 72 apps declare one, they
resolve to 61 distinct glyphs, and the worst collision is three apps sharing
one. No two apps share a `Display` label either. So the row is *unambiguous* —
it can tell two entries apart — while saying almost nothing about either.
Recognition works if you already know the app; discrimination does not work at
all.

Meanwhile `keelson('apps')` serialises eighteen columns per manifest, and the
launcher renders two of them.

### 2.3 Descriptive prose exists for 54 of 72 apps and reaches no surface

`Manifest` has no description field. It does have `Help fs.FS`, and 54 of the
72 registrations ship one — 289 markdown documents in total. Every one of
those documents has a lead paragraph, averaging 317 characters, whose first
sentence is typically 50–110 characters: "The Capability Inspector is
keelson's introspection app for the runtime's capability subjects", "`play` is
a graphical ClickHouse SQL playground".

That is a written, committed, reviewed one-line description of most of the
corpus, sitting one field away from the launcher, and no launcher surface can
reach it. This is the same shape as the discarded `BookID` that ADR-0158 §Context
diagnosed — authored grouping information thrown away because no slot held
it — recurring for description instead of placement.

Two caveats. The 18 apps with no help corpus have no such sentence at all. And
where a corpus holds several documents (54 apps, 289 documents) nothing marks
which one describes the app, so "the lead paragraph" is not yet a
well-defined thing to read.

### 2.4 The facet is barely multi-valued

68 of 72 apps declare exactly one topic; 4 declare two. ADR-0158's central
decision — an entry appears under every topic it carries — is therefore doing
almost nothing in practice, and the browse pane's 76 rows are 72 rows plus
four. This is not an argument against the decision (it cost nothing and the
multiplicity is available when authors want it), but it does mean the topic
axis currently partitions rather than cross-references, and a reader browsing
`data` sees 19 entries with no signal about which of them are about the same
thing.

Retrieval metadata is thinner still: 24 of 72 apps carry any `Keywords`.

### 2.5 Where the surface is visible, its register is technical

The search box's hint text reads `Search apps (regex, space = AND)`, and below
the results a note can appear saying `some tokens are not valid regexps and
match literally`. Both are honest and both are addressed to someone who knows
what a regex is. The topic chips are labelled with the vocabulary's internal
tokens — `runtime`, `topology`, `ui` — rather than reader-facing words.

This matters for the "not too nerdy" half of the request: ADR-0164's regex
battery is a good retrieval engine, and nothing in §4 argues against keeping
it. What §4 does argue is that a regex-first *presentation* is the wrong
default surface for someone who does not yet know what they are looking for.

### 2.6 What is absent

- **No description, no summary, no subtitle** anywhere in the manifest.
- **No preview.** The ADR-0057 tour driver can render any app to PNG and SVG,
  but nothing retains those images for the launcher to show.
- **No ranking, recents, or pins** — ADR-0158 §SD10's recorded deferral.
  Its stated blocker has partly dissolved: app-lifecycle facts rows
  (`started` / `stopped`, per app, per run, timestamped) are written to
  `boxer.facts` on every open and close under ADR-0026's audit trail. What is
  missing is a cross-run aggregate read — the existing reader deliberately
  requires a run anchor, because a global scan had no consumer. Frecency now
  needs a query, not a write path.
- **No open-state indication.** Both render paths call `Open`, not
  `OpenOrRaise`, so clicking an app that already has a window silently opens a
  second one, and the row never says a window exists.
- **No search once a window is open.** The only search field lives in the
  empty-state pane, which disappears at the first open; the `Apps ▾` menu has
  no search affordance. The predecessor page recorded this at 34 apps; at 72
  it means the corpus is unsearchable for the entire time the runtime is in
  use.

## 3 What the decision actually asks for

"Decide what the app contains" is three questions a user asks in sequence,
each answerable with less information than the next:

- **Recognise** — "is this the thing I already have in mind?" Answered by a
  name and an icon. The current row does this well.
- **Discriminate** — "which of these several plausible entries is the one?"
  Needs one line of what-it-does per candidate, and needs it for *all*
  candidates simultaneously, so it must fit in a row.
- **Commit** — "is this worth opening?" Needs more than fits in a row: what it
  operates on, what it will do to the screen, whether it needs a database, an
  example of its output.

The standard remedy is progressive disclosure: each stage gets its own
surface, and the expensive information is available on demand rather than
rendered 72 times. The corresponding usability principle is recognition over
recall — the launcher should let a user recognise the right entry rather than
requiring them to remember which internal name it was filed under.

The two complaints in §1 land on different stages. "Too many apps" is a
recognise/discriminate failure caused by rendering the whole corpus at once
with no ordering. "Not enough information" is a discriminate/commit failure
caused by the row carrying only identity.

## 4 Prior art — the friendly launcher, surveyed

The convergent shape below is remarkably consistent across products with very
different audiences, which is the main argument for it. ADR-0158 §Prior art
already recorded the *filter-first* half (a single ranked input as the primary
path, categories demoted to scopes) and the frecency half; this section
surveys what those launchers put *in* and *around* a result row.

### 4.1 The row: name, one line, and a type badge

Every launcher that works at scale gives a result three slots: identity, a
single line of description, and a marker of what kind of thing it is.

- **freedesktop Desktop Entry** — the specification this repository already
  borrows `Categories` and `Keywords` from — carries `Name`, `GenericName`,
  and `Comment`. `Comment` is a one-line description, and GNOME and KDE render
  it as the subtitle beneath the name in search results and app grids. keelson
  adopted two of that spec's descriptive fields and left the third; §2.3 is
  the consequence.
- **AppStream metainfo**, the layer above it, splits description in two by
  length: a `<summary>` of one line and a `<description>` of paragraphs, plus
  `<screenshots>`. GNOME Software renders summary in the list and description
  on the detail page — the same split as §3's discriminate/commit stages.
- **Raycast** rows are icon, title, subtitle, and right-aligned accessories;
  the subtitle usually says where the command comes from, and accessories
  carry small state.
- **Alfred** rows are title plus subtitle, with the subtitle carrying the path
  or context that disambiguates same-named results.
- **VS Code**'s quick-open rows have a label, a dimmed description on the same
  line, and an optional second detail line; the command palette puts the
  keybinding in the right-hand slot.
- **App Store product pages** pair the name with a subtitle field, both
  constrained to a very short budget (on the order of 30 characters each —
  check before quoting), specifically to force one claim per app.

The consistent lesson is not "add a description field" but **one line, always
present, never the name again**. A subtitle that repeats or elaborates the
title costs a row of screen and buys nothing.

### 4.2 The detail pane: list plus detail is the standard answer to this exact complaint

When a row cannot hold enough to commit, every one of these products adds a
second region rather than a taller row.

- **macOS Spotlight** groups hits by kind on the left and shows a preview of
  the selected hit on the right, including metadata for the entry.
- **Raycast** has this as an API primitive: a list item can carry a detail
  view holding a markdown body plus a metadata panel of key–value rows, shown
  beside the list for whichever item is selected.
- **Windows 11's** Start search puts "best match" beside a pane naming the app
  and offering its actions.
- **JetBrains Search Everywhere** and **GNOME Software** are the same pattern
  at different scales — a scannable list, and one selected entry expanded.

Two properties make this the right fit here. It costs *nothing per row* — the
expensive content renders once, for the selection — and it has somewhere to put
the eighteen `keelson('apps')` columns that are currently either invisible or
would be noise. Declared `Caps` in particular answer "what does this app touch"
(ClickHouse? the filesystem? the network?), which is close to what "what does
it contain" is really asking, and belongs in a detail pane and nowhere else.

### 4.3 Pictures discriminate faster than prose

For "what does this app contain", every consumer-facing catalogue leads with an
image: GNOME Software, the App Store, the VS Code extension marketplace, the
Obsidian plugin browser, Steam's library capsules. A screenshot answers "is
this a table, a chart, a map, or a form" in one glance, and the corpus here is
visually diverse enough for that to carry real information — a terrain
line-of-sight view, a SQL playground, a process monitor and a markdown editor
are unmistakable from a thumbnail and near-indistinguishable from their names.

This repository is unusually well placed for it: ADR-0057's driver already
renders any registered app to PNG and SVG on demand, with per-app stage sizes
already declared in `SurfaceHints`. What is missing is not capture but a
decision about where captured images live and how they stay current — the same
question [play-screenshot-capture-options.md](./play-screenshot-capture-options.md)
asks for a different consumer.

### 4.4 The landing view is curated, not exhaustive

No modern launcher's default view is "every entry, alphabetically, by
category". What they show instead, when nothing has been typed:

- **Spotlight** and **Alfred**: recent and learned-frequent entries only.
- **Raycast**: a short suggestion list, with the full command set one keystroke
  or one "all commands" step away.
- **iOS App Library**: auto-categorised groups plus an explicit "Recently
  Added"; the alphabetical list of everything is a separate, deliberate view.
- **macOS Launchpad** and the **GNOME overview**: paginated grids that are
  explicitly the browse-everything surface, reached separately from search.

The exhaustive list is not removed — it is demoted to a second view for the
case where you genuinely want to survey the corpus. The default view answers
"what was I just doing" and "what would I most likely want", which needs
ranking (§4.7) and nothing else.

### 4.5 Entries carry actions, not just "open"

Raycast's action panel, Alfred's universal actions, Windows' "Run as
administrator / Pin to Start", macOS's dock context menus: the row is a noun
with several verbs, one of which is default. Here the verbs already exist as
runtime concepts — open, raise an existing window, open with a launch config
(ADR-0135), restore the stored workingset (ADR-0148), show the app's help
book, pin — and the launcher exposes exactly one of them. That the workingset
restore is currently indistinguishable from a plain open is a small instance
of the same gap: two different things happen and the launcher offers one
button.

Only 3 of 72 apps declare a `LaunchKind` and 2 participate in workingsets, so
this layer is thin today. It is worth knowing the shape before the row's
click behaviour is designed around a single verb.

### 4.6 Badges say what is true right now

Running-state dots in the macOS dock, GNOME's running badge, "update
available" in software centres, "last played" in Steam. Cheap, right-aligned,
and they answer questions prose cannot because they change. The candidates
here: a window is already open (§2.6 — currently a silent double-open), the
app is a demo rather than a working tool (`Kind`, currently only a filter), the
app needs ClickHouse and ClickHouse is down (derivable from `Caps` plus the
facts-store fallback state), and last-opened time once §4.7 exists.

### 4.7 Ranking is the lever for "too many apps", and its blocker has moved

ADR-0158 §SD10 deferred frecency for two stated reasons: it needed a launch
record in `boxer.facts`, and the ordering problem was not yet demonstrated at
that corpus size. Both premises have changed. The corpus has doubled, and the
launch record exists — ADR-0026's audit trail writes a timestamped
app-lifecycle row per open and per close. The remaining work is a cross-run
aggregate read, which the current run-anchored reader deliberately declines to
offer.

The mechanics are the well-trodden ones the predecessor page recorded
(exponential decay, a tuned half-life, no authored metadata required). The
property that matters here is that ranking is the *only* layer that improves as
the corpus grows, and the corpus is growing at authoring speed.

### 4.8 What makes a launcher feel nerdy

Collected as an explicit anti-pattern list, since "user friendly, not too
nerdy" is half the request. Each is something at least one launcher does badly
and the good ones avoid:

- **Showing identifiers.** Import paths, slugs, aliases, numeric codes. Useful
  for `--launch` predicates and audit rows; noise in a row a person reads.
  (The launcher does not currently show `Id` — worth keeping that way even as
  rows get richer.)
- **Exposing the query language as the primary affordance.** A regex hint in
  the empty box tells a first-time reader that this surface is not for them.
  Keep the power, change the invitation: a plain placeholder, with the syntax
  discoverable rather than announced.
- **Internal vocabulary as user-facing labels.** Section headings and chips
  reading `runtime`, `ui`, `topology` are the registry's spelling, not a
  reader's.
- **Rendering every metadata field at once.** The failure mode on the other
  side of §2.2 — eighteen columns per row is not an improvement on two.
- **Exhaustive alphabetical browse as the default view** (§4.4).
- **Silently doing something different from what the row implies** — the
  double-open in §2.6, and an unannounced workingset restore.
- **Empty states that explain the mechanism instead of offering a next step.**

## 5 Writing the one line, if a description field lands

The ecosystems that require a one-liner have converged on similar guidance,
and it is worth writing down before 72 of them get authored:

- **Say what it does, for whom**, in one clause. Verb-first reads better than
  a noun phrase.
- **Do not repeat the name.** "Terrain scope — a scope for terrain" is the
  common failure.
- **No marketing register** — which also happens to be this repository's
  committed-prose rule (AGENTS § Writing style), so the two agree.
- **Budget it tight.** ~60 characters fits a row at usable font sizes without
  truncation; the App Store's ~30 is tighter than needed here but the
  discipline it enforces is the point.
- **Distinguish siblings.** The one-liner's real job in this corpus is telling
  19 `data` entries apart, so a description that would fit any of them is not
  doing the work.

§2.3's finding is that most of these sentences are already written, in the help
corpora, by the same author under the same style rules. Deriving rather than
re-authoring is available, and would make the field cheap — at the cost of
needing a convention for which document, and for whether a derived line may be
overridden.

## 6 Where each layer's information would come from

Sequenced so each is independently useful, and marked with the question each
forces. Not a plan — none of this is decided.

1. **A one-line description per entry.** Source: a new manifest field, a
   convention over the existing help corpora, or both (authored field, derived
   fallback). Forces: whether `app.Manifest` grows a field (exported API under
   `public/`, so Tier-1), and which document speaks for a multi-document help
   corpus. Answers §1's second complaint at the discriminate stage.
2. **A detail region beside the list.** Source: manifest fields already
   serialised by `keelson('apps')` — caps, launch kind, workingset,
   registration, version, keywords — plus the help book's lead section.
   Forces: a layout decision in the empty-state pane, and whether the `Apps ▾`
   menu can host anything of the sort or should be replaced by a surface that
   survives a window being open (§2.6).
3. **A curated default view and a persistent search surface.** Source: nothing
   new — a reordering of what exists, plus (for "recent") layer 4.
   Forces: what the default view shows before any launch history exists.
4. **Ranking from launch history.** Source: the app-lifecycle rows already in
   `boxer.facts`; needs a cross-run aggregate read the current API declines to
   offer. Forces: whether that read belongs in the facts store, an
   introspection provider, or a small per-app rollup.
5. **Previews.** Source: the ADR-0057 driver. Forces: where images live, how
   staleness is handled, and whether a launcher may render an SVG per row.

## 7 Open forks

1. **Authored one-liner, or derived from the help corpus?** Authored is
   explicit and duplicates prose that exists; derived is free and couples the
   launcher to a documentation convention. A third option — authored field
   with a derived fallback — costs both mechanisms.
2. **Does the launcher become a real surface of its own?** Everything in §4
   assumes a launcher that exists while apps are open. Today it is an
   empty-state pane and a menu. This is the largest structural question on the
   page, and the answer bounds layers 2–5.
3. **Is `Topics` still the right browse axis at 72 entries** when 68 of them
   carry exactly one topic and the largest section holds 19? Ranking may
   subsume browsing, or the sections may need a second level.
4. **How much of §4 is worth building before ranking exists?** A defensible
   answer is that ranking alone fixes complaint (1) and a one-liner alone fixes
   complaint (2), and that layers 2, 5 wait for evidence.
5. **Do previews earn their cost** in a launcher whose apps are mostly dense
   tables? Four or five entries are visually distinctive; the applet corpus
   may all look alike.
6. **What does the row's default click do** once open-state and workingsets are
   visible — plain open, raise-if-open, or restore-if-stored?

## 8 Sources

External claims in §4–§5 are summarised from the following, read as
specifications or product documentation rather than measured for this page.
Per the provenance note, specific numeric limits were not re-verified.

- freedesktop.org **Desktop Entry Specification** — `Name`, `GenericName`,
  `Comment`, `Icon`, `Keywords`, `Categories`.
- freedesktop.org **AppStream** metainfo specification — `<summary>` vs
  `<description>`, `<screenshots>`.
- **GNOME Human Interface Guidelines** — app naming and summary guidance;
  GNOME Software's list-and-detail presentation.
- **Raycast API documentation** — `List.Item` (title, subtitle, accessories,
  icon), `List.Item.Detail` and its metadata panel, the action panel.
- **Alfred documentation** — result rows, subtitles, learned ordering,
  universal actions.
- **Apple Human Interface Guidelines / App Store Connect** — app name and
  subtitle fields and their length budgets; Spotlight result grouping and
  preview.
- **Visual Studio Code API** — `QuickPickItem` label/description/detail;
  extension marketplace list and detail presentation.
- **Microsoft Windows 11** Start and search — best-match plus action pane.
- **Nielsen Norman Group** — progressive disclosure; "recognition rather than
  recall" among the usability heuristics.
- Frecency mechanics: as cited in
  [app-organization-and-launching.md](./app-organization-and-launching.md) §5.3
  (Firefox Places, `fre`, `fzf`), not re-surveyed here.

## References

- [ADR-0158](../adr/0158-app-classification-topics-keywords-kind.md) — topics,
  keywords, kind; §SD10 records the ranking deferral this page revisits.
- [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) — the app
  contract, declared caps, and the audit trail that already records launches.
- [ADR-0057](../adr/0057-demo-registry-and-drivers.md) — the screenshot driver
  behind §4.3.
- [ADR-0135](../adr/0135-app-launch-requests.md),
  [ADR-0148](../adr/0148-app-workingsets.md) — the launch and workingset verbs
  behind §4.5.
- [ADR-0164](../adr/0164-documentation-regex-search.md) — the pattern battery
  the launcher's search box compiles.
- [app-organization-and-launching.md](./app-organization-and-launching.md) —
  predecessor survey: classification, the launcher school, frecency, facets.
