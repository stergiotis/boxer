---
type: explanation
audience: app author
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# `runtime/app` — App author guide

The `runtime/app` package is the contract every program in the boxer
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
  `github.com/stergiotis/boxer/apps/play`).
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
  process and one OS window: the window host draws each open app's
  window inside it.

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
- **`Frame`** runs on every render pass, for every open window, focused
  or not. The `FrameContextI` adds what a host offers during a frame:
  `WindowFocused()`, `CloseWindow(reason)` and `OperationGesture(op,
  args)`. Headless apps return immediately.
- **`Unmount`** is your cleanup hook; the host calls it at every closing
  edge, after `Cancel()` has closed.
- **Every capability is a method** of the two contexts, with a documented
  result for a host that lacks it (ADR-0396 §4). A wrapper that changes
  one capability embeds the interface it wraps and forwards the rest; a
  fake embeds `*app.StaticMountContext` or `*app.StaticFrameContext`.
- **Work that outlives a frame** counts itself into the instance's
  lifetime scope (`ctx.Lifetime()`, package `lifetime`, ADR-0322):
  the scope's work context ends with `Cancel()`, its tail context
  survives it, and the window host waits for the scope after `Unmount`
  and before it closes the bus, up to a deadline. Tasks spawned through
  `task.ForApp` are counted in until the worker calls `Done` or `Error`.

### The manifest

