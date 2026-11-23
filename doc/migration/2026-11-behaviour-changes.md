---
type: how-to
audience: maintainer of a module that depends on boxer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Each line names the decision it comes
> from; the list was assembled from those decisions and has not been walked
> through against a depending module from start to finish.

# Behaviour changes a depending module reacts to

The changes below compile unchanged in a depending module, so the compiler
does not point at them; a test, a deployment or the module's own gate does,
later. The API breaks and package moves of the same period are in
[2026-11-library-layering.md](./2026-11-library-layering.md); the context
now passed to store, persist and bus calls is in
[2026-11-io-context.md](./2026-11-io-context.md); the app contract —
alias, registration, context capabilities, subjects — is in
[2026-11-app-contract.md](./2026-11-app-contract.md).

## When to use this recipe

You upgrade a module past these changes. Read the list once and check each
line against what the module does; most lines need nothing unless the module
relied on the old behaviour.

## Processes, listeners and hosts

- **Native host fingerprint.** The native imzero2 host refuses a Go side
  built from another IDL; build both from one boxer version
  ([ADR-0278](../adr/0278-tab-mode-for-downstream-apps.md), update of
  2026-11-20).
- **Carrier bind gate and origin check.** The WebSocket carrier refuses a
  non-loopback bind unless opted in, refuses an upgrade from another site's
  page, and sends a content security policy
  ([ADR-0082](../adr/0082-imzero2-remote-session-auth-tls.md), update of
  2026-11-20).
- **Listeners bind through one helper.** Every Go listener binds through
  `loopback.Gate`; codelint CS018 reports a direct `net.Listen` or
  `http.ListenAndServe`. The helper binds the address it is given, `:8080`
  included, returns a failed bind instead of logging it, and lists the open
  listener in `keelson.listeners`. A listener marked `LoopbackOnly` refuses
  any other address (ADR-0082, updates of 2026-11-20 and 2026-11-21).
- **Test-lane knobs need a scene mark.** `BOXER_AGENT_TEST_GRANTS`,
  `BOXER_LLM_SCRIPT`, `BOXER_LLM_TRANSCRIBE_SCRIPT` and
  `BOXER_AGENT_ACTIONS_FILE` take effect only on a headless host carrying a
  `scene-*` component mark, which `scene.Launch` sets; a host driven by hand
  sets `BOXER_COMPONENT=scene-<name>`
  ([ADR-0298](../adr/0298-an-unattended-mode-the-host-decides-within-preset-bounds.md)
  for the test grants; no ADR records the mark).
- **`hmi.sh` opt-ins.** pprof, the flight recorder and a coverage directory
  are off unless `HMI_PPROF_ADDR`, `HMI_FLIGHT_RECORDER` or `HMI_GOCOVERDIR`
  asks for them (no ADR; `rust/imzero2/hmi.sh` reads them).
- **A host whose state table does not open keeps retrying.** Where the
  facts store reached ClickHouse and `boxer.persiststate` did not open at
  boot, the trail is held and a background retry releases it: with
  `BOXER_TRAIL_REQUIRED` set, model calls and egress fetches are refused
  only until the retry succeeds, not for the process's life, and the trail
  becomes durable although app state stays in memory. The span lands as a
  `trail.held` audit event. `trail.Recorder.Admit`'s refusal now ends with
  the reason the trail is not durable; match on the prefix, not the whole
  text
  ([ADR-0408](../adr/0408-a-host-whose-store-does-not-open-at-boot-holds-its-trail-and-keeps-retrying.md),
  proposed).
- **Tab-link allow-listing.** A tab link decides what a tab shows, not what
  it writes or reaches; the variables a link may set are listed in
  `tabhost.Options.LinkEnv`
  ([ADR-0391](../adr/0391-what-a-tab-link-may-decide.md)).

## Environment and secrets

