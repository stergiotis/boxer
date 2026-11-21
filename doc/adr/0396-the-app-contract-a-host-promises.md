---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0396: The app contract a host promises: identity, registration, context capabilities, subjects

## Context

An app is a manifest and an `AppI` registered from `init` ([ADR-0026](./0026-app-runtime-and-capability-subjects.md)).
Several parts of that contract were implicit, and each failed quietly when
an app outside this module relied on it:

- **Identity.** The persist subject space (`runtime.persist.{alias}.>`),
  workstatelet documents and the window host's catalog lookups key an app
  by an alias derived from the last path segment of its id. Two apps whose
  ids end alike — the same directory name in two modules — collide, and
  renaming an app's directory orphans every stored value of its users.
- **Registration.** `app.Register` / `app.RegisterFactory` logged a warning
  and dropped the app on any error; an operations catalog failing its
  validation was withdrawn with a warning. Other registries panicked in
  `init`, returned an error the caller logged, or silently replaced an
  earlier entry. A mistake in another module's manifest showed up as an
  app missing from the launcher.
- **Context capabilities.** Focus, close, the operation gesture, the
  lifetime scope and the mount values were optional interfaces found by
  type assertion on the context. Every production host implemented all of
  them, so the assertion bought nothing, and a wrapper context — a host
  embedding a pane with its own storage — hid them all.
- **Subjects.** Runtime services sat under about eighteen unrelated roots
  (`llm.`, `adhoc.`, `fs.`, `task.`, `watchbill.` beside `runtime.`),
  wildcard constants did not cover their own families, and no root was
  set aside for a service another module defines. Capabilities were
  hand-written subject literals in every manifest.
- **Scenes.** A scene's host picked the ClickHouse persist backend like any
  other host, so a scene's writes reached the state of the person whose
  machine it ran on; one app guarded itself against that.
- **Conformance.** The checks that keep the in-tree apps honest — frame
  every app headless, the help corpus, the widget contract, capslock —
  ran only over this tree.

## Decision

1. **An app has an explicit, validated alias.** `Manifest.Alias` is
   optional; empty means the alias derived from the id exactly as before
   (`AppIdT.DerivedAlias`: the last `/` segment, every byte outside
   `[A-Za-z0-9_-]` replaced by `_`), so every existing app keeps its alias
   byte for byte and no stored state moves. A set alias must be one subject
   token of `[A-Za-z0-9_-]`. `Manifest.SubjectAlias()` is the alias a host
   uses; `app.AliasOf(id)` resolves a registered id, falling back to the
   derivation. Registration refuses a second app with the same alias, and
   the error names both ids. A module outside this one prefixes an alias
   with a token of its own (`acme_play`) when its derived alias collides;
   an app whose directory is renamed sets `Alias` to its old alias to keep
   its users' state.
2. **Registration fails loudly.** A registration made at package
   initialisation that fails is recorded in `keelson/runtime/registration`,
   and a host — `hostboot.Boot`, the tab host — refuses to start while any
   is recorded, listing every one. This covers the app registry (an invalid
   manifest, a duplicate id or alias, an operations catalog that fails
   validation, operations on a singleton registration), launch-kind probes,
   HTTP egress destinations, host services, notebook renderers and the log
   and trail viewer sources. Registration never panics and never replaces
   an earlier entry. A manifest declaring operations whose instance does
   not implement `OperationsAppI` is refused by `Registry.Open`, the one
   point where the instance exists.
3. **Topics stay a closed vocabulary, extended by `app.RegisterTopic`.**
   A module adds a topic at package initialisation; a duplicate, a
   malformed token or a vocabulary past the launcher's filter width is a
   registration failure. `app.Topics()` is the vocabulary in display order:
   the built-in topics first, then registered ones in registration order.
