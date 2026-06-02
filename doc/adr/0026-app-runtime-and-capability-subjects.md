---
type: adr
status: accepted
date: 2026-05-11
reviewed-by: "@spx"
reviewed-date: 2026-05-11
---

# ADR-0026: App runtime and capability subjects

## Context

The pebble2impl monolith hosts a growing collection of interactive programs — `play` (SQL playground), `imztop` (resource monitor, [ADR-0020](./0020-imzero2-imztop-resource-monitor.md)), the regex explorer, the Hacker News explorer, the leewaywidgets tour, the widgets showcase (under [ADR-0057](0057-demo-registry-and-drivers.md)) — plus a parallel collection of headless CLI subcommands (`kafka`, `gov`, `badger`, `funccharacterize`, the `spinnaker` tree, …). Three structural deficiencies have accumulated:

- **No "app" type.** Graphical programs are wired as numbered render closures (`appCode` 1–7) in [`public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go). `--launch a001,a005` accepts a comma list but resolves to sequential rendering in one CentralPanel, not coexistence. The only registry — `public/thestack/imzero2/egui2/demo/apps/registry/registry.go` — is consumed only by the widgets showcase. Adding an app means editing the switch.
- **No capability mediation.** Every program reaches `os.Open`, `clickhouse.OpenDB`, `kafka.NewClient`, etc. directly. There is no broker between an app's intent and the system resource, no audit of which app touched what, no consistency in how a "file open" is requested. The file picker at `public/thestack/imzero2/egui2/widgets/filepicker/` is already backed by `io/fs.FS` — the substrate for a capability handle exists, but nothing uses it that way.
- **NATS is forthcoming and will be the dominant inter-component transport.** Per stakeholder direction, NATS will serve the role HTTP serves in a browser tab — the ambient cold-path transport for everything that is not an egui draw call. The runtime architecture must absorb NATS coherently when it lands, rather than retrofit later.

Adjacent forces:

- **One OS viewport is invariant.** The remote-desktop / video-streaming direction (`project_imzero1_video_prior_art`) requires the entire UI to render as one frame surface. Multi-window app coexistence is foreclosed; multi-pane coexistence inside a single eframe viewport is the only option.
- **ClickHouse + leeway is the persistence layer.** State is not opaque blobs; it is leeway-encoded rows in a shared CH instance. Data-centricity (apps' state queryable from other apps with the right permissions) is a feature, not an emergent property.
- **Threat model is hygiene, not security.** Apps are first-party. The capability system exists to make resource access *visible*, *consistent*, and *auditable* — not to enforce a boundary against malicious code. No process isolation, no syscall sandboxing.
- **Long-term CLI/GUI unification is desired but not immediate.** The `Manifest` schema must accommodate a `Headless` surface from day one; the actual migration of `cli.Command`s lives in a later phase.

Invariants the design must respect:

- **One OS viewport** (above).
- **ImZero2 frame discipline.** WidgetIdStack must be re-prepared between consuming FFFI2 calls; FFFI `r9_*` register bindings reset every Sync and must be re-registered per frame; `AllocateUiAtRect` is absolute and breaks Vertical/Horizontal flow; CentralPanel is required for non-floating UI; continuous repaint is unconditional.
- **CGO-free Go build.**
- **Incremental migration.** Seven graphical apps and ~20 CLI subcommands cannot move atomically; old and new paths must coexist during the transition (as in [ADR-0057](0057-demo-registry-and-drivers.md)).

## Design space (QOC)

**Question.** How should pebble2impl organise its launchable programs so that (a) multiple apps can coexist inside one viewport, (b) every external resource access is mediated by a uniform broker, and (c) the architecture accommodates NATS-as-universal-transport without later restructuring?

**Options.**

- **O1 — Status quo + incremental hardening.** Keep the appCode switch. Add audit at each `os.Open`/`clickhouse.OpenDB` callsite by hand. Add a tab system inside the carousel for coexistence. Capabilities remain implicit.
- **O2 — `AppI` + typed capability handles.** Introduce `AppI`/`Manifest`/`Registry`. Capabilities are typed Go interfaces (`FsReadCapI`, `ClickhouseQueryCapI`, `KafkaProducerCapI`, …). NATS, when it lands, is one more typed cap kind alongside the others.
- **O3 — `AppI` + capability-as-subject (chosen).** Introduce `AppI`/`Manifest`/`Registry`. Capabilities are NATS subject filters; the broker issues, audits, and (later) revokes them. Pre-NATS, an in-proc bus with the same subject semantics serves as a drop-in shim. egui_dock provides the multi-app surface. State is a single polymorphic CH+leeway facts table.
- **O4 — Process isolation per app.** Each app is a Go subprocess driving FFFI2 into the same Rust HMI, multiplexed by a runtime kernel. Capabilities are syscalls over a constrained IPC dialect; the kernel is the only thing that can reach the outside world.

**Criteria.**

- **C1 — Hygiene & auditability.** Can every external resource access be observed in a uniform record?
- **C2 — Coexistence in one viewport.** Can ≥2 apps render simultaneously without one capturing global state (panel scope, ID stack, FFFI registers)?
- **C3 — NATS readiness.** When NATS lands, is the swap from in-proc shim to NATS authoritative, or does it require restructuring application-facing APIs?
- **C4 — CLI/GUI unification readiness.** Does the architecture leave room to lift `cli.Command`s into the same shape later, without rewriting once they get there?
- **C5 — Migration cost.** Effort to port the 7 existing graphical apps without breaking `--launch` or the screenshot tour mid-migration.
- **C6 — Data-centricity.** Does app state participate in the leeway-on-CH schema such that other apps (with permission) can read it as ordinary data?
- **C7 — Avoidance of misleading security framing.** Does the architecture honestly represent that boundaries are hygiene, not enforcement?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ |
| C2 | −  | +  | ++ | ++ |
| C3 | −− | −  | ++ | +  |
| C4 | −− | +  | ++ | +  |
| C5 | ++ | +  | +  | −− |
| C6 | −  | −  | ++ | +  |
| C7 | +  | +  | ++ | −  |

O3 dominates O2 on every axis except migration cost, where the two tie. The differentiator is C3: typed capability handles (O2) commit to a Go-shaped API that does not naturally unify with NATS subject patterns; when NATS lands, the handles either become thin wrappers around subject filters (in which case the typed surface is dead weight) or stay alongside (in which case there are two parallel resource APIs forever). O3 collapses to a single shape that the in-proc bus and NATS both serve. O4 has the same architectural posture as O3 but spends a large engineering budget on a security boundary the threat model does not require (C7 negative — claims enforcement we are not committing to). O1 is the current trajectory and worsens on C1/C3/C4/C6 with each new app added.

## Decision

We will introduce a first-class app runtime with three concrete pieces, and we will mediate every external resource access through a capability broker whose capability tokens are NATS subject filters.

1. **`AppI` interface and `Registry`** (Go package `public/keelson/runtime/app/`). Apps register a static `Manifest` and implement `Mount`/`Frame`/`Unmount`. The numeric `appCode` switch and the screenshot-tour-specific `apps/registry/` are replaced by this single registry. The Hosts (`DockHost`, `InteractiveHost`, `ScreenshotHost`, eventually `CliHost`) consume the registry.
2. **Capabilities are NATS subject filters.** A capability is a set of subject patterns the holder may publish to and/or subscribe from. Every resource — filesystem reads, ClickHouse queries, Kafka produce/consume, inter-app events, persistence — is reached via a subject. The runtime hosts a capability broker that issues, records, and (later) revokes these filters.
3. **One polymorphic facts table on ClickHouse + leeway** is the runtime's state layer. App state, capability grants, and audit records are leeway-encoded rows discriminated by a `kind` column. The single-table choice (over per-app tables) optimises for the data-centricity property — every fact is queryable from `play` without schema sprawl.

Pre-NATS, an in-proc bus implementing the same subject-publish/subscribe semantics serves as the broker's transport. Post-NATS, the bus is replaced by a NATS client per app with NATS-native subject permissions; subject names, capability shape, and the AppI-facing API are unchanged across the swap.

The threat model is **hygiene, not security**. Capability discipline is enforced by lint guard and code review, not by memory or process isolation. The ADR is explicit about this: no claim of sandbox, no isolation theatre.

## Subsidiary design decisions

### SD1 — Manifest schema

The minimum static description of an app:

```go
// public/keelson/runtime/app/manifest.go
type Manifest struct {
    Id            AppIdT          // dotted, e.g. "org.pebble2.play"
    Version       string          // semver
    Display       string          // human-readable label
    Title         string          // window title; falls back to Display
    Icon          string          // optional Unicode glyph prefix on Title

    Surface       SurfaceE        // SurfaceHeadless | SurfaceWindowed
    SurfaceHints  SurfaceHints    // initial dock affinity, min/preferred size

    Caps             []SubjectFilter // declared up-front; broker grants on Mount
    BackgroundTickHz uint8           // 0 = no tick when unfocused

    PersistedKeys []string        // auto-managed cold state keys (SD6)
}

type SubjectFilter struct {
    Pattern   string         // e.g. "ch.query.boxer", "app.play.event.>"
    Reason    string         // shown in cap-grant prompt
    Direction CapDirectionE  // CapDirectionPub | CapDirectionSub | CapDirectionBoth
    Sticky    bool           // remember grant across sessions
}
```

`SurfaceHeadless` is reserved from day one for the eventual CLI unification (`SD13`); the windowhost ignores headless manifests (no window is created for them).

### SD2 — `AppI` interface and Registry

```go
type AppI interface {
    Manifest() (m Manifest)
    Mount(ctx MountContext) (err error)
    Frame(ctx FrameContext) (err error)  // no-op for Headless
    Unmount(ctx MountContext) (err error)
}

var _ AppI = (*PlayApp)(nil)  // compile-time check at every concrete app

type MountContext interface {
    AppId() AppIdT
    Log() zerolog.Logger          // pre-tagged with app_id
    Storage() StorageI            // CH+leeway-backed cold state
    Bus() BusI                    // pub/sub on the granted subject filter
    Cancel() <-chan struct{}
}

type FrameContext interface {
    MountContext
    Egui() *egui2.Context         // scoped to allocated rect; ID stack pre-prepared
}
```

Apps register via `init()`, mirroring the screenshot-tour pattern (`SD6` of [ADR-0057](0057-demo-registry-and-drivers.md)). Two registration paths cover singleton vs multi-instance lifecycles:

- **`Register(a AppI)`** — singleton. Every `Open(id)` returns the same `a`. Appropriate for apps that own per-package state.
- **`RegisterFactory(m Manifest, ctor AppCtor)`** — factory. Each `Open(id)` invokes the ctor for a fresh `AppI`. Required when two windows of the same app must run independently; the app must keep state on the struct rather than in package-level vars.

`Open(id) (AppI, error)` is the host-side dispatch; `LookupManifest(id)` and `AllManifests()` enumerate metadata without instantiating. Registration enforces `AppId` uniqueness; iteration is sorted by display name.

### SD3 — Subject taxonomy

Subjects are the runtime's public API surface. The taxonomy:

```
runtime.app.{id}.{event}            lifecycle: mount, focus, suspend, unmount
runtime.persist.{id}.{key}.{op}     state read/write/delete, CH+leeway-backed
runtime.cap.request                 ask for a new cap (broker prompts user)
runtime.cap.revoke                  voluntary release
runtime.audit.{id}                  read-only audit stream for own app

fs.dialog.read                      open "pick file" dialog, returns handle subject
fs.dialog.write                     "save as" dialog
fs.dialog.bundle                    "pick folder" dialog
fs.handle.{uuid}.{op}               read/write/stat/walk/close on granted handle

ch.query.{db}                       SQL execution, single result set
ch.stream.{db}                      SQL execution, streaming row iterator
ch.schema.{db}                      schema introspection (read-only)

kafka.produce.{topic}               produce
kafka.consume.{group}.{topic}       consume

app.{id}.event.{name}               app-published events (pub/sub)
app.{id}.request.{name}             app-published requests (req/reply pattern)
```

Wildcards follow NATS conventions (`>` matches the rest of the subject; `*` matches one token). A manifest such as

```go
Caps: []SubjectFilter{
    {Pattern: "fs.dialog.read",            Reason: "import CSV/Parquet",            Direction: CapDirectionPub},
    {Pattern: "fs.handle.>",               Reason: "read files the user picked",    Direction: CapDirectionPub},
    {Pattern: "ch.query.boxer",            Reason: "execute user-edited SQL",       Direction: CapDirectionPub, Sticky: true},
    {Pattern: "ch.stream.boxer",           Reason: "stream large result sets",      Direction: CapDirectionPub, Sticky: true},
    {Pattern: "ch.schema.boxer",           Reason: "table & column auto-complete",  Direction: CapDirectionPub, Sticky: true},
    {Pattern: "app.play.event.>",          Reason: "publish own events",            Direction: CapDirectionPub},
    {Pattern: "app.*.event.row_selected",  Reason: "react to selections elsewhere", Direction: CapDirectionSub},
}
```

is enough for the play app. The broker prompts on Mount for any non-sticky cap that has not been previously granted; sticky grants are looked up in the facts table (`SD6`).

### SD4 — External NATS, NKey per app

NATS runs as an external server; the monolith connects to it as a client. Each app gets its own NKey and JWT-encoded permissions matching its granted `SubjectFilter` set. The runtime mints these on first Mount and stores them (encrypted) in the facts table; rotation is on a TTL declared in the JWT.

The monolith does not launch or supervise the NATS process. Configuration is a single connection string (`NATS_URL` env or config file). For development, contributors run `nats-server -js` locally; for production, a deployment provisions it. This decision is explicit and rules out an embedded `nats-server` library dependency.

### SD5 — Pre-NATS in-proc bus shim

Until NATS arrives, `BusI` is implemented by `inprocbus.Inst` — a goroutine-safe subject-pattern router with the same semantics as NATS core (publish, subscribe, queue-group subscribe, request-reply). The bus accepts the same `SubjectFilter` permission set as NATS server-side authz; permission checks happen at publish/subscribe time and produce the same error shape NATS does (`nats.ErrPermissionViolation`).

The cap-broker subject handlers (`fs.dialog.*`, `fs.handle.*`, `ch.query.*`, etc.) are registered on the in-proc bus by the runtime at boot, behind the same interface they will present once NATS lands. The transport swap (`SD12` phase M4) replaces `inprocbus.Inst` with a `nats.Conn` per app; handler code is untouched.

### SD6 — Leeway-shaped `runtime.facts` table, modelled on `spinnaker.facts`

Runtime state, capability grants, and audit records all live in a single CH+leeway table named `runtime.facts`, modelled on the existing `spinnaker.facts` schema at `public/spinnaker/schema/spinnaker_schema.go` and emitted by `spinnaker_sql_ch.go`. A naive single-`payload`-string column is rejected because it abandons every property the leeway encoding provides — typed columnar query, dictionary compression on low-cardinality values, membership-as-ACL, co-section streaming, subset-projection for hot readers. The vocabulary used here (plain values, tagged value sections, memberships, streaming groups, co-sections) is defined in [`doc/skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md).

