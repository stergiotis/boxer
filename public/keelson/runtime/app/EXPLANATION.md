---
type: explanation
audience: app author
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# `runtime/app` — App author guide

The `runtime/app` package is the contract every program in the pebble2impl
monolith implements: a small interface (`AppI`), a static description
(`Manifest`), and a process-wide registry. ADR-0026 spelled out the
architecture; this file is the day-to-day guide for someone *writing* an
app — what to declare, where to register, which surface to pick, which
runtime services arrive over the bus, and what the M-phase migration
window will and won't do for you.

## Background

The monolith used to dispatch graphical programs by a numeric `appCode`
switch and ran CLI commands as `cli.Command`s with no shared lifecycle.
ADR-0026 lifted the concept of "app" into a first-class runtime type so
every launchable program — graphical or headless, internal or third-party
— can be inventoried, named, granted capabilities, and rendered in
predictable surfaces.

Three keystone decisions shape the contract you implement:

- **Identity is the Go import path.** `Manifest.Id` is the package path
  that owns the app's primary code (e.g.
  `github.com/stergiotis/pebble2impl/src/go/public/boxerstaging/spinnaker/hmi/play`).
  This is stable across implementation churn and naturally unique within a
  module graph. Demos that ship as one logical entry inside a larger
  package extend the path with a synthetic basename
  (`…/apps/widgets/table`).
- **Capabilities are subject filters.** Every external resource access
  (filesystem, ClickHouse, Kafka, inter-app pub/sub, persistence, audit)
  flows over a NATS-shaped subject bus. Your manifest declares which
  subject patterns you intend to publish to or subscribe from; the cap
  broker arbitrates anything you didn't declare.
- **One viewport.** The runtime supports many apps coexisting in one
  process, but only one OS window — the M3 window host is the
  long-term solution; in M1–M2 a launcher app switches focus among
  registered apps.

## How it works

### The interface

```go
type AppI interface {
    Manifest() (m Manifest)
    Mount(ctx MountContextI) (err error)
    Frame(ctx FrameContextI) (err error)
    Unmount(ctx MountContextI) (err error)
}
```

Lifecycle: `Mount` once → `Frame` N times → `Unmount` once.

- **`Mount`** is where you read environment variables, open connections,
  subscribe to bus subjects, and load persisted state. The `MountContextI`
  hands you a logger pre-tagged with your `AppId`, a `BusI` scoped to your
  declared caps, a `StorageI` for state (CH+leeway-backed in production),
  and a `Cancel()` channel that closes on host teardown.
- **`Frame`** runs once per frame while you hold focus. The
  `FrameContextI` adds an egui rendering scope (`EguiScope()` — typed as
  `any` until the M3 window host wires real scopes; today's hosts pass nil
  and apps fall back to the package-global `c.NewWidgetIdStack`). Headless
  apps return immediately.
- **`Unmount`** is your cleanup hook. The M1 launcher does not call it on
  swap-out (will be fixed when M3 lands); long-lived hosts will call it
  reliably.

### The manifest

```go
type Manifest struct {
    Id      AppIdT       // dotted Go import path
    Version string
    Display string       // human-readable label shown in launchers
    Title   string       // window title; falls back to Display
    Icon    string       // optional Unicode glyph prefixed to title
    Category string      // grouping in launcher UIs

    Surface      SurfaceE      // see Surfaces below
    SurfaceHints SurfaceHints  // initial size / screenshot stage

    Caps             []SubjectFilter // declared subject permissions
    BackgroundTickHz uint8           // unfocused tick rate; 0 = no tick

    PersistedKeys []string  // auto-managed state keys (M2.5+ via storage)
}
```

The manifest is read once at `Register` time and then treated as
immutable. Don't expose mutable state through it — the runtime caches the
returned value.

### Surfaces — and the 1:1 window invariant

`Manifest.Surface` tells the host whether your app has a window at all:

