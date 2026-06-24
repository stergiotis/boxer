---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: time-range picker

A time-range picker is the affordance that selects a `(start, end)` interval — and, optionally, an auto-refresh cadence — for time-series data. In dashboards, it is *the* source of truth for the X axis of every linked plot ([patterns/plots.md](./plots.md) §multi-pane-composition). Without it, a Grafana-style dashboard cannot exist; with it, dozens of plots coordinate to one consistent view.

This doc explains the visual / interaction pattern. The technical contract — wire format, defaults, semantics — lives in [ADR-0016](../../adr/0016-imzero2-time-range-picker.md) (and the in-flight port from Grafana v7.5.17, per [`project_time_range_picker_port`]). The two are read together: ADR-0016 is the *what*; this doc is the *how it looks and behaves*.

## Background

**Why a single picker.** A dashboard with three plots each picking their own X range is unreadable — the user has to mentally align three different windows. The Grafana convention solves this with one dashboard-level time-range picker plus axis-linking on the plots; IDS adopts the same idiom because operators familiar with Grafana will reach for it without a learning curve.

**Wire format** ([ADR-0016](../../adr/0016-imzero2-time-range-picker.md)): `(start_ns: i64, end_ns: i64, refresh_s: u16)`.

- `start_ns`, `end_ns` — nanoseconds since the Unix epoch (1970-01-01 UTC); signed `i64` accommodates pre-epoch ranges if a domain needs them.
- `refresh_s` — auto-refresh cadence in seconds; `0` disables auto-refresh; `u16` caps at 65 535 s ≈ 18 hours, which exceeds any sensible auto-refresh.

This is the canonical representation everywhere the range crosses a boundary (RPC, persistence, app-to-app coordination). UI surfaces render it as relative (`"Last 1 hour"`) or absolute (ISO 8601) text per §display below.

**Foundational dependencies.**

