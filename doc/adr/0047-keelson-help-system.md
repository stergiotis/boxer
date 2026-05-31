---
type: adr
status: proposed
date: 2026-05-24
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0047: Keelson inline help system

## Context

[ADR-0026](0026-app-runtime-and-capability-subjects.md) gave the
runtime a first-class `AppI` abstraction and an `app.Manifest`
identity record. Every windowed app the carousel hosts (logviewer,
capinspector, helphost itself, the demo apps) reaches the user as a
running binary — but the documentation describing each app reaches
them through entirely different surfaces:

- Go doc comments (`pkgsite`) — developer surface, requires the
  reader to know the package path.
- Markdown under `doc/` — written for contributors; out of reach of an
  end-user who just opened the binary.
- ADRs under `doc/adr/` — design history, not operator-facing.
- The repo's CLAUDE.md — agent-facing, intentionally not shipped.

Operators running the binary (the very audience the dock host is
meant to serve) had **no in-app way to read documentation** for the
windows they were looking at. The carousel's status-bar
capability-inspector chip was the only inline-information affordance,
and it explained capability subjects rather than apps.

Three additional pressures shaped the timing:

- The markdown widget (`thestack/imzero2/egui2/widgets/markdown`)
  had reached feature parity for an Obsidian-flavoured subset
  (frontmatter, callouts, code-block highlighting per language,
  wikilinks, inline images via `resolver.LoadImage`) — a renderable
  surface already existed.
- ADR-0055 / boxer's `DOCUMENTATION_STANDARD.md` mandates Diátaxis
  frontmatter (`type:`, `audience:`, `status:`) on every Markdown
  file under `doc/`. That metadata is exactly what a help-reader
  needs to populate navigation without per-app boilerplate.
- The `app.DefaultRegistry` already gathered every windowed app's
  manifest at init() time. A help system that hooked into that
  registry would auto-populate for free as new apps were written.

We chose to land an inline help system as a parallel pair of
keelson runtime services: a render-agnostic library that owns
parsing and indexing, and a windowed reader app that consumes it.

## Decision

We will ship the help system as two cooperating packages with a
narrow contract between them, an `fs.FS`-shaped manifest field on
every app, and a single runtime-owned keyboard binding.

### SD1 — Library / host split

`runtime/help` is a render-agnostic library: `BookI` (one app's
parsed corpus), `LibraryI` (registry indexed by `app.AppIdT`),
`DocInfo` / `SectionInfo` (heading-table metadata), `RefT` (typed
"open at this section" handle), and `FSImageResolver` (inline image
decoder over an `fs.FS`). It depends on `markdown` and `app`; it
does not depend on any UI rendering scope.

`runtime/helphost` is a windowed `AppI` that consumes
`help.DefaultLibrary`. It owns selection state, the
Rendered/Source view toggle, the once-per-click scroll-to-section
guard, and the two-pane layout. Three packages can consume `BookI`
without dragging in the reader app: a future CLI exporter, a
tooltip popup, a printable PDF builder.

### SD2 — `Manifest.Help fs.FS` field

Apps declare help by setting `Manifest.Help` to an `fs.FS`,
typically an `embed.FS` filtered through `help.MustSub(fsys, "help")`
to root paths at the `help/` directory. `fs.FS` rather than the
concrete `embed.FS` lets dev hosts swap in `os.DirFS` for
live-reload editing without changing the manifest shape.

The field carries the FS only — parsing is the library's job.
Adding this field does not pull `goldmark` or the markdown widget
into the `app` package's transitive imports; consumers of `Manifest`
that don't read `.Help` see no dependency change.

### SD3 — Auto-sync from `app.DefaultRegistry`

`LibraryI` lazy-syncs from `app.DefaultRegistry` on first read.
Every registered `Manifest` whose `Help` is non-nil becomes a
`BookI` automatically; apps don't need a second registration step
beyond the existing `app.DefaultRegistry.RegisterFactory` call.

The cycle direction is `help → app`, never the reverse, so
`help` can import `app` to reach `AllManifests()` without breaking
the `app` package's narrow surface. Explicit `Register` /
`SyncFromRegistry` hooks exist for tests and runtime-injected docs
that don't belong to any single app.

### SD4 — Markdown widget reuse, not a custom renderer

`Doc.Render` from the existing markdown widget renders the body;
the help library only adds parsing-time metadata extraction
(`Doc.Headings()`, `Doc.Frontmatter()`) and exposes the raw source
bytes for the Source view toggle. No new rendering primitives,
no help-specific markdown dialect. The Obsidian subset already
supported by the widget (callouts, wikilinks, embedded images via
the resolver) became the help dialect.

### SD5 — Frontmatter-driven navigation