| Surface           | Meaning                                                                 |
|-------------------|-------------------------------------------------------------------------|
| `SurfaceWindowed` | Exactly one logical window. The runtime creates and owns the chrome.    |
| `SurfaceHeadless` | No UI; one-shot side-effects. Activated in M5 when `cli.Command` lands. |

**The relationship between an app and its window is 1:1.** A windowed
app declares one window via `Title` (text) and optional `Icon` (Unicode
glyph). The runtime composes the chrome — title bar, drag handle, close
button — and invokes your `Frame()` inside that window's body scope.
You **do not** call `c.Window(...)` or `c.PanelCentral()` from your
`Frame()`; the host has already wrapped you. Where the window goes — a
docked tile, a floating panel, a fullscreen claim — is the host's
decision and changes between hosts (launcher today, window host in M3,
fullscreen variant for embedded contexts). Same source, different
placement.

The `Title` field falls back to `Display` when empty; the `Icon` field
is prefixed verbatim as `"{Icon} {Title}"`. One Unicode codepoint is the
intended shape, but multi-character strings work for users who prefer
text labels over emoji.

The runtime enforcement lives in
[`carousel.adaptToRenderer`](../../../thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go):
when a `SurfaceWindowed` AppI is dispatched, its `Frame()` is invoked
inside a `c.Window` whose title comes from `Manifest.WindowTitle()` and
whose default size comes from `SurfaceHints.PreferredWidth/Height` (with
sensible fallbacks). The M3 window host replaces this with a per-app
child UI; the contract stays the same.

> **Migration history.** This contract was sharpened on 2026-05-12; the
> prior `SurfaceDockTile`/`SurfaceFullCentral`/`SurfaceModal` values are
> gone. The runtime-owns-the-window enforcement landed on the same date.
> Apps that previously created their own `c.Window` (`hn_explorer`,
> `regex_explorer`, `leewaywidgets`, the widgets launcher catalog) are
> now body-only; apps that previously used top-level `c.PanelTop/Bottom/
> Left/Central` (`imztop`, `play`) migrated to the `*Inside` variants so
> they compose inside the host-created window.

### Capabilities

`Manifest.Caps` is the up-front list of subjects you want to talk on.
Each entry is a `SubjectFilter`:

```go
SubjectFilter{
    Pattern:   "fs.dialog.read",
    Reason:    "import CSV/Parquet",
    Direction: CapDirectionPub,
    Sticky:    true,
}
```

Patterns follow NATS wildcards (`*` matches one token, `>` matches the
rest). The subject taxonomy (ADR-0026 §SD3) is:

```
fs.dialog.read         | fs.handle.{uuid}.{op}     | fs file access
ch.query.{db}          | ch.stream.{db}            | ClickHouse queries
kafka.produce.{topic}  | kafka.consume.{group}.…   | Kafka
app.{id}.event.{name}  | app.{id}.request.{name}   | inter-app pub/sub
runtime.persist.{alias}.{key}.{op}                 | state read/write/delete
runtime.cap.request                                | request additional caps
runtime.audit.{id}                                 | observe your own audit
```

You'll need at minimum `runtime.persist.{ownAlias}.>` to keep state and
`runtime.cap.request` if you ever ask for more permissions at runtime.

Your AppId is mapped to a NATS-token-safe alias via
`AppIdT.SubjectAlias()` — `github.com/.../play` → `play`,
`runtime.broker` → `runtime_broker`. Use the alias when constructing
your own subject patterns.

### Registration

Two registration paths. Pick based on whether two simultaneous instances
of your app would share state or not.

**Singleton — `app.Register(a)`** — the same AppI value is returned on
every dispatch. Suitable for apps that use package-level state
(`var ids = c.NewWidgetIdStack()`, `var clientCache map[K]V`) and can't
sensibly coexist with a sibling instance. This is the M1–M2 default.