- Color ([ADR-0031](../../adr/0031-imzero2-design-system-color.md)) — semantic palette for the live-streaming indicator; never the data-encoding palette.
- Typography ([ADR-0030](../../adr/0030-imzero2-design-system-typography.md)) — `Body.Mono.Numeric` for ISO timestamps; `Body` for relative ranges; `Caption` for the refresh-interval label.
- Spacing ([ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — `Padding.Default` for picker button; `Gap.Items` for quick-pick list rows; `Motion.Quick` for the dropdown expand/collapse.
- Icons ([patterns/iconography.md](./iconography.md)) — calendar (`fa-calendar` ≈ `\u{f073}`), clock (`fa-clock-o` ≈ `\u{f017}`), refresh (`fa-refresh` ≈ `\u{f021}`), live-dot (`fa-circle` ≈ `\u{f111}`), step controls (caret-left / caret-right).
- Boxer time ticks (`reference_boxer_timeticks`) — calendar-aware tick labels are used by *plots driven by* the picker, not the picker itself, but the same module formats datetimes for display.

**Relationship to plots.** Per [patterns/plots.md](./plots.md), linked plots (via `Plot::link_axis(group, true, false)`) follow the picker's range. Pan / zoom on a plot can propagate back to the picker (bidirectional sync) — the picker reflects whatever range the user has converged on, regardless of which surface drove the change.

## How it works

### Anatomy

Three top-level forms — inline button (default), dashboard toolbar, plot-attached compact — share an underlying anatomy:

```
┌─ inline button (default form) ─────────────────────────────┐
│  📅  Last 1 hour              ▼   ●  refresh: 10s          │
│       ↑                       ↑    ↑                       │
│   current range display      open  live indicator + cadence│
└────────────────────────────────────────────────────────────┘
```

When the button is clicked, a dropdown opens:

```
┌─ dropdown ─────────────────────────────────────────────────┐
│  Quick                            │  Custom range          │
│   • Last 5 minutes                │  Start: [2026-05-14    │
│   • Last 15 minutes               │          14:00:00 UTC] │
│   • Last 1 hour      ←active      │  End:   [2026-05-14    │
│   • Last 6 hours                  │          15:00:00 UTC] │
│   • Last 24 hours                 │                        │
│   • Today                         │  [Apply]    [Cancel]   │
│   • Yesterday                     │                        │
│   • Last 7 days                   │  Refresh interval:     │
│   ... (more)                      │  [Off ▾]               │
└────────────────────────────────────────────────────────────┘
```

Sub-elements:

- **Trigger button** — shows current range, calendar icon, dropdown caret. Density-scaled padding.
- **Quick-pick column** — list of canonical relative-time presets (see §quick-picks below); active preset is highlighted via `semantic.accent.subtle` background + `border.default` left border.
- **Custom range inputs** — paired datetime inputs (start, end), validated `start < end`; ISO 8601 with timezone (UTC default, app-local optional per [patterns/plots.md](./plots.md) §axes).
- **Refresh interval selector** — dropdown of canonical refresh values (see §refresh-intervals below); `Off` is the default.
- **Apply / Cancel** — primary / secondary CTAs for the custom range. Quick picks apply immediately (no Apply button needed).
- **Live indicator + cadence** — pulsing dot + cadence label (`"refresh: 10s"`) visible whenever `refresh_s > 0`. Hidden when refresh is `Off`.
- **Step controls (optional)** — caret-left / caret-right beside the trigger button, shifting the range backward / forward by the current window width. Useful for pre/post comparison.

### Layout variations

- **Inline button** (default). Lives in the panel header or toolbar of the dashboard / panel that owns the picker. Used when the picker drives one panel or one dashboard tab.
- **Dashboard-level toolbar**. Pinned at the top of a dashboard view; drives every plot in the dashboard via axis linking. Larger trigger button; explicit refresh-interval pill alongside.
- **Plot-attached compact**. A reduced form attached to a single plot's top edge — just the current range display + a click target that opens the full dropdown elsewhere. Used in side-panel plots that don't own the dashboard range.

The three forms share the same dropdown content; only the trigger surface differs.

### Quick-pick canonical set

Two categories — rolling-window ("Last X") and calendar-aligned (Today / Yesterday / This week / etc.):

| Quick-pick | Resolved `(start_ns, end_ns)` |
|---|---|
| Last 5 minutes | `(now - 5m, now)` |
| Last 15 minutes | `(now - 15m, now)` |
| Last 30 minutes | `(now - 30m, now)` |
| Last 1 hour | `(now - 1h, now)` |
| Last 3 hours | `(now - 3h, now)` |
| Last 6 hours | `(now - 6h, now)` |
| Last 12 hours | `(now - 12h, now)` |
| Last 24 hours | `(now - 24h, now)` |
| Last 2 days | `(now - 2d, now)` |
| Last 7 days | `(now - 7d, now)` |
| Last 30 days | `(now - 30d, now)` |
| Today | `(00:00:00 today, now)` |
| Yesterday | `(00:00:00 yesterday, 00:00:00 today)` |
| This week | `(Monday 00:00:00, now)` |
| Last week | `(prev Monday 00:00:00, this Monday 00:00:00)` |
| This month | `(1st of month 00:00:00, now)` |
| Last month | `(prev 1st 00:00:00, this 1st 00:00:00)` |

Seventeen entries total. Calendar-aligned entries respect the active timezone — "Today" in UTC starts at `00:00:00 UTC`; in CEST at `00:00:00 CEST`. Week starts Monday (ISO 8601); apps with a different week-start convention need a Tier 3 deviation.

The set is fleet-wide; extending it is a Tier 3 ([ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) §SD10) decision. Apps that need rare windows (e.g., "Last fiscal quarter") use the custom range inputs rather than adding presets.

### Refresh-interval canonical set

| Label | `refresh_s` value |
|---|---:|
| Off | 0 |
| 5 seconds | 5 |
| 10 seconds | 10 |
| 30 seconds | 30 |
| 1 minute | 60 |
| 5 minutes | 300 |
| 15 minutes | 900 |
| 30 minutes | 1800 |
| 1 hour | 3600 |

The chosen interval must exceed the panel's typical render budget (~50 ms for a busy dashboard); selecting `5 seconds` on a dashboard that takes 8 s to refresh causes thrash. Apps can warn at picker time if the data-fetch p95 is incompatible with the chosen interval — implementation-level concern, not a picker rule.

### Display formats

The picker shows current range in two forms depending on whether the active selection is a quick-pick or a custom range:

- **Quick-pick active** — relative text, `Body` (proportional): `"Last 1 hour"`, `"Today"`, `"Yesterday"`.
- **Custom range active** — absolute text, `Body.Mono.Numeric`: `"2026-05-14 14:00:00 → 15:00:00 UTC"`. The arrow separator (`→` U+2192) is preferred over a hyphen because the range direction is semantic.

Both forms show the timezone abbreviation when not UTC. UTC display omits the abbreviation by convention (it's the implicit default); local time always shows `CEST`, `EST`, `JST`, etc.

For sub-minute precision (rare but real — performance investigation, latency analysis), the format extends to milliseconds or microseconds: `"14:00:00.000 → 14:00:01.500 UTC"`. Nanosecond precision shows in the underlying wire format but is rarely displayed unless the range is < 1 ms.

### Custom range inputs

Two side-by-side datetime fields (start, end). Each accepts:

- ISO 8601 (`2026-05-14T14:00:00Z` or `2026-05-14 14:00:00 UTC`)
- Relative shorthand (`now`, `now-1h`, `now-7d`) — Grafana-style, recognised by the parser
- Calendar-widget input (optional, opt-in per app — egui has no built-in calendar; a custom calendar widget is a Tier 3 escalation)

The parser is permissive on input (accepts several timezone forms, several date separators) but normalises on Apply (always emits canonical ISO 8601 on the wire). Validation rule: `start_ns < end_ns` strictly; equal values are rejected with an inline error.

Keyboard input is mandatory. Pickers that require pointer interaction for custom range entry are an anti-pattern — operators using keyboard navigation lose the affordance.

### Live indicator and pause semantics

When `refresh_s > 0`:

- The live-indicator dot (`fa-circle`) pulses with `Motion.Standard` opacity oscillation, tinted `semantic.success.default` when actively refreshing on schedule. Reduced-motion preference resolves the pulse to a static filled dot per [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md).
- The cadence label (`"refresh: 10s"`) appears next to the dot in `Caption.Mono.Numeric`.
- **User pan / zoom on a linked plot pauses auto-refresh** automatically, per [patterns/plots.md](./plots.md) §streaming. The live dot transitions to `semantic.warning.default` (warm tint) and the cadence label becomes `"paused"`. Click the indicator to resume.
- Cancelling auto-refresh entirely (selecting `Off`) hides the indicator + cadence.

### Step controls

Optional caret-left / caret-right buttons beside the trigger shift the range backward / forward by the current window width:

- `Last 1 hour` + step-back → `(now - 2h, now - 1h)`.
- `(2026-05-14 14:00, 15:00)` + step-forward → `(15:00, 16:00)`.

Step controls are most useful for pre/post incident comparison and for browsing historical data at fixed window-width. They are *not* shown by default — opt-in per panel because they consume horizontal space in the toolbar. When shown, they always step by *current width*, not a fixed amount.

### Time zone

UTC is the default fleet-wide ([patterns/plots.md](./plots.md) §axes). Apps may render in local time via app-level config; when they do:

- Quick-pick calendar entries (Today / Yesterday / This week) respect the local timezone for boundaries.
- The display always shows the timezone abbreviation (`CEST`, `EST`, `JST`).
- The wire format remains `i64` nanoseconds since Unix epoch — timezone is *display*, not storage.

Daylight-saving transitions are handled at the display layer; the wire-format timestamps are unambiguous (epoch nanoseconds have no timezone).

### Integration with linked plots

Per [patterns/plots.md](./plots.md) §multi-pane-composition, the picker is the source of truth for the X axis of every plot linked to its `link_axis` group. Two synchronisation modes:

- **Unidirectional (default)**: picker drives plots. User interactions on the picker (quick-pick selection, custom range Apply) immediately update every linked plot. Plot pan / zoom does *not* propagate back.
- **Bidirectional (opt-in)**: pan / zoom on any linked plot also updates the picker. The picker display transitions to "Custom" with the resolved range; the picker becomes a *view* of whatever the user has converged on. This is the Grafana convention.

Bidirectional is recommended for investigation surfaces (Grafana-replacement panels); unidirectional for read-only display dashboards.

### Density behavior

| Element | Tight | Standard | Roomy |
|---|---|---|---|
| Trigger button padding | `Padding.Tight` | `Padding.Default` | `Padding.Outer` |
| Quick-pick list row height | Body line-height + `Padding.Hair` | + `Padding.Inner` | + `Padding.Tight` |
| Custom input field height | `Body` height + `Padding.Inner` | + `Padding.Tight` | + `Padding.Default` |
| Gap between trigger + step controls | `Gap.Inline` (4/6/8 px) | per density | per density |
| Dropdown padding | `Padding.Outer` | `Padding.Loose` | `Padding.Loose` |

Active density preset scales all of the above. The picker layout itself does not override density.

### Performance

The picker is UI; performance concerns are at the *data fetch* layer it triggers, not the picker itself. The picker:

- Re-renders only on user interaction or refresh tick (no per-frame work).
- Emits `range_changed(start_ns, end_ns, refresh_s)` events to subscribers.
- Holds no state beyond the current `(start_ns, end_ns, refresh_s)` triple plus dropdown-open boolean.

The auto-refresh implementation (timer that fires every `refresh_s`, triggers data fetch, dispatches re-render) is per-app and lives outside the picker pattern.

## Invariants

- Wire format is `(i64 start_ns, i64 end_ns, u16 refresh_s)` per [ADR-0016](../../adr/0016-imzero2-time-range-picker.md). Apps that need finer precision than seconds for refresh, or wider than ±292 years for timestamps, escalate via Tier 3 (effectively never).
- `start_ns < end_ns` strictly; equal-time ranges are rejected.
- UTC is the default display timezone; non-UTC display always shows the timezone abbreviation.
- The picker is the source of truth for linked plot X axes. Plots set their own X range without consulting the picker only when *not* linked (rare; a Tier 2 finding when used inside a dashboard).
- Quick-pick canonical set is fleet-wide. Apps add rare ranges via custom inputs, not preset extensions.
- Refresh-interval canonical set is fleet-wide. Auto-refresh below 5 s is reserved for live diagnostics; the picker UI does not advertise sub-5s cadences in the default set.
- Live indicator animation respects reduced-motion (`Motion.*` → 0 with the indicator falling back to a static filled dot).
- User pan / zoom on a linked plot pauses auto-refresh automatically; resume requires explicit click.
- Calendar-aligned quick-picks (Today / Yesterday / This week) respect the active display timezone for boundaries.
- The arrow separator (`→` U+2192) is used in absolute range displays.
- Custom range inputs accept keyboard entry; pointer-only entry is an anti-pattern.
- Step controls (when shown) shift by *current* width, not a fixed amount.

## Trade-offs

- **Quick-pick count.** 17 presets cover the common cases; more would overflow the dropdown and dilute the canonical set. Apps with rare needs use custom inputs.
- **Unidirectional vs bidirectional plot sync.** Unidirectional is simpler and more predictable; bidirectional matches the Grafana convention and is more flexible for investigation. Default: unidirectional for read-only dashboards; bidirectional for investigation surfaces; per-dashboard config.
- **Calendar widget vs text-only input.** Calendar widgets are familiar to non-technical users; text input is faster for keyboard-first operators (and IDS skews operator-heavy). Default: text input with relative-shorthand parsing (`now`, `now-1h`); calendar widget as opt-in per app.
- **Auto-refresh defaults.** "Off" by default avoids surprise auto-refresh; "10 s on" by default matches Grafana convention. Default for IDS: `Off` — explicit opt-in to live mode. Operator-trained users can change per-dashboard.
- **Display format: relative vs absolute.** Relative is easier to scan; absolute is unambiguous. Default: relative for quick-pick-active; absolute for custom-range-active. Both show timezone when non-UTC.
- **Step-control visibility.** Always-on adds horizontal weight to the toolbar; opt-in keeps the toolbar quiet. Default: opt-in; show on dashboards explicitly built for pre/post comparison.
- **Sub-second precision in display.** Most operators never see ranges < 1 minute; sub-second display adds clutter without value. Default: hide sub-second precision unless the range is < 1 minute, then show milliseconds.

## Anti-patterns

- Missing timezone indication on non-UTC display.
- Auto-refresh cadence shorter than panel render p95 (causes thrash).
- Quick-pick list with > 25 presets (cognitive overload — use custom range).
- Custom range input that requires pointer interaction (fails keyboard accessibility).
- Live indicator that ignores reduced-motion preference.
- Auto-applying custom range as the user types (flicker; wait for Apply).
- Each plot in a dashboard picking its own X range without a shared picker (every plot disagrees about "now").
- Step controls that shift by a fixed amount rather than current window width.
- Picker that doesn't pause auto-refresh on user pan / zoom of linked plots.
- Showing `0` as a refresh-interval value in the dropdown ("Off" is the label; `0` is the wire value).
- Picker positioned away from the plot(s) it drives (visual disconnect; operators lose the affordance).
- Allowing `start >= end` (validation must catch).
- Hand-rolled timestamp parser (use a dedicated library; Grafana's `now-1h` syntax is non-trivial).

## Further reading

- [ADR-0016 — ImZero2 time-range picker](../../adr/0016-imzero2-time-range-picker.md) — technical contract; wire format; Grafana lineage.
- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; Tier 2 rubric V5 (legend completeness — time-range visibility is part of plot legend completeness) and V6 (iconography consistency) apply.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — `Body.Mono.Numeric` for ISO timestamps; `Body` for relative ranges; `Caption.Mono` for cadence.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — `semantic.success.default` for active live indicator; `semantic.warning.default` for paused; semantic palette §SD2.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — dropdown transitions use `Motion.Quick`; live indicator pulse uses `Motion.Standard` opacity oscillation; reduced-motion plumbing.
- [patterns/plots.md](./plots.md) — linked plot X axis follows the picker; pan / zoom pauses auto-refresh; streaming-data conventions.
- [patterns/status-and-legends.md](./status-and-legends.md) — live indicator anatomy; semantic palette for status colors.
- [patterns/iconography.md](./iconography.md) — calendar / clock / refresh / live-dot / caret codepoints.
- [patterns/empty-states.md](./empty-states.md) — "No data in this time range" is the No-data-for-context variant; CTA is "Expand range" via this picker.
- `boxer/public/math/numerical/timeticks` ([`reference_boxer_timeticks`]) — calendar-aware tick formatting used by plots driven by the picker.
- Grafana v7.5.17 time-range picker — design lineage; per [`project_time_range_picker_port`].
