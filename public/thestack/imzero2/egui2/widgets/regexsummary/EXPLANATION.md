---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# regexsummary — Explanation

`regexsummary` is the imzero2 widget that surfaces a single regular
expression at two levels of detail in the same cell of UI:

- **Level 1 (in-flow target)** — a compact monospace row: a Phosphor
  `magnifying-glass` glyph hinting at expandability, the pattern in
  monospace truncated to a configurable cap, a green / red dot
  reporting whether Go's `regexp` engine accepts the pattern, and the
  standard [`inspector.AnchorToggle`](../inspector/) glyph that opens
  the level-2 window. Cheap (the only work per frame is one
  `regexp.Compile` on the host pattern), stateless modulo the toggle
  bit, and width-friendly enough to drop into a table column or a
  key-value row.
- **Level 2 (pinned inspector window)** — a draggable `c.Window`
  hosting the full
  [`github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/demo/apps/regex_explorer`](../../demo/apps/regex_explorer/)
  body: the cheatsheet panel, the pattern / haystack / multi-pattern inputs,
  the Test / List / Replace tabs, and the bottom status bar. The
  same widget the standalone `regex_explorer` AppI presents — not a
  forked copy. A bezier tether (via
  [`inspector.AnchorTether`](../inspector/)) visually ties the
  toggle to the open window so the source row and the floating
  inspector read as one continuous system.

## Why two levels

Regex testing is *workflow-heavy*: the operator needs a pattern
input, a haystack input, a match preview, a captures list, a
replace preview, and (often) a multi-pattern panel — all at once.
That is `regex_explorer`'s entire reason for existing. Dropping any
subset of that workflow next to every regex-valued field in a
dashboard would balloon the row to hundreds of pixels tall and
drown out the rest of the surface.

The level-1 row is small enough that hundreds of regex-valued
fields fit on one screen; the level-2 window stays out of the way
until the operator clicks the anchor on the row they care about,
and then they get the *full* explorer, not a watered-down preview.
The two views are derived from the same host pattern, so the
transition is consistent by construction — what the level-1 row
displays is what the level-2 explorer is seeded with.

This mirrors the design pattern that `distsummary` established for
statistical distributions ([ADR-0046]). regexsummary is the second
inhabitant of that pattern; the third (a date / interval inspector,
a JSON inspector, …) will follow the same shape.

## Composition

This widget owns no regex logic and no rendering primitives. It is
glue:

- **`regex_explorer.EmbeddedApp`** (sibling package) — the entire
  level-2 body, exposed as a four-method surface (`NewEmbedded` /
  `SetBus` / `SetPattern` / `Render`) that mirrors
  `AppInstance.Frame`'s package-`app`-pointer swap, `c.IdScope`
  salt push, and SD1 tripwire kick without forking the renderer.
  Per-instance state (pattern, haystack, flags, query results,
  tripwire) persists across close/reopen cycles within one widget
  instance.