4. **Context capabilities are methods.** `MountContextI` gains
   `Lifetime()` and `MountValue(key)`; `FrameContextI` gains
   `WindowFocused()`, `CloseWindow(reason)` and
   `OperationGesture(op, args)` and loses `EguiScope()`. Each documents its
   absent result (nil scope, nil value, focused, not asked, a refusal). The
   optional interfaces `LifetimeI`, `MountValuesI`, `WindowFocusI`,
   `WindowCloseI`, `OperationsGestureI` and the helpers `LifetimeOf`,
   `MountValueOf` go. A wrapper embeds the interface it wraps, and so
   forwards methods added later; a fake embeds `*app.StaticMountContext` or
   `*app.StaticFrameContext`, which are the base implementations.
   `app.ContextOf` stays a function over `Lifetime()`: its result is
   derived, and a method would make every implementer re-derive it. The
   column-width store reaches an app as a mount value under a key
   `colwidth` owns, since `colwidth` imports `app`. Type assertion remains
   for what an app optionally implements — `PausableAppI`, `OperationsAppI`,
   `OperationsAsyncI`, `WorkingsetComposerI` — and for the host-side bus
   capabilities.
   An operation handler's call carries its context too
   (`app.OperationCall.Context()`): a query or external read runs under the
   request's own context; a command an agent queued runs under the window's
   lifetime with the request's values — its call identity — since the
   request was answered before the command ran; a gesture runs under the
   window's lifetime.
