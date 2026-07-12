---
type: reference
audience: IDS app authors and contributors fixing designlint violations
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Rule set may change before M2 of [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md); pin a commit if you build tooling against it.

# IDS policy: Tier 1 mechanical rules

This is the rule catalogue for the **Tier 1 — Mechanical** policy layer of the ImZero2 Design System ([ADR-0029 §SD8](../../adr/0029-imzero2-design-system-and-policy-as-code.md)). Rules are enforced by `designlint`, a Go AST walker built on `golang.org/x/tools/go/analysis`, wired into `scripts/ci/lint.sh` alongside vet / staticcheck / errcheck / nilaway / doclint.

**Audience:** contributors and IDS app authors fixing `designlint` violations or extending the rule set. **Status:** draft — the rule set is in flux through M2 of [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md); review entries before relying on stability.

## How to read this catalogue

Each rule entry has:

- **ID** — `L1`, `L2`, … — stable identifier; used in annotations and config.
- **Implementation status** — `shipped` (live in v1), `deferred` (in the catalogue but the designlint binary does not yet enforce). Deferred rules name the M2 API-survey finding that gates them.
- **Default severity** — `warn` (visible but non-blocking), `error` (CI fails), `off` (disabled by default). Apps opt in to stricter severity per §Annotations.
- **What it catches** — the pattern detected by the AST walker.
- **Rationale** — the ADR / pattern doc / memory note that motivates the rule.
- **Example violation + fix** — concrete code.
- **Configuration** — per-rule knobs in `designlint.yaml`.
- **Exemption** — how to opt out (file-level allowlist, line-level annotation) when the rule's heuristic produces a false positive.

The full configuration grammar lives in §Configuration below; per-rule entries reference the relevant knobs by name.

## v1 implementation status (M2 bedrock)

The `designlint` binary at `public/keelson/designsystem/lint/cmd/designlint/` ships eight rules in v1 — the subset whose patterns survived the M2 API survey against real demo code. The remaining three rules stay catalogued (per [ADR-0029 §SD8](../../adr/0029-imzero2-design-system-and-policy-as-code.md)) but require either rule-spec revision against the actual API surface or upstream infrastructure that does not yet exist.

