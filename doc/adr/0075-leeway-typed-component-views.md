---
type: adr
status: proposed
date: 2026-06-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0075: Typed per-component views for leeway records

## Context

The single-record renderer that exists today, `leewaywidgets.Table2CardEmitter`, is
**fully generic**: it implements `streamreadaccess.SinkI` and renders *any* leeway
table by walking sections → rows into one unified `egui_extras` table
(`[entity?] · section · primary · secondary · values`), with click-to-collapse
section-header bars. It knows nothing about what a section *means*. The `play`
detail pane drives it on row-click; non-leeway schemas fall back to a prefix-based
key/value layout (`apps/play/play_detail.go`).

We want the **specific, semantic complement**: recognize that a section (or a bundle
of sections) *is* a known component, decode it into a concrete Go struct, and hand
that typed value to a **bespoke widget** built for that component. Each per-component
widget is, in ECS terms, a *system that draws* — a renderer matched to entities that
carry a given component set. This realizes the ECS-for-data-processing thesis (see
`anchor/ecsdemo/EXPLANATION.md`) in the UI: an entity's **archetype** — the set of
components it carries — becomes legible, per entity, across a heterogeneous table.

The engine is the pair `ecsdemo` already names: **detection** (`Presence` /
`ArchetypePresence`) and **unmarshalling** (`Unmarshal[T]`). Those are `encoding/json/v2`
(stage-1). Two forks were settled in design dialogue:

- **Provenance — direct leeway typed read** (not a canonical-JSON detour). This *is*
  ecsdemo stage-2: detect + decode straight off the leeway read side.
- **Seed set — drone components first, fact-components later.** Build and verify against
  the ecsdemo drone components; keep the registry open so real keelson/spinnaker
  fact-components register afterward without touching the dispatcher.

**What already exists (verified):**

- `marshallreflect.Unmarshal[T any](args UnmarshalArgs, out *[]T, lookup LookupI) error`
  (`leeway/marshall/go/marshallreflect/unmarshal.go`) decodes a `lw:`-tagged DTO per
  entity from Arrow, via callbacks (`PlainCol`, `SectionAttrs`, `SectionMembs`) that
  hand it generated ReadAccess (RA) readers. `lw:"section,canonicalType[,modifier]"`
  tags map struct fields onto sections; `lw:",id"` / `,naturalKey` mark plain columns.
- Generated RA readers expose `…Attributes.GetNumberOfAttributes(entityIdx) int64`
  (`anchor/card_anchor_ra.out.go`) — a per-entity, per-section **population** count.
- The anchor example schema **already declares** the sections the drone components
  need (`anchor/card_anchor_schema.go`): `symbol`, `u64Array`,
  `geoPoint{pointLat:f32, pointLng:f32, h3:u64}`, `timeRange{beginIncl:z64, endExcl:z64}`,
  `symbolArray`. A `card_anchor_data_drone.go` dataset already populates them. So the
  drone components map onto **real, existing** leeway sections:
  `Identity{Status}→symbol`, `Battery{Charge}→u64Array`,
  `Located{Lat,Lng,Cell}→geoPoint`, `Tasked{Window}→timeRange` (+ tags `→symbolArray`).