- **`regexp.Compile`** (stdlib) — drives the green / red compile-
  status dot on the level-1 row. RE2 in Go and libre2 in ClickHouse
  almost always agree (the SD1 tripwire inside `regex_explorer`
  surfaces the rare drifts in the inspector's status bar), so the
  level-1 dot is a Go-only signal — calling out to clickhouse-local
  on every frame to verify compile status would be wasteful.
- **`inspector.AnchorToggle`** — the canonical "open this inspector
  in a pinned window" affordance. Handles its own click capture
  (badge-pattern via `Frame.SenseClick`), tooltip, and accent / fill
  swap. State ownership is on the caller — regexsummary keeps the
  `*bool` in its per-instance state map.
- **`inspector.AnchorTether`** — the shared bezier-connector
  infrastructure. Constructed once per call scope, captured at the
  level-1 row and the top of the inspector body, painted after the
  body has emitted. Endpoints, accent colour, and overlay routing
  match the proof-of-concept so every value inspector joins one
  visual vocabulary.
- **`keelson/runtime/icons.PhMagnifyingGlass`** and
  **`icons.PhDot`** — Phosphor glyphs for the level-1 expandability
  affordance and the compile-status dot (ADR-0044 iconography).
- **`styletokens.SuccessDefault` / `ErrorDefault`** — green / red
  colour tokens for the compile-status dot; consumed via
  `color.Hex(token.AsHex())` per the designlint-blessed bridge.

## Interaction model

The pinned-window toggle is *the* interaction. Other shapes were
considered and deferred:

- **Hover-popup** (distsummary's M1 shape) — would let the user
  glance at the embedded explorer without committing a screen-area
  budget. Wrong fit here: the explorer expects keyboard focus for
  its `TextEdit` inputs, and hover popups lose focus the moment the
  pointer drifts off. The pinned window can keep focus across
  pointer movement, which is what regex authoring needs.
- **Inline expand** (swap level-1 for the embedded body at the same
  site) — breaks the row's height invariant; the surrounding
  vertical flow would ripple-resize when the inspector opens. Wrong
  for table cells.
- **Modal dialog** — would force a context switch and freeze the
  rest of the surface. Wrong because the operator often wants to
  refer back to the surrounding rows while testing the regex.

## Pattern propagation

Propagation is one-way: each `false → true` toggle transition seeds
the embedded explorer's pattern from the host-supplied value. The
operator can then edit the pattern inside the inspector freely;
those edits stay local to the embedded `*App` and do not flow back
to the host's pattern source. Closing and reopening the inspector
re-seeds from whatever the host pattern now is.

This honours the contract the calling site implicitly carries:
inspectors do not mutate their source value. Bidirectional
propagation will land when the broader "bidirectional inspector"
infrastructure does (tracked alongside the rest of the inspector
work — same place ADR-0046 references). When that lands, the
embedded explorer's pattern field can publish edits back through a
caller-supplied sink without altering this widget's API.

The host's *other* inputs (haystack, replacement, flags, multi-
pattern) are never seeded from the host — they belong to the
inspector session entirely. Closing and reopening preserves them.

## Id stack contract

`Render` takes a `c.WidgetIdCreatorI` and consumes **exactly one**
prepared id via `idGen.Derive()`. The derived `uint64` is combined
with the developer-supplied `idPrefix` into a per-call scope string
(`idPrefix#<hex>`), and every stable id this widget needs is
derived from that scope via `c.MakeAbsoluteIdStr`:

- the anchor toggle's widget id
- the inspector window's widget id (also the OpenBound databinding key)
- the `AnchorTether`'s two R21 rect-capture seqs
- the lazy `EmbeddedApp`'s salt (`uint64(c.MakeAbsoluteIdStr(scope+"-embedded"))`)

`AbsoluteWidgetId`s (rather than stack-prepared ones) are required
for the toggle and window because their response-by-id lookups need
to match across frames; a stack-prepared id XORs the surrounding
stack top in and would silently miss the previous-frame slot.

The per-call scope is what lets the same `Renderer` be `.Render`-ed
multiple times in one frame (e.g. once per row of a list of regex
rules) without colliding on toggle / window / embedded-explorer
state. The `idGen` value is the disambiguator.

What the widget deliberately does **not** do:

- It does not call `ids.PrepareStr` itself on the caller's stack.
  Doing so on top of the caller's already-`Prepared` state would
  re-enter the state machine and panic. The single `Derive` at the
  top of `Render` is the only consumption.
- It does not invoke any FFFI2 *fetcher* outside the inspector
  window's body. The level-1 row is pure opcode emission — no
  fetchers, no deferred-block-capture re-entry risk. Inside the
  window body the embedded explorer owns its own id stack via
  `c.IdScope`, so fetcher calls there sit under their own scope.

## Per-instance state

State lives in a package-level `sync.Map` keyed by per-call scope.
Each entry carries:

- `pinned bool` — the toggle / window open flag, also bound to
  egui's title-bar X via `OpenBound` + R10 databinding so closing
  through the window's native chrome flips the same flag the toggle
  click would.
- `embedded *regex_explorer.EmbeddedApp` — lazily allocated on the
  first `false → true` transition. Renderers that are never pinned
  pay no allocation cost. Once allocated, the embedded state
  (pattern, haystack, flags, query results, tripwire) persists
  across close/reopen cycles within the same widget instance.
- `lastSeededPat string` — the host pattern at the most recent
  seeding event. Compared against the current host pattern to
  decide whether a new seeding is needed on the current frame's
  open transition.
- `seededAtLeast1 bool` — guards the first-ever seeding so a
  widget instance opened then closed before the host pattern was
  ever set still gets re-seeded the next time it opens.

Entries are never reclaimed. Acceptable for typical app shapes
(dozens to hundreds of unique regexsummary instances over a
session); apps that dynamically mount / unmount short-lived
regexsummary instances with one-shot `idPrefix`es leak `O(mounts)`
memory. Document but don't engineer for that yet — mirrors the same
posture as `distsummary` and `fieldview`.

## Invariants

- **One `Derive` per Render.** The caller's `Prepared` state must
  be drained exactly once per `Render` call. The `Derive` at the
  top of `Render` is the only consumption; all downstream ids
  derive from `MakeAbsoluteIdStr` over the per-call scope, which
  does not touch the caller's stack.
- **Stable scope across frames.** For a given call site, the scope
  string is deterministic — `idGen.Derive()` is stable for the
  same prepared id under the same surrounding `c.IdScope`. This is
  what makes the toggle / window / embedded-app state survive
  across frames.
- **Pattern propagation is read-only host → inspector.** Edits
  inside the inspector never write back to the host's pattern.
  Hosts can rely on this when reasoning about their own state.
- **Bus is optional.** When no `BusI` is attached, the embedded
  explorer's clickhouse-local-backed tabs (extractAll,
  replaceRegexpAll, multiMatchAllIndices) return the NoopBus error
  shape; the Go-side preview and the cheatsheet still work. The
  level-1 row is unaffected by bus state.