| Rule | Status | Why |
|---|---|---|
| L1 — Label casing | **shipped (v1 partial)** | v1 covers the unambiguous case: `c.Label("...")` whose first non-whitespace character is a lowercase Unicode letter. Atom-builder routes (`c.Atoms().Text("...").Keep()` inside `c.Button(...)` / `c.MenuButton(...)`) and `c.WidgetText().Text("...")` titles still need a context-walker to apply per-widget-class casing; v2 expansion deferred. |
| L2 — Token-only palette | **shipped** | `color.RGB(...)` / `color.RGBA(...)` is the actual Go-side constructor; syntactic detection is reliable. Rust-side equivalents (`Color32::from_rgb`) deferred to a future Rust-side lint (clippy or custom). |
| L3 — Token-only spacing | **shipped** | Trigger surface narrowed to `.AddSpace(N)` / `.InnerMargin(N)` / `.OuterMargin(N)` with a numeric BasicLit; hairline allowlist (0 / 0.0 / 1 / 1.0). Variable-bound args (`styletokens.PaddingDefault(d)` etc.) never trigger because they're not BasicLit nodes — the math/ratio/animation-delta false-positive concern dissolves under that scope. |
| L4 — Token-only rounding | **shipped** | Mirrors L3 on the rounding ladder (ADR-0032 §SD3). Trigger surface: `.CornerRadius(N)` with a numeric BasicLit; sharp-corner allowlist (0 / 0.0). Variable-bound args (`styletokens.RoundingSm` etc.) never trigger because they're not BasicLit nodes. The original L4 slot ("Direct Style mutation outside tokens") was a Rust-side concern structurally out of scope for the Go AST walker; that catalogue entry has been moved to the Rust-side future-work pile and the L4 number is reused for the Go-side rounding rule that fits the same shape as L3 / L2. |
| L5 — `AllocateUiAtRect` in flow | **shipped** | Lexical detection over the AST stack works cleanly; idiom is `for range c.<Flow>().KeepIter() { c.AllocateUiAtRect(...) }`. |
| L6 — Required `PanelCentral` | deferred | The `// imzero2:fullscreen` annotation infrastructure doesn't exist in the codebase yet. Also, the actual idiom is `c.PanelCentralInside().KeepIter()` not `c.PanelCentral()` — needs API alignment first. |
| L7 — Status semantics via enum | deferred | The `statussemantics` enum package described in the original rule does not exist yet. Gate: introduce the enum, then revisit. |
| L8 — Mandatory `Tree(id).Send()` drain | deferred | Requires control-flow analysis to verify the drain follows the node ops within the same lexical block. Higher-effort detector; deferred pending demand. |
| L9 — RadioButton `HasChanged` misuse | **shipped** | Syntactic chain-walk works; the chained form `c.RadioButton(...).<...>.HasChanged()` is the common case. Variable-broken chains are a known v1 false-negative; documented in the analyzer. |
| L10 — Token-only stroke-width | **shipped** | Mirrors L3 / L4 on the stroke ladder (ADR-0032 §SD4). Trigger surface: `.Stroke(...)` with a numeric BasicLit in **either** positional arg, because the binding has both `Stroke(width, col)` (FrameFluid / TintedScopeFluid) and `Stroke(col, width)` (H3RegionFluid / MapPolylineFluid) forms; numeric literals only ever appear in the width position because color args are `color.Hex(...)` CallExprs, so the lit-vs-not-lit signal is reliable without type info. Sentinel allowlist: 0 / 0.0 (disable stroke). Painter free-functions (`c.PaintRectStroke`, `c.PaintCircleStroke`) are out of v1 scope — their positional shape mixes radius and width in receiver-specific orders. |
| L11 — Token-only motion-duration | **shipped** | Mirrors L3 / L4 / L10 on the motion ladder (ADR-0032 §SD5). Trigger surface: `.AnimateBoolWithTime(...)` / `.AnimateBoolWithTimeBind(...)` / `.AnimateValueWithTime(...)` / `.AnimateValueWithTimeBind(...)` with a numeric BasicLit at arg index 2 (the `durSecs float32` slot). The arg-index is per-name (every wrapper today sits at index 2; the table keeps room for future signatures). Sentinel allowlist: 0 / 0.0 (instantaneous — matches the reduced-motion collapsed value). Required prereq: `styletokens.MotionQuickSecs()` / `MotionStandardSecs()` / `MotionSlowSecs()` accessors return the ladder values as float32 seconds (the wire form egui's `animate_*` API expects) and gate through the runtime `motionEnabled` flag so reduced-motion mode collapses to zero — a raw literal does not. Named-const fields (`defaultAnimDurSecs float32 = 0.28`) evade detection because they're not BasicLit nodes inside the trigger call; same shape as L3's named-const limitation. |
| L12 — `Manifest.Id` matches package path | **shipped (warn at landing)** | Pattern rule, not a token rule — added to close the silent-drift hole in [ADR-0026 §SD12](../../adr/0026-app-runtime-and-capability-subjects.md) where `Manifest.Id` literals can diverge from the enclosing Go import path without any compiler signal. Trigger surface narrowed to two shapes (`app.Manifest{Id: …}` field assignment and package-scope `const|var X app.AppIdT = …` typed declarations) so cross-reference tables — `map[K]app.AppIdT{…}` and `[]app.AppIdT{…}`, e.g., the carousel's `legacyCodeToId` — pass through. Test files (`_test.go`) skipped: registry/broker tests use deliberate ad-hoc Ids and forcing them to match the package path would be churn for no signal. `AllowedSpecialIds` allowlist enumerates the runtime services that use NATS-aligned dotted names rather than import paths (`runtime.broker`, `runtime.persist`, `runtime.fs`, `runtime.chlocal`, `runtime.clipboard`, `runtime.sysmetrics`, `runtime.introspect.query`). The list stabilised over the ADR-0026 broker fleet on 2026-07-12 and the rule graduated with the fleet flag-day (§Graduation lifecycle). |

Adding any of the deferred rules is a Tier 3 decision per [tier3-human-review.md](./tier3-human-review.md); each follow-on ADR captures the API survey findings that unblock the rule plus the detector design.

## Rule catalogue

### L1 — Label casing

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L2 / L3 / L4 / L10 / L11).

**What it catches.** String literals passed to label-bearing widget calls — `c.Button(...)`, `c.MenuItem(...)`, `c.Header(...)`, `c.SelectableLabel(...)`, `c.CollapsingHeader(...)`, `c.Window(...)`-title-style — that don't conform to the per-widget casing policy.

**Rationale.** Cohesion across the fleet; consistency is the kind of thing humans tire of pointing out in review (precisely what Tier 1 catches mechanically per [ADR-0029 §Decision](../../adr/0029-imzero2-design-system-and-policy-as-code.md)).

**Per-widget casing policy** (defaults; per-call-site override in `designlint.yaml`):

| Widget class | Casing | Example |
|---|---|---|
| Button | Title Case | `"Save Changes"` |
| MenuItem | Sentence case | `"Save changes"` |
| Header / Panel title | Section Case (Title Case, no trailing punctuation) | `"Recent activity"` |
| SelectableLabel | Sentence case | `"Show advanced options"` |
| CollapsingHeader | Section Case | `"Advanced settings"` |
| Window title | Title Case | `"New Connection"` |

**Example violation:**

```go
c.Button("save changes")        // L1: Button label should be Title Case
c.MenuItem("Open File")         // L1: MenuItem label should be Sentence case
```

**Example fix:**

```go
c.Button("Save Changes")
c.MenuItem("Open file")
```

**Configuration:**

```yaml
L1:
  severity: warn
  per_widget:
    Button: title
    MenuItem: sentence
    Header: section
  allowlist_strings:
    - "OK"        # acronyms / short tokens
    - "URL"
```

