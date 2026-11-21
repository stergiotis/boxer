---
type: how-to
audience: maintainer of a module that registers apps on the keelson runtime
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The rewrites below were applied to
> this repository; they have not been run against another module.

# How to follow the app-contract change

[ADR-0396](../adr/0396-the-app-contract-a-host-promises.md) makes the
contract between a host and its apps explicit: an alias that keys stored
state, registrations that stop the host when they fail, context
capabilities as methods, one subject grammar, and a conformance kit. No
stored table changes, and no app's alias changes unless it sets one. Bus
subjects change: every runtime service gains the `runtime.` root. The
context these calls take is in
[2026-11-io-context.md](./2026-11-io-context.md), and the period's changes
that compile unchanged in
[2026-11-behaviour-changes.md](./2026-11-behaviour-changes.md).

## When to use this recipe

You register apps with `app.Register` or `app.RegisterFactory`, implement or
wrap `app.MountContextI` / `app.FrameContextI`, declare `Manifest.Caps`,
or talk to a runtime service by subject, and upgrade past this change. The
symptoms of skipping it: a build failure naming a symbol below, a host that
refuses to start and lists registration failures, or a capability refused
as "outside the subject grammar".

## Checklist for an app author

1. **Registration.** Call `app.RegisterFactory(manifest, ctor)` from `init`
   and drop the error handling around `app.DefaultRegistry.RegisterFactory`:
   a refusal is recorded and stops the host. Book, prompt-book and help
   registrations in `init` go through `registration.Fail(name, err)`.
2. **Alias.** Leave `Manifest.Alias` empty unless the host reports a
   collision; then set it to a token prefixed with your module's name
   (`acme_play`). If you rename an app's directory, set `Alias` to the old
   alias. Key anything by `m.SubjectAlias()` or `app.AliasOf(id)`, never by
   `AppIdT.DerivedAlias()` of a registered app.
3. **Capabilities.** Build `Caps` from the services' helpers
   (`fsbroker.DialogReadCap(reason)`, `windowopen.OpenCap(reason)`,
   `chlocalbroker.ExecCap(pool, reason)`, …). A literal pattern starts with
   `runtime.`, `app.<alias>.`, or — for a service your module defines —
   `ext.<module-token>.<service>.`.
4. **Context.** Replace each type assertion on the context with the method
   (table below). Keep `app.ContextOf(mountCtx)` in a field set in `Mount`;
   delete any hand-written "tail context" helper it replaces. An operation
   handler does its onward I/O under `call.Context()`.
5. **Tests.** Add `TestConforms`: `apptest.ManifestOK(t, manifest)`,
   `apptest.HelpCorpus(t, id)`, `apptest.HelpNamesCatalog(t, id)` when the
   app has a catalog, and `apptest.FrameHeadless(t, app.DefaultRegistry, id,
   apptest.FrameOpts{Strict: true})`. A test that builds an app without
   `Mount` takes its context from `app.ContextOf(apptest.MountContext(t, id))`
   or `t.Context()`. Import the headless harness as `egui2test`; its
   `InstallRecording`, `Messages`, `FindOpcode` and `CountOpcode` read back
   what a frame emitted. An app that declares external programs adds
   `extbin.CheckDeclarations(t)`.
6. **Gate.** `gov gate` applies CS017 to `<module>/apps` and CS020 to the
   module; pass `--app-prefix`, `--ctx-prefix` or `--ctx-baseline` to change
   that.

## Old → new

### Identity and registration

| Old | New |
|---|---|
| `AppIdT.SubjectAlias()` | `Manifest.SubjectAlias()`, `app.AliasOf(id)`, `(*Registry).AliasOf(id)`; `AppIdT.DerivedAlias()` for an id with no manifest |
| — | `Manifest.Alias`, `app.ValidateAlias` |
| `persist.NewClient(bus, appId)` | `persist.NewClient(bus, alias)` |
| `persist.AliasOf(id)` | `app.AliasOf(id)` |
| `app.Register*` warns and drops a bad app | the failure is recorded in `keelson/runtime/registration`; `hostboot.Boot` and the tab host refuse to start |
| a catalog failing validation is withdrawn | the registration is refused |
| `Registration.OperationsDiagnostic`, `Registry.OperationsDiagnostic`, `appops.View.Diagnostic`, `keelson('operations').diagnostic` | removed |
| windowhost warns on operations without `OperationsAppI` | `Registry.Open` returns an error |
| `kindcheck.Register`, `httpegress.Register`, `hostboot.RegisterHostService`, `notebookview.RegisterRenderer` panic on a bad entry | they record it; `kindcheck.Add`, `httpegress.Add`, `hostboot.AddHostService`, `notebookview.AddRenderer` return it |
| `app.AllTopics` | `app.Topics()`; a module adds one with `app.RegisterTopic` |
| `app.Lookup`, `app.All`, `(*Registry).Lookup`, `(*Registry).All` | `LookupManifest` / `AllManifests`, or `Open` |

### Context capabilities