5. **Dead members go.** `FrameContextI.EguiScope`,
   `Manifest.BackgroundTickHz` (with its `keelson('apps')` column and the
   carousel's list column) and `SurfaceHints.ScreenshotStage` /
   `ScreenshotFlags` with `ScreenshotFlagsE` are removed; the `runtime/app`
   documentation states the contract as it holds, without milestones.
6. **Subjects follow one grammar, validated at registration.** The first
   token of every capability pattern a manifest declares is one of:
   `runtime` — services this module defines, as `runtime.<service>.…`;
   `app` — subjects an app owns, as `app.<alias>.…` (the operation
   subjects `app.<alias>.<instance>.op.<name>` are one family of it);
   `ext` — reserved for services another module defines, as
   `ext.<module-token>.<service>.…`. `_INBOX` stays the transport's. A
   pattern whose first token is a wildcard or another word is refused.
   Every runtime service moves under `runtime.`, and each service package
   exports one helper per capability taking the reason text
   (`llm.CompleteCap(reason)`), the shape `windowopen.OpenCap` set. A
   wildcard constant covers exactly its family (`…All` is `….>`).
7. **Scenes never touch live app state.** A host a scene launched — the
   `sceneLaunched` predicate: headless and under a `scene-` component mark
   — always selects the in-memory persist backend. Apps carry no scene
   guard of their own.
8. **A conformance kit any module runs.** The headless frame harness moves
   to `thestack/imzero2/egui2/egui2test`; `keelson/runtime/apptest` holds
   `ManifestOK`, `HelpCorpus`, `CatalogNotWithdrawn`, `FrameHeadless` and
   `FrameAll`, the last mounting every windowed app and running two frames
   under the discard harness, strict about duplicate ids and unbalanced
   pops. Tests run with the render-goroutine check on.
9. **A bus client is free functions plus capability helpers.** A service
   package exports its subjects, one `…Cap(reason)` per capability, and
   functions taking `ctx` and `app.BusI` first. A client type is kept only
   where it implements a runtime interface (`persist.Client` is an
   `app.StorageI`) or holds per-instance state (`task.ForApp`).

## Alternatives

- **A required `Manifest.Alias`.** Killed: every manifest in every module
  would change in the same commit for a value the derivation already gives
  correctly in almost every case, and a typo in the new field would orphan
  state exactly as a directory rename does.
- **Namespacing launch kinds at registration.** Killed: a kind name is
  written into every stored workingset record and launch config as the
  facts vocabulary kind the bytes claim, so renaming orphans them. Two
  apps declaring one kind is a feature ([ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md)).
  The collision that matters, two codecs for one kind, is refused by the
  launch-kind probe registry as a registration failure.
- **Panicking from `init` on a bad registration.** Killed: a panic in
  `init` names one failure and takes down every binary linking the
  package, a test binary included; collecting lets a host list them all and
  lets a test registry inspect them.
- **`MountContextBase` / `FrameContextBase` beside the static contexts.**
  Killed: the static contexts are the only implementations and already
  embeddable; a second pair of types would differ from them in name only.
- **A `ClientCaps()` per package returning every capability.** Killed: an
  app needs a subset, and the reason text is the app's, not the service's.
- **A client type per service.** Killed for request/reply services: a type
  wrapping a bus adds a constructor and a nil check and nothing else, and
  the capability belongs to the manifest, not to the client.
- **A disposable namespace for scenes on the live store.** Killed: it
  still writes to the person's server, and needs cleanup a crashed scene
  never runs.

## Consequences

### Positive

- A module outside this one finds a collision, a bad manifest or a bad
  catalog at its first start, listed in full.
- A wrapper or fake context inherits capabilities added later.
- A capability pattern says by its first token whose service it reaches.

### Negative

- Every consumer of a renamed subject, a removed interface or a removed
  manifest field changes in step; stored audit rows keep the old subject
  names, so a query over history spans both.
- A registration mistake that used to cost one app now stops the host.

### Neutral

- An alias change still orphans stored state; the field makes a directory
  rename survivable, it does not make an alias change one.

## Migration — Tier 1

- **Breaks.** `AppIdT.SubjectAlias` → `DerivedAlias` (hosts use
  `Manifest.SubjectAlias` / `app.AliasOf`); `persist.NewClient(bus, id)` →
  `persist.NewClient(bus, alias)`; the optional context interfaces and
  `LifetimeOf` / `MountValueOf` → context methods;
  `NewStaticFrameContext(mc, scope)` → `NewStaticFrameContext(mc)`; the
  removed manifest fields; `app.AllTopics` → `app.Topics()`; the renamed
  subjects.
- **Path.** The app-contract note under `doc/migration/` carries the
  old → new table and an app author's checklist.
- **Regeneration.** The package-props table and `doc/SURFACE.md` for the
  new packages; nothing crosses the FFI boundary.
- **Old shape.** Removed outright; no compatibility names are kept.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the registry tests, the manifest tests
  (alias, subject grammar), `apptest.FrameAll` over this tree's registry,
  and the carousel test that the registration record is empty.
- **What would fail.** A second app with an existing alias, a capability
  outside the grammar, a withdrawn catalog, or an app whose two headless
  frames push an id twice fails its test; a host started with a recorded
  failure exits with the list.
- **Gap.** A collision between modules that never link into one binary is
  not detectable, and need not be: aliases are per process.

## Choices for review

- `Alias` is optional with the derivation as default (not required).
- Launch kinds are left un-namespaced (see Alternatives).
- The static contexts are the embeddable bases; no new type names.
- `ContextOf` stays a function.
- A queued command's context takes cancellation from the window and values
  from the request, rather than the request's own (already ended) context.
- `ext` as the reserved root for other modules' services.
- Topics extended by registration rather than kept closed by policy.
- A registration failure stops the host rather than the app.

## Status

Proposed — awaiting review by the code owner.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the app runtime and capability subjects.
- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — topics.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the operations catalog.
- [ADR-0322](./0322-app-instances-drain-a-lifetime-scope-between-unmount-and-unload.md) — the lifetime scope; its `BackgroundTickHz` keep is undone here.
- [ADR-0386](./0386-panic-boundaries-at-runtime-owned-entry-points-and-what-the-in-process-bus-promises-a-requester.md), [ADR-0394](./0394-library-packages-do-not-import-the-app-platform.md), [ADR-0395](./0395-io-takes-a-context.md).