**Exemption.** Line-level: `// designlint:ignore=L1 (reason)`. File-level: add the file to `L1.allowlist_files`.

### L2 — Token-only palette

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L3 / L4 / L10 / L11).

**What it catches.** Calls to `egui::Color32::from_rgb(...)`, `Color32::from_hex(...)`, `Color32::from_rgba_*(...)`, and Go-side equivalents (`color.RGBA{...}` literals) outside the token module.

**Rationale.** [ADR-0031](../../adr/0031-imzero2-design-system-color.md) — every color in IDS-conformant apps comes from the semantic palette, the data-encoding palette (Crameri / viridis), or `text.*` / `bg.*` / `border.*` neutrals. Raw color literals break the centralised palette discipline that makes M0 IP boundary checks reproducible.

**Example violation:**

```rust
let label_color = Color32::from_rgb(220, 38, 38);  // L2: raw red literal
```

**Example fix:**

```rust
let label_color = tokens::semantic::ERROR_DEFAULT;
```

**Configuration:**

```yaml
L2:
  severity: error
  allowlist_files:
    - "rust/imzero2/imzero2_egui/src/style/tokens/**"
    - "rust/imzero2/imzero2_egui/src/style/data_encoding/**"
    - "rust/imzero2/imzero2_egui/assets/colors/**"
    - "public/keelson/designsystem/styletokens/**"
```

**Exemption.** Line-level annotation as above. The token module and the vendored scientific palette LUTs are allowlisted by default.

**Recipe.** Step-by-step migration guide for fixing L2 findings in a widget: [howto/migrate-widget-to-ids-tokens.md](../howto/migrate-widget-to-ids-tokens.md). The recipe is sourced from the M5 backfill arc and covers semantic + neutral + data-encoding + sentinel mappings plus the `color.Hex(styletokens.X.AsHex())` bridge.

### L3 — Token-only spacing

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L2 / L4 / L9 / L10 / L11).