**Plain values** (one instance per row, identifying the fact; written by the runtime, never by an app):

- `id` (`u64`, `PlainItemTypeEntityId`) — synthetic per-fact id, delta-encoded + lightly compressed (same encoding hints as spinnaker's `id` column).
- `naturalKey` (`Y`, `PlainItemTypeEntityId`) — domain-stable identifier (a blake3 hash over the fact's salient fields) for idempotent upsert and dedupe.
- `ts` (`Z32`, `PlainItemTypeTimestamp`) — observation timestamp.
- `expiresAt` (`Z32`, `PlainItemTypeLifecycle`) — eviction trigger for grant- and audit-retention; written only for facts that age out.

**Tagged value sections** (mirror the spinnaker `"data"` streaming group so the schema is composable with the existing leeway DSL and `play`-driven introspection):

`text`, `string`, `symbol` (canonicalised low-cardinality), `blob`, `u8`–`u64`, `i8`–`i64`, `f32`, `f64`, `time`, `bool`. Each section declares membership spec `[MembershipSpecLowCardRef, MembershipSpecHighCardRef, MembershipSpecMixedLowCardRefHighCardParameters]` — the spinnaker triple — and streaming-group `"data"`.

A `foreignKey` tagged section (`MembershipSpecLowCardRef`, streaming-group `"foreignKey"`, use-aspect `AspectLinking`) carries cross-fact references — e.g., an `audit` fact's link to the `grant` fact that authorised it, or a `state`-write fact's link back to the `grant` for the persistence subject.

**Fact "kind" is a membership, not a column.** A capability grant materialises as a row whose tagged values carry the memberships `runtime.kind.grant`, `runtime.app.{appId}` (where `{appId}` is a high-card parameter on `MembershipSpecMixedLowCardRefHighCardParameters`), plus value-bearing memberships such as `runtime.subjectFilter.pattern` on the `symbol` section, `runtime.subjectFilter.direction` (enum-as-low-card-ref), and `runtime.subjectFilter.reason` on `string`. An audit fact uses `runtime.kind.audit` with `runtime.audit.requestSubject`, `runtime.audit.result`, and `runtime.audit.latencyMs` on the `i64` section. Because membership is a *set* (not exclusive), a single row can carry several kinds when semantics overlap — a state-write fact is simultaneously an audit event without duplicating storage.

**Logs are facts too (`runtime.kind.log`).** Every `zerolog` event emitted by the host or by any app is captured as a row tagged `runtime.kind.log`, carrying the same `runtime.app.{appId}` mixed membership as grants and audits plus a stable log-envelope vocabulary: `runtime.log.level` (low-card-ref on `symbol`), `runtime.log.message` (low-card-ref on `string`), `runtime.log.caller` and `runtime.log.service` (low-card-ref on `symbol`), `runtime.log.error` (low-card-ref on `string`), `runtime.log.stack` (low-card-ref on `text`). Arbitrary user-supplied context fields (`.Str(k,v)`, `.Int`, `.Float`, `.Bool`, …) become per-field `runtime.log.field` mixed-memberships whose high-card parameter is the field NAME and whose value lands in the typed section matching the field's CBOR-decoded Go type — preserving columnar query advantages over the entire log corpus, not just the envelope. The bridge from zerolog's wire format to runtime.facts lives in `public/keelson/runtime/logbridge/`; with the `binary_log` build tag (already on the project tag list) zerolog emits CBOR maps that the bridge decodes into typed `LogRow`s, queues in a fixed ring, and ships through `factsstore.FactsStoreI.WriteLog`. This makes `runtime.kind.log` the fourth peer of grant/audit/state under one query surface — operators no longer choose between "tail the log" and "tail the facts": `SELECT … WHERE has(symbol.lr, MembKindLog.id)` answers both.

**`LoadRuntimeFactsMapping`.** The runtime ships a `LoadRuntimeFactsMapping(manip common.TableManipulatorFluidI)` constructor mirroring spinnaker's `LoadSourceCodeMapping`. DDL emission goes through the existing `boxer/public/semistructured/leeway/ddl/clickhouse` code generator; no bespoke `CREATE TABLE` SQL. Membership identifiers (the `runtime.kind.*`, `runtime.app.*`, `runtime.subjectFilter.*` vocabulary) are declared as compile-time constants in `public/keelson/runtime/app/factsmemberships.go` so the publisher and consumer agree by reference, not by string.

**Why one table, not three.** Per-kind tables (state / grants / audit) impose a schema-migration discipline on every new app or new audit field; the leeway membership model collapses the discriminator into the existing tagged columns at zero schema cost. Cross-kind analytics ("every grant that has had an audit event in the last hour") become a normal join on `naturalKey` or `foreignKey` rather than a `UNION` over heterogeneous schemas. Hot-path readers that prefer a denormalised view materialise it over `runtime.facts`.

**Why not extend `spinnaker.facts` directly.** Considered. Rejected because the runtime's facts are operational telemetry, not source-code observability: retention policies diverge (audit ages out at 90 days; spinnaker history is long-lived), ACLs diverge (a reader authorised for spinnaker should not transitively see grant records), and the membership vocabularies do not overlap. Two tables, same shape, different lifecycles — `play` can `UNION ALL` across them when a query genuinely spans both.

### SD7 — Capability broker; file dialog as Powerbox

The broker is a runtime-internal subject handler bound to `runtime.cap.request`. On request, it:

1. Consults the facts table for a sticky grant matching `(app_id, subject_filter)`.
2. If absent, renders a grant prompt (`egui::Window` in the dock host's overlay layer) showing app identity, the requested subject pattern, the human-readable Reason, and a checkbox for "remember across sessions".
3. On approve, writes a `kind='grant'` row to the facts table, augments the app's bus permissions (or NATS server-side ACL post-M4), and replies on the request inbox.
4. On deny, replies with `nats.ErrPermissionViolation`-shaped error.

The **file picker is the Powerbox** for `fs.*` grants — the user-facing UI by which an app's "I want to read a file" request becomes a concrete subject permission. The picker's selection produces a fresh `fs.handle.{uuid}.>` subject set, and the runtime maps subsequent `fs.handle.{uuid}.read` requests onto the actual `os.Open`. The app sees no path. (The picker's existing `io/fs.FS` backing makes the implementation a thin adapter.)

Pattern reference: macOS sandbox extensions / Sandstorm.io's Powerbox / FreeBSD Capsicum file descriptors. The semantics are well-established; the novelty here is that *every* resource (not just files) is granted this way.

### SD8 — Host owns the window; one window per app

`runtime/windowhost.Inst` is the multi-app interactive host. Each `Manifest.Surface == SurfaceWindowed` app renders inside a runtime-created `egui::Window` (movable, resizable, titled with `Manifest.WindowTitle()`). Apps must not call `c.Window(...)` or `c.PanelCentral()` from their `Frame()` — the host has already wrapped the rendering scope; apps that need top-level panels use the `*Inside` variants so they compose inside the runtime-owned window. The relationship between an app and its window is **1:1**; opening the same `AppId` twice yields two windows with monotonically allocated keys (never reused, so egui's per-widget `Memory` keys do not alias).

Window-level placement (floating vs maximised) is host's call, not app's. Window identity is derived from `ids.PrepareStr("window-<key>")`; egui's built-in `Memory` keys per-widget Window position / size / collapsed state by that id, so layout persists across frames for free.

The screenshot-tour path keeps the legacy `adaptToRenderer` driver that bypasses the host for visual-regression determinism.

Floating dialogs (file picker, cap-grant prompt) render as `egui::Window` instances at the overlay layer above the host's window set — still inside the single OS viewport.

> The M3 host design originally placed each open app in an `egui_dock` tab inside a single `c.DockArea`; the dock model was reverted to per-app `egui::Window` after empirical UX work (see [Update 2026-05-13](#2026-05-13--m3-host-reverted-from-egui_dock-to-per-app-eguiwindow)). The Apps menu, audit-trail wiring, factory-based multi-instance dispatch, and mount/frame/unmount lifecycle were unchanged by the pivot; only the host package and the per-app rendering surface changed.

### SD9 — Per-app ID and register scoping

`Frame()` is wrapped by the host with the following discipline so apps cannot interfere with each other:

- `WidgetIdStack` is pre-prepared by the host before each `Frame()` call. Apps must not call `Prepare()` themselves.
- FFFI `r9_*` register bindings are partitioned: the host pre-allocates a per-app subrange (`r9_app{N}_*`) and the egui2 helpers consume the active subrange via a context-local. Apps using hardcoded register indices fail the lint guard (`SD10`).
- The host calls `RequestRepaint` based on window-visible state and `BackgroundTickHz`. Apps do not call `RequestRepaint` themselves; the runtime owns frame cadence.

### SD10 — Capability enforcement via `google/capslock`

Capability discipline is enforced at lint time by [`google/capslock`](https://github.com/google/capslock) — Google's static-analysis tool that reports the transitive set of standard-library capabilities each Go package reaches. Capslock's vocabulary is coarser than the subject taxonomy of `SD3` but catches transitive escapes that a hand-rolled import-grep would miss: an indirect dependency reaching `os/exec` is flagged even if the app's own source never imports `os/exec` directly. The constants it reports:

`CAPABILITY_FILES`, `CAPABILITY_NETWORK`, `CAPABILITY_RUNTIME`, `CAPABILITY_READ_SYSTEM_STATE`, `CAPABILITY_MODIFY_SYSTEM_STATE`, `CAPABILITY_OPERATING_SYSTEM`, `CAPABILITY_SYSTEM_CALLS`, `CAPABILITY_ARBITRARY_EXECUTION`, `CAPABILITY_CGO`, `CAPABILITY_UNANALYZED`, `CAPABILITY_UNSAFE_POINTER`, `CAPABILITY_REFLECT`, `CAPABILITY_EXEC`, `CAPABILITY_SAFE`, `CAPABILITY_UNSPECIFIED`.

**Tooling integration.** A new `scripts/ci/capslock.sh` runs

```bash
capslock -packages ./public/.../apps/... -output j
```

A thin Go wrapper at `public/app/commands/capslock/` ingests the JSON, looks up each app's `Manifest.Caps` subject filter set, and verifies that capslock-reported capabilities for the app's package map onto subject patterns the manifest actually declares. The mapping (project convention, not a capslock feature):

| capslock capability                               | Allowed only when manifest declares                                  | Notes                                                                                              |
|---------------------------------------------------|----------------------------------------------------------------------|----------------------------------------------------------------------------------------------------|
| `CAPABILITY_FILES`                                | `fs.*` subjects                                                      | Picker substrate (`SD7`) is the sole approved path.                                                |
| `CAPABILITY_NETWORK`                              | `nats.*`, `ch.*`, `kafka.*`, or explicit `net.*` subjects            | Includes DNS, HTTP, raw sockets.                                                                   |
| `CAPABILITY_READ_SYSTEM_STATE`                    | `sysmetrics.*` subjects (post-M2 for imztop)                         | Today bypass-only via `// nolint:cap-bypass`.                                                      |
| `CAPABILITY_OPERATING_SYSTEM`, `CAPABILITY_EXEC`, `CAPABILITY_ARBITRARY_EXECUTION`, `CAPABILITY_SYSTEM_CALLS` | None                                                                 | Hard fail; bypass requires manifest `LintBypass: true` + a code-owner sign-off in PR review.        |
| `CAPABILITY_CGO`, `CAPABILITY_UNSAFE_POINTER`, `CAPABILITY_REFLECT` | None                                                                 | Hard fail for app packages. FFFI2 bridge code lives outside `apps/` and is exempt by path.          |
| `CAPABILITY_MODIFY_SYSTEM_STATE`                  | None                                                                 | Hard fail.                                                                                         |
| `CAPABILITY_RUNTIME`                              | Always allowed                                                       | Goroutines, channels, etc.                                                                         |
| `CAPABILITY_SAFE`, `CAPABILITY_UNSPECIFIED`       | Always allowed                                                       | No restriction.                                                                                    |
| `CAPABILITY_UNANALYZED`                           | Investigation required                                               | Capslock could not analyse the package; treat as a build-system bug, not an allowed default.       |

**Adoption mode.** Phased: advisory in M2 (capslock output posted to PR comments, no merge gate), `compare`-mode in M3 (CI fails when a PR introduces a new capability for an app package without a corresponding manifest change), hard-fail post-M4. The companion tool `capslock-git-diff main HEAD ./public/.../apps/...` surfaces capability deltas in PR checks.

**Why capslock over a hand-rolled import linter.** A naive `import "os"` check misses indirect escapes (a dependency that calls `os.OpenFile`); capslock's call-graph analysis catches them. Google maintains capslock for production-grade Go supply-chain analysis (background at https://blog.deps.dev/capslock/), so adopting it costs less than carrying a bespoke `ast`-walking guard forever.

**Limitations.** Capslock has no native per-package allowlist mode; the JSON-output + wrapper script is the canonical integration shape. Capslock cannot reason about runtime configuration (an `os.Exec("git", …)` looks the same whether or not it's gated by a manifest cap), so the wrapper enforces the manifest cross-reference and capslock provides the raw signal. A motivated app author can hide a syscall through `unsafe.Pointer` arithmetic or `reflect.Value.Call`; the threat model (hygiene, not security) accepts this. Capslock raises the bar on accident, not on adversarial intent.

### SD11 — Frame metrics extended per app

The frame timing overlay gains per-window breakdown inside the Go-render slot: `play: 4.2 ms, imztop: 0.8 ms, regex: 0.3 ms`. `fetchFrameMetrics` is extended with a `per_app` map keyed by `AppId`.

### SD12 — Migration phasing M1 → M5

Phases are independently shippable.

**M1 — `AppI` and registry.** Introduce `app.AppI`, `app.Manifest`, `app.Registry`. Migrate the seven graphical apps from numbered render closures to `AppI` implementations. Replace `--launch a001,a005` resolution with manifest-ID resolution (`--launch org.pebble2.play,org.pebble2.imztop`). Numeric codes accepted as aliases for one release. Old screenshot-tour `registry/` is folded into `app.Registry`. No cap broker, no NATS, no behavioural change for users.

**M2 — Cap broker as in-proc subject router.** Introduce `BusI` and `inprocbus.Inst`. Bind the `fs.*` subject family first (the file picker substrate is ready). Add the polymorphic `runtime.facts` table; route `runtime.persist.{id}.{key}` reads and writes through it. Audit-record every subject request. Lint guard added in advisory mode.

**M3 — Multi-app host. Landed 2026-05-12 as dockhost, reverted to windowhost 2026-05-13.** `runtime/windowhost.Inst` replaces the M2 launcher in interactive mode (screenshot mode keeps `adaptToRenderer`'s tour driver path). Each open app renders as a top-level `c.Window` (egui::Window — movable, resizable, titled, with `Manifest.Title`/`Icon` in the title bar). Windows are opened from a top-bar "Apps ▾" menu or from `--launch` at startup. Per-window mount/frame/unmount lifecycle with monotonic non-reused window keys. Cap-grant dialog as an overlay layer is deferred (the host doesn't yet route fsbroker / capbroker overlays). See [Amendment 2026-05-13 — M3 host reverted from egui_dock to per-app egui::Window](#2026-05-13--m3-host-reverted-from-egui_dock-to-per-app-eguiwindow) for the design-change rationale.

**M4 — NATS lands.** Provision external NATS server (per `SD4`). Replace `inprocbus.Inst` with `nats.Conn` per app. Mint NKey/JWT per app from manifest's `SubjectFilter` set. Sticky grants migrate the JWT permission set, not just the in-proc filter set. `BusI` API unchanged; all subject names unchanged. The in-proc bus stays as the test transport (`testbus`) for unit tests that don't want to require a NATS server.

**M5 — CLI unification (long horizon).** Lift `cli.Command`s to `AppI{Surface: SurfaceHeadless}`. `urfave/cli` becomes a `CliHost` that resolves CLI args to a manifest, mounts the app, runs `Frame()` once (or not at all for pure side-effect apps), unmounts, exits. Headless apps gain the same cap broker, audit, and state-layer story as graphical apps. Migration is one CLI subcommand at a time.

### SD13 — `SurfaceHeadless` reserved from M1

Even though M5 is the long-horizon phase that activates `SurfaceHeadless`, the enum variant is defined and the manifest field is normative from M1. This costs nothing in M1 and guarantees that the M5 lift is mechanical (no manifest schema change at that point).

## Alternatives

- **O1 (status quo).** Rejected: every axis except migration cost worsens with each app added; capability discipline becomes harder, not easier, the longer it is deferred.
- **O2 (typed capability handles).** Rejected: commits to a Go-shaped API that does not unify with NATS subject patterns. When NATS lands, either the typed handles become thin wrappers (dead weight) or persist alongside (two parallel APIs forever). The subject-as-cap shape (O3) collapses both transports onto one surface.
- **O4 (process isolation).** Rejected: the threat model is hygiene, not security. A process-isolation architecture spends a large engineering budget on a boundary the requirements do not demand and inherits substantial complexity (FFFI2 sub-protocol stability, dock-tile ID scoping across processes, IPC for every cap call). Revisit only if a future requirement introduces untrusted third-party apps.
- **Embedded NATS server.** Rejected per stakeholder direction (`SD4`). External NATS is the standard deployment shape, swappable for a managed broker, and avoids inheriting NATS' lifecycle into the monolith.
- **Per-app CH tables for state.** Rejected (`SD6`). Per-app schemas impose a schema-migration discipline on every new app and fragment the data-centricity property the leeway membership model preserves. Apps with structured-payload needs project a materialised view over `runtime.facts` rather than owning a separate base table.
- **Single polymorphic `payload` string column on `runtime.facts`** (the original draft of this ADR). Rejected (`SD6`): abandons the leeway columnar advantages (dictionary compression, typed query, membership ACL, subset projection) for a flat blob, and would require an opaque per-`kind` decoder on every read. The leeway-shaped table modelled on `spinnaker.facts` preserves these properties at the same row-count cost.
- **Hand-rolled import-grep lint guard** (the original draft of `SD10`). Rejected: catches only direct imports, misses transitive escapes through dependencies. Capslock's call-graph analysis is the right primitive.
- **`capmap` integration for credential custody.** Out of scope per stakeholder direction. NATS' native NKey/JWT machinery serves this role in M4+.
- **Multi-OS-viewport coexistence.** Foreclosed by the remote-desktop / video-streaming direction. The single-viewport invariant is non-negotiable.

## Consequences

### Positive

- **Every external resource access becomes an inspectable record.** The audit stream alone is a substantial diagnostic improvement over the current implicit-syscall posture.
- **Multi-app coexistence becomes real (M3).** The dock surface gives users multiple tools side by side — query in `play`, monitor system load in `imztop`, debug published events from a generic event-viewer app — without the carousel's serial-rendering constraint.
- **NATS landing in M4 is a transport swap, not a redesign.** The capability shape, subject taxonomy, AppI surface, state layer — none of it changes at M4. The risk that NATS arrival forces another architectural revision is structurally eliminated.
- **Data-centricity becomes intrinsic.** Apps' state, capability grants, and audit records all live in queryable CH tables. `play` is a debugger for the whole runtime as a free side-effect.
- **CLI unification has a clear endpoint.** M5 is mechanical because `SurfaceHeadless` is reserved from M1. Today's `cli.Command`s can migrate at their own pace without architectural negotiation.
- **The Powerbox pattern formalises an existing intuition.** The file picker has always served a permission-mediation role implicitly; making that explicit codifies it for fs and generalises it to every other resource kind.

### Negative

- **Indirection cost on every external call.** A subject-routed `fs.handle.{uuid}.read` is more expensive than a direct `os.Open`. For frame-hot paths (e.g., streaming a large file during scroll) the cost matters; mitigations include subject-handler co-location and bypass for read streams once a handle is granted (stream the file via an in-proc reader rather than per-chunk subject requests).
- **The lint guard is the only enforcement.** A motivated app author can `import "os"` and call `Open` directly. The hygiene framing is honest about this; deployments that require enforcement need O4, which this ADR explicitly defers.
- **Membership-vocabulary design becomes a load-bearing artefact.** The `runtime.kind.*`, `runtime.app.*`, `runtime.subjectFilter.*` membership families in `factsmemberships.go` are a public-stability surface; once an app writes facts under those identifiers, renaming requires a vocabulary-migration pass. The spinnaker mapping is the precedent for keeping this disciplined.
- **Leeway tooling is a hard prerequisite for queries.** Direct SQL over `runtime.facts` without the leeway DSL is awkward (physical column names are encoded). `play` and the boxer clickhouse DSL are the natural query paths; anyone reaching for `clickhouse-client` ad hoc will have a worse time.
- **Cap-grant prompts add a UX surface to design.** Every new cap kind needs a grant prompt that explains the request in user terms. The bar is the macOS / web-Permission style — not difficult, but not free either.
- **Migration spans several releases.** M1 alone touches seven existing apps; M3 reworks the host model. The two-path-coexistence discipline of [ADR-0057](0057-demo-registry-and-drivers.md) applies throughout.

### Neutral

- **AppId becomes a public-stability surface.** Once an app ships under `org.pebble2.play`, the ID outlives implementation churn (similar to package paths). A future rename is a deprecation event.
- **Subject taxonomy becomes a public-stability surface.** Same shape as a URL scheme; renaming a subject is a deprecation event with a migration window.
- **The runtime grows a service-broker posture.** This is appropriate given the NATS direction but is a meaningful conceptual shift from the current "library of subcommands" framing.

## Status

Accepted on 2026-05-11 by @spx. Implementation begins at phase M1 per `SD12`.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## Updates

### 2026-05-30 — markdown consumer API: `Doc.RenderActions` (generic, immediate-mode) supersedes the `WithClipboard` sketch

The clipboard-button entry below sketched the markdown consumer hook as `WithClipboard(write func(string))` — a clipboard-specific callback. Implementation generalised it: the markdown widget should not know about clipboards, and a callback is an awkward fit for an immediate-mode renderer. The shipped API is:

```go
type CodeBlockAction struct {
    Text  string // verbatim author source (pre-highlighter-canonicalisation)
    Lang  string // normalised fence language: "go", "sql", "" (unlabelled)
    Index int    // 0-based ordinal among the doc's code blocks
}

func (inst *Doc) RenderActions(ids *c.WidgetIdStack, label string, opts ...RenderOpt) iter.Seq[CodeBlockAction]
```

`RenderActions` draws the whole document eagerly (placing a **small IDS `Button`** labelled `label` above each code/verbatim block) and returns the blocks clicked this frame as an `iter.Seq[CodeBlockAction]`. Properties:

- **Generic, not clipboard-specific.** The widget reports clicks; the caller decides the action. Copy-to-clipboard is one consumer (`capdemo`, `helphost`), but the same button can drive run-in-REPL, open-in-editor, etc.
- **Immediate-mode.** The pixels are drawn when `RenderActions` is called; the returned sequence only replays already-captured clicks. Ranging, breaking early, or ignoring the result all leave identical output — no deferred/retained callback.
- **Multiple clicks per frame.** The sequence yields once per button clicked before the next paint (0, 1, or many), so a consumer never silently drops a click.
- `Doc.Render` stays void and button-free; consumers that don't want actions (SVG export, screenshot tour, `capinspector`, the widgets demo) are unchanged. `segment` gained `codeLang` alongside `codeText` to populate the action.

The capability, broker, subject, and egui opcode below are **unchanged** — only the widget-facing hook evolved. The button changed from the originally-shipped frameless `PhCopy` icon to a labelled small IDS button per design-system review.

### 2026-05-30 — `clipboard.write` capability: copy-to-clipboard as a brokered subject + bus→egui bridge

**Status: design agreed; implementation pending.** This entry records the decision and wire surface ahead of the code, per the design-first norm for new packages.

**Origin.** The markdown widget ([`widgets/markdown`](../../public/thestack/imzero2/egui2/widgets/markdown/)) wants an icon-only copy-to-clipboard button on each code/verbatim block. That small UI ask surfaced a structural gap: there is no clipboard capability, and **Go cannot reach the system clipboard at all** today (no dependency, no call site).

**Why this cap is unlike `fs.*` / `ch.*` / `kafka.*`.** Every capability shipped so far has a *pure-Go mechanism* that runs off the frame loop — `fsbroker` does `io/fs` on a bus goroutine, `chstore` runs SQL, etc. The clipboard has no such Go-side mechanism:

- The build is **CGO-free** (a hard invariant of this ADR), which rules out `golang.design/x/clipboard`.
- A pure-Go shell-out (`atotto/clipboard` → `wl-copy`/`xclip`/`pbcopy`) is technically CGO-free but targets the **wrong clipboard**: whatever X11/Wayland selection the Go process can reach, not the one owned by the **egui/winit viewport** the user is actually looking at. It fails outright under Wayland (the clipboard is bound to the focused surface) and is meaningless under the remote-viewport direction of [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) (where the real clipboard is the browser's, and "clipboard sync" is explicitly deferred).

So this is the **first capability whose mechanism lives on the egui draw path** — the path that §SD3's taxonomy and §SD5's bus deliberately keep *off* the bus. Bridging the cold bus path to the hot frame path is the genuinely new work here; the alternatives (a self-contained Rust-side copy widget that bypasses the broker, or a Go shell-out) were rejected because they either forgo the per-copy audit the capability exists to provide, or touch the wrong clipboard.

**Decision (write-only at v1).** A copy becomes five small pieces:

**1. Subject — `clipboard.write` (`CapDirectionPub`).** A new top-level family in the SD3 taxonomy, addressed via **request/reply** (not bare publish — see the audit note below) with a tiny ack reply:

```
clipboard.write                     copy UTF-8 text to the viewport clipboard (request/reply, empty ack)
```

`CapDirectionPub` is the right direction even though this is a Request: `Request` publishes to the subject (needs Pub) and reads the ack on an ephemeral `_INBOX.*` that bypasses cap checks — the same shape every `fs.dialog.*` cap uses.

An app declares it like any other cap; the broker grants on Mount:

```go
{Pattern: "clipboard.write", Direction: app.CapDirectionPub,
 Reason: "copy code blocks / query results to the clipboard"},
```

The v1 payload is the **raw UTF-8 text bytes** — no codec. A format/flavour discriminator (a `clipboardwrite` codec under `runtime/codec/`, mirroring `dialogreply`) is the documented extension point if a second clipboard flavour (rich text, image) ever lands.

**2. Broker — `clipboardbroker.Service`.** A new package [`public/keelson/runtime/clipboardbroker`](../../public/keelson/runtime/) mirroring `fsbroker`'s shape: a synthetic `ServiceAppId app.AppIdT = "runtime.clipboard"`, a `SubjectWrite = "clipboard.write"` const, and a `Service` that subscribes to the subject at boot, **accumulates each request's text off-frame**, and replies with an empty ack so the requester's `Request` returns (and is audited). It is simpler than `fsbroker` — no picker, no handle minting, no per-uuid grants — exposing only:

```go
func NewService(bus *inprocbus.Inst, log zerolog.Logger) (*Service, error)
func (s *Service) DrainPending() (texts []string)  // host frame loop drains this
```

**3. Mechanism — egui opcode `CopyTextToClipboard(text)`.** A new procedural node in the egui2 definition layer, modelled exactly on [`scrollToCursor`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go) — a fire-and-forget command, not a widget:

```go
idl.NewProceduralNode("copyTextToClipboard").
    AddArguments(idl.NewArgumentsBuilder().PlainArg("text", ctabb.S).Build()).
    WithApplyCodeClientRust(rustClientCode("c.copy_text(text);\n"))
```

This is the only thing that physically touches the OS clipboard, via egui ≥0.34's `Context::copy_text` (resolved here to 0.34.2). Note the apply code uses the interpreter's frame-scoped `c: &egui::Context` directly — **not** the optional `{{EguiUiOptionalOuter}}` Ui. `copy_text` is a `Context` method (it pushes an `OutputCommand`), not a `Ui` method, and `c` is in scope in every interpreter match arm (the same handle the `codeView` node already uses for its layout-job cache). This is the key simplification for the bridge below: emitting the op has **no active-Ui-scope requirement**, so the host can drain and emit after its panels have closed. There is currently zero clipboard code on the Rust side, so this is net-new there too.

**4. Bridge — bus→egui, drained in the host frame loop.** The broker handler runs off-frame and cannot emit a draw call. Because the op rides the frame-scoped `Context` (point 3) rather than an active `Ui`, the bridge is trivial: the windowed renderer ([`carousel/imzero2_demo_resolve.go`](../../public/thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go)) drains `Service.DrainPending()` once per host frame — after `host.Frame()` and the picker overlay have run — and emits `c.CopyTextToClipboard(text)` for each drained string. No panel scope to thread, no Ui borrow. This mirrors how [`fsbroker/pickerbridge`](../../public/keelson/runtime/fsbroker/pickerbridge/bridge.go) couples the off-frame broker queue to the on-frame egui picker, but is simpler (the picker needs a Ui; the clipboard op does not). Net latency from click to OS clipboard is ~1–2 frames (click readback → request → enqueue → drain → opcode) — imperceptible for a copy.

**5. Consumer — markdown copy button.** `markdown` is a pure render path with no bus handle, so the capability stays with the **host app**, wired via a new functional option alongside `WithScrollToSection`:

```go
func WithClipboard(write func(text string)) (opt RenderOpt)
```

The host app declares and holds `clipboard.write`, and passes a `write` sink that fires the bus request off the frame goroutine: `WithClipboard(func(t string){ go func(){ _, _ = bus.Request("clipboard.write", []byte(t)) }() })` (a goroutine because `Request` blocks until the broker acks, and the frame thread must not block — the same idiom `capdemo`'s `runWatchPick` uses). When the option is set, each `segKindCodeBlock` emits an icon-only button (the Phosphor `PhCopy` glyph) keyed on the same per-seq id as its `CodeView`; on click-readback the renderer invokes the `write` sink with that block's text. When the option is absent — the app lacks the cap, or rendering has no viewport (SVG export, screenshot tour) — **no button renders**. The CodeView's existing `selectable(true)` (Ctrl+C) is unaffected; the button is an additional affordance.

**Audit is why this is a Request, not a Publish.** The `inprocbus` `AuditSinkI` ([`SetAuditSink`](../../public/keelson/runtime/inprocbus/bus.go)) records exactly one `AuditRecord` per call — and that record is emitted in `Client.Request`'s defer (`client.go`), **not** in `Client.Publish`, which is fire-and-forget and unaudited (matching NATS core: a bare publish has no server-acked lifecycle to record). A `Publish`-based copy would therefore be invisible to the audit trail — defeating the entire reason for making the clipboard a capability. Routing through `Request` lands every copy in the trail with sender `AppId`, result, latency, and `RequestSizeB` (the byte count copied) — no audit logic in the broker itself. An app without the cap gets a publish-time permission error on the `Request`'s outbound leg, the same shape NATS will return (§SD5). Consistent with the **hygiene-not-security** posture: we observe and attribute clipboard writes; we do not sandbox them.

**Capslock.** The `clipboardbroker` package is the sole point that constructs `CopyTextToClipboard` opcodes from bus traffic; it joins the §SD10 allowlist as a capability surface, keeping the lint guard's "every external-resource touch is in a named broker" invariant intact.

**Scope.** Write-only. **Read/paste is deliberately out of scope** at v1: egui delivers paste as an `Event::Paste` (event-driven, not a synchronous pull), so `clipboard.read` needs its own Rust-side event plumbing and a request/reply bus shape — a separate follow-up if a consumer needs it. Also out of scope: rich-text/image flavours (the raw-UTF-8 payload is the v1 wire), and a programmatic (non-click) copy API beyond a plain `bus.Request("clipboard.write", …)`.

### 2026-05-15 — `fs.watch` gains recursive mode

Supersedes the "Out of scope" line from the 2026-05-14 amendment that read "Recursive watches (single-level only; an explicit follow-up if recursive use cases land)". A use case landed; recursive watching is now opt-in via a new `WatchRequest.Recursive` field.

**Wire surface**

```go
type WatchRequest struct {
    PollFallback   bool
    PollIntervalMs int32
    Recursive      bool  // NEW
}
```

Default remains `false` (single-level), so every consumer that already shipped — `capdemo`'s watch section, the existing tests, `play_renderer`'s future migration target — keeps its current semantics until it explicitly opts in.

**`WatchEvent.Name` semantics under recursion.** In single-level mode `Name` stays a basename ("foo.txt"). In recursive mode `Name` becomes a forward-slash relative path under the watch root ("sub/foo.txt", "deep/nested/leaf.txt"). The wart in the old comment ("the basename of the affected entry within the watched directory") was a deliberate single-level simplification; under recursion the watch root has no single parent dir to anchor names against, so the full relpath is the right answer.

**Inotify backend** ([`fsbroker/watcher.go`](../../public/keelson/runtime/fsbroker/watcher.go)).

- `inotifyWatcher` gains a `wdToRelDir map[int32]string` keyed by inotify watch descriptor. The root wd maps to `""`; subdirectory wds map to their forward-slash relpath under the root.
- At construction (`recursive=true`), `walkAndAddSubdirs` traverses the tree with `filepath.WalkDir`, calls `InotifyAddWatch` on every directory it finds (best-effort — `max_user_watches` / permission-denied entries are skipped silently), and records each wd in the map. Symlinks are not followed (the walker uses `Lstat`).
- At runtime, `parseBuf` looks up the event's wd in the map to recover its relDir, joins with the event's basename, and emits the full relpath as `Name`. On `IN_CREATE+IN_ISDIR` (a new subdir appearing) the parser calls `addSubdirWatch` so the new directory's contents fire events on the same wd→relDir machinery. The same dynamic-add fires on `IN_MOVED_TO+IN_ISDIR` (subdir moved INTO the watch tree from elsewhere).
- `IN_IGNORED` events (the kernel announcing a watch was implicitly removed because its inode vanished) trigger map cleanup. `IN_DELETE_SELF`/`IN_MOVE_SELF` on the root wd terminates the watch (emit Closed + exit); on a non-root wd it just cleans up the map entry — the parent's `IN_DELETE` already announced the subdir's disappearance from the user's perspective.

**Poller backend.** `pollerWatcher.scanRecursive` walks the subtree with `filepath.WalkDir` and keys the snapshot by forward-slash relpath. The diff loop is unchanged — same Create/Delete/Modify detection, just over a deeper key space. Symlinks are not followed (WalkDir uses Lstat). Permission-denied subtrees are skipped via `fs.SkipDir` so an unreadable corner doesn't fail the whole tick.

**Constructor signatures.**

```go
newInotifyWatcher(path string, recursive bool)
newPollerWatcher(path string, interval time.Duration, recursive bool)
```

`pickBackend(path, req)` threads `req.Recursive` through unchanged. A non-directory path with `recursive=true` is degenerate — the poller branch clamps to non-recursive (`recursive: recursive && fi.IsDir()`); the inotify branch is consistent because `walkAndAddSubdirs` no-ops on a non-directory root (the WalkDir callback's `p == inst.path` skip covers it).

**Memory model.** `wdToRelDir` is single-writer (the parser goroutine, plus the constructor-time `walkAndAddSubdirs` which completes before `Start()` spawns the parser). The Go memory model's happens-before-goroutine-start covers the handoff; no lock needed.

**Test coverage** ([`fsbroker/watcher_test.go`](../../public/keelson/runtime/fsbroker/watcher_test.go)):

- *Recursive inotify, pre-existing subdir* — `mkdir sub/`, start watch, write `sub/inner.txt`, assert `Create{Name:"sub/inner.txt"}` arrives. Exercises `walkAndAddSubdirs`.
- *Recursive inotify, dynamic subdir* — start watch on empty dir, `mkdir later/`, then write `later/child.txt`, assert both events arrive. Exercises the `IN_CREATE+IN_ISDIR` → `addSubdirWatch` path inside `parseBuf`.
- *Recursive poller* — same shape with `WatchRequest{Recursive: true, PollFallback: true, PollIntervalMs: 100}`, asserts deep `Create{Name:"deep/nested/leaf.txt"}` arrives.
- *Non-recursive drift guard* — default-mode watch (no `Recursive`) must NOT surface subdir-internal events. Pins the contract so a future "always recursive" change loudly breaks.

**Out of scope (still).** Symlink-followed traversal (always Lstat), max-depth caps (deferred until a real `max_user_watches` blow-out lands), per-tile recursive-default opt-in (every consumer asks explicitly). Cookie pairing across moves into/out-of subtrees works inside one watch root but not across watch roots — same limitation as fsnotify and inotify itself.

### 2026-05-15 — `capdemo`: fourth section exercises fs.watch end-to-end

`apps/capdemo` gains a fourth section that drives the fs.watch round-trip introduced by the 2026-05-14 amendment below. The demo now demonstrates every M2 cap-broker path: fs.dialog.read, runtime.persist.scratchpad, fs.dialog.watch, and the status pane.

**Section: fs.dialog.watch — folder change notifications.**

- "Pick folder to watch…" button → `runWatchPick` goroutine: `bus.Request(fs.dialog.watch)` → broker queues → pickerbridge resolves with a folder → broker grants `fs.handle.{uuid}.>` `CapDirectionBoth` → app `Subscribe(fs.handle.{uuid}.event, handleWatchEvent)` → `bus.Request(fs.handle.{uuid}.watch, payload)` (payload encodes `WatchRequest{PollFallback, PollIntervalMs}` when the checkbox is set) → backend streams events on the subscribed subject. Subscribe-before-start avoids a window where early kernel events would be missed.
- "Stop watching" button → `runWatchStop`: `Request(.unwatch)` + invoke the saved `unsubscribe`. The handle stays alive — a subsequent Pick can restart the watch (the broker mints a fresh uuid keyed on app+path, so the previous cap may still apply).
- "Close handle" button → `runWatchClose`: `Request(.close)` which tears the watch down on the broker side and evicts the handle entirely.
- "Force poller backend" checkbox → bound to `watchUsePoller` via `Checkbox.SendRespVal`; the next Pick sends `WatchRequest{PollFallback: true, PollIntervalMs: 250}` and the reply's `Backend` field surfaces as "poller" instead of "inotify".
- Rolling event buffer (cap 50, displays the trailing 12) renders `Kind  Name  HH:MM:SS.mmm` per row. Event handler appends under `inst.mu`, gated by `watchActive` so late events arriving after Stop's unsubscribe-race are dropped silently.

**Manifest cap addition:**

```go
{Pattern: fsbroker.SubjectDialogWatch, Direction: app.CapDirectionPub,
 Reason: "demo: request a folder-watch dialog via Powerbox"},
```

The eager `fs.handle.>` Pub already covered the per-handle watch/unwatch publishes; the narrower per-uuid `fs.handle.{uuid}.>` Both grant from `Service.Resolve` covers the `.event` Subscribe.

**Test coverage** (`capdemo_test.go` additions):

- `TestApp_FsWatch_RoundTrip` — drive `runWatchPick` against a tmpdir, write a file, assert `WatchEventCreate{Name: "hello.txt"}` lands in `app.watchEvents` within 2s. Backend reads as "inotify".
- `TestApp_FsWatch_PollFallback_BackendReported` — set `watchUsePoller=true` before the pick, assert `app.watchBackend == "poller"`.
- `TestApp_FsWatch_StopReleasesSubscription` — start watch, fire one event, call `runWatchStop`, write another file, assert `watchEvents` doesn't grow past the pre-stop count. Pins the active-flag gate against late event-handler races.
- `TestManifest_DeclaresExpectedCaps` — drift guard updated for the new cap count (2 → 3) and the new `fs.dialog.watch` pattern.

**No screenshot tour.** Same rationale as the 2026-05-14 base amendment: goroutine-driven async dialogs don't compose with the 4-frame tour.

### 2026-05-14 — `fs.watch`: streaming filesystem-change notifications over the bus

Fourth concrete service on the `fs.*` family (after `fs.dialog.read|write|bundle` + the per-handle `read`/`close` ops). Adds Linux filesystem notifications behind a Powerbox-mediated grant; events stream as bus messages on the conventional `fs.handle.{uuid}.event` subject sibling.

**New subjects** (§SD3 taxonomy extension):

- `fs.dialog.watch` — Powerbox prompt. The user picks a directory; the broker mints a `HandleModeWatch` handle. Reuses the `pickerbridge` path; title in M2 is "Pick folder to watch". File-only watch (tail-a-log style) lands later — for now apps select the parent directory and filter by `Name`.
- `fs.handle.{uuid}.watch` — start streaming. Optional `WatchRequest{PollFallback, PollIntervalMs}` payload. Returns `WatchReply{Started, EventSubject, Backend}` where `Backend` ∈ `{"inotify", "poller"}`.
- `fs.handle.{uuid}.unwatch` — stop streaming, keep the handle alive (a subsequent `watch` may restart).
- `fs.handle.{uuid}.event` — broker → app stream. JSON `WatchEvent{Kind, Name, Cookie, Ts}` with relative `Name`s only; the absolute path stays inside the broker per the Powerbox invariant.

**Cap-direction wrinkle.** `Service.Resolve` now grants `CapDirectionBoth` on `fs.handle.{uuid}.>` for watch-mode handles only — the app needs Sub on `.event`. Read / write / bundle handles continue to receive Pub-only grants. The conditional lives at [`fsbroker/service.go`'s Resolve path](../../public/keelson/runtime/fsbroker/service.go).

**Backends** ([`runtime/fsbroker/watcher.go`](../../public/keelson/runtime/fsbroker/watcher.go)):

- *inotify* — `unix.InotifyInit1(IN_NONBLOCK|IN_CLOEXEC)` + `InotifyAddWatch` with `IN_CREATE|DELETE|MODIFY|ATTRIB|MOVED_FROM|MOVED_TO|DELETE_SELF|MOVE_SELF`. Single-level (no recursive add-on-subdir-create). `IN_Q_OVERFLOW` surfaces as `WatchEventOverflow` — the app's rescan signal. `IN_DELETE_SELF`/`IN_MOVE_SELF` surfaces as `WatchEventClosed` and tears the watch down. Inotify header parsing uses `binary.LittleEndian` rather than `unsafe.Pointer` to keep the `CAPABILITY_UNSAFE_POINTER` capslock surface minimal even inside broker code.
- *poller* — `time.Ticker` + `os.ReadDir` + per-entry mtime/size diff against a snapshot. Auto-selected via `Statfs.Type` against `PROC_SUPER_MAGIC` (0x9fa0), `SYSFS_MAGIC` (0x62656572), `NFS_SUPER_MAGIC` (0x6969), `FUSE_SUPER_MAGIC` (0x65735546), `CIFS_MAGIC_NUMBER` (0xff534d42). Forced via `WatchRequest.PollFallback: true`. Synthesises `Create`/`Delete`/`Modify`; rename surfaces as Delete+Create without a paired `Cookie`. Default interval 500ms; values below 100ms clamp to 100ms.

**Self-publish loop guard.** The broker subscribes to `fs.>` to receive app requests; the same wildcard would also match the `.event` payloads the broker itself publishes. `Service.handleRequest` now short-circuits on `msg.Sender == ServiceAppId`, so the broker doesn't dispatch on its own broadcasts.

**Lifecycle.** Per-handle `activeWatch` struct holds the backend + uuid under `Service.mu` in a new `watches map[string]*activeWatch`. Teardown paths: (1) explicit `unwatch`, (2) `close` on the handle, (3) `Service.Close` reaping every live watch, (4) backend self-terminates on root-vanished (`IN_DELETE_SELF` / poller `os.Stat` ENOENT). Each path emits a final `WatchEventClosed` where the backend supports it and closes the events channel.

**Backpressure.** One pump goroutine per watch, reads `backend.Events()` and `Publish()`es inline. Bus handler invocation is synchronous (`inprocbus.publish` iterates subscriptions in the publisher's goroutine), so a slow app subscriber stalls the pump. Channel-full inside the backend drops the event and substitutes a synthetic `WatchEventOverflow`; in extremis the kernel-side `IN_Q_OVERFLOW` arrives with the same kind. The contract is: an `Overflow` event means "events were lost; rescan from scratch."

**Test coverage** ([`fsbroker/watcher_test.go`](../../public/keelson/runtime/fsbroker/watcher_test.go)):

- *Inotify e2e* — stage a tmpdir, drive `fs.dialog.watch` → `Resolve` → `fs.handle.{uuid}.watch`, subscribe to `.event`, write a file, assert `WatchEventCreate` lands with `Name="hello.txt"`.
- *Forced-poller e2e* — same shape with `WatchRequest{PollFallback: true, PollIntervalMs: 100}`; asserts `WatchReply.Backend == "poller"` and the Create event arrives within a few ticks.
- *Wrong-mode rejection* — a read-mode handle that publishes `.watch` is rejected with "handle not opened for watch".
- *Unwatch terminates stream* — after a watch is active, `.unwatch` stops further events while the handle stays alive.
- *Handle close tears down watch* — `.close` evicts the handle and the watch in one operation; a subsequent `.watch` fails "unknown handle".

**Out of scope (deferred).** Recursive watches (M3+ if a use case lands), separate `fs.dialog.watch.file` for single-file picks (M3+; today watch always picks a directory), sticky grants for watch (every Mount re-prompts; aligns with the rest of M2's fsbroker UX). `pickerbridge` is the same widget code, only the title differs — a folder-only picker mode is a future picker-widget improvement that this amendment does not block on.

### 2026-05-14 — `capdemo`: first M2 consumer (fs.dialog.read + runtime.persist round-trip)

`apps/capdemo` is the first real app that uses `MountCtx.Bus()` and `MountCtx.Storage()` end-to-end. Acts as the canonical M2 smoke test: every code path the broker wiring established (Phases A → C) is exercised in one window from real user gestures.

**Three sections:**

- **fs.dialog.read** — "Pick a file…" launches a goroutine that calls `bus.Request("fs.dialog.read", nil)`. The picker overlay (Phase B) resolves; the goroutine receives the granted handle subject prefix, then issues a second `bus.Request(<prefix>.read, nil)` to fetch the file body. The path never reaches the app — only `fs.handle.{uuid}.read` does. A "Close handle" button publishes `<prefix>.close` so the broker evicts the handle.
- **runtime.persist.scratchpad** — A TextEdit binds to a `scratchpad` string; Save / Load / Delete buttons translate to `storage.Set/Get/Delete` on the host-injected persist client. The key constraint (single NATS token) is enforced in `persist.Client`'s doc; `scratchpadKey = "scratchpad"` complies.
- **Status pane** — Shows last operation status, granted handle subject, and a preview of the loaded bytes (capped at 1KiB by `previewLimit`).

**Manifest declarations** are the demo's point:

```go
Caps: []app.SubjectFilter{
    {Pattern: "fs.dialog.read", Direction: CapDirectionPub, Reason: "request a file pick via Powerbox"},
    {Pattern: "fs.handle.>",   Direction: CapDirectionPub, Reason: "read file contents through the granted handle"},
},
PersistedKeys: []string{"scratchpad"},  // host auto-injects runtime.persist.{ownAlias}.>
```

The `fs.handle.>` cap is declared eagerly even though the broker also adds the narrower `fs.handle.{uuid}.>` per resolve — belt-and-braces while the broker's per-grant augmentation is the only authoritative source. A future commit can narrow once an "ask for grant before publishing" idiom is preferred.

**Async UI model.** `bus.Request` is synchronous from the caller's POV but the picker resolves over many frames; calling it from `Frame` would block the render loop. `runPick`, `runPersistSet`, etc. all run in goroutines with state writes guarded by `App.mu`. The Frame goroutine reads the snapshot under the same lock. Memory `[[project-imzero2-continuous-rendering]]` ensures Frame keeps ticking after the goroutine updates state, so the user sees the result without manual repaint requests.

**No screenshot tour.** The goroutine-driven asynchronous picker doesn't compose with the 4-frame tour shape; capdemo's purpose is interactive validation. The factory ctor unconditionally returns `newApp()`.

**Test coverage.** Five unit tests in `capdemo_test.go`:

- `TestApp_PersistSet_Get_Delete_RoundTrip` — Set / Get / Delete via the persist client through a fixture bus + service.
- `TestApp_FsPick_HandleReadRoundTrip` — Stages a tmpfile, drives `runPick` in a goroutine, resolves via the main goroutine, asserts handle subject + preview bytes land.
- `TestApp_FsPick_NoBus_Errors` / `TestApp_PersistSet_NoStorage_Errors` — Mount with NoopBus / NoopStorage records an error rather than crashing.
- `TestManifest_DeclaresExpectedCaps` — Drift guard on the declared `Caps` + `PersistedKeys` shape.

**Closes** the "first consumer migration" follow-up from the M2 Phase C ship message. Future commits can migrate `play`'s `os.Open` path to `fs.dialog.read` against the same seams.

### 2026-05-14 — M2 Phase C: `runtime.persist.*` bound; `MountCtx.Storage()` over the bus

Third piece of M2 wiring. Apps now have a working cold-state surface that flows through the same subject router as `fs.*`: `MountCtx.Storage().Set/Get/Delete` translates to `bus.Request("runtime.persist.{ownAlias}.{key}.{op}", payload)` on the wire.

**New `persist.Client`.** Adapts `app.BusI` into `app.StorageI` by encoding each call into the canonical `runtime.persist.{alias}.{key}.{op}` subject and unmarshalling `PersistReply`. The alias is baked in at construction so the request subject matches the app's declared `runtime.persist.{ownAlias}.>` cap. Key constraint: a single NATS token — no dots, no wildcards (the service rejects dotted subjects as "malformed"). The Client doc-string flags this so callers reach for camelCase / snake_case rather than `editor.font`.

**Carousel wiring.** `imzero2_demo_cli.go` constructs `persist.NewService(bus, log.Logger, persist.NewMemoryBackend())` immediately after the fsbroker service. The in-memory backend matches Phase C scope; a `runtime.facts`-backed backend lands in a follow-up so state writes appear as auditable rows alongside grants + lifecycle events. The service is closed at process exit via a `defer` on the carousel's Action return.

**Host-side cap auto-injection.** `windowhost.Inst.Open` checks `manifest.PersistedKeys` and, when non-empty, calls `client.AddCap(...)` to inject `runtime.persist.{ownAlias}.>` `CapDirectionPub` on the per-app bus client. The manifest field was forward-declared in M1 for exactly this purpose; apps don't repeat the boilerplate cap pattern, and forgetting to declare keys is now a loud failure (every `Storage()` call errors at the permission gate). Once the cap is set the host also constructs `persist.NewClient(busC, m.Id)` and passes it as the `storage` argument to `NewStaticMountContext`, so `MountCtx.Storage()` returns a real client. Without the bus wiring or without declared keys the field falls through to `NoopStorage` — every call errors loudly.

**End-to-end tests.** Two new windowhost tests pin the contract:

- `TestSetBus_PersistStorageEndToEnd` — register an app with `PersistedKeys: []string{"selectedTab"}`, Open through the windowhost, Set/Get/Delete via `MountCtx.Storage()`, confirm round-trip semantics including found=false for missing keys.
- `TestSetBus_PersistWithoutPersistedKeys_PermissionDenied` — register an app with empty `PersistedKeys`; the bus client gets no auto-injected cap; every `Storage().Set` errors at the permission gate before reaching the service. Pins the "manifest declares intent" contract.

A separate persist `client_test.go` round-trips Set/Get, missing-key, empty-value semantics, delete-then-get, and the no-cap permission path — exercising the wire protocol without the windowhost.

**Phase D next.** Flip `capslock-check` lint from advisory to hard-fail. Now that bus paths cover fs.* and runtime.persist.*, the residual direct-syscall surface is small enough to enforce.

### 2026-05-14 — M2 Phase B: `fs.*` Powerbox bound via `fsbroker.Service`

Second piece of M2 wiring. Builds on Phase A (the bus seam): now that `MountCtx.Bus()` is a real `inprocbus.Client`, the `fs.*` subject family lands as the first concretely-served family.

**Carousel wiring.** `imzero2_demo_cli.go` constructs `fsbroker.NewService(bus, log.Logger)` immediately after the bus is built. The service subscribes to `fs.>` (its own client mints permissive caps via `inst.NewClient(ServiceAppId, …)`) and queues dialog requests pending user selection. A `NewService` error is logged and falls through — `fsSvc` stays nil and the picker overlay is skipped, but the rest of the runtime keeps working.

**Picker overlay.** `buildWindowedRenderer` accepts the new `*fsbroker.Service` parameter and, when non-nil, constructs `pickerbridge.NewBridge(svc, log.Logger, pickerbridge.Config{})` (default `FsRoot="/"` + `defaultStartDir($HOME)`). A fresh `bridgeIds` `WidgetIdStack` keeps the picker's widget ids disjoint from `bodyIds` / `menuIds` — `Bridge.Render(bridgeIds)` runs every frame as the last call inside the inner render closure, drawing the picker on top of the window host body whenever a pending dialog exists.

**End-to-end test.** `TestSetBus_FsBrokerEndToEnd` exercises the carousel-shape wiring without the egui render loop:

1. Register a test app declaring `Caps = [fs.dialog.read pub, _INBOX.> sub]`.
2. Build `bus` + `fsbroker.Service` + `windowhost.Inst`, `SetBus(bus)`.
3. `Open` the app, pull the per-app `MountCtx.Bus()` client.
4. From a goroutine, `Request("fs.dialog.read", …)` — blocks awaiting the broker.
5. Main goroutine polls `svc.Pending()` until the queued entry appears, then calls `svc.Resolve(reqId, "/tmp/picked-file.txt")`.
6. The goroutine's `Request` unblocks with `DialogReply{Granted: true, HandleSubjectPrefix: "fs.handle.<uuid>"}`; the app's caps were augmented in-place to include `fs.handle.<uuid>.>` so the subsequent `fs.handle.<uuid>.read` call passes the permission gate.

This pins the contract that motivated SD7 — the path is never exposed to the app, the broker is the only code that resolves dialogs, and the cap broker is implicit in the broker's `client.AddCap` call.

**What this enables and what it doesn't.** Any future app that declares an `fs.dialog.read`/`.write`/`.bundle` cap can now request a file via the bus and receive a handle subject. No existing app declares these caps yet — `play`, the obvious first consumer, currently calls `os.Open` directly. The per-app migration to `MountCtx.Bus().Request("fs.dialog.read", …)` is incremental and lands when each app's file-IO path is refactored.

**Phase C next.** `persist.Service` binds `runtime.persist.{appAlias}.*` and `MountCtx.Storage()` becomes a thin client over it.

### 2026-05-14 — M2 Phase A: bus seam through `MountCtx.Bus()`

First piece of M2 wiring (the in-proc cap broker described by §SD12). Stages M2 as four phases so each landing is independently shippable:

- **Phase A (this amendment).** `inprocbus.Inst` flows through the window host to `MountCtx.Bus()`. No subject services bound yet; this is purely the seam.
- **Phase B.** Bind `fs.*` via `fsbroker.Service` (the file picker substrate is ready).
- **Phase C.** Bind `runtime.persist.{appAlias}.*` via `persist.Service`; route `MountCtx.Storage()` over it.
- **Phase D.** Flip the `capslock-check` lint from advisory to hard-fail once the bus paths replace the residual direct-syscall uses.

**Phase A landing.** `windowhost.Inst` gains a `*inprocbus.Inst` field and a `SetBus(bus)` setter mirroring `SetAudit`. On every `Open`, the host mints a per-app client via `bus.NewClient(manifest.Id, manifest.Caps)` and threads it through `app.NewStaticMountContext` so `MountCtx.Bus()` returns a real `app.BusI`. Without `SetBus`, the host hands out `NoopBus` — the M1 shape every existing app was bootstrapped against — so nothing breaks for apps that haven't declared `Caps` yet.

**Carousel wiring.** `imzero2_demo_cli.go` constructs the bus unconditionally with `inprocbus.NewInst(log.Logger)`, attaches `factsstore.AsAuditSink(facts)` so every allowed Subscribe/Publish/Request lands as a `runtime.facts` audit row, and passes the bus into `buildWindowedRenderer` alongside the existing `runId` / `facts` audit pair. The audit sink is best-effort: when CH is unreachable the in-memory fallback absorbs the rows.

**Test coverage.** Two new windowhost tests pin the seam:

- `TestSetBus_WiresInprocClientThroughMountCtx` — opens an app whose manifest declares `fs.dialog.>` `CapDirectionPub`, publishes to `fs.dialog.open` (passes the permission gate), then publishes to `ch.query.boxer` (rejected — the error wording mentions "permission").
- `TestSetBus_NilDefaultsToNoopBus` — without `SetBus`, the bus client returned from `MountCtx.Bus()` errors on every Publish ("broker not available in M1"), which is the documented lockdown.

**What this enables and what it doesn't.** Apps can now opt into the bus by declaring `Caps` in their `Manifest`; today no app declares any, so the wiring is dormant. Phase B will be the first concrete consumer when `fsbroker.Service` subscribes to the `fs.*` family and `play` gets a `fs.dialog.>` cap. The persist service (Phase C) and the cap broker (deferred — the per-grant overlay UI is its own work) are not wired yet.

### 2026-05-14 — Rust-side run_id bridge

Closes follow-up (b) from the [2026-05-12 — Runtime-run identity & app-lifecycle audit events](#2026-05-12--runtime-run-identity--app-lifecycle-audit-events) amendment. With (a) and (c) landed, the last open follow-up was cross-process attribution: the Rust client and the Go parent both had to be readable as one run in a combined log stream, and any future Rust-side audit write needed a stable handle on the inherited identity.

**New `rust/imzero2/src/runinfo.rs` module.** Mirrors the Go-side `runtime/runinfo` package surface:

- `runinfo::ENV_VAR` (= `"PEBBLE2_RUN_ID"`, kept in sync with `runinfo.EnvVar` on the Go side).
- `runinfo::run_id() -> Option<&'static str>` — memoised env-var read via `OnceLock`. Empty values map to None so a consumer can rely on `Some(...) ⇒ non-empty`.
- `runinfo::run_id_or_standalone() -> &'static str` — falls back to the `"standalone"` sentinel for tracing contexts that need a non-Option field.
- `runinfo::enter_root_span() -> EnteredSpan` — constructs and enters the `rust { run_id = … }` tracing span. The caller (`main.rs`) keeps the returned `EnteredSpan` alive for the duration of program execution; every event emitted while it's entered carries the `run_id` field through tracing-subscriber's compact formatter.
- `runinfo::log_bound_run()` — one-shot info event announcing the bind (or the standalone fallback) so a human reading the merged Go + Rust log can locate the bridge moment.

**`main.rs` wiring.** Right after `setup_tracing()`, `main()` calls `enter_root_span()` and binds the guard to `_run_span`. The guard's drop lifetime is the lexical scope of `main`, so every subsequent event — `setup_tracing`'s puffin server log, the `imzero2`/`ipc` dispatch path, every interpreter event — is implicitly tagged. Smoke test: with `PEBBLE2_RUN_ID=smoketest-01234567` set, every stderr line carries `run_id="smoketest-01234567"`; with an empty/unset env var, it reads `run_id="standalone"`.

**What this enables.** A future Rust-side audit-write path (screenshot capture, FFFI desync diagnostic, etc.) pulls the run_id from `runinfo::run_id()` and writes to the same `runtime.facts` table — a single `LifecyclesByRun` / `LookupRunStart` query reads both Go-originated and Rust-originated audit rows under one run. The amendment landed today is the prerequisite identity-flow piece; no audit writer crosses the FFFI boundary in this commit.

**All three follow-ups from the 2026-05-12 amendment are now closed.** (a) chstore read-path helpers (2026-05-13); (b) Rust run_id bridge (this amendment); (c) periodic heartbeats (2026-05-14, see below).

### 2026-05-14 — Runtime heartbeat (liveness ticks for crash detection)

Closes follow-up (c) from the [2026-05-12 — Runtime-run identity & app-lifecycle audit events](#2026-05-12--runtime-run-identity--app-lifecycle-audit-events) amendment. A crashed process (no `app-lifecycle stopped` rows reaped, no clean shutdown) was previously indistinguishable from a clean exit at audit-read time. Heartbeats close that gap.

**New `runtime/heartbeat` package.** `heartbeat.Start(ctx, store, runId, interval, logger)` launches a goroutine that writes one `HeartbeatRow` into `FactsStoreI` every `interval` until `Stop()` is called or `ctx` is cancelled. Default interval is 30 seconds. Sub-100ms inputs clamp to 100ms so a mis-configuration doesn't burn the audit table with millions of rows. Stop is idempotent and nil-safe — the carousel's reap-once hook calls it alongside `ReapAll("shutdown")` so SIGINT/SIGTERM and clean exit both stop the ticker.

**New vocab membership.** `MembKindRuntimeHeartbeat` tags one row per tick. The row carries the kind tag and the `MembRuntimeRun` mixed-LCR(run_id) — that's it. No other payload; the row's `ts` is the heartbeat moment.

**New FactsStoreI method.** `WriteRuntimeHeartbeat(HeartbeatRow{RunId, Ts}) (id, err)`. Empty `RunId` is rejected by both backends so every heartbeat is joinable back to a `runtime-start` row. The chstore natural key includes nanosecond ts to keep a hot cadence from collapsing on insert.

**New chstore read helper.** `LastHeartbeatForRun(ctx, runId) (time.Time, found, err)` returns the latest tick's timestamp for one run. Reuses the `runIdPredicate` from the (a) amendment above. Consumers compute liveness as `(now - last_heartbeat) < threshold`; a missing-heartbeat + missing-stopped state at the end of a runtime-start audit is the "this run crashed" signal.

**Follow-ups remaining from the 2026-05-12 amendment.** (b) Bridging the run_id into the Rust side. With (a) and (c) landed, only the cross-process attribution path is open.

### 2026-05-13 — `chstore` read-path helpers for runtime-run / app-lifecycle

The [2026-05-12 — Runtime-run identity & app-lifecycle audit events](#2026-05-12--runtime-run-identity--app-lifecycle-audit-events) amendment listed three open follow-ups. (a), the chstore read-path that joins `app-lifecycle` rows back to their parent `runtime-run` row across the `run_id` column, lands here.

**New methods on `chstore.Store`** (live-CH only — InMemoryFactsStore already exposes `Runs()` / `Lifecycles()` for snapshot reads):

- `LookupRunStart(ctx, runId) (RuntimeStartRow, found, err)` — fetches the runtime-start row for one `run_id`. Returns `found=false` when no matching row exists (e.g., a run minted by a process where facts persistence wasn't wired). Reconstructs `Hostname` / `GoVersion` / `VcsRevision` / `ModulePath` / `VcsBuildInfo` / `Pid` / `VcsModified` from the symbol-, string-, u64-, and bool-section LCR positions.
- `LifecyclesByRun(ctx, LifecycleFilter{RunId, AppId?, Limit}) ([]AppLifecycleRow, err)` — returns the children of one run, chronologically ordered by `ts` (started before stopped in process time). `AppId` narrows to one app within the run. `RunId` is required; empty returns an error rather than scanning the global firehose.

The query shape is the "single positional `arrayElement` predicate" the prior amendment named: `arrayFirst((p, m) -> m = MembRuntimeRun, symMRHP, symLMR) = '<runId>'` zips the parallel `lmr` / `mrhp` arrays and compares the high-card parameter to the requested run id. The same predicate shape applied to `MembRuntimeApp` provides the optional app filter. Numeric LCR fields (pid, tile_key, vcs_modified) go through a new `pickLcrNumeric` helper paralleling the existing `pickLcrString` — the cumulative-sum lookup is identical; only the "not-found" zero constant differs.

**What this enables.** `play`-style fact queries can now resolve "show me the whole session for run X" (one `LookupRunStart` + one `LifecyclesByRun`) instead of scanning the full table client-side. The pattern generalises for future read-path additions (e.g., "all states written by app Y in this run" follows the same `runIdPredicate`).

**Follow-ups remaining from the 2026-05-12 amendment.** (b) Bridging the run_id into the Rust side; (c) periodic "still-running" heartbeats. The heartbeat path will exercise a query in the same shape ("what is the last heartbeat row for run X?") and will reuse the predicate helpers here.

### 2026-05-13 — M3 host reverted from egui_dock to per-app egui::Window

The M3 design originally placed each open app in an `egui_dock` tab inside a single `c.DockArea` (see [Amendment 2026-05-12 — M3 dock host](#2026-05-12--m3-dock-host-carousel--dockhost-demo-fold-reversed) below). Empirical UX work after landing the dock host revealed that the dock model was the wrong fit for the multi-app runtime use case:

- **"Click app, see nothing change."** `push_to_first_leaf` auto-activates the new tab, but the dominant interaction is "user already has app A open, clicks app B in the Apps menu, expects to see B". With both A and B in the same leaf the tab strip is a subtle UI signal — users perceived the click as a no-op.
- **The 1:1 contract was strained.** The Manifest already carried `Title` / `Icon` / `SurfaceHints.PreferredWidth/Height`, which fed directly into `c.Window` chrome. Routing them through `dock.Tab` labels lost the windowed-app affordance set up in [Amendment 2026-05-12 — App↔window 1:1](#2026-05-12--appwindow-11-sharpened-surfacee-titleicon-on-manifest).
- **Drag-and-split UX was over-budget for need.** The dock's split-and-rearrange story is valuable in IDE workflows but unused in the "two or three apps open at once" pattern that the runtime actually serves.

**Decision (2026-05-13):** Replace `runtime/dockhost` with `runtime/windowhost`. Each open app renders as a top-level `c.Window` (egui::Window — movable, resizable, titled). The Apps menu, audit-trail wiring, empty-state pane, mount/frame/unmount lifecycle, monotonic non-reused window keys, factory-based multi-instance dispatch, and screenshot-tour bypass path are unchanged. The package rename is the only API surface change in carousel; the manifest contract is unchanged.

**Window identity & layout persistence.** Each window's widget id is derived from `ids.PrepareStr("window-<key>")`, stable for the window's lifetime. egui's built-in `Memory` keys per-widget Window position / size / collapsed state by that id, so layout persists across frames for free. Cross-run persistence is whatever `egui::Memory` natively offers (out of scope here).

**Snapshot of the dock implementation.** [`517fc46b`](../../) snapshots the dock host at its first actually-working revision (PanelCentral wrap, dual WidgetIdStacks for menu vs body, fetcher-discipline runtime guard active). Kept in history for reference and to document the bug class that the design flip avoided. The 2026-05-12 dock amendment below stays in the ADR — it's the design as originally accepted; this 2026-05-13 amendment supersedes the implementation choice without invalidating the rest of M3 (Apps menu, audit trail, empty state, fetcher discipline all stand).

**Carousel rename.** `buildDockedRenderer` → `buildWindowedRenderer`. The carousel passes a `*windowhost.Inst` to its render closure (was `*dockhost.Inst`); call sites for `Open` / `Close` / `RenderAppsMenu` / `SetAudit` / `ReapAll` are unchanged because those method names work for either host shape. Internal type rename `TileKeyT` → `WindowKeyT` and `OpenTiles` → `OpenWindows`; `factsstore.AppLifecycleRow.TileKey` keeps its name in the persistence layer so existing audit rows in `runtime.facts` are not invalidated.

### 2026-05-12 — Runtime-run identity & app-lifecycle audit events

The runtime now writes auditable events to the leeway facts table for every process boot and every app open/close. A *run* is one pebble2impl process invocation, identified by a 16-character nanoid (`run_id`) that:

- Is generated (or inherited from `PEBBLE2_RUN_ID`) at startup by the new `runtime/runinfo` package.
- Is exported back into the environment as `PEBBLE2_RUN_ID` so child processes and app code that prefers env reads see the same value.
- Is injected into the base `zerolog.Logger` so every log line in the process — including per-app loggers built via `app.AppLogger` — carries a `run_id` field automatically.

**Two new fact kinds** in `runtime.facts`, via two new `FactsStoreI` methods (`WriteRuntimeStart`, `WriteAppLifecycle`):

- `MembKindRuntimeRun` tags one row per process boot. Symbol section carries the kind tag + `MembRuntimeRun` mixed-low-card-ref (high-card-param = run_id bytes) + `MembRunHostname` / `MembRunGoVersion` / `MembRunVcsRevision` / `MembRunModulePath`. String section carries the full VCS build info. U64 section carries the pid. Bool section carries the VCS-modified flag.
- `MembKindAppLifecycle` tags one row per `DockHost.Open` and one per tile close. Symbol section: kind tag + app reference (`MembRuntimeApp`) + run reference (`MembRuntimeRun`) + `MembLifecyclePhase` (`started` / `stopped`). String section: optional `MembLifecycleStopReason` (`user-close` / `mount-error` / `shutdown`). U64 section: `MembLifecycleTileKey` so two concurrent tiles for the same app are distinguishable.

**Backend selection.** A new helper `chstore.NewWithFallback(cfg, logger, pingTimeout)` returns a `chstore.Store` if ClickHouse is reachable + DDL succeeds, else falls back to `InMemoryFactsStore`. The carousel calls this once at startup; the fallback is silent (logged at warn, never blocks boot). This satisfies the "data-centricity" criterion (C6) without making CH a hard dependency for development.

**Shutdown audit.** `DockHost.ReapAll(reason)` walks every still-open tile, runs Unmount, and emits one `app-lifecycle stopped` row each. The carousel triggers it from two paths to cover boxer's signal-handler `os.Exit` path (which skips Go-level defers in our goroutine): a defer on Action return *and* a parallel `signal.Notify` goroutine listening for SIGINT/SIGTERM, both gated by `sync.Once`. After shutdown, the audit trail is complete — every `started` has a matching `stopped` row.

**Run-id propagation invariants.**

- `PEBBLE2_RUN_ID` is set exactly once per process; subsequent `runinfo.Init()` calls are idempotent.
- The env-var-inheritance path is intentional: a wrapper script can pre-set `PEBBLE2_RUN_ID` so a multi-binary tool (`./hmi.sh` → `main_go` → `pebble2_rust`) writes events under one identity. The Rust client doesn't write events directly today, but the inheritance shape is reserved for that.
- Apps do not need to thread `run_id` through their code. The logger pre-tag handles every `Log()`/`Info()`/`Warn()` call automatically; explicit reads via `os.Getenv("PEBBLE2_RUN_ID")` are available for cases that fall outside the logger (e.g. embedding the run_id in a network request header).

**Open follow-ups.** (a) Read-path queries — joining `app-lifecycle` rows back to their parent `runtime-run` row across the `run_id` column is a single positional `arrayElement` predicate, but no chstore helper exposes it yet. (b) Bridging the run_id into the Rust side so client-originated audit events (e.g. screenshot requests) attribute to the same run. (c) Periodic "still-running" heartbeats so a crashed process (no `stopped` row, no `shutdown` reap) is detectable from the audit trail alone.

### 2026-05-12 — M3 dock host (carousel → DockHost, demo fold reversed)

The M3 dock host (`runtime/dockhost`) replaces the M2 single-focus launcher in interactive mode. It wraps `egui_dock` (via the bindings' `c.DockArea` / `dock.Tab` iterators) and exposes one tile per open app, each with a `× Close tile` button and a runtime-managed `Manifest.WindowTitle()` tab label. egui_dock owns the split / drag / reorder layout state on the Rust side; cross-run persistence is deferred until DockState serde bindings exist.

**Carousel wiring.** `imzero2_demo_cli.go` now branches on `IMZERO2_SCREENSHOT_DIR`:

- *Screenshot mode* — keeps the legacy renderer-slice path with `decorateRenderer(adaptToRenderer(a), nil)` per --launch arg. Tour drivers (`RenderLoopHandlerTour`) continue running at top scope inside their host-created `c.Window`. Existing screenshots are unchanged.
- *Interactive mode* — builds a single `DockHost` over `app.DefaultRegistry`, seeds it with `--launch` refs (no --launch ⇒ empty dock), and installs an "Apps ▾" menu next to File / Layout via the new `decorateRenderer(inner, extraMenus)` parameter.

**Demo fold reversed.** Pre-M3, every `registry.Demo` was dual-registered as a `*DemoApp` into `app.DefaultRegistry` so the M2 launcher could list both top-level apps and individual demos in one catalog. Under M3 this would put 33+ demo entries in the DockHost's Apps menu — clutter without benefit. The fold is reversed: `registry.Register(d)` stops calling `app.Register(&DemoApp{...})`, the `DemoApp` AppI adapter is removed, and demos are reachable only through the widgets app's gallery body. The Apps menu shows six top-level entries: hn_explorer, leewaywidgets, regex_explorer, imztop, play, and the widgets Demo gallery. `WidgetsPkgPath` survives for the capslock-check package-mapping table.

**Widgets app repurposed.** The widgets package was both the M2 launcher *and* the demo gallery. With the dock host owning launcher duty, widgets becomes gallery-only: `Display` / `Title` flip to "Demo gallery" (icon = nf-fa-th grid glyph), the activeApp/runActiveApp/deactivateApp dispatch code is stripped, and `RenderLoopHandlerInteractive`'s body iterates `registry.All()` directly with the substring filter + collapsible-by-category layout it had before. The `launcherSelfId` constant is gone; the dock host doesn't need it.

**Per-tile mount/unmount.** Each `DockHost.Open` allocates a fresh `TileKeyT`, calls `registry.Open(id)` for a fresh AppI (factory-registered apps yield independent instances; singletons share), and queues `Mount` to run on the first Frame. `Close` (via the in-body `× Close tile` button or external `Close(key)`) marks the tile for reaping; reapClosed runs Unmount and removes the tile after the current frame. Mount errors are sticky — the tile body renders an error label so the user can dismiss it; Frame errors flow into per-tile labels too (one app's bug doesn't blank-screen the dock).

**Tile keys are monotonic and never reused.** egui_dock's persistent state keys on Tab id; reusing a closed tile's key would alias the resurrected state of a different app. The DockHost holds a `uint64` counter; opening the same AppId twice yields tiles `1` and `2`, not `1` twice.

**Known follow-ups.** (a) Cross-run dock layout persistence — needs DockState serde bindings + runtime.persist integration. (b) True multi-instance for singleton-registered apps — they share state across tiles; per-app migration to `RegisterFactory` + instance-state encapsulation is incremental work. (c) Filepicker / cap-grant overlay dialogs (M2.6 fsbroker, M2.3 capbroker) are not yet routed through the dock surface; today they need a host that knows about them, which the dock host gains in a follow-up.

### 2026-05-12 — Factory registration for multi-instance dispatch

The original `Registry` stored one `AppI` per `Manifest.Id` — a singleton model where opening an app twice in two tiles (or two CLI invocations) returned the same AppI and shared state. The M3 dock host wants real multi-instance: two tiles for the same `--launch a005` should run independently.

`Registry` now stores `(Manifest, AppCtor)` entries, where `AppCtor` is `func() (AppI, error)`. Two registration paths:

- **`Register(a AppI)`** — singleton. Internally stores `ctor = func() { return a, nil }`. Every `Open(id)` returns `a`. Pre-M3 apps keep this path; nothing changes for them.
- **`RegisterFactory(m Manifest, ctor AppCtor)`** — factory. Each `Open(id)` invokes the ctor for a fresh AppI. Apps that want isolated per-tile state migrate to this path *and* keep state on the struct rather than in package-level vars. The API is necessary but not sufficient — per-app state migration is the bigger work and happens incrementally as apps adopt the factory shape.

New methods: `Open(id) (AppI, error)` (dispatch), `LookupManifest(id) (Manifest, bool)` and `AllManifests() []Manifest` (metadata-only enumeration). The old `Lookup(id) (AppI, bool)` and `All() []AppI` remain as backward-compat shims that invoke the ctor — fine for singletons, slightly wasteful for factories.

This commit is the API foundation only. The DockHost (next commit) is the first consumer of `Open()`; apps continue to register as singletons until they have a reason to convert.

### 2026-05-12 — App↔window 1:1, sharpened `SurfaceE`, `Title`/`Icon` on `Manifest`

The original `SurfaceE` enum (`SurfaceHeadless | SurfaceDockTile | SurfaceFullCentral | SurfaceModal`) conflated two orthogonal concerns: *does the app have a window?* and *where does the window go?*. In practice this produced inconsistent app-side code — `SurfaceDockTile` apps and `SurfaceFullCentral` apps both ended up calling `c.Window(...)` or `c.PanelCentral()` themselves, depending on the author's taste, and `SurfaceModal` turned out to be dead code (the file picker and cap-grant prompts are *runtime services*, not registered apps, and render via the broker's overlay rather than as `AppI` entries).

**Sharpened enum.**

```go
SurfaceUnspecified SurfaceE = 0
SurfaceHeadless    SurfaceE = 1  // no window
SurfaceWindowed    SurfaceE = 2  // exactly one logical window
```

`SurfaceWindowed` is now the single non-headless surface. The relationship between an app and its window is **1:1**, and the **runtime creates and owns the window**. Apps must not call `c.Window(...)` or `c.PanelCentral()` from their `Frame()`; the host has already wrapped the rendering scope.

**Placement is host's call, not app's.** Whether the window is a docked tile (M3 `DockHost`), a floating panel, a maximised fullscreen claim (M2 launcher), or an overlay above the dock all live in the host implementation. The same app source runs unchanged across hosts. The earlier framing of `SurfaceFullCentral` as "a degenerate dock with one maximised tile and the dock chrome hidden" stands — that arrangement is the M2 launcher's current behaviour, but it is now a host-side choice driven by `Surface == SurfaceWindowed`, not a separate enum value.

**New `Manifest` fields.**

```go
Title string  // window title bar text; falls back to Display when empty
Icon  string  // optional Unicode glyph prepended verbatim to the title
```

A `WindowTitle()` helper on `Manifest` composes the displayed string as `"{Icon} {Title}"` (with sensible fallbacks). Hosts call it when constructing the runtime-owned window chrome.

**Backward-compatibility plan.** The contract changes in two commits, both dated 2026-05-12:

1. *Data layer* — this amendment plus the migration of all six top-level app manifests and the folded demo registry to `SurfaceWindowed`, the `Title`/`Icon` fields, and the `WindowTitle()` helper. Apps still call `c.Window`/`c.PanelCentral` internally at this point.
2. *Behavioural enforcement* — `carousel.adaptToRenderer` wraps `Frame()` in a `c.Window` from `Manifest.WindowTitle()`. Class-A apps (`hn_explorer`, `regex_explorer`, `leewaywidgets`, the widgets launcher catalog) strip their outer `c.Window`; class-B apps (`imztop`, `play`) migrate top-level panel calls (`c.PanelTop/Bottom/Left/Central`) to the `*Inside` variants so they compose inside the runtime-created window. After this commit, no registered app calls `c.Window` or top-level panel constructs from its `Frame()` body.

**Phasing impact.** `SD12` phases are unchanged. M3 absorbs the runtime-wraps-window plumbing and the per-app `c.Window`/`c.PanelCentral` stripping; M4 and M5 are untouched.

### 2026-05-15 — keelson namespace path migration (ADR-0035)

Runtime-tree path references in this ADR were swept from `public/thestack/runtime/...` to `public/keelson/runtime/...` as part of the keelson namespace introduction ([ADR-0035](./0035-keelson-namespace-introduction.md)). The decision recorded here (AppI/Manifest/Registry, cap-as-subject, §SD7 fsbroker, §SD10 capslock cross-check) is unchanged; only path strings reflect the new location. Per ADR-0026's own identity rule, `Manifest.Id` strings were also rewritten to match the new import paths: runtime-side AppIs (logviewer) follow the `keelson/runtime/...` paths; standalone apps moved to `apps/<name>/` (Step 5: imztop, capdemo, capinspector) follow the `apps/...` paths. Historical fact rows tagged by old AppIds are orphaned, accepted because the runtime is pre-stable. `status` and `reviewed-date` are deliberately not re-stamped. The capslock-check binary at `public/app/commands/capslock/` is preserved as a thin shim; the cross-check library lives at `public/keelson/security/capslock/`.

## References

- [ADR-0057](0057-demo-registry-and-drivers.md) — Demo registry pattern that `app.Registry` generalises.
- [ADR-0012](./0012-imzero2-collapsible-retained-bodies.md) — IDL collapsible drain semantics that constrain the dock host's tile-loop discipline.
- [ADR-0013](./0013-imzero2-stateful-widget-contract.md) — Stateful widget contract; apps must honour it inside their `Frame()`.
- [ADR-0014](./0014-imzero2-context-typed-ui.md) — Context-typed UI; `FrameContext.Egui()` follows this shape.
- [ADR-0005](0005-streaming-persisted-kafka-from-connect.md) — Kafka package whose API the `kafka.*` subject family wraps.
- [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) — imztop's existence as a CLI-launchable graphical app is one of the motivating examples.
- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — Remote-access direction that establishes the single-viewport invariant.
- [`doc/skills/leeway-advanced/SKILLS.md`](../skills/leeway-advanced/SKILLS.md) — Authoritative leeway vocabulary (plain values, tagged value sections, memberships, streaming groups, co-sections) used by `SD6`.
- `public/spinnaker/schema/spinnaker_schema.go` — `spinnaker.facts` mapping; the structural precedent `runtime.facts` follows.
- `public/spinnaker/sql/spinnaker_sql_ch.go` — Leeway → ClickHouse DDL emission used by `SD6`.
- `boxer/public/semistructured/leeway/ddl/clickhouse` — Code generator that `LoadRuntimeFactsMapping` reuses.
- `boxer/public/db/clickhouse/dsl` — DSL package available for cap-broker query-shape validation and for `play`-side fact querying.
- `boxer/public/streaming/persisted/kafka` — Kafka client wrapper backing the `kafka.*` subject family.
- [`public/thestack/imzero2/egui2/widgets/filepicker/`](../../public/thestack/imzero2/egui2/widgets/filepicker) — Powerbox substrate (`SD7`).
- [`google/capslock`](https://github.com/google/capslock) — Static-analysis tool used by `SD10`; deps.dev integration write-up at https://blog.deps.dev/capslock/.
- NATS authorization model — https://docs.nats.io/running-a-nats-service/configuration/securing_nats/authorization (subject-permission semantics that `SubjectFilter` matches).
- Sandstorm.io Powerbox — capability-broker prior art for the user-mediated grant flow.
- FreeBSD Capsicum — capability-mode prior art for resource-as-handle semantics.
- macOS Sandbox extensions — production-shipping precedent for file-dialog-as-cap-broker.