Each `BookI` reads `title:`, `type:`, and `status:` from the
Diátaxis frontmatter the boxer doc standard already mandates.
`type` becomes the `[explanation]` / `[how-to]` / `[reference]`
suffix in the nav; `title` overrides the H1 fallback; `status` is
read but not surfaced today (will gate "show drafts in release
builds" later). This keeps the help corpus aligned with the
contributor docs — same frontmatter shape, same `doclint.sh`
enforcement, same `scripts/new-doc.sh` seeding.

### SD6 — Single runtime-owned F1 binding

F1 globally opens HelpHost via a new FFFI2 fetcher
(`fetchF1KeyPressed` → `ctx.input_mut().consume_key`). The
fetcher's `consume_key` strips the event from egui's input queue
so widgets inside the focused app cannot intercept it; the
runtime owns this binding exclusively. Future runtime-level
shortcuts (debugger, command palette) will each define their own
fetcher to keep consumed-event ownership explicit per binding —
no generic "any key" fetcher.

### SD7 — Section anchors via slug match against the side-table

Each `Doc.Headings()` entry carries a slug derived by the same
algorithm boxer's `resolver.NoopResolver` uses for wikilink
fragments (lowercase, ' ' → '-'). The nav lists H2+ headings
under the active doc; clicking sets `inst.selectedSection` and
triggers a one-shot `WithScrollToSection` hint on the next
`Doc.Render`. The hint is consumed exactly once per
`(app, doc, section)` change to keep the user's manual scroll
position stable.

## Alternatives

- **External doc site.** Linking from each app to a `keelson.dev`
  pages site (or `pkgsite`) shifts the writing workflow to a
  parallel repo and breaks for air-gapped operators. Rejected
  because the user the help is for is the one running the
  binary — they shouldn't need a browser tab open to read it.
- **Embed HTML, render with a webview.** Faster authoring (real
  HTML/CSS), but pulls a webview dependency the runtime
  otherwise avoids and breaks the egui scope contract. Rejected
  because the markdown widget already exists and the Obsidian
  subset covers everything a help doc needs.
- **Centralised "docs corpus" loaded at runtime startup.** One
  big `fs.FS` shared across apps, indexed once. Easier search,
  but a single regression in the corpus breaks every app's help,
  and the lifecycle / versioning ties to nothing recognisable.
  Per-app `Manifest.Help` keeps the docs colocated with the code
  they describe — refactoring a package moves its help with it.
- **Eager indexing at registration.** Parse every doc when
  `Register` is called instead of lazy on first library read.
  Predictable startup latency but blocks apps that never get
  opened. Lazy wins because most operators read help for one
  app per session.
- **Per-app help-host.** Every app implements its own help
  rendering. Maximum flexibility, minimum reuse. Rejected
  because nav, section anchors, search, and "Source view" are
  obviously shared concerns — a per-app implementation would
  diverge in superficial ways.
- **Bake the scroll target into `markdown.Doc` state.** Add
  `Doc.SetScrollTarget(slug)` consumed during the next `Render`.
  Cleaner caller code but mutates `Doc` after construction,
  violating its documented immutability. The
  `WithScrollToSection` variadic option keeps `Doc` immutable
  and is explicit about the per-call nature.
- **F1 in HelpHost only.** The simplest binding — F1 inside the
  HelpHost window focuses search or jumps to TOC. Rejected
  because users can't reach HelpHost without it, defeating the
  "global help" affordance.

## Consequences

### Positive

- **Help authoring is one directory + one manifest line.** App
  authors write `help/overview.md` next to `app_register.go` and
  set `Manifest.Help: help.MustSub(helpFS, "help")`. No new
  pipeline, no separate repo, no CI surprises — `doclint.sh`
  already enforces frontmatter on the same paths.
- **Render-agnostic library unlocks future consumers.** The
  CLI exporter, tooltip popups, printable PDFs, and the
  eventual bus subscriber all consume the same `BookI` without
  duplicating the parse/index work.
- **Self-documenting framework.** HelpHost itself ships
  `help/howto/add-help.md` so an app author searching for the
  pattern doesn't need to leave the running binary. The
  dogfood corpora across helphost / logviewer / capinspector
  validated the design end-to-end before this ADR.
- **No new runtime dependencies.** The work reused the existing
  markdown widget, the existing app registry, the existing
  egui ScrollArea, the existing carousel render loop. The only
  new FFFI2 ops (`fetchF1KeyPressed`, `scrollToCursor`) are
  small, single-purpose, and useful beyond help.
- **Markdown.Headings() / SlugHeading became reusable exports.**
  Originally added for help, they're now consumable by any
  future TOC sidebar or anchor-aware UI.

### Negative — open points and limitations

- **No bus subject yet.** The bus-driven `runtime.help.open`
  carrying `RefT` (CBOR via buscodec) is the natural way for
  apps to publish "open help here" intents — e.g. a window-chrome
  "Help" button, an error message that links to a recipe. Until
  it lands, programmatic opens go through direct
  `app.DefaultRegistry.Open(helphost.ManifestId)` calls.
- **F1 opens HelpHost at last selection, not the focused app.**
  A more useful F1 would open help focused on whichever app
  window the user was in when they pressed it. Requires either
  a focus-tracking API on `windowhost` or the bus subject
  above plus a "currently-focused" cap published by the host.
- **Cross-app wikilinks are not resolved.**
  `[[capinspector/overview#picker]]` inside a doc renders as a
  fragment-style link via `NoopResolver`; clicking is a no-op.
  Resolving it to a `RefT` event needs the bus subject.
- **Image resolver only handles vault-rooted refs.**
  `![](logo.png)` works; `![](../assets/logo.png)` (relative)
  does not. Per-doc base-path resolution is deferred.
- **No full-text search.** The index is title + heading + path.
  Adding `bleve` (or even `grep`-shaped) is a future build-tag
  feature; it would need a freshness story for live-reload mode.
- **Section anchor scroll is one-shot, not bidirectional.**
  The nav highlights the selected section but doesn't track the
  user's manual scroll position — scrolling away leaves the
  active section row unchanged. Two-way binding would need a
  per-heading `captureUiRect` walk.
- **Two open HelpHost tiles share `DefaultLibrary` but have
  independent selection state.** That's correct, but their
  scroll-to-section interactions don't coordinate (each guards
  its own last-scrolled state).