**What it catches.** Literal floats in `egui::Spacing` field assignments and as direct arguments to spacing-aware APIs (`ui.add_space(x)`, `Vec2::new(x, y)` when used as spacing, etc.) outside the token module. Values in the magnitude ladder (2.0, 4.0, 6.0, 8.0, 12.0, 16.0, 24.0, 32.0, 48.0 — per [ADR-0032 §SD2](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) are the canonical violations.

**Rationale.** [ADR-0032 §SD2](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — spacing tokens drive density resolution. Raw literals bypass the density preset; the panel that should adapt to Tight stays at Standard.

**Example violation:**

```rust
ui.spacing_mut().item_spacing = Vec2::new(8.0, 8.0);  // L3: raw 8.0 spacing
ui.add_space(12.0);                                    // L3: raw 12.0
```

**Example fix:**

```rust
ui.spacing_mut().item_spacing = Vec2::splat(tokens::gap_items(density));
ui.add_space(tokens::padding_outer(density));
```

**Configuration:**

```yaml
L3:
  severity: warn
  allowlist_floats: [0.0, 1.0, NaN]  # zero, hairline strokes, sentinels
  allowlist_files:
    - "rust/imzero2/imzero2_egui/src/style/tokens/**"
    - "public/keelson/designsystem/styletokens/**"
```

**Exemption.** Line-level annotation; allowlist for hairline strokes (1 px is sometimes correct) and zero / NaN sentinels.

### L4 — Token-only rounding

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L3).

**What it catches.** Calls to `.CornerRadius(N)` whose first positional argument is a numeric `BasicLit` (INT or FLOAT) outside the styletokens module. The trigger is the *selector name only* — works for `FrameFluid.CornerRadius`, `ProgressBarFluid.CornerRadius`, `TintedScopeFluid.CornerRadius`, and any future receiver that exposes the same setter.

**Rationale.** [ADR-0032 §SD3](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — the rounding ladder is fixed at 0 / 2 / 4 / 6 px (`RoundingNone` / `RoundingSm` / `RoundingMd` / `RoundingLg`). A raw literal in `.CornerRadius()` bypasses the ladder, so a panel that should adopt `RoundingMd` (4 px cards/dialogs) instead drifts to whatever happened to be typed at the call site. Unlike L3, rounding is density-independent — the case here is purely "literal numbers fragment the visual system".

**Example violation:**

```go
c.NewFrame().CornerRadius(4)            // L4: raw literal 4
c.NewFrame().CornerRadius(6.0)          // L4: raw literal 6.0
c.NewProgressBar().CornerRadius(3)      // L4: off-ladder
```

**Example fix:**

```go
c.NewFrame().CornerRadius(styletokens.RoundingMd)   // ladder-aligned card
c.NewFrame().CornerRadius(styletokens.RoundingLg)   // floating-window radius
c.NewFrame().CornerRadius(0)                         // sharp corners (allowlisted)
```

**Configuration:**

```yaml
L4:
  severity: error
  allowlist_literals: [0, 0.0]   # sharp corners are sentinel, not a token
  allowlist_files:
    - "public/keelson/designsystem/styletokens/**"
```

**Exemption.** Line-level: `// designlint:ignore=L4 (reason)`. Required justification because the ladder is small and almost always covers the case.

> **Catalogue note.** The L4 slot was originally reserved for "Direct Style mutation outside tokens" — a Rust-side `ctx.set_visuals(...)` / `style_mut()` check that is structurally out of scope for this Go AST walker. The Rust-side check stays on the future-work pile (probably a clippy lint or a custom Rust analyzer); the L4 number is reused here for the Go-side rounding rule that fits the same mechanical shape as L2 / L3.

### L10 — Token-only stroke-width

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L3 / L4).

**What it catches.** Calls to `.Stroke(...)` whose **either** positional argument is a numeric `BasicLit` (INT or FLOAT) outside the styletokens module. The trigger is the *selector name only* — works for `FrameFluid.Stroke(width, col)`, `TintedScopeFluid.Stroke(width, col)`, `H3RegionFluid.Stroke(col, width)`, `MapPolylineFluid.Stroke(col, width)`, and any future receiver that exposes the same setter. The width arg can sit in either position depending on the receiver's signature; color args are always `color.Hex(...)` CallExprs (never bare numeric BasicLits), so the analyzer reliably picks out the width without needing type info.

**Rationale.** [ADR-0032 §SD4](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — the stroke ladder is fixed at 1.0 / 1.5 / 2.0 px (`StrokeHair` / `StrokeRegular` / `StrokeStrong`). Strokes are density-independent perceptual constants (any thinner and they vanish on HiDPI), so the case is purely "literal numbers fragment the visual system".

**Example violation:**

```go
c.NewFrame().Stroke(1.5, border)              // L10: raw literal 1.5
c.NewFrame().Stroke(2, border)                // L10: raw literal 2
c.MapPolyline(...).Stroke(color.Hex(...), 3)  // L10: raw literal 3 (off-ladder)
```

**Example fix:**

```go
c.NewFrame().Stroke(styletokens.StrokeRegular, border)   // standard border
c.NewFrame().Stroke(styletokens.StrokeStrong, border)    // emphasised
c.MapPolyline(...).Stroke(color.Hex(...), styletokens.StrokeStrong)
c.NewFrame().Stroke(0, border)                            // disable (allowlisted)
```

**Configuration:**

```yaml
L10:
  severity: error
  allowlist_literals: [0, 0.0]   # disable-stroke sentinel
  allowlist_files:
    - "public/keelson/designsystem/styletokens/**"
```

**Exemption.** Line-level: `// designlint:ignore=L10 (reason)`. Required justification because the three-rung ladder almost always covers the case; off-ladder strokes are typically a visual-test smell.

**Out of v1 scope.** `c.PaintRectStroke(x1,y1,x2,y2,radius,color,width)` and `c.PaintCircleStroke(cx,cy,r,color,width)` free functions. Their positional shape mixes radius/width in receiver-specific orders; the syntactic walker can't disambiguate without type info. Defer to a v2 rule or a Painter-API simplification first.

### L11 — Token-only motion-duration

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L3 / L4 / L10).

**What it catches.** Calls to `.AnimateBoolWithTime(...)` / `.AnimateBoolWithTimeBind(...)` / `.AnimateValueWithTime(...)` / `.AnimateValueWithTimeBind(...)` whose `durSecs float32` argument (arg index 2 for every wrapper today) is a numeric `BasicLit` (INT or FLOAT) outside the styletokens module. The trigger is the *selector name only* — works for `c.AnimateBoolWithTimeBind`, `bindings.AnimateBoolWithTimeBind`, and any future receiver that exposes the same setter at the same arg index.

**Rationale.** [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — the motion ladder is fixed at 80 / 160 / 320 ms (`MotionQuickSecs` / `MotionStandardSecs` / `MotionSlowSecs`, surfaced as the float32 seconds the egui binding API expects). Two cases compound here: literal durations *fragment timing* across the fleet (timing inconsistency reads as visual jank when two panels animate at different durations side-by-side), **and** literals silently bypass the runtime reduced-motion gate — the ladder accessors collapse to 0 when `styletokens.MotionEnabled()` is false (OS preference, tour-capture mode), but a raw literal does not.

**Example violation:**

```go
c.AnimateBoolWithTimeBind(animId, st.expanded, 0.4, &st.t1)         // L11: raw literal 0.4
c.AnimateValueWithTimeBind(animId, targetVal, 0.6, &st.t3)          // L11: raw literal 0.6 (off-ladder)
```

**Example fix:**

```go
c.AnimateBoolWithTimeBind(animId, st.expanded, styletokens.MotionStandardSecs(), &st.t1)
c.AnimateValueWithTimeBind(animId, targetVal, styletokens.MotionSlowSecs(), &st.t3)
c.AnimateBoolWithTimeBind(animId, st.expanded, 0, &st.t1)            // disable (allowlisted)
```

**Configuration:**

```yaml
L11:
  severity: error
  allowlist_literals: [0, 0.0]   # instantaneous / reduced-motion sentinel
  allowlist_files:
    - "public/keelson/designsystem/styletokens/**"
```

**Exemption.** Line-level: `// designlint:ignore=L11 (reason)`. Required justification because the three-rung ladder almost always covers the case; off-ladder durations are typically a "I haven't decided on the timing yet" smell.

**Known v1 limitations.** Named-const fields (`defaultAnimDurSecs float32 = 0.28` in `widgets/treemap/`) evade detection because they're not BasicLit nodes inside the trigger call. Same shape as L3's named-const limitation; cleanup is tracked alongside that work. The non-Bind variants (`AnimateBoolWithTime`, `AnimateValueWithTime`) are also in the trigger map even though current consumers all go through the `*Bind` wrappers — future receivers that bypass the binding helper still get caught.

### L5 — `AllocateUiAtRect` inside flow containers

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L2 / L3 / L4 / L9 / L10 / L11).

**What it catches.** Calls to `c.AllocateUiAtRect(...)` inside a lexical `Vertical(...)` / `Horizontal(...)` / `Grid(...)` block. The check is *lexical* — it walks the AST upward from the `AllocateUiAtRect` call site and flags any containing flow container, regardless of dynamic control flow.

**Rationale.** [`project_imzero2_allocate_ui_at_rect`] memory note — `AllocateUiAtRect` positions a child Ui at absolute parent coordinates and silently breaks the enclosing flow. Hard to debug without the rule because the flow simply renders wrong.

**Example violation:**

```go
c.Vertical(func() {
    c.Label("Header")
    c.AllocateUiAtRect(rect, func() {  // L5: absolute positioning breaks Vertical flow
        c.Label("Off-grid content")
    })
})
```

**Example fix.** Move the absolute-positioned content outside the flow container, or use an overlay panel:

```go
c.Vertical(func() {
    c.Label("Header")
})
c.AllocateUiAtRect(rect, func() {
    c.Label("Overlay content")
})
```

**Configuration:**

```yaml
L5:
  severity: warn
  # No allowlist — AllocateUiAtRect inside flow is essentially always a bug.
```

**Exemption.** Line-level: `// designlint:ignore=L5 (reason)`. Required justification because false-positives are rare and the bug class is real.

### L6 — Required `PanelCentral` for full-screen apps

**Default severity:** `warn`.

**What it catches.** Top-level app entrypoints annotated `// imzero2:fullscreen` that do not call `c.PanelCentral()` somewhere in their render path.

**Rationale.** [`reference_egui2_panel_central`] memory note — full-screen apps must call `c.PanelCentral()`. Widgets outside any panel have no ui scope and flicker + lose input.

**Example violation:**

```go
// imzero2:fullscreen
func render(c *Ctx) {
    c.Label("hello")                  // L6: no PanelCentral; will flicker
}
```

**Example fix:**

```go
// imzero2:fullscreen
func render(c *Ctx) {
    c.PanelCentral(func() {
        c.Label("hello")
    })
}
```

**Configuration:**

```yaml
L6:
  severity: warn
  annotation: "imzero2:fullscreen"   # the trigger annotation
```

### L7 — Status semantics via enum, not color literal

**Default severity:** `warn` (heuristic; may stay warn longer than other rules).

**What it catches.** Variables / function returns named like `status_color`, `state_color`, `*_status_color`, or values passed to widgets in known status-coding contexts (e.g., a `Badge::status_color(...)` setter) whose value is a raw `Color32` literal or a semantic palette tone that doesn't come from the `statussemantics` enum mapping.

**Rationale.** [patterns/status-and-legends.md](../patterns/status-and-legends.md) — status uses the canonical 6-role enum (info / success / warning / error / neutral / accent), never raw colors. Status semantics is fleet-wide; per-app deviations are Tier 3.

**Example violation:**

```rust
let status_color = Color32::from_rgb(220, 38, 38);  // L7: raw color in status context
badge.color(status_color);
```

**Example fix:**

```rust
badge.color(statussemantics::Error.default_color());  // L7: enum-routed
```

**Configuration:**

```yaml
L7:
  severity: warn
  status_contexts:
    # Functions whose color args are status-coded:
    - "Badge::color"
    - "Badge::status_color"
    - "StatusBar::set_color"
  variable_name_patterns:
    - "*status_color*"
    - "*state_color*"
```

**Exemption.** Heuristic-heavy rule with the highest false-positive rate; line-level ignore is acceptable when the context is genuinely not status-coded.

### L8 — Mandatory `Tree(id).Send()` drain after node ops

**Default severity:** `warn`.

**What it catches.** Calls to `c.NodeDir(...)` / `c.NodeLeaf(...)` not followed by a `c.Tree(id).Send()` drain in the same lexical control flow.

**Rationale.** [`feedback_tree_drain`] memory note — `NodeDir` / `NodeLeaf` enqueue `r3_node_cmds`; rendering happens inside `c.Tree(id).Send()`. Skipping the drain leaves nodes invisible.

**Example violation:**

```go
c.NodeDir("root", func() {
    c.NodeLeaf("child")
})
// L8: no c.Tree("root").Send() — nothing renders
```

**Example fix:**

```go
c.NodeDir("root", func() {
    c.NodeLeaf("child")
})
c.Tree("root").Send()
```

**Configuration:**

```yaml
L8:
  severity: warn
```

### L9 — `HasPrimaryClicked` for RadioButton

**Default severity:** `error` (graduated after M5 fleet backfill, mirroring L3 / L4 / L10 / L11).

**What it catches.** `c.RadioButton(...).HasChanged()` calls. The egui `RadioButton` widget never calls `mark_changed`; `HasChanged()` silently drops every click.

**Rationale.** [`feedback_radio_haspricked`] memory note. Hard-to-detect bug because the code compiles and runs; the radio simply never responds.

**Example violation:**

```go
if c.RadioButton("Option A", &selected, "A").HasChanged() {  // L9
    handleChange()
}
```

**Example fix:**

```go
if c.RadioButton("Option A", &selected, "A").HasPrimaryClicked() {
    handleChange()
}
```

**Configuration:**

```yaml
L9:
  severity: error
```

### L12 — `Manifest.Id` matches package import path

**Default severity:** `warn`. Ships warn at landing. Tree was at zero L12 findings at the analyzer's introduction (the M5 fleet-backfill equivalent happened concurrently with the rule's debugging) but graduation to `error` is deferred until the `AllowedSpecialIds` list stabilises — adding a fifth runtime service is a known follow-on that's easier to land while the step is non-blocking.