- **Sensitive variables leave child processes.** extbin removes every
  `Sensitive` registry variable from a child's environment unless the
  program names it in `InheritSensitive` or sets `InheritAllSensitive`
  ([ADR-0118](../adr/0118-extbin-external-process-chokepoint.md), update of
  2026-11-20).
- **`NAME_FILE` companions.** Every `Sensitive` `NAME` has a `NAME_FILE`
  read when `NAME` is unset, so `Get` may return a file's contents. A
  module's own `NAME_FILE` declaration joins the companion when it is a
  `NewPath` with no default; a rule of the module's own about setting both
  reads `Lookup` or `Resolve`, not `Get`
  ([ADR-0009](../adr/0009-environment-variable-registry.md), updates of
  2026-11-20 and 2026-11-21).
- **Undeclared program variables.** An `OverrideEnv` or `InheritSensitive`
  entry naming a variable no package declared is logged at start, and
  resolving the program returns `extbin.ErrUndeclaredVariable` instead of
  panicking; `extbin.CheckDeclarations(t)` finds it in a unit test (ADR-0118,
  update of 2026-11-21).
- **`BOXER_CLICKHOUSE_LOCAL` chooses the pool's binary too.** With the
  variable set, the `clickhouse local` worker pool runs the binary it names
  even where a packaged `/usr/bin/clickhouse` exists; before, the packaged
  path won there and the variable reached only direct invocations
  ([ADR-0028](../adr/0028-chlocal-low-latency-sql-cap.md), update of
  2026-11-23).
- **Tombstones and start-up warnings.** A set variable the registry retired,
  or a set `BOXER_*` / `IMZERO2_*` variable nothing declares, is warned
  about once at start; a conflicting second declaration is logged as an
  error (ADR-0009, update of 2026-11-20).

## Runtime calls

- **Bus requests.** A request with no responder fails at once with
  `ErrNoResponders`, its wait is bounded by its timeout, and its handlers
  run on a goroutine of the request's own rather than on the requester's
  ([ADR-0386](../adr/0386-panic-boundaries-at-runtime-owned-entry-points-and-what-the-in-process-bus-promises-a-requester.md)
  SD4, SD5).
- **Panics returned by `task.Run`.** `Run` and `RunToCompletion` return a
  panic in the work as an error instead of re-raising it (ADR-0386 SD3).