| Old | New | Absent |
|---|---|---|
| `app.LifetimeOf(ctx)`, `app.LifetimeI` | `ctx.Lifetime()` | nil |
| `app.MountValueOf(ctx, k)`, `app.MountValuesI` | `ctx.MountValue(k)` | nil |
| `ctx.(app.WindowFocusI).WindowFocused()` | `ctx.WindowFocused()` | true |
| `ctx.(app.WindowCloseI).CloseWindow(r)` | `ctx.CloseWindow(r)` | false |
| `ctx.(app.OperationsGestureI).OperationGesture(op, a)` | `ctx.OperationGesture(op, a)` | `*app.OperationRefusal` |
| `ctx.(colwidth.HostI).ColumnWidthStore()` | `colwidth.StoreOf(ctx)`; a host sets `colwidth.StoreKey{}` | nil |
| `FrameContextI.EguiScope()` | removed | |
| `app.NewStaticFrameContext(mc, scope)` | `app.NewStaticFrameContext(mc)` | |
| `app.OperationCall` | gains `Context()` and `WithContext(ctx)` | |
| `opengine.New(catalog, h)`, `Engine.Submit(op, req)` | `opengine.New(life, catalog, h)`, `Engine.Submit(ctx, op, req)` | |

A type that implements `MountContextI` or `FrameContextI` by hand gains the
methods, or embeds `*app.StaticMountContext` / `*app.StaticFrameContext`, or
the interface it wraps. A gallery tour that called `Frame(nil)` passes a
static frame context.

### Removed manifest members

`Manifest.BackgroundTickHz` (with `keelson('apps').background_tick_hz` and
the carousel list's `bg_tick_hz`, so the list's later columns move up one),
`SurfaceHints.ScreenshotStage`, `SurfaceHints.ScreenshotFlags`,
`app.ScreenshotFlagsE` and its constants.

### Subjects

Every subject keeps its name under the `runtime.` root; the Go constants
keep theirs, so code that names them changes nothing.

| Old | New |
|---|---|
| `llm.{describe,complete,cancel,transcribe}`, `llm.retain.*`, `llm.ration.*`, `llm.event.*` | `runtime.llm.…` |
| `adhoc.…` | `runtime.adhoc.…` |
| `fs.dialog.…`, `fs.handle.<uuid>.<op>`, `fs.appdata.…` | `runtime.fs.…` |
| `task.<id>.<verb>`, `task.>`, `task.*.cancel`, `task.list.inflight` | `runtime.task.…` |
| `watchbill.job.<op>`, `watchbill.wake`, `watchbill.changed` | `runtime.watchbill.…` |
| `externaledit.…` | `runtime.externaledit.…` |
| `net.http.fetch.<destination>` | `runtime.net.http.fetch.<destination>` |
| `keelson.query.<table>` | `runtime.keelson.query.<table>` |
| `windowhost.open`, `windowhost.raise` | `runtime.windowhost.open`, `runtime.windowhost.raise` |
| `clipboard.write`, `clipboard.resolve` | `runtime.clipboard.…` |
| `audio.out.open` | `runtime.audio.out.open` |
| `lading.publish.<op>` | `runtime.lading.publish.<op>` |
| `queryrun.progress…` | `runtime.queryrun.progress…` |
| `moderator.event.signal` | `runtime.moderator.event.signal` |
| `applet.store.save` | `runtime.applet.store.save` |
| `ch.local.exec.<pool>` | `runtime.ch.local.exec.<pool>` |
| `ch.server.read.<target>` (advisory) | `runtime.ch.server.read.<target>` |
| `sysmetrics.<host>.bundle` | `runtime.sysmetrics.<host>.bundle` (a NATS scraper and reader move together) |
| `coverage.<host>.sample` | `runtime.coverage.<host>.sample` |

Audit rows already stored keep the subjects they were written with.

### New capability helpers

`fsbroker.DialogReadCap`, `DialogWriteCap`, `DialogBundleCap`,
`DialogWatchCap`, `AppDataCap`; `clipboardbroker.WriteCap`, `ResolveCap`;
`audioout.OpenCap`; `adhocdata.PublishCap`, `RetractCap`, `ResolveCap`,
`ReadCap`, `BundlePublishCap`, `BundleRetractCap`, `BundleResolveCap`,
`EventsCap`, `BundleEventsCap`; `appletstore.SaveCap`;
`sysmetricsbus.PublishCap`, `SubscribeCap`; `coveragebus.PublishCap`,
`SubscribeCap`; `chlocalbroker.ExecCap(pool, reason)`, `ExecAllCap`.

### Moved packages and tools

| Old | New |
|---|---|
| `…/egui2/widgets/graphview/scenetest` (`scenetest.Install`, `InstallRecording`, `Messages`, `FindOpcode`, `CountOpcode`) | `…/egui2/egui2test` (same names; `Install` now binds the channel to the calling goroutine) |
| per-app help-corpus test functions | `apptest.HelpCorpus`, `apptest.HelpNamesCatalog` |
| codelint CS017/CS020 fixed to boxer's trees | `codelint.Scope`, `NewLinterFor`, `ModuleScope`; gate flags `--app-prefix`, `--ctx-prefix`, `--ctx-baseline` |

## Scenes

A host a scene launched keeps app state in memory: a scene no longer reads
or writes the state of the machine it runs on, and an app needs no guard of
its own against it. Scene hosts run with `IMZERO2_RENDER_GOROUTINE_CHECK`
on.