**What it catches.** Two AST patterns where an `app.AppIdT`-typed string literal asserts "this is my Id":

- `app.Manifest{Id: <literal>}` composite-literal field assignment — the common case for every app's `app_register.go`.
- Package-scope `const|var X app.AppIdT = <literal>` typed declarations — the `ManifestId` export pattern used by `apps/capinspector/` (so the carousel can call `host.Open(capinspector.ManifestId)`) and by the four runtime services (`capbroker.BrokerAppId`, `persist.ServiceAppId`, `fsbroker.ServiceAppId`, `chlocalbroker.ServiceAppId`).

A literal passes when it (a) exactly equals the enclosing package's import path, (b) is prefixed by `<package-path>/` (synthetic-subpath case for folded demos like `…/apps/widgets/table`), or (c) appears in `AllowedSpecialIds` — NATS-aligned runtime services that use a dotted name rather than an import path: `runtime.broker`, `runtime.persist`, `runtime.fs`, `runtime.chlocal`, `runtime.clipboard`, `runtime.sysmetrics`, `runtime.introspect.query`.

**Rationale.** [ADR-0026 §SD12](../../adr/0026-app-runtime-and-capability-subjects.md) makes `Manifest.Id` a public-stability surface and equates it with the Go import path. Three downstream consumers rely on the equation:

- `keelson/security/capslock.packageForManifest` ([`check.go:182`](../../../public/keelson/security/capslock/check.go)) looks up the Id verbatim as a Go package path in the capslock JSON report. Drift = silent loss of capslock cross-check coverage; `evaluateAll` hits the nil-map branch and skips with `continue`, no error.
- The `--launch <id>` CLI surface and `windowhost.Open(id)` accept the Id literally; typos build clean and only surface at dispatch.
- Audit rows (factsstore `MembRuntimeApp`) reference the Id as the stable cross-process identifier.

The rule closes the silent-degradation hole specifically. The ADR-0035 keelson rename ([2026-05-17 amendment](../../adr/0026-app-runtime-and-capability-subjects.md)) required a manual sweep of every Id literal — the kind of cost this rule makes mechanical going forward.

**Example violation:**

```go
package mywidget

import "github.com/stergiotis/boxer/public/keelson/runtime/app"

var manifest = app.Manifest{
    Id: "github.com/stergiotis/boxer/public/mywidegt",  // L12: typo (mywidegt); package import path is "…/mywidget"
}
```

**Example fix:**

```go
var manifest = app.Manifest{
    Id: "github.com/stergiotis/boxer/public/mywidget",
}
```

For an intentional alias (legacy id kept for audit-row continuity, vendor compatibility), suppress per-site:

```go
var manifest = app.Manifest{
    // designlint:ignore=L12 (legacy alias kept for stored audit-row continuity)
    Id: "github.com/example/legacy-name",
}
```

**Configuration:**

```yaml
L12:
  severity: error
  allowed_special_ids:
    - "runtime.broker"
    - "runtime.persist"
    - "runtime.fs"
    - "runtime.chlocal"
    - "runtime.clipboard"
    - "runtime.sysmetrics"
    - "runtime.introspect.query"
```

`allowed_special_ids` is the configurable mirror of the analyzer's package-level `AllowedSpecialIds`; ships pre-populated with the runtime services (seven as of 2026-07-12) and accepts repo-local additions for new dotted-namespace runtime services as ADR-0026's broker fleet grows.