```go
type Manifest struct {
    Id      AppIdT       // dotted Go import path
    Version string
    Display string       // human-readable label shown in launchers
    Title   string       // window title; falls back to Display
    Summary string       // one line: what it does (required for windowed apps)
    Icon    string       // optional Unicode glyph prefixed to title

    Topics   []TopicT    // subject classification from the registered vocabulary
    Keywords []string    // free retrieval text; ungoverned by design
    Kind     KindE       // provenance: app / applet / demo

    Alias   string       // optional subject token; empty derives it from Id

    Surface      SurfaceE      // see Surfaces below
    SurfaceHints SurfaceHints  // initial window size

    Caps []SubjectFilter // declared subject permissions

    PersistedKeys []string  // state keys; the host adds the persist capability

    LaunchKind string  // config kind accepted over windowhost.open (ADR-0135)
    Workingset bool    // participates in workingsets (ADR-0148); needs LaunchKind
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
| `SurfaceHeadless` | No UI; a service the service host mounts at boot (ADR-0352).           |

**The relationship between an app and its window is 1:1.** A windowed
app declares one window via `Title` (text) and optional `Icon` (Unicode
glyph). The runtime composes the chrome — title bar, drag handle, close
button — and invokes your `Frame()` inside that window's body scope.
You **do not** call `c.Window(...)` or `c.PanelCentral()` from your
`Frame()`; the host has already wrapped you. Where the window goes — a
docked tile, a floating panel, a fullscreen claim — is the host's
decision and changes between hosts (the window host, a browser tab, a
fullscreen variant for embedded contexts). Same source, different
placement.

The `Title` field falls back to `Display` when empty; the `Icon` field
is prefixed verbatim as `"{Icon} {Title}"`. One Unicode codepoint is the
intended shape, but multi-character strings work for users who prefer
text labels over emoji.

`Summary` is the one line the launcher renders under `Display`
([ADR-0214](../../../../doc/adr/0214-launcher-as-app-summary-detail-recall.md)
§SD4), and `Validate` requires it of every windowed app. Write it for
someone choosing between your app and its neighbours on the same topic, not
for someone who already opened it: verb-first, one clause, around 60
characters, no full stop, and never opening with your own `Display` again.
The style budget is enforced by a tree-wide test rather than by `Validate`,
so an over-long line is a red build and not a silently dropped app.

The runtime enforcement lives in
[`carousel.adaptToRenderer`](../../../thestack/imzero2/egui2/demo/carousel/imzero2_demo_resolve.go):
when a `SurfaceWindowed` AppI is dispatched, its `Frame()` is invoked
inside a `c.Window` whose title comes from `Manifest.WindowTitle()` and
whose default size comes from `SurfaceHints.PreferredWidth/Height` (with
sensible fallbacks).

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

Your app is known on the bus by its alias, `Manifest.SubjectAlias()`:
`Manifest.Alias` when set, else the alias derived from the id
(`AppIdT.DerivedAlias()` — `github.com/.../play` → `play`,
`runtime.broker` → `runtime_broker`). The alias keys your stored state, so
it must not change: set `Alias` when your derived alias collides with
another app's (a module outside this one prefixes a token of its own,
`acme_play`), or to your old alias when you rename your app's directory
(ADR-0396 §1).

### Registration

Two registration paths. Pick based on whether two simultaneous instances
of your app would share state or not.

**Singleton — `app.Register(a)`** — the same AppI value is returned on
every dispatch. Suitable for apps that use package-level state
(`var ids = c.NewWidgetIdStack()`, `var clientCache map[K]V`) and can't
sensibly coexist with a sibling instance.

```go
func init() {
    app.Register(newSingleton())
}
```

**Factory — `app.RegisterFactory(manifest, ctor)`** — the host invokes
the ctor once per dispatch, yielding a fresh AppI instance with isolated
per-instance state, so two windows of the same app run independently. To benefit, your AppI must keep state on
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

Registration does not drop a bad app with a warning: a manifest that does
not validate, a duplicate id or alias, or an operations catalog that does
not validate is recorded with package `registration`, and a host refuses
to start while any is recorded, listing them all (ADR-0396 §2).

The host imports your package for its side effect — the carousel pulls
in every app in this module via blank imports in
`imzero2/egui2/demo/carousel/imzero2_demo_resolve.go`.

### Migration window — LegacyFuncApp

If your entry point is a `func() error` renderer,
`app.NewLegacyFuncApp(manifest, renderer)` wraps it in an
`AppI`. `Mount`/`Unmount` are no-ops; `Frame` calls your renderer. The
legacy adapter discards the `FrameContextI` — you can't reach the bus
from a LegacyFuncApp.

Apps that need late binding (e.g., `play` reads ClickHouse credentials
from environment variables) implement `AppI` directly. See
`spinnaker/hmi/play/app_register.go` for the pattern — a `PlayLauncher`
struct constructs the inner `PlayApp` in its `Mount` body, where env
vars are guaranteed to be populated.

### The window host

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

> **Design history.** The window host originally placed apps in `egui_dock` tabs
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

Every boxer process boot is a *run*, identified by a 16-character
nanoid (the `run_id`). The carousel initialises it at startup via
`runtime/runinfo.Init()`, which:

- Allocates the run_id (or inherits `BOXER_RUN_ID` from a parent
  process so wrapped invocations participate in the parent's run).
- Captures hostname, pid, Go version, VCS revision / modified flag /
  build info, and module path — using `boxer/public/observability/vcs`
  for the build metadata.
- Sets the `BOXER_RUN_ID` env var so app code that prefers plain
  `os.Getenv` reads it without importing runinfo.
- Returns a logger wrapper so the carousel can tag `log.Logger` with
  `run_id` once; per-app loggers built via `app.AppLogger` inherit it
  through zerolog's context chain. **Your app's log lines carry
  `run_id` automatically** — no extra wiring needed.

The carousel then writes three kinds of auditable events to
`boxer.facts` via `factsstore.FactsStoreI`:

| Kind | Memb tag | When |
|------|----------|------|
| `runtime-started` | `MembKindRuntimeRun` | Once at process boot; carries hostname / pid / Go / VCS / module fields. |
| `app-lifecycle started` | `MembKindAppLifecycle` + `MembLifecyclePhase` | One per `WindowHost.Open` (window created). |
| `app-lifecycle stopped` | `MembKindAppLifecycle` + `MembLifecyclePhase` + optional `MembLifecycleStopReason` | One per tile close. `user-close` (× button), `mount-error` (sticky), `shutdown` (signal-driven reap). |

Each app-lifecycle row references its parent run via the `MembRuntimeRun`
mixed-low-card-ref (high-card-param = run_id bytes), so a
`boxer.facts` JOIN by run_id correlates all events from one process
boot in a single column scan.

The backend is chosen at startup by
`chstore.NewWithFallback(Defaults(), logger, 2s)`: localhost:8123 is
pinged with a 2s timeout, and on failure an `InMemoryFactsStore` is
used silently. The audit trail is *best-effort* — write errors are
logged at warn, but never block the runtime.

### Workingsets — what a closed window leaves behind

A *workingset* is the named record of your app's user-authored working
state, and it is an instance of your own launch-config DTO: "the launch
that would reproduce this window". The host writes it at the closing edge
exactly as it writes a launch row at the opening edge, and hands it back
at a later plain open. ADR-0148 has the reasoning; this is what you
implement.

**Participation is three things.** A non-empty `LaunchKind` (the record
is an instance of that DTO, so an app without a launch config cannot
have a workingset — `Manifest.Validate` refuses the pair), `Workingset:
true` in the manifest, and one method:

```go
type WorkingsetComposerI interface {
    ComposeWorkingset() (cfg []byte, dirty bool, err error)
}
```

Also register with `RegisterFactory`: `Register` refuses a manifest that
declares `Workingset`, because a singleton instance can only consume a
config at its one `Mount`. A ctor that *returns* a shared instance is
indistinguishable from one that allocates, so that case is caught later
instead — the host refuses to deliver a config, restores included, to an
instance that already has a window, which is the refusal ADR-0135 already
applies to caller-delivered ones. Which mode an app registered under is
not in its manifest either, so the registry records it: `Registrations()`
reports it and `keelson('apps').registration` renders it.

**The host calls compose at close, before `Unmount`.** That ordering is
load-bearing: `Unmount` is where you release things, so composing
afterwards would read a torn-down app. Return the same bytes a caller
would send to open your app in this state (`buscodec.Encode` over your
launch DTO).

**`dirty` is the whole gate.** It means a person acted in *this* window —
an edit, a manual run, a deliberate view change. The host writes nothing
when it is false, and nothing anywhere byte-compares your bytes against
the stored record (a config carrying its own timestamp is never
byte-equal). This rule is what keeps a window someone opened with
arguments and closed untouched from overwriting the state a user built
by hand. Compose errors are logged and skip the save; they never disturb
the close.

**Scope: drafts in, caches and chrome out.** Carry what the user authored
or chose — buffers, draft parameters, the active view. Not result
histories or memos (recomputable), not window geometry (the host's), not
published artifacts. Content too large for a 64 KiB config stays under
your declared persist keys, and the record carries the reference.

**Restore arrives through the door you already have.** There is no second
delivery channel: the host routes the stored record through its own
`OpenWithConfig`, so you decode a launch config in `Mount` as usual.
What tells the two apart is `MountContextI.LaunchReason()`:

| Reason | What happened |
|--------|---------------|
| `plain` | No config was delivered. |
| `caller` | Another app opened this window with these arguments. |
| `restore` | The host recovered your own stored record. |

The documented precedence for adopters is **caller config > env override
> restored config > default**, which is why the reason exists — without
it you cannot place an environment override between the two config
tiers. Two rules worth copying from play:

- On `restore`, apply optional fields **unconditionally**. The
  caller-tier habit of "apply only when non-empty" is wrong for a
  restored record: an empty field there is a value the user arrived at,
  and skipping it silently resurrects what they cleared.
- Leave alone whatever a restored record composes by construction rather
  than by intent. play composes `AutoRun` false always — restoration is
  not re-execution — so its restore tier does not touch the `AutoRun`
  decision at all.

**Where the record lives.** `boxer.facts`, beside the launch rows, keyed
by (app id, name). v1 wires exactly one name, `default`. Restored opens
are audited as launches whose caller is `runtime.workingset`, so "which
windows opened from restored state" is a facts query. Rows are
append-only — the trail is the history. With ClickHouse down the facts
store falls back to memory and workingsets last only for the process;
that is the same best-effort stance as the audit trail, not a bug.

`apps/play` is the reference adoption: `ComposeLaunch` /
`WorkingsetDirty` on `PlayApp`, `ComposeWorkingset` on its launcher, and
a reason-aware precedence chain in `Mount`.

## Invariants

- **`Manifest.Id` is stable for the lifetime of an app.** Once an Id
  ships to users, renaming is a deprecation event (a new Id + a
  redirect). The Id is what `--launch` accepts, what audit logs record,
  and what the cap broker keys grants on. Treat it as durably public.
- **`Manifest.SubjectAlias()` is stable.** It is `Alias` when set, else
  derived from `Id`; registration refuses an alias another app holds,
  naming both ids. Changing it orphans the app's stored state.
- **`Frame` runs on the main goroutine.** The host pre-prepares the
  `WidgetIdStack` and the FFFI register subrange around each call; your
  code does *not* call `Prepare()` itself, `ctx.request_repaint()` is
  the runtime's call to make, and `AllocateUiAtRect` is forbidden
  inside layout containers (its absolute coordinates silently break
  `Vertical`/`Horizontal` flow).
- **The bus is the only sanctioned route to outside-world resources.**
  Direct `os.Open`, `clickhouse.OpenDB`, `net.Dial` etc. compile, and the
  capslock comparison of declared capabilities against real calls
  reports them. Authoring apps to publish to the right subject family
  from day one is much cheaper than retrofitting.
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
  behaviour is undefined when both tiles render simultaneously.
- **`RegisterFactory(m, ctor)`** stores the ctor; each `Open(id)`
  invokes it for a fresh AppI. Two tiles run in isolation *iff* the AppI
  keeps state on the struct rather than in package-level vars. Apps
  that use package-level `c.WidgetIdStack`, package-level caches, or
  module-scoped goroutines need refactoring to fully benefit; the
  factory API is necessary but not sufficient.

Practical consequence: factory-registered apps with proper instance
state coexist as independent tiles. Singleton-registered apps still
collapse to one logical instance no matter how many tiles open them.
The window host honours both shapes uniformly.

### State is the runtime's responsibility, not yours

Your app does not own a filesystem path or a sqlite handle. Anything
you need to persist across launches goes through `MountContextI.Storage()`
— read/write `[]byte` blobs keyed by `(yourAlias, key)`. The backing
store is `boxer.persiststate` where ClickHouse is reachable and memory
otherwise; a host a scene launched always uses memory (ADR-0396 §7).

That means: don't pickle to disk, don't open badger, don't shell out to
`mv ~/.config/myapp/state.json.new ~/.config/myapp/state.json`. The
Storage handle is the only sanctioned path; the runtime takes care of
backups, retention, and multi-app discovery.

For the state a user *authored* — buffers, drafts, the active view — the
storage handle is the lower layer, not the interface you want: see
Workingsets above. Raw keys remain right for content too large or
complex to sit in a launch config, which the record then references.

### Hygiene, not security

The capability model is enforced by `google/capslock` plus code review,
not by memory or process isolation. A motivated app author can bypass
the runtime via `unsafe.Pointer` or `reflect.Value.Call` and reach raw
syscalls. The threat model accepts this — the boundaries exist to make
resource access *visible* and *consistent*, not to defend against
malicious code. ADR-0026 §SD10 documents the trade-off explicitly.

## Further reading

- [ADR-0026](../../../../doc/adr/0026-app-runtime-and-capability-subjects.md)
  — architecture, subject taxonomy, phasing.
- [ADR-0135](../../../../doc/adr/0135-app-launch-requests.md) — launch
  configs, the `windowhost.open` subject, `kindcheck`.
- [ADR-0148](../../../../doc/adr/0148-app-workingsets.md) — workingsets:
  the compose contract, the intent gate, the restore precedence.
- [`registry.go`](./registry.go), [`manifest.go`](./manifest.go),
  [`app.go`](./app.go) — the contract surface and Go-doc reference.
- `runtime/inprocbus` — the in-process bus.
- `runtime/persist` — the state service behind `StorageI`.
- Existing apps as exemplars: widgets (the launcher itself; demo-folder
  iteration), play (custom `PlayLauncher` AppI; env-var late binding),
  imztop (env-driven mode selection, simple LegacyFuncApp).