## Trade-offs

- **Level-1 status uses Go regexp only.** A regex that compiles
  under Go's `regexp` but fails under libre2 (or vice-versa) will
  show a misleadingly-green dot. The SD1 tripwire inside the
  embedded explorer surfaces the divergence in the inspector's
  status bar, so the operator sees the truth as soon as they open
  the inspector — but the level-1 row is, by construction, only as
  truthful as one of the two engines. Acceptable because RE2 in Go
  and libre2 in ClickHouse almost always agree.
- **Truncation can hide structural cues.** A 200-char regex
  truncated to 32 characters loses the tail — including, often,
  the anchor or the alternation branch that distinguishes two
  similar patterns. The full pattern is always available inside
  the inspector window; callers who need to disambiguate at the
  level-1 row can `.PatternMaxLen(n)` up to a value that fits their
  surface.
- **One AppI's worth of body per pinned instance.** Each pinned
  inspector window owns its own `*App` — including the
  `compileCache`, the in-flight goroutine state, and the SD1
  tripwire snapshot. Operators who pin many regexsummary
  inspectors at once pay one AppI's footprint per pin. Reasonable
  in the typical "open one or two to investigate a finding" flow;
  potentially noticeable if a dashboard auto-pins dozens.

## What this widget deliberately does not do

- **No regex compilation cache.** Each level-1 frame recompiles
  the host pattern. The `regexp` package compiles `\w+`-class
  patterns in single-digit microseconds; caching would add state
  for negligible gain. The embedded explorer carries its own
  `compileCache` for the multi-pattern path because *that* code
  compiles the patternList line-by-line on every keystroke; the
  level-1 row does not.
- **No bidirectional pattern propagation (yet).** See the Pattern
  propagation section. The API surface (`Render(idGen, pattern)`)
  was chosen so that adding a `Pattern(*string)` overload later —
  or wiring a sink through `inspector.Provenance` — is purely
  additive.
- **No re-styled `regex_explorer`.** The embedded body uses the
  same panels, tabs, and inputs the standalone AppI uses. If a
  caller wants a different shape, the right move is to fork
  `RenderWindow` inside `regex_explorer` (or expose a more
  granular `RenderBody`), not to bypass it from this widget.
- **No tour-mode special case.** The screenshot tour picks up the
  carousel demo automatically; the widget itself has no
  IMZERO2_SCREENSHOT_DIR awareness.

## Composition example

```go
r := regexsummary.New("user-search-regex").
    Bus(ctx.Bus()).                          // optional — enables CH-backed tabs
    Provenance(inspector.Provenance{
        Subject:   "app.spinnaker.event.rules.user-search",
        SourceApp: "spinnaker",
        SampledAt: now,
    })
for _, rule := range searchRules {
    for range c.Horizontal().KeepIter() {
        c.Label(rule.Name).Send()
        r.Render(ids.PrepareSeq(rule.RowID), rule.Pattern)
    }
}
```

One `Renderer` per visual style; the `Render` call is the per-row
cost. Allocations inside `Render` are bounded by one `regexp.Compile`
plus a `[]rune(pattern)` for truncation; the embedded explorer is
lazy and costs zero until first open.

## Further reading

- Decisions: [ADR-0046: imzero2 value-inspector infrastructure](../../../../../../../../doc/adr/0046-imzero2-value-inspector-infrastructure.md)
- Sibling inspector pattern: [`widgets/distsummary/EXPLANATION.md`](../distsummary/EXPLANATION.md)
- Embedded host pattern: [`demo/apps/regex_explorer/embedded.go`](../../demo/apps/regex_explorer/embedded.go)