**The gaps** (what this ADR's work fills): (1) no leeway-level **Presence** helper
— nothing answers "does this entity carry a `geoPoint` section with the expected
canonical types?" without a full decode; (2) no typed per-component DTOs, widgets, or
registry; (3) `play` reads Arrow flatly (`play_format.go:formatCell`) with no typed
decode.

## Design space (QOC)

**Question.** How do typed per-component widgets attach to a leeway record?

**Options.**

- **O1 — Open per-component renderer registry.** Each entry owns its required sections,
  a `Detect` predicate, a typed decode, and a widget. A dispatcher runs detection,
  renders each present component with its widget, and routes the unclaimed remainder to
  `Table2CardEmitter`.
- **O2 — Single typed DTO (closed).** One `lw:`-tagged struct with optional
  `*Component` fields (the `ecsdemo.Entity` shape), decoded once via `marshallreflect`;
  non-nil fields dispatch to widgets.
- **O3 — Enrich `Table2CardEmitter`.** Add a per-section typed "enrichment" hook that
  swaps a generic section's rows for a bespoke widget when the section is recognized.

**Criteria.**

- **C1 — Extensibility** to fact-components (register without touching the dispatcher).
- **C2 — Faithfulness** to the explicit ask: ECS *detection* + *unmarshalling* per component.
- **C3 — Reuse** of the mature `marshallreflect` / RA typed read.
- **C4 — Scope** / complexity for the first cut.
- **C5 — Composability** with the generic `Table2CardEmitter` fallback and the
  collapsible single-record report shell.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −− | −  |
| C2 | ++ | +  | −  |
| C3 | +  | ++ | +  |
| C4 | +  | ++ | −  |
| C5 | ++ | −  | +  |

O1 wins on the criteria that motivated the request (extensibility, faithfulness,
composability); O2's decode-once simplicity is borrowed *inside* a renderer for
multi-section components rather than adopted as the whole architecture.

## Decision

We will build an **open registry of typed per-component renderers** in a new
imzero2 package (`leewaywidgets/componentview`), with the headless detection
primitive living in leeway core so it stays reusable and testable without a GUI.

A renderer is one registered "system that draws":

```go
type ComponentRendererI interface {
    Kind() ComponentKindE                  // which component
    RequiredSections() []SectionSpec       // section name + expected canonical types
    Detect(rec RecordView) PresenceE       // Absent | Approximate | Exact
    Render(rec RecordView) error           // typed unmarshal + bespoke widget
}
```

- **Detection is two-level**, mirroring ADR-0066's Presence/Validator/Projection and
  `ecsdemo`'s json `Presence`/`Validate`, lifted from json members to leeway canonical
  types: *approximate* = the schema declares the required sections with compatible
  canonical types (a `common.TableDesc` shape match) **and** they are populated for the
  entity (`GetNumberOfAttributes(entityIdx) > 0`); *exact* = a typed unmarshal
  succeeds. Approximate is a necessary precondition for exact, never the reverse — the
  same one-sided guarantee `ecsdemo` documents. The leeway-side approximate check is the
  small reusable helper we build (not codegen, for the first cut).
- **Typed unmarshal** is `marshallreflect.Unmarshal[T]` over an `lw:`-tagged component
  struct, leaning on the generated RA readers it already composes. A multi-section
  component (e.g. `Located`) decodes through one small struct; a scalar component may read
  its RA accessor directly.
- **Struct unification is sequenced behind a codec feature.** `marshallreflect` decodes
  **flat** DTOs only: a multi-sub-column section is two flat fields with a
  `section:subColumn` selector (`lw:"window,timeRange:beginIncl"` / `:endExcl`), and there
  is no nested-struct support. So *truly* unifying `ecsdemo`'s **nested** components
  (`Tasked{TimeRange}`, `Located{GeoPoint}`) onto one `json:`+`lw:` definition needs a new
  `marshallreflect` nested-struct capability; flattening `ecsdemo` instead would regress
  its deliberate nested composition model. The light version therefore decodes via a
  **flat leeway DTO** — the existing `anchor/codecdemo` `DroneMission`, extended with
  `Tasked`'s `timeRange`/`symbolArray` fields — leaving `ecsdemo` untouched. Full
  unification (nested components shared by both representations) is deferred behind the
  codec feature.
- **Dispatch** renders every detected component with its widget inside the single-record
  **collapsible report shell** (one foldable panel per component; collapse state keyed by
  `ComponentKindE` so it survives clicking through records). Sections claimed by **no**
  registered renderer fall through to `Table2CardEmitter` — *specific where the component
  is known, generic everywhere else*. `Table2CardEmitter` is unchanged.

**Seed renderers** are the drone components against the existing anchor schema/data,
reusing widgets where they exist. The first cut — the **light version** — registers only
the three that reuse an existing widget; `Located` (a proper map) is **deferred** to a
follow-up, so its `geoPoint` section renders through the generic `Table2CardEmitter`
fallback meanwhile:

| Component | leeway section(s) | widget | status |
|---|---|---|---|
| `Identity{Status}` | `symbol` | tone/status badge (ADR-0031) | reuse |
| `Battery{Charge}` | `u64Array` | radial gauge (ADR-0068) | reuse |
| `Tasked{Window, Tags}` | `timeRange` + `symbolArray` | timeline band + tag chips | reuse (`play` timeline) |
| `Located{Lat, Lng, Cell}` | `geoPoint{pointLat, pointLng, h3}` | proper map (basemap + point / H3 cell) | **deferred** — follow-up; renders via generic fallback for now |

**Consumers** are the `play` detail pane (a toggle: generic `Table2CardEmitter` ⇄ typed
component view of the selected record) and a registered demo in the leewaywidgets tour for
screenshot capture.

## Alternatives

- **O2 (single closed DTO).** Simplest and one decode call, but a closed struct
  contradicts the settled "fact-components later" requirement — a new component means
  editing the DTO, and there is no clean per-section remainder to route to the generic
  renderer. Adopted only as an in-renderer decode tactic.
- **O3 (enrich `Table2CardEmitter`).** Entangles a typed, semantic concern with a
  generic, structural renderer tuned for cross-section uniformity; detection/unmarshal
  bolt awkwardly onto its row model. Rejected to keep the two renderers each tuned to
  their job.
- **Canonical-JSON provenance** (leeway → entity JSON → `ecsdemo` `Presence`/`Unmarshal`).
  Reuses existing pieces and dodges the read-side, but was rejected in dialogue in favour
  of the direct leeway typed read, which advances ecsdemo stage-2 and avoids a json
  round-trip per record.
- **Codegen'd Presence** (emit `HasSection` per RA struct). Deferred: the
  `GetNumberOfAttributes` population check plus a `TableDesc` shape match is enough for the
  first cut; ADR-0066 is the home for a generated prefilter if it becomes a hot path.

## Consequences

### Positive

- Realizes ecsdemo **stage-2 detection over real data** with near-zero schema work — the
  drone sections and dataset already exist in anchor.
- The ECS **archetype becomes visible per entity** across a heterogeneous leeway table:
  detection identifies which components each entity carries regardless of which dataset
  produced it.
- The light version reuses the gauge, timeline, and tone-badge widgets — **no net-new
  widget** — so scope-one is wiring (registry + detection + report shell + a `play` toggle)
  over existing parts; the `Located` map is the only deferred piece.
- The registry is **open** — fact-components register their own `(detect, decode, render)`
  triple later without touching the dispatcher — and **complements** rather than replaces
  `Table2CardEmitter`, which remains the generic fallback.

### Negative

- `marshallreflect` decode is per-entity reflection — fine for a single selected record in
  a detail pane, not for bulk rendering. (The generic table viewer stays on direct Arrow.)
- The `Located` **map** is **deferred** out of scope-one: it is the largest single piece
  and carries an unresolved basemap-provenance choice (offline-bundled raster / online tiles
  / projected-vector) with offline-capture and sovereignty implications. Until it lands,
  `geoPoint` renders through the generic `Table2CardEmitter` fallback — which the
  architecture already routes there, so the deferral costs nothing structurally.
- The leeway-level approximate Presence is a hand-written helper until/unless ADR-0066
  codegen lands.
- Full struct unification is **deferred**: `marshallreflect` is flat-only, so the nested
  `ecsdemo` components cannot carry `lw:` tags until a nested-struct codec feature lands.
  The light version uses the flat `codecdemo` DTO meanwhile, so two drone definitions
  (nested-json `ecsdemo` + flat-leeway `codecdemo`) coexist until then.

### Neutral

- The collapsible per-component report shell (from the prior design dialogue) is the host;
  if it proves load-bearing it deserves its own note, but it is not the subject of this ADR.
- Long-term, the registry could subsume `Table2CardEmitter` as its "unknown remainder"
  branch rather than calling it as a sibling; left open.

## Status

Proposed — awaiting review by the leeway / imzero2 owner. Scope-one is the **light
version**: the three reuse-widget components + the registry + the detection helper + the
report shell + a `play` toggle. The `Located` **map** and its basemap-provenance choice
(offline-bundled raster / online tiles / projected-vector) are **deferred** to a follow-up.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- ECS background and the json stage-1 detect/unmarshal: `anchor/ecsdemo/EXPLANATION.md`,
  `anchor/ecsdemo/ecsdemo_json.go` (`Presence`, `Validate`, `Unmarshal`, `ArchetypePresence`).
- [ADR-0066: leeway DQL ClickHouse read-back generator](0066-leeway-dql-clickhouse-readback-generator.md)
  — the Presence / Validator / Projection trichotomy detection mirrors.
- [ADR-0070: leeway entity assembly](0070-leeway-entity-assembly.md),
  [ADR-0071: leeway value and emission](0071-leeway-value-and-emission.md),
  [ADR-0072: leeway membership carriage](0072-leeway-membership-carriage.md),
  [ADR-0073: leeway membership role](0073-leeway-membership-role.md).
- [ADR-0074: leeway marshall package layout](0074-leeway-marshall-package-layout.md)
  — where `marshallreflect` and the RA readers live.
- The generic complement: `leewaywidgets/table2_emitter.go` (`Table2CardEmitter`);
  the read protocol: `doc/skills/leeway-streamreadaccess/SKILLS.md`.
- Schema/data home for the seed: `anchor/card_anchor_schema.go`,
  `anchor/card_anchor_data_drone.go`.
