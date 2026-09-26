---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Keelson wasm frame cost — logbook

Chronological, append-only record of runs of the
[keelson wasm frame cost](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-09-25 — M0, the spike — wasip1 is the target; the bridge cost is the flush count; Go under wasm is about 4× native

- **Build under test:** boxer at the commit that adds this entry, over
  `410fdf8b`; Go 1.27.0, rustc 1.96.1, Node 23.4, Ungoogled Chromium 144.
- **Environment:** a handheld-class AMD custom APU, 8 hardware threads,
  14 GiB shared memory, Linux 6.11. **Not idle**: other sessions ran
  throughout, and the run was stopped by memory pressure during its last
  (Firefox) arms after every other arm had completed; the environment file
  was written by hand afterwards.
- **Attempted:** M0 — 2 scenes × (native in-process, native pipe eager and
  lazy, 2 wasm targets × (in-process, bridge eager, bridge lazy) in Node,
  the same bridge arms in Chromium and Firefox). 28 of 36 cells completed;
  the 8 Firefox cells produced nothing.
- **Hypotheses:** H1 confirmed — the wasip1 binary ran the unmodified
  `application` package over fd 0/1 against a JS shim with no Go-side
  change. H2 refuted — in-process, Go under V8 is about 4× native on both
  scenes and both targets, above the 1.5–3× ADR-0077 assumed. H3 confirmed
  and quantified — a per-message flush costs about 30 µs on `GOOS=js` and
  1.3 µs on `GOOS=wasip1`; deferring flushes to the next blocking read
  brings both within about a millisecond of the in-process arm, and js keeps
  a residual cost of about 60 µs per fetch read.
- **Findings:** the competence vault is not in the working tree, so every
  finding anchors at the toolbelt root with a proposed slug.
  - **[pain boxer-toolbelt → proposed:fffi2-transport / performance-efficiency.time-behaviour / S2]**
    `InlineIoChannel` flushes after every message; natively that is 1.3 µs a
    message, 11 ms of a 14 ms frame at 8 533 messages, and the same flush
    is what makes the js bridge unusable. Deferring it to the next blocking
    read (`SetDeferFlush`, off by default) removes it on every arm
    (evidence: runs/2026-09-25-phase0-spike/results.tsv, the `pipe-eager`
    against `pipe-lazy` rows).
  - **[pain boxer-toolbelt → proposed:go-wasm-codegen / performance-efficiency.time-behaviour / S2]**
    the in-process arms put Go under wasm at 3.8–4.2× native per frame; the
    bridge is not the long pole once flushes are deferred (evidence: the
    `inproc` rows).
  - **[pain boxer-toolbelt → proposed:imzero2-bindings / performance-efficiency.time-behaviour / S3]**
    a widget id used twice logs a warning per widget per frame; a first
    version of the `labels` scene did this 3 400 times a frame and the log
    writes dominated every wasm arm. The command now sets the global log
    level to error for a run. Evidence is in git history before this entry's
    commit, not in the run dir.
  - **[pain boxer-toolbelt → proposed:imzero2-demo-registry / functional-correctness / S3]**
    the `sccmap` demos read the filesystem, so their frame differs between
    native and wasm; a gallery arm is only comparable across targets once
    they are left out (evidence: per-demo message counts in this session, not
    kept; the exclusion is in `wasmspike/main.go`).
  - **[broken boxer-toolbelt → proposed:headless-browser-harness / reliability / S3]**
    Firefox 146 from flatpak started headless but the report server saw no
    request from it within the timeout; cause not found at the time — the
    next entry has it (evidence: runs/2026-09-25-phase0-spike/environment.md).
  - **[pain boxer-toolbelt → proposed:go-wasm-host-shims / functional-completeness / S4]**
    Go's wasip1 runtime needs `poll_oneoff` for timers and a dozen file
    imports that a program without a filesystem never calls; the shim
    honours clock subscriptions and answers the rest ENOSYS. Under `GOOS=js`,
    a Go timer still scheduled at exit makes `wasm_exec.js` throw; the host
    clears them (evidence: harness/bridge.js).
  - **[positive boxer-toolbelt → proposed:keelson-browser-wasm / portability / —]**
    `application`, the bindings and the `idsshowcase`, `leewaywidgets` and
    `logdemo` demo packages build for js and wasip1 unchanged; `widgets`
    does not (go-findfont via go-graphviz) and neither does anything on the
    sealed-file path.
- **Outcome:** ADR-0077's gate holds for a gallery-class frame on wasip1 in
  Chromium (2.8 ms with deferred flushes) and fails for an 8 500-message
  frame (12.8 ms), where the Go side's own wasm cost is the limit. The
  `GOOS=js` target is out unless flushes are deferred, and stays 1.3× the
  wasip1 arm even then. Next: M1, a real app frame's message count.
- **Results:** runs/2026-09-25-phase0-spike/results.tsv, raw reports under
  `raw/`.

## 2026-09-25 — M0 on a second machine — the 4× holds on a desktop-class CPU; Firefox measured; the bridge cost scales with the CPU

- **Build under test:** the same tree as the entry above, packed on the
  handheld with `measure.sh --pack` (`build-info.txt` in the run) and run
  from the pack; Node 24.21, Firefox 149 (snap), no Chromium available.
- **Environment:** a desktop-class AMD Ryzen AI MAX+ PRO 395, 32 hardware
  threads, 115 GiB, Linux 7.0, **idle** (load 0.3–2.5), Node installed from
  the official tarball into the user's home; no display.
- **Attempted:** M0 — 2 scenes × (native in-process, native pipe eager and
  lazy, 2 wasm targets × (in-process, bridge eager, bridge lazy) in Node,
  the same bridge arms in Firefox). All 26 cells completed on the third
  attempt; the first two lost their Firefox cells to the profile-directory
  finding below.
- **Hypotheses:** H2 refuted again — Go under wasm is 3.8× (gallery) and
  4.3× (labels) native in-process here, the same ratio as on the handheld,
  so the ratio is the codegen's, not the machine's. H3 confirmed with the
  costs scaled by the CPU: a per-message flush is 0.26 µs natively, 0.5 µs
  on the wasip1 bridge and 9.3 µs on the js bridge here, against 1.3, 1.3
  and 30 µs on the handheld. H4 refuted for this workload — Firefox's
  SpiderMonkey lands within about 15 % of Node's V8 on the same arms, read
  from wall time per frame (its worker clock is 1 ms coarse).
- **Findings:**
  - **[pain boxer-toolbelt → proposed:headless-browser-harness / usability.operability / S3]**
    a sandboxed browser cannot open a throwaway profile under a path its
    sandbox hides: a snap sees the home directory but not its hidden
    entries (`~/.cache` failed, `~/kwfc` worked), the Firefox flatpak sees
    only `~/Downloads`. The symptom is a browser that starts and never
    fetches the page. `measure.sh` now takes `PROFILE_DIR` and defaults to a
    visible directory under home (evidence: run.log of the failed attempts,
    not kept; the fix is in measure.sh).
  - **[pain boxer-toolbelt → proposed:headless-browser-harness / functional-correctness.accuracy / S4]**
    Firefox rounds `performance.now` in a worker to 1 ms, so the Go side's
    per-frame p50 columns for Firefox are quantised to whole milliseconds and
    its Sync column reads 0; the run's `wall_ms` over 330 frames is the
    usable per-frame figure for that browser (evidence: the `firefox-*` rows
    of runs/2026-09-25-ryzen-ai-max/results.tsv).
  - **[positive boxer-toolbelt → proposed:keelson-browser-wasm / portability / —]**
    a pack built on one machine ran unchanged on another with only Node and
    a browser present; the native binaries ran too since both are
    x86_64 Linux.
- **Outcome:** on this machine ADR-0077's gate holds for both scenes on
  wasip1 with deferred flushes (0.9 ms and 4.8 ms in Node, about 1.5 ms
  and 5 ms in Firefox) and for the gallery scene even with per-message
  flushes; the js target with per-message flushes stays out (5.7 ms and
  84 ms). The handheld's failing 8 500-message frame is a handheld result.
- **Results:** runs/2026-09-25-ryzen-ai-max/results.tsv, raw reports under
  `raw/`.

## 2026-09-25 — M1, real play frames — about 180 messages a frame; the bytes are one deferred body; the lever is Go's own work

- **Build under test:** the same tree, plus `fffi2stub tee` (the counting
  pass-through) and the two M1 scenes under `scenes/`; the CPU-rasterizer
  headless client built the same day; the scene runner from the tree.
- **Environment:** the handheld again (8 threads, not idle, `powersave`
  governor), the local ClickHouse server; 17 of the tour's 80 scenes skipped
  for want of `anchor.facts`, which is empty here.
- **Attempted:** M1 — the two M1 table scenes (200 and 1 000 rows) and the
  whole play tour, each scene's frames counted by the tee; four captures
  read for the app's own Go and Rust frame times.
- **Findings:**
  - **[positive boxer-toolbelt → proposed:imzero2-deferred-bodies / performance-efficiency.resource-utilization / —]**
    a play frame is about 180 messages regardless of screen (159–276 over
    63 scenes) because the dock's content is one deferred body; 80–95 % of
    a frame's bytes are that one message (evidence:
    runs/2026-09-25-m1-play-frames/scenes.tsv and opcodes/).
  - **[pain boxer-toolbelt → proposed:keelson-browser-wasm / performance-efficiency.time-behaviour / S2]**
    the wasm cost of a real frame is Go's own per-frame work times the
    in-process ratio, not the transport: the handheld's native Go frame is
    2.9–7.6 ms on four play screens, so about 12–30 ms under wasm before
    the Rust half (evidence: the status bars of the tour's captures, figures
    in README §0; not a wasm measurement).
  - **[pain boxer-toolbelt → proposed:trial-harness / functional-correctness / S3]**
    the spike's `labels` scene was a poor proxy: forty times the messages of
    a real frame, a fifth of its per-message Go work; the gallery scene is
    the closer one (evidence: scenes.tsv against results.tsv of the spike
    runs).
  - **[pain boxer-toolbelt → proposed:imzero2-scene-runner / usability.operability / S4]**
    the runner ends the client with a signal, so a pass-through that reports
    at end of stream reports nothing; the tee writes its per-frame and
    per-opcode tables incrementally and announces the report path in the
    host log, which is how a tour's reports are matched to scenes (evidence:
    tmp/scenes/logs, not kept; the mechanism is in fffi2stub/src/main.rs).
  - **[pain boxer-toolbelt → proposed:imzero2-metrics / functional-completeness / S4]**
    the app's per-frame metrics carry bytes but not messages, and the only
    way to read Go and Rust frame times out of a headless run was the status
    bar in a capture; a messages-per-frame counter in `metrics` and a
    machine-readable frame-metrics dump would make M1 a one-liner.