```go
//go:build llm_generated_opus47

package myapp

import (
    "github.com/rs/zerolog/log"
    "github.com/stergiotis/pebble2impl/src/go/public/keelson/runtime/app"
)

func init() {
    a, err := app.NewLegacyFuncApp(app.Manifest{
        Id:       "github.com/example/myapp",
        Version:  "0.1.0",
        Display:  "My app",
        Title:    "My app",
        Icon:     "🛠",
        Category: "tools",
        Surface:  app.SurfaceWindowed,
    }, RenderLoopHandler)
    if err != nil {
        log.Warn().Err(err).Msg("myapp: register")
        return
    }
    app.Register(a)
}
```

**Factory — `app.RegisterFactory(manifest, ctor)`** — the host invokes
the ctor once per dispatch, yielding a fresh AppI instance with isolated
per-instance state. The M3 WindowHost honours this so two windows for the
same app run independently. To benefit, your AppI must keep state on
the struct, not in package-level vars.

```go
func init() {
    m := app.Manifest{ Id: "github.com/example/myapp", /* … */ }
    app.RegisterFactory(m, func() (a app.AppI, err error) {
        a = &myApp{ /* fresh state */ }
        return
    })
}
```

Ctors are invoked at dispatch time, not registration; keep them
lightweight (allocation only). Heavy I/O belongs in `Mount`.

The host imports your package for its side effect — the carousel pulls
in every M1-era app via blank imports in
`imzero2/egui2/demo/carousel/imzero2_demo_resolve.go`. New apps add an
import there until M3 generalises discovery.

### Migration window — LegacyFuncApp

If your existing entry point is `func() error` (the M1-era render-loop
shape), `app.NewLegacyFuncApp(manifest, renderer)` wraps it in an
`AppI`. `Mount`/`Unmount` are no-ops; `Frame` calls your renderer. The
legacy adapter discards the `FrameContextI` — you can't reach the bus
from a LegacyFuncApp.

Apps that need late binding (e.g., `play` reads ClickHouse credentials
from environment variables) implement `AppI` directly. See
`spinnaker/hmi/play/app_register.go` for the pattern — a `PlayLauncher`
struct constructs the inner `PlayApp` in its `Mount` body, where env
vars are guaranteed to be populated.

### The window host (M3)

In interactive mode (no `IMZERO2_SCREENSHOT_DIR`) the runtime's top-
level renderer is `runtime/windowhost.Inst`. Each open app renders
as a top-level `c.Window` (egui::Window — movable, resizable, titled,
with `Manifest.Title` / `Manifest.Icon` in the title bar):

- **Apps menu.** A top-bar "Apps ▾" lists every registered app
  (sorted by Id). Clicking an entry calls `windowHost.Open(id)`,
  which invokes the registered ctor and adds a new window on the
  next frame.
- **Window chrome.** egui draws the title bar (showing
  `Manifest.WindowTitle()`), the drag handle, the resize affordance,
  and the collapse triangle. Layout state (position, size, collapsed)
  persists across frames via egui's built-in `Memory`, keyed by the
  window's stable widget id.
- **Close button.** A `× Close` button at the top of each window
  body sets the closeReq flag; the window is unmounted at the end of
  the frame.
- **`--launch foo,bar`** at startup pre-opens those apps as windows.
  No `--launch` means no windows are open — an empty-state pane lists
  every registered app with an open button per app, so the user has a
  visible affordance to launch from.

In screenshot mode (`IMZERO2_SCREENSHOT_DIR` set) the carousel skips
the window host and uses the per-app tour driver path
(`adaptToRenderer` wraps `Frame` in a `c.Window`). This preserves
the existing tour screenshots without coupling them to the host
layout.

> **Design history.** M3 originally placed apps in `egui_dock` tabs
> (the `dockhost` package, snapshotted in commit `517fc46b`). The
> dock model's "one active tab per leaf" semantics meant clicking an
> app in the Apps menu produced a subtle UI signal (a new tab in the
> strip) that users perceived as a no-op. The design was reverted to
> per-app `egui::Window` on 2026-05-13 — see ADR-0026 Amendment
> 2026-05-13 for the rationale. The audit-trail / Apps-menu / empty-
> state / lifecycle / multi-instance machinery is unchanged.