- **Writes at stop.** Store and facts writes take the caller's context, so a
  write under a context cancelled at stop does not land; the last flush and
  close run under `context.WithoutCancel` of the producer's context
  ([2026-11-io-context.md § Writes at stop](./2026-11-io-context.md#writes-at-stop),
  [ADR-0395](../adr/0395-io-takes-a-context.md)).
- **Decode bounds.** An fffi2 read of a length past `DefaultMaxFieldLength`
  fails the read before allocating; a caller that sends more raises it with
  `SetMaxFieldLength` (no ADR).
- **Plot-state ageing.** implot drops the retained state of a plot no frame
  has drawn for 36000 frames, and `U64Edit` drafts age the same way; a plot
  drawn again after that starts from its defaults (no ADR; `retainFrames` in
  `implot` holds the age).
- **A number written against a word is a syntax error.** `nanopass.Parse`
  and `ParseCanonical` refuse `1A`, `00FROM`, `0b101` and `1_000`, with the
  position. ClickHouse reads each as one token — an identifier, or the
  numbers 5 and 1000 — and the grammars read a number and a separate word,
  so the pipeline used to send a statement with another meaning. A statement
  that relied on the spelling is written with a space, a backquoted
  identifier, or the decimal number (no ADR; the note beside
  `refuseNumberGluedToWord` records the lexer change that would accept
  them).

## Sealed data and labels

- **A play result stream's `Confined` is the label, `Sealed` the wall.**
  `Confined` on the stream `play.Client.ExecuteArrowStream` returns no
  longer says "the run read sealed data": it is false for a task's run
  that read only its own publications at TLP:CLEAR. A module that decides
  where a result may be written by it asks `Sealed` instead; one that
  labels its outcome keeps `Confined`
  ([ADR-0407](../adr/0407-an-exposed-dataset-a-program-on-this-machine-reads-an-ad-hoc-dataset-by-alias-and-token.md),
  proposed, section of 2026-11-23 on reading back).
- **A bundle is a task's own clear work only when its publisher asserts
  so.** An app that publishes a table for a task sets `FromNothingSealed`
  on the bundle (`playground.BundleSpec`, `adhocdata.BundleProvenance`)
  when it knows the rows derive from nothing sealed: its own label of
  what it computed from, never the tier of the call. Left unset, the
  default, the bundle is sealed data to every reader: the task reads it
  back labelled and `expose_dataset` refuses it, at whatever tier the
  publish was declared. A module that published on a task's behalf
  before this and expects the task to expose the result has to add the
  assertion; the service still overrides it for an input that is not
  the task's own clear publication (ADR-0407, same section).
- **A window's label may name the one task it does not apply to.** An
  app's `Confined` (declared with `appops.Set.Confined`) stays one label
  for every reader and needs no change. An app whose sealed content can
  be one task's own clear work declares `appops.Set.LabelFor` instead:
  the label for every reader, and the task whose calls are answered
  unlabelled. Play does. A module that reads `opengine.Engine.Confined`
  or a window's `Confined` in a listing gets the label for every reader,
  which is true for such a window (ADR-0407, same section).
- **A read answers whose the revision is.** `adhocdata.ReadAll` returns
  `OwnClear`. A module that reads a dataset on a task's behalf may leave
  its outcome unlabelled only for a true answer; one that does not look
  keeps labelling, as before. `sessiondata.Binding` and `DatasetInfo`
  carry the same answer (ADR-0407, same section).
- **A task exposes only what was asserted clear, and only while it is
  its own.** `expose_dataset` and `adhocdata.ExposeRequest` refuse a task
  a dataset whose publisher did not assert that it derives from nothing
  sealed, and a republish that is not the exposing task's own clear
  publication — not asserted, above TLP:CLEAR, the person's or another
  task's — ends an exposure the task made (ADR-0407, item 3 of *The
  wall*).

## Stored data and formats

- **Record-store schema verdict.** A store judges the live table by its
  columns and stamp; a check that requires an ok verdict refuses a table the
  verdict calls migratable or incompatible, and an executor that answers no
  catalog read sees every table as absent (`recordstoretest.CatalogExecutor`
  answers them)
  ([ADR-0387](../adr/0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md)).
- **Deduplication token and stamps.** A created table keeps an insert
  deduplication window and a stamp comment; a retried insert carries the
  token of its first attempt, so a wrapper executor must pass
  `InsertArrowWith` through (ADR-0387).
- **`schema apply` names what it may run.** `--yes` is removed: pass
  `--alter` where a script passed `--yes`, and `--create` to create a table
  that does not exist where its declaration carries the statements. The
  command then judges the tables again and exits 0 only when all are in
  line (3 pending, 4 refused, 5 unreadable), as the new `schema check`
  does; a script that ran `apply` and ignored a table left migratable now
  sees a non-zero exit
  ([ADR-0387](../adr/0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md),
  update of 2026-11-22).

  contract: cli/boxer schema apply --yes