- **Outcome:** message count is not the lever for real frames; §0 now says
  what a play frame is and what its wasm cost would be under this trial's
  ratio. Next: M2 (the Rust half under wasm) and a profile of one native
  Table frame's Go side.
- **Results:** runs/2026-09-25-m1-play-frames/ (scenes.tsv, opcodes/, the
  two M1 scenes' per-frame tables, environment.md).

## 2026-09-25 — drill-down and two follow-ups — the 4× is codegen, not GC or knobs; deferred flush shipped; play's table cells retained across frames

- **Build under test:** the same tree, plus `wasmspike -cpuprofile`; play
  launched by hand with `--pprofCpuOutputFile` for the frame profile.
- **Environment:** the handheld, `powersave` governor; the GOMAXPROCS and
  knob arms on a loaded machine, the cache A/B on a quiet one (load below
  5, 240 frames each, the same scene run twice with the cache switched by
  a constant).
- **Attempted:** (1) explain the 4×: GOMAXPROCS=1, GOGC=off, the two
  `GOWASM` features and `-gcflags=all=-B`, CPU profiles of the in-process
  arm under Node and natively; (2) a native profile of play's Table frame;
  (3) deferred flush as the channel default; (4) a cache that replays the
  master table's cells while nothing they depend on changes.
- **Hypotheses:** "lost GC parallelism" refuted — one processor costs
  nothing natively. "Codegen knobs" refuted — under 10 %. "The wasm profile
  has a hot spot" refuted — it is flat across marshalling, allocation,
  maps and pools, the same shape as native. The 4× is Go's wasm code
  generation and runtime model.
- **Findings:**
  - **[pain boxer-toolbelt → proposed:go-wasm-codegen / performance-efficiency.time-behaviour / S2]**
    the ratio is uniform and structural; only doing less work per frame
    moves it (evidence: runs/2026-09-25-go-wasm-drilldown/results.md and
    the profiles beside it).
  - **[pain boxer-toolbelt → proposed:imzero2-etable-cells / performance-efficiency.time-behaviour / S2]**
    play's Table frame spends 1.6 of 2.6 ms of Go emitting about 2 000
    visible cells at five opcodes and two closures each; the strings are
    not the cost (evidence: pprof-play-table.txt).
  - **[pain boxer-toolbelt → proposed:go-gc-pressure / performance-efficiency.resource-utilization / S3]**
    GC scan and allocation are about a third of play's CPU natively and
    40 % of the gallery frame (GOGC=off arm); under wasm the collector runs
    inside the frame.
  - **[pain boxer-toolbelt → proposed:codeview-memo / performance-efficiency.time-behaviour / S3]**
    the SQL parser takes 20 % of play's idle CPU: the Docs pane's markdown
    re-lowers its SQL code blocks through `codeview.PrepareSql` and the memo
    misses every time, and `ExtractParamSlots` parses the editor's query on
    every frame (evidence: pprof-play-table.txt, the cumulative section).
  - **[pain boxer-toolbelt → proposed:sysmetrics-sampling / performance-efficiency.resource-utilization / S4]**
    the system-metrics producer's per-second `/proc` walk is 12 % of the
    process's CPU on a machine with many processes (same evidence).
  - **[positive boxer-toolbelt → proposed:fffi2-transport / — / —]**
    deferred flushing is now `InlineIoChannel`'s default; the fffi2 tests,
    eight play tour scenes and the M1 scenes pass under it. On play's
    180-message frame the status bar does not move; the gain is for
    message-heavy widgets and the in-page bridge.
  - **[positive boxer-toolbelt → proposed:imzero2-etable-cells / — / —]**
    `masterCellsCache` (apps/play) replays the last capture of the cells map
    (`EndETableFluid.CellsBytes` / `SendWithRawCells`) while a key of every
    input holds and no cell reports an interaction flag
    (`StateManager.ResponseFlagsAny`), so clicks still take the live path.
    296 of 300 idle frames replayed; a click on a replayed frame reached the
    Detail pane in the M1 scenes' `expect` step. A/B on the same scene: Go
    2.8 → 2.1 ms, Rust 5.1 → 4.3 ms per frame (status-bar averages; the
    profile had attributed more to the cells, so the rest of that frame is
    the next profile's question).
- **Outcome:** the wasm tax cannot be tuned away; the frame can be made
  smaller. Two of the four levers are in the tree; the parser and metrics
  findings are cheap follow-ups; M2 is next.
- **Results:** runs/2026-09-25-go-wasm-drilldown/ (tables and profiles); the
  cache A/B captures were read by eye and are not kept.

## 2026-09-26 — M2, the Rust half — the interpreter and egui run under wasm; a gallery frame is 5 to 6 ms whole; the viewer page paints it from a worker

- **Build under test:** the same tree plus the browser host
  (`rust/imzero2 --features browser`, `src/imzero2/browser.rs`,
  `build_rust_browser.sh`), an 8.8 MB wasm32 module whose only real import is
  the clock; the Go module rebuilt with the channel's deferred-flush default
  (and the spike's `-lazyFlush` flag now defaulting to it, which the earlier
  same-day runs of the real host had not — those are not kept).
- **Environment:** the handheld, `powersave` governor, load 1.6 at start,
  Chromium 144 and Firefox 146 as flatpaks with `PROFILE_DIR` under
  `~/Downloads`, Node 24.
- **Attempted:** every arm of the previous runs plus `*-host`: the Go module
  driving the real Rust host through the same fd 0/1 shim, in Node and in
  both browsers, both scenes; then the demonstrator, the viewer page in its
  new `?worker=` mode painting the worker's mesh, screenshotted through the
  DevTools protocol after twenty seconds.
- **Findings:**
  - **[positive boxer-toolbelt → proposed:keelson-browser-wasm / portability / —]**
    the shared interpreter, egui, tessellation and the mesh serializer run
    under wasm32 with no change to the interpreter's dispatch: a reader that
    reports WouldBlock at a message boundary (mapped to "no more messages"),
    a clock import, `pub(crate)` on the io field and the mesh lane's prefix
    constant decoupled from the carrier's input module. Fetches are answered
    outside any pass since no fetcher touches a `Ui`; the lock-step protocol
    guarantees a step sees either one fetch or a whole frame plus its first
    fetch (evidence: browser.rs, and zero host errors over every arm).
  - **[pain boxer-toolbelt → proposed:go-wasm-toolchain / operability / S3]**
    the module cannot avoid wasm-bindgen glue in its import list: `jiff`,
    `web-time` (via egui_graphs) and one getrandom major declare
    JavaScript-binding imports; the loader stubs them to throw and the two
    getrandom majors get the crate's own generator. Those paths (graph
    layouts, the time-range picker's wall clock) are unreached in the
    scenes measured and would need real glue in a shipped build.
  - **[pain boxer-toolbelt → proposed:fffi2-transport / performance-efficiency.time-behaviour / S3]**
    in the host arm the 24 Sync fetches each cost a JS→wasm step: on the
    gallery frame the host spends 3.0 ms of which the pass is 1.8 ms and
    tessellation plus serialization 0.5 ms; the remaining ~0.7 ms is the
    fetch steps. This is the first place fetch coalescing (ADR-0077 SD3)
    shows as time rather than hygiene (evidence:
    runs/2026-09-26-m2-handheld/results.tsv, rust_us_frame against
    rust_interpret_us).
  - **[pain boxer-toolbelt → proposed:egui-layout-cost / performance-efficiency.time-behaviour / S4]**
    the synthetic `labels` scene costs the Rust side 29 to 39 ms a frame
    under wasm: egui lays out 6 800 widgets in a scroll area without
    virtualization. A real table is virtualized (etable), so this bounds
    the widget-heavy pathological case, not play.
  - **[positive boxer-toolbelt → proposed:imzero2-viewer / — / —]**
    the existing viewer page painted the worker's frames unchanged in its
    painter: status "1024×600 @ppp 1 — 543 frames painted (mesh lane) — 4
    cached bodies" after twenty seconds in headless Chromium with
    `--enable-unsafe-swiftshader`, showing the IDS data-encoding demo. The
    page gained a `?worker=` transport of about thirty lines; input is not
    delivered, since the worker never yields.
  - **[pain boxer-toolbelt → proposed:trial-harness / functional-correctness / S4]**
    the browser `eager` arms of this run are lazy-flush runs: the worker
    only learned to ask for the per-message flush after the run (their
    `flush` column says so). Not rerun; the Node rows carry the eager arm.
  - the two parser fixes of the day are not in these numbers, which never
    start play: markdown now highlights a code block on first render, and
    the applet catalogue classifies a document from the parse it already
    holds.
- **Outcome:** whole gallery frames on the handheld: 5.1 ms in Node, 6.1 ms
  in Chromium, 6.0 ms in Firefox on wasip1 (Go 2.2–3.0 plus Rust 3.0–3.7);
  8.2 and 8.0 ms on js. ADR-0077's Phase 1 renderer web build exists as a
  measured artifact, and the recomposition of the background page — both
  modules in one worker, the mesh to the existing painter — is demonstrated
  end to end. Next: fetch coalescing, the frame envelope for input, and a
  real play frame through this host (needs the build-tag sweep).
- **Results:** runs/2026-09-26-m2-handheld/results.tsv (46 arms, three new
  columns: Rust µs per frame, the pass's µs, mesh bytes per frame).

## 2026-09-26 — M2b, input and a real app — the worker yields, the page's input reaches egui, fibscope runs in the tab

- **Build under test:** the same tree plus: `Application.Begin/Step/CloseAll`
  (Run split so a host can own the cadence), the spike's wasip1 reactor
  entry (`-buildmode=c-shared`, exports `argbuf`, `argcap`, `setup`, `frame`),
  `host_input` in the browser host (the carrier's `InputTranslator` and the
  protobuf `InputEvent`, so the viewer page's input encoding is reused
  unchanged), the worker calling `frame` per tick and forwarding input, and
  fibscope linked into the spike through its own tour registrations.
- **Environment:** the handheld, `powersave` governor, other sessions active
  (load 2 to 3), Chromium 144 headless with software WebGL.
- **Attempted:** (1) the reactor against the stub and the real host in Node;
  (2) the reactor build against command mode, paired, on the gallery scene;
  (3) the demonstrator with fibscope's three tour demos painted by the page;
  (4) a click delivered through DevTools on a replayed frame.
- **Findings:**
  - **[pain boxer-toolbelt → proposed:go-wasm-toolchain / self-descriptiveness / S3]**
    a wasip1 `c-shared` module runs neither `main` nor sees argv at
    `_initialize`; a probe export read both as absent. An earlier hello was
    misread as showing the opposite. The spike therefore exports a buffer
    and a `setup(n)` that runs main's body on NUL-separated arguments the
    host writes in (evidence: reactor_wasip1.go, bridge.js `startReactor`).
  - **[positive boxer-toolbelt → proposed:keelson-browser-wasm / portability / —]**
    the reactor costs nothing over command mode: paired on the gallery,
    Go 5.7 against 5.6 ms and the host 5.4 against 5.5 ms per frame (both
    inflated by a loaded machine; the pair is the evidence, not the
    level). Between frames the worker yields, so input arrives.
  - **[positive boxer-toolbelt → proposed:imzero2-viewer / — / —]**
    the page's input path is reused end to end: DOM events → the page's
    protobuf writer → `postMessage` → `host_input` → the carrier's
    translator → egui → the Go app's response read. A DevTools click on
    fibscope's "invalid" button on a replayed frame changed the app's id to
    42 and the next frames showed "not a tagged id"; the "example" button
    showed only its hover state, correctly, since it sets the id already on
    screen (evidence: the status lines in this session's captures; the
    captures are not kept).
  - **[positive boxer-toolbelt → proposed:keelson-browser-wasm / performance-efficiency.time-behaviour / —]**
    fibscope's explore frame in the tab, whole: Go 0.3 ms plus host 1.05 ms
    (pass 0.43, tessellate 0.10, serialize 0.14) on the handheld — a real
    app's frame at about 1.5 ms.
  - **[pain boxer-toolbelt → proposed:imzero2-browser-host / functional-completeness / S3]**
    still missing for a shipped tab: fonts as bytes (egui's defaults render
    now), the session-control channel (viewport resize, cursor shape,
    clipboard), and a reactive cadence — the worker ticks at a fixed rate
    and every tick runs a Go frame.
- **Outcome:** the recomposition of the background page now runs a real
  application with input in a browser, from the existing viewer page, with
  no server behind it. The measure script gained a `reactor-host` arm.
- **Results:** no run directory; the paired numbers above were read from
  Node's `ARM` lines in this session.