### The demo gallery

The widgets app (`Manifest.Id = ".../apps/widgets"`) is the **Demo
gallery**: an ordinary AppI that, when opened in a window, renders
the 33+ demos registered with the local `registry.All()` grouped by
category, with a substring filter. Pre-C3 it was both launcher *and*
gallery (dual-registering each demo as a folded AppI in
`app.DefaultRegistry`); since C3 it's gallery-only, and demos are
reached exclusively through this window, not the WindowHost Apps menu.

### Run identity & audit trail

Every pebble2impl process boot is a *run*, identified by a 16-character
nanoid (the `run_id`). The carousel initialises it at startup via
`runtime/runinfo.Init()`, which:

- Allocates the run_id (or inherits `PEBBLE2_RUN_ID` from a parent
  process so wrapped invocations participate in the parent's run).
- Captures hostname, pid, Go version, VCS revision / modified flag /
  build info, and module path — using `boxer/public/observability/vcs`
  for the build metadata.
- Sets the `PEBBLE2_RUN_ID` env var so app code that prefers plain
  `os.Getenv` reads it without importing runinfo.
- Returns a logger wrapper so the carousel can tag `log.Logger` with
  `run_id` once; per-app loggers built via `app.AppLogger` inherit it
  through zerolog's context chain. **Your app's log lines carry
  `run_id` automatically** — no extra wiring needed.

The carousel then writes three kinds of auditable events to
`runtime.facts` via `factsstore.FactsStoreI`:

| Kind | Memb tag | When |
|------|----------|------|
| `runtime-started` | `MembKindRuntimeRun` | Once at process boot; carries hostname / pid / Go / VCS / module fields. |
| `app-lifecycle started` | `MembKindAppLifecycle` + `MembLifecyclePhase` | One per `WindowHost.Open` (window created). |
| `app-lifecycle stopped` | `MembKindAppLifecycle` + `MembLifecyclePhase` + optional `MembLifecycleStopReason` | One per tile close. `user-close` (× button), `mount-error` (sticky), `shutdown` (signal-driven reap). |

Each app-lifecycle row references its parent run via the `MembRuntimeRun`
mixed-low-card-ref (high-card-param = run_id bytes), so a
`runtime.facts` JOIN by run_id correlates all events from one process
boot in a single column scan.

The backend is chosen at startup by
`chstore.NewWithFallback(Defaults(), logger, 2s)`: localhost:8123 is
pinged with a 2s timeout, and on failure an `InMemoryFactsStore` is
used silently. The audit trail is *best-effort* — write errors are
logged at warn, but never block the runtime.

## Invariants

- **`Manifest.Id` is stable for the lifetime of an app.** Once an Id
  ships to users, renaming is a deprecation event (a new Id + a
  redirect). The Id is what `--launch` accepts, what audit logs record,
  and what the cap broker keys grants on. Treat it as durably public.
- **`Manifest.SubjectAlias()` is deterministic from `Id`.** Two distinct
  Ids must produce distinct aliases — the host enforces this at runtime
  by registering aliases as natural keys. If you pick an Id whose
  basename collides with another registered app, registration fails
  loudly.
- **`Frame` runs on the main goroutine.** The host pre-prepares the
  `WidgetIdStack` and the FFFI register subrange around each call; your
  code does *not* call `Prepare()` itself, `ctx.request_repaint()` is
  the runtime's call to make, and `AllocateUiAtRect` is forbidden
  inside layout containers (its absolute coordinates silently break
  `Vertical`/`Horizontal` flow).
- **The bus is the only sanctioned route to outside-world resources.**
  Direct `os.Open`, `clickhouse.OpenDB`, `net.Dial` etc. work today but
  will trip the capslock advisory in M2.7 and the eventual hard-fail in
  M4. Authoring apps to publish to the right subject family from day
  one is much cheaper than retrofitting.
- **One process, one OS viewport.** No spawning windows, no separate
  eframe instances. The remote-desktop direction (ADR-0024) needs the
  entire UI to render as one streamable surface — `egui::Window`s and
  the dock surface compose inside that one viewport.

## Trade-offs

### Multi-instance dispatch — opt-in via factories

`app.Registry` keys by `Manifest.Id`. Dispatch behaviour depends on the
registration path:

- **`Register(a)`** stores a singleton ctor that returns `a` on every
  `Open(id)`. Opening the app twice (two dock tiles, two `--launch`
  entries) yields the same AppI — state is shared, egui IDs collide,
  behaviour is undefined when both tiles render simultaneously. This is
  the default for migrated M1 apps.
- **`RegisterFactory(m, ctor)`** stores the ctor; each `Open(id)`
  invokes it for a fresh AppI. Two tiles run in isolation *iff* the AppI
  keeps state on the struct rather than in package-level vars. Apps
  that use package-level `c.WidgetIdStack`, package-level caches, or
  module-scoped goroutines need refactoring to fully benefit; the
  factory API is necessary but not sufficient.

Practical consequence: factory-registered apps with proper instance
state coexist as independent tiles. Singleton-registered apps still
collapse to one logical instance no matter how many tiles open them.
The WindowHost (M3) honours both shapes uniformly; per-app migration to
factory-with-instance-state happens incrementally.

### State is the runtime's responsibility, not yours

Your app does not own a filesystem path or a sqlite handle. Anything
you need to persist across launches goes through `MountContextI.Storage()`
— read/write `[]byte` blobs keyed by `(yourAlias, key)`. From M2.5
onwards the backing store is `runtime.facts` (CH+leeway); from M4
onwards the audit + state writes are reachable from other apps that
hold the right `app.{id}.>` subject permission.

That means: don't pickle to disk, don't open badger, don't shell out to
`mv ~/.config/myapp/state.json.new ~/.config/myapp/state.json`. The
Storage handle is the only sanctioned path; the runtime takes care of
backups, retention, and multi-app discovery.

### Hygiene, not security

The capability model is enforced by `google/capslock` plus code review,
not by memory or process isolation. A motivated app author can bypass
the runtime via `unsafe.Pointer` or `reflect.Value.Call` and reach raw
syscalls. The threat model accepts this — the boundaries exist to make
resource access *visible* and *consistent*, not to defend against
malicious code. ADR-0026 §SD10 documents the trade-off explicitly.

### Migration debt during M1–M2

The carousel still passes apps a `NoopBus` and `NoopStorage` in
M1–M2.5. Your `Mount` will see those if you check, and any
`Bus().Publish(...)` will fail. Real wiring lands when the window host
(M3) constructs proper per-app contexts. For now, design your `Mount`
to tolerate a noop bus — log the failure and degrade gracefully, or
guard the bus call behind a feature flag.

The legacy numeric codes (`--launch a005`) remain valid for one release
after M1 closes; new external integrations should use manifest Ids
directly.

## Further reading

- [ADR-0026](../../../../../../doc/adr/0026-app-runtime-and-capability-subjects.md)
  — architecture, subject taxonomy, phasing.
- [`registry.go`](./registry.go), [`manifest.go`](./manifest.go),
  [`app.go`](./app.go) — the contract surface and Go-doc reference.
- `runtime/inprocbus` — bus implementation (M2.1).
- `runtime/factsstore/chstore` — live-CH storage backend (M2.5c).
- Existing apps as exemplars: widgets (the launcher itself; demo-folder
  iteration), play (custom `PlayLauncher` AppI; env-var late binding),
  imztop (env-driven mode selection, simple LegacyFuncApp).