- **ClickHouse floor.** The facts store, `storeexec.Connect` and the
  `clickhouse local` pool refuse a server or binary older than 26.8
  (`chversion.Floor`; no ADR beyond the README's stated floor).
- **Format version keys.** A notebook, a workstatelet and a scene name their
  format in their frontmatter; a document of a newer format is refused
  rather than misread
  ([ADR-0371](../adr/0371-sealed-notebooks-a-report-as-a-markdown-document-of-typed-cells.md),
  [ADR-0369](../adr/0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md),
  ADR-0248).
- **Generated headers.** Leeway output names its generator and ABI
  generation instead of a module version; regenerate once, and a drift check
  then holds across pin and workspace changes
  ([ADR-0394](../adr/0394-library-packages-do-not-import-the-app-platform.md)).

## Windows, widgets and scenes

- **A dock tab that appears later.** A `c.DockArea` tab declared only on
  some frames is placed in the leaf of its declared group, and that group's
  split is made again when none of the group is shown; it used to join the
  first leaf. A dock that counted on a late tab landing beside the root
  declares it in the root's group
  ([ADR-0404](../adr/0404-what-a-person-reads-in-a-window-a-stated-reader-one-way-to-draw-a-decision-and-the-chat-window-as-the-first-case.md)
  §SD8). The IDL moved with it: the dock gained `Closable`,
  `CloseRequested` and `HideTabBarWhenAlone`, the button `Extend`,
  `HoverText`, `ReserveFrame` and `Fill`; on 2026-11-23 it gained
  `CaptureUiClipRect`, which renumbers the procedures after it
  ([ADR-0270](../adr/0270-play-operations-catalog-and-agent-limits.md),
  update of 2026-11-23). A native host or a browser host
  built before the change refuses a Go side built after it, and the other
  way round; build both from one boxer version, as the fingerprint line
  under *Processes, listeners and hosts* says.

  contract: idl/fingerprint
- **Segmented bars.** `selector.StyleSegmented` draws its options under one
  outline, and the options' widget ids sit under that outline's scope: a
  test or a scene that reached an option by id reaches it by name. Captures
  and SVG goldens of every such bar change (ADR-0404 §SD5).
- **Questions on the host's cards.** The agent's request and confirmation,
  the moderator's question and the queue's cards are drawn by
  `widgets/decision`: the options run left to right with the affirmative
  first, an asker's stated reason sits below them, and a stale request
  shows its affirmative disabled with the reason. The buttons keep their
  names, so a scene that clicks one by name needs nothing (ADR-0404 §SD3,
  §SD4).
- **The phase chip's popup.** An auto-anchored `fsmview` popup opens beside
  its toggle, not over it; a scene that reached into the popup by position
  moves with it (no ADR; `popupAutoOffset` in `fsmview` holds the offset).
- **The chat window.** For a module that embeds the chat, mints chatlets or
  drives the window in a scene: the Simple view setting, its `simple-view`
  persisted key and the bar's Details fold are gone, and a chatlet
  document's `view` key is read and changes nothing; side views open and
  close from the View menu or from their tab, where there was a fixed
  column of tabs; a step in the transcript reads in words, so a scene that
  waited on a tool's name or on the endpoint line waits on the new wording
  or on the Send button; a menu item is pressed with `"pointer":true`
  (ADR-0404 §SD7;
  [ADR-0364](../adr/0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md),
  update of 2026-11-22).
- **A closable dock tab's close button.** It is named `Close <title>`, so a
  scene that finds the tab by a part of its title finds two nodes; match
  the name whole (ADR-0404 §SD8).
- **Layout invariant `cut`.** A scene run reports text that reaches past
  its window's right edge. The finding is advisory and fails no scene
  (ADR-0404 §SD6).

## The gate a depending module runs

- **DL013, CS018, CS019.** A package-qualified identifier in a Go fence of a
  document must resolve (DL013), a listener binds through the gate (CS018),
  and a package-level error is `errors.New`, or `fmt.Errorf` with `%w` over
  other sentinels (CS019, CS001). The gate is
  [ADR-0179](../adr/0179-downstream-consumption-gate-and-skeleton.md)'s;
  CS018 is ADR-0082's, CS019
  [ADR-0394](../adr/0394-library-packages-do-not-import-the-app-platform.md)'s
  and DL013 is described in
  [DOCUMENTATION_STANDARD](../DOCUMENTATION_STANDARD.md).
- **CS017 and CS020 do not reach it.** Both are registered for boxer's own
  package prefixes, so a depending module's gate does not report a rooted
  `context.Background()` the module added; review for it by hand.

## Checking the result

The module's own tests and gate, then a start of each host it ships with
the log level at warn: the start-up warnings above name what is still set
or declared the old way.