- **Status banner shows in every draft doc.** The dogfood
  corpora all carry the mandated "draft — pre-human-review"
  blockquote because they haven't been reviewed yet. Once
  flipped to `status: stable`, the banner disappears — the
  current visual noise is intentional and self-correcting.

### Neutral

- **HelpHost is itself an `AppI`** registered alongside other
  windowed apps. Multi-instance behaviour (two open Help tiles)
  works via the factory ctor pattern without special-casing.
- **The library's auto-sync vs explicit-register dual path** —
  most callers will never touch `Register`, but it stays
  exported for tests and for runtime-injected docs that aren't
  owned by any single app.
- **Heading extraction lives in the markdown widget**, not in
  help. An earlier pivot tried a parallel ATX walker in the
  help package; consolidating back to `Doc.Headings()` was the
  right call once the markdown package stabilised.

## Status

Proposed — awaiting review by code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-05-30 — Front-matter validation and the shared `docstd` vocabulary

SD5 read `title:`, `type:`, and `status:` but did not validate them: a
malformed or absent value fell through to the H1/filename fallback
silently, so a misfiled doc (`type: tutoral`, a missing `status:`) reached
operators with no signal. The help library now checks the `type`/`status`
contract.

The type/status enums and the conformance logic were extracted to a new
pure leaf package,
[`github.com/stergiotis/boxer/public/gov/docstd`](../../public/gov/docstd),
now the single source of truth. The repo-wide linter
([`doclint`](../../public/gov/doclint)) was refactored to consume it:
DL001 delegates the type/status conformance check (so the runtime help
check and the CI check can no longer drift), and the sibling frontmatter
rules — DL003 (review metadata), DL004 (draft banner), DL010 (ADR
sections), DL011 (open-drafts report) — now reference `docstd`'s
status/type constants instead of their own string literals. This is the
concrete form of SD5's "same frontmatter shape, same enforcement" intent.

The two consumers differ on exactly one axis, captured by
`docstd.ValidateFrontmatter`'s `allowADR` parameter: repo-wide linting
accepts `type: adr`, while inline help does not — an ADR is design history,
not operator-facing help (consistent with the Context section above, which
lists ADRs as a non-operator surface).

Surface in `help`: `BookI.Validate()` and the standalone `ValidateDocInfo`
return the problems; the same problems are emitted as Warn logs during the
one-shot index walk, so a misconfigured corpus surfaces in runtime logs
without a caller opting in. Validation is advisory — parsing still succeeds
and the title fallback is unchanged, so a missing `title:` remains a
non-issue by design; only `type` and `status` are validated. The play
corpus test now asserts conformance through `Validate()` against a real
shipped corpus.

## References

- [ADR-0026](0026-app-runtime-and-capability-subjects.md) — `app.AppI` + `Manifest` foundation that `Manifest.Help` extends.
- [ADR-0035](0035-keelson-namespace-introduction.md) — `runtime/` namespace where both `help` and `helphost` live.
- [ADR-0044](0044-imzero2-design-system-iconography.md) — `icons.PhBookOpen` used by HelpHost's manifest.
- `public/keelson/runtime/help/` — library implementation + tests.
- `public/keelson/runtime/helphost/` — reader app + tests.
- Commit chain shipping the system:
  - `92d05b66 feat(help): inline help library`
  - `dfe65d80 feat(helphost): windowed reader app`
  - `8cf8dde7 feat(helphost): dogfood corpora + carousel wire`
  - `fd3bc7e6 feat(helphost): Rendered/Source view toggle`
  - `9b8e4de3 fix(helphost): drop reader-pane title to avoid H1 duplication`
  - `903ba484 feat(helphost): section anchors in nav`
  - `d0db26cc feat(help): FSImageResolver for inline images`
  - `f752285a feat(helphost): F1 fetcher`
  - `2a6b05c9 feat(helphost): scroll-to-section + Headings export`