**Exemption.** Line-level: `// designlint:ignore=L12 (reason)` on or immediately above the `Id:` field (per `ignoreann`'s line-N / line-N+1 coverage contract — the ignore must sit next to the keyvalue, not above the enclosing `var manifest = app.Manifest{` opener). Reason required because legitimate exemptions are rare and named — every existing one is documented either in this catalogue (`AllowedSpecialIds`) or in code.

**Known v1 limitations.**

- **Test files skipped.** `_test.go` sources use ad-hoc literal Ids (`"test.app"`, `"org.test.x"`, `"a"`, `"play"`) to exercise registry / broker / supervisor logic; forcing those to match the package path would be churn without signal. The skip is hard-coded; there is no flag to opt back in.
- **Cross-reference tables exempt.** `map[K]app.AppIdT{…}` and `[]app.AppIdT{…}` literals deliberately hold *other packages'* Ids — e.g., the carousel's `legacyCodeToId` mapping numeric codes to AppIds across the fleet. The analyzer narrows to `app.Manifest{Id: …}` and typed-const declarations specifically by walking up the AST stack to verify the immediately-enclosing CompositeLit's type is `app.Manifest`; cross-reference structures sit in other CompositeLit types and pass through. A dedicated regression test (`TestL12_CrossReferenceTablesExempt`) locks this in.
- **Variable-bound Ids evade detection.** A `const importPath = "…"; var manifest = app.Manifest{Id: app.AppIdT(importPath + "/foo")}` shape doesn't resolve to a string constant via `pass.TypesInfo.Types[expr].Value`, so the check returns false and the literal isn't validated. Rare in practice (no production occurrences as of the rule's landing); same shape as L3's named-const limitation.

### Precedent: `TestStatefulWidgetsAreGated`

The drift guard from [ADR-0013](../../adr/0013-imzero2-stateful-widget-contract.md) — a Go test that fails when stateful-widget apply specs don't go through `applyCodeWidgetRustOnEvent` — is the precedent for mechanical enforcement of a stylistic rule. It is *not* migrated to `designlint`; the test form is shipping and changing it carries no benefit. New mechanical rules going forward land as `designlint` passes; the stateful-widget contract remains gated by its existing Go test.

## Annotations

Three annotation forms are catalogued; **only the line-level ignore is implemented** (`internal/ignoreann`). The two strict forms were designed for the per-app graduation path that §Graduation lifecycle records as skipped — the fleet went to `error` in one flag-day, so nothing consumes them. They stay catalogued for a future rule that needs per-file graduation; implementing them then is part of that rule's landing.

- **Line-level ignore** *(implemented)*. `// designlint:ignore=<rule-id> (<reason>)` placed on the line before or at the end of the line containing the violation. Suppresses one rule at one site; reason is mandatory.
- **Per-rule strict** *(catalogued, not implemented)*. `// designlint:strict-rules=<rule-id>[,<rule-id>...]` placed at the top of a file (before any non-comment statement). Graduates the named rules from warn to error for the whole file.
- **App-level strict** *(catalogued, not implemented)*. `// designlint:strict` placed at the top of a file. Graduates *all* rules from warn to error for the whole file.

Annotations are read by the AST walker at lint time; they have no runtime presence in the app.

**Example annotated file:**

```go
// designlint:strict
package main

import "..."

func renderPanel(c *Ctx) {
    // designlint:ignore=L5 (overlay tooltip; intentional absolute positioning)
    c.AllocateUiAtRect(tooltipRect, func() { ... })
}
```

## Configuration: `designlint.yaml`

> **Implementation status: not implemented.** The analyzer reads no configuration file — severities are fixed by the `lint.sh` gate (any finding fails the build since the M5 flag-day), and allowlists live in code next to the rule that owns them (e.g. `l12manifestid.AllowedSpecialIds`, the L3/L4/L10/L11 sentinel allowlists). The schema below is the designed shape, kept for the day a rule needs per-repo or per-package configuration; wiring the reader is part of that rule's landing.

The repo-root `designlint.yaml` (or per-package `.designlintrc.yaml`) controls per-rule severity and allowlists. Schema sketch:

```yaml
# designlint.yaml
defaults:
  severity: warn        # default for any rule not listed below

rules:
  L1:
    severity: error      # M5-graduated; fleet at zero, lint.sh fails on any finding
    per_widget:
      Button: title
      MenuItem: sentence
      Header: section
    allowlist_strings: ["OK", "URL"]
  L2:
    severity: error      # M5-graduated
    allowlist_files:
      - "rust/imzero2/imzero2_egui/src/style/tokens/**"
      - "rust/imzero2/imzero2_egui/src/style/data_encoding/**"
  L3:
    severity: error      # M5-graduated
    allowlist_floats: [0.0, 1.0, NaN]
  L5:
    severity: error      # M5-graduated; line-level ignores only.
  L7:
    severity: warn       # deferred — statussemantics enum package does not exist yet
    status_contexts:
      - "Badge::color"
      - "StatusBar::set_color"

# Per-app graduation tracking (M2 backfill artefact):
strict_apps:
  - "public/thestack/imzero2/egui2/demo/apps/carousel/"
  - "public/thestack/imzero2/egui2/demo/apps/imztop/"
```

**Discovery rule.** `designlint` walks upward from each Go file's directory looking for the nearest `.designlintrc.yaml`, then falls back to the repo-root `designlint.yaml`. Per-package configs override repo-root entries for the rules they touch.

## Graduation lifecycle

Per [ADR-0029 §SD14 M2 / M5](../../adr/0029-imzero2-design-system-and-policy-as-code.md):

- **M2 — Rules ship as `warn`.** Landed 2026-06-24: designlint wired into `lint.sh` as a warn-only step. Apps that violate Tier 1 rules emit warnings; CI does not block.
- **Per-app graduation (skipped).** The originally-planned `// designlint:strict` per-file annotation path was bypassed: with a single-maintainer fleet, sweeping each rule to zero across the tree was less ceremonial than per-file flipping. The strict annotations were consequently never implemented (§Annotations).
- **M5 — Fleet backfill complete (2026-07-12).** Every shipped rule reached zero findings tree-wide in one sweep: L3 / L4 / L10 raw literals replaced with `styletokens` accessors (value-preserving at Standard density where the ladder allowed it), L1 casing fixed, the two intentional L5 sites given line-level ignores, and L12's `AllowedSpecialIds` stabilised over the ADR-0026 broker fleet (L2 / L9 / L11 were already at zero). The `lint.sh` designlint step then flipped from `step_end warn` to `step_end fail` on any output. Any new violation now fails CI; the `// designlint:ignore=<rule-id> (reason)` per-line annotation stays as the intentional-exception escape hatch.

The graduation was a fleet-wide flag day, executed once every shipped rule sat at zero — no per-app `// designlint:strict` annotations were ever added. New Tier 1 rules added later (Tier 3 escalation per [tier3-human-review.md](./tier3-human-review.md)) ship at `warn` and follow the same backfill-then-graduate pattern.

## Adding new rules

Adding a Tier 1 rule is a Tier 3 ([ADR-0029 §SD10](../../adr/0029-imzero2-design-system-and-policy-as-code.md)) decision, captured as an Amendment to [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) §SD8 *and* a corresponding entry in this catalogue.

The criteria for a new Tier 1 rule:

- The pattern is mechanically detectable via the Go AST (or via a string-literal scan).
- False-positive rate is bounded (≤ ~5% across the existing apps) — heuristic rules with higher false-positive rates belong in Tier 2.
- The rationale is documented in an ADR, pattern doc, or memory note.
- Exemption mechanism is specified.
- The rule starts at `warn` severity.

Candidate rules under consideration (not yet implemented):

- **L13 — Raw codepoint literals.** Catches `"\u{f05a}"` style icon-codepoint literals in panel code (use the `icons.Ph<Name>` / `icons.NF<Name>` accessors from `keelson/runtime/icons/` instead per [ADR-0044](../../adr/0044-imzero2-design-system-iconography.md)). [patterns/iconography.md](../patterns/iconography.md) §icon-name-conventions.
- **L14 — Grid-alignment.** Catches explicit positions that aren't multiples of 2 px — heuristic-heavy; may stay Tier 2 V9 (rubric) per [ADR-0032 §SD2](../../adr/0032-imzero2-design-system-spacing-density-motion.md).
- **L15 — Mixed-density tables.** Catches multiple density settings within one app — heuristic; cross-file analysis required.

> **Numbering note.** Earlier drafts of this section catalogued L10–L12 as candidates; L10 (Token-only stroke-width), L11 (Token-only motion-duration), and L12 (Manifest Id matches package path) have all shipped under different designs than those drafts. The candidate slots above are renumbered to L13–L15 to reflect actual availability.

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; §SD8 enumerates the rule set.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — typography tokens that L1 / L3 enforce against.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — palette tokens that L2 / L7 enforce against.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — spacing tokens that L3 enforces against.
- [ADR-0013 — ImZero2 stateful widget contract](../../adr/0013-imzero2-stateful-widget-contract.md) — precedent for mechanical enforcement via Go test (`TestStatefulWidgetsAreGated`); not migrated to `designlint`.
- [patterns/iconography.md](../patterns/iconography.md) — raw codepoint literal anti-pattern (candidate L10).
- [patterns/status-and-legends.md](../patterns/status-and-legends.md) — status semantic enum (basis for L7).
- `tier2-llm-review.md` *(forthcoming)* — companion catalogue for Tier 2 rubrics (V1–V8); the heuristic rules that resist mechanical detection.
- `tier3-human-review.md` *(forthcoming)* — process for Tier 3 ADR additions; how new tokens, patterns, and rules enter the system.
- `golang.org/x/tools/go/analysis` — Go AST analysis framework used by `designlint`.
