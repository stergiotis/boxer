---
type: how-to
audience: maintainer of a module that imports boxer packages
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The rewrites below were applied to
> this repository; they have not been run against another module.

# How to follow the I/O context change

[ADR-0395](../adr/0395-io-takes-a-context.md) puts `ctx context.Context`
first on every I/O interface and removes the ctx-less twins. No stored table
and no wire format changes. A consuming module regenerates its generated
stores, then passes a ctx at each call the compiler reports.

## When to use this recipe

You import a boxer package, or generate a record store with boxer's
generator, and upgrade past this change. The symptom of skipping it is a
build failure: `not enough arguments in call to …`, an undefined
`BeginCtx`, `RequestWithTimeout` or `RequestCtx`, or a type that no longer
implements one of the interfaces below.

## Which ctx to pass

- Inside a function that has a ctx: that ctx.
- In a bus request handler: `msg.Context()`, the requester's; for a row that
  must land whether or not the requester still waits,
  `context.WithoutCancel(msg.Context())`.
- In an app with no call context (`Frame`, a button's handler): keep
  `app.ContextOf(mountCtx)` from `Mount` in a field and pass it. A test that
  builds the app without mounting it sets the field to `t.Context()`.
- At shutdown: the ctx `hostboot.Runtime.Close` and `OnClose` hand you.

## Generated stores

1. Regenerate every store generated with `public/storage/recordstore/gen`.
2. Rename: `X.BeginCtx(` → `X.Begin(`, `X.DeleteCtx(` → `X.Delete(`,
   `X.Ingest<Kind>Ctx(` → `X.Ingest<Kind>(`.
3. Every remaining `Begin(id, order[, env])`, `Delete(id, order)` and
   `Ingest<Kind>(order, rows)` takes the caller's ctx first.

## Interfaces and functions

| Old | New |
| --- | --- |
| `app.BusI.Request(subject, payload)` | `Request(ctx, subject, payload)` — ctx's deadline is the wait, the transport default without one |
| `app.BusI.RequestWithTimeout(subject, payload, d)` | `app.RequestWithin(ctx, bus, d, subject, payload)` |
| `(*inprocbus.Client).RequestCtx` / `RequestWithTimeout`, `(*natsbus.Client).RequestWithTimeout`, `(*app.NoopBus).RequestWithTimeout` | removed; `Request(ctx, …)` |
| `app.StorageI` `Get` / `Set` / `Delete` | ctx first |
| `persist.StorageBackendI` `Get` / `Set` / `Delete`; `(*persist.StoreBackend)` manage and state verbs | ctx first |
| `statestore.WorkingsetStoreI`, `statestore.ColumnWidthStoreI`, `colwidth.StoreI`, `appstate.StoreI`, `appstate.BatchDeleterI`, `attention.PersonStoreI` | ctx first on every method |
| `factsstore.FactsStoreI` `WriteGrant` … `WriteLaunch` | ctx first |
| `(*trail.Recorder)` `LlmCall` … `CoverageAgentApply` | ctx first |
| `auditrec.BatchI.Seal(id, …)` | `Seal(ctx, id, …)` |
| `task.TaskApiI.ListInflight()` | `ListInflight(ctx)` |
| `jackstay.FilesI` `ReadFile` / `WriteFile` / `AppendFile`; `LoadPlan(In)`, `OpenJournal(In)`, `ReadJournal(In)`, `(*Plan).Save(In)`, `(*PackManifest).SaveIn` | ctx first |
| client helpers over the bus: `clipboardbroker`, `attention`, `adhocdata`, `audioout`, `windowopen`, `ladingpublish`, `externaledit`, `playground`, `(*watchbill.Client)`, `(*fsbroker.AppDataClient)`, `(*appstate.Client)`, `play.PublishBundle`, `mdeditlink.Open` | ctx first |
| `windowhost.NewInst(reg, log)` | `NewInst(ctx, reg, log)` |
| `(*windowhost.Inst).ReapAll(reason)`, `(*servicehost.Inst).ReapAll(reason)` | `ReapAll(ctx, reason)` |
| `(*hostboot.Runtime).Close()`, `Reap()`, `OnClose(func())` | `Close(ctx)`, `Reap(ctx)`, `OnClose(func(ctx context.Context))` |
| `logbridge.NewSink(store, cfg)`, `(*Sink).Close()`; the closers of `InstallGlobal`, `NopCloser`, `loghost.Install` | `NewSink(ctx, store, cfg)`, `Close(ctx)`; closers take ctx |
| `(*heartbeat.Inst).Stop()`, `(*supervisor.Supervisor).Start()`, `(*taskmonitor.Monitor).Start()` | ctx first |
| `colwidth.New(store, opts)`, `filepicker.NewColumnWidths(store)`, `attention.AttachPersonStore(levels, store, log)`, `appletstore.Start(reg, …)`, `workstatelet.StartStore(reg, …)`, `agent.NewService(bus, …)`, `covtrail.New(opts)`, `sysmtee.Start(opts)`, `adhocdata.NewFollower(cfg)`, `adhocdata.NewDeferredFollower(cfg)` | ctx first — the object's lifetime |
| `(*play.PlayApp).SetCapabilities(bus, storage, log)`, `sqlapplet.NewEmbedded(def, cfg)`, `(*externaledit.Editing).Start(bus, …)` | ctx first |

New: `app.Msg.Context()`, `app.Msg.WithContext(ctx)`, `app.ContextOf(mc)`,
`app.RequestWithin`, `(*hostboot.Runtime).Context()`. `editexternally.Input`
gains `BusCtx`.

## Verify

`go build ./... && go vet ./...` in the consuming module.
