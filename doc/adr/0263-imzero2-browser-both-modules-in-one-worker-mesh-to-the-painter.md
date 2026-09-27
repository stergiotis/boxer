---
type: adr
status: accepted
date: 2026-09-26
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-27
---

# ADR-0263: imzero2 in a browser tab — both wasm modules in one worker, the mesh to the existing painter

## Context

[ADR-0077](./0077-keelson-browser-wasm-execution.md) decided that keelson
runs in a browser as two sibling wasm modules — the Go application layer
and the Rust interpreter with egui — joined by a synchronous FFFI2 bridge,
with `GOOS=wasip1` as the working hypothesis (O2). Its SD1 put both modules
on the page's main thread: eframe's `update()` opens the egui pass, calls
out to run the Go frame, and Go's messages are dispatched against the open
pass through a re-entrant "sandwich". Its O3, Go in a Web Worker with the
renderer on the main thread, was killed because two threads that must
alternate synchronously need `SharedArrayBuffer`, hence COOP/COEP headers
the target fleet cannot rely on. Nothing of it was built; the Phase-0
spike (SD2) that gates the whole plan had not been run.

Two things changed since. [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md)
gave the tree a display seam below egui: the mesh lane serialises egui's
tessellated primitives and texture deltas to a wire format, and the viewer
page paints that wire with its own WebGL2 painter, needing nothing from the
carrier but the bytes, a hello and a canvas. And the spike has now been run
as the [keelson-wasm-frame-cost](../trials/keelson-wasm-frame-cost/README.md)
trial, whose §0 is the only place to quote its figures from: Go under wasm
costs about four times its native time on two machines a decade apart; the
bridge's cost is set by how often Go flushes, not by bytes; a real play
frame is about 180 messages; and, with the Rust half compiled to wasm too,
a whole gallery frame on a handheld is 5 to 6 ms in both browser engines,
inside ADR-0077's gate. The background page
[imzero2-in-browser-worker-mesh](../adr-background-work/imzero2-in-browser-worker-mesh.md)
records the dialogue that led here.

The question this ADR answers is where the two modules sit in the tab and
how pixels reach the screen, given that seam. It does not reopen ADR-0077's
choice of two modules over a nested engine, fusion, or a Rust host, and it
does not change the browser tier's standing as a demo and per-user tier
rather than an answer to the hostile-guest design (ADR-0077 §Consequences,
[ADR-0087](./0087-imzero2-client-compositor-compartmentalization.md) SD1).

## Design space (QOC)

**Question.** On which thread does each wasm module run, and what crosses
between the worker and the page?

**Options.**

- **O1 — Main-thread sandwich (ADR-0077 SD1).** Both modules on the main
  thread; egui paints through eframe's own GL painter on the page canvas;
  Go's frame runs inside the open egui pass through a re-entrant call.
- **O2 — Split threads over `SharedArrayBuffer` (ADR-0077 O3).** Go in a
  worker, the renderer on the main thread, a ring buffer and `Atomics` for
  the lock-step.
- **O3 — Both modules in one worker, the mesh to the page.** The worker
  runs Go and the Rust module on one thread with ADR-0077's synchronous
  bridge between them; each frame's tessellated mesh crosses to the main
  thread as a transferable buffer in the ADR-0128 wire format, and the
  existing viewer page paints it and sends DOM input back over the same
  port.
- **O4 — Both modules in one worker, `OffscreenCanvas`.** As O3, but egui
  paints in the worker through its own GL painter on a canvas transferred
  from the page; no mesh wire, no shared painter.

**Criteria.**

- **C1 — No cross-origin isolation.** No `SharedArrayBuffer`, no
  COOP/COEP; ADR-0077 SD7(a) as an acceptance criterion.
- **C2 — Main-thread responsiveness.** A slow frame must lower the frame
  rate, not delay input handling and page chrome.
- **C3 — One display path.** The server-tessellated appliance mode of
  ADR-0128 and the in-tab mode share the painter and the wire, so a fix or
  a feature lands once.
- **C4 — Rust build surface in the tab.** What the wasm module must carry
  beyond the interpreter and egui's layout and tessellation.
- **C5 — Interpreter change.** How much of the interpreter's pull loop
  must be reshaped for the browser.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 sandwich | O2 SAB | O3 worker + mesh | O4 worker + OffscreenCanvas |
|----|----|----|----|----|
| C1 | ++ | −− | ++ | ++ |
| C2 | −− | +  | ++ | ++ |
| C3 | −  | −  | ++ | −  |
| C4 | −  | −  | ++ | −  |
| C5 | −− | +  | +  | +  |

**Assessment notes.** O1 and O3 both avoid isolation headers; O3 also takes
every Go frame off the main thread, which O1 cannot. O3 and O4 differ only
in how pixels leave the worker: O3 reuses the ADR-0128 painter and wire, so
the tab and the appliance stay one display path and the Rust module needs
no GPU dependency at all (it is the lean mesh host of ADR-0128 M3 without
the carrier); O4 would carry eframe's painter into the module and give the
tab a second rendering path to keep in step. On C5, O1's re-entrant plumbing
was ADR-0077's declared long pole; O3 needs only that the interpreter treat
"no more bytes now" at a message boundary as the end of what can be
interpreted rather than a peer error, since the worker never blocks on a
port.

## Decision

We will run both wasm modules in **one Web Worker** with ADR-0077's
synchronous bridge between them, and have the worker post each frame's
tessellated mesh to the main thread in the **ADR-0128 wire format**, where
the **existing viewer page** paints it and returns DOM input over the same
port. This amends ADR-0077 SD1 (the bridge moves off the main thread and
the egui pass no longer wraps the Go frame) and takes what O3 wanted, the
Go frame off the main thread, without what killed it, since the two modules
that must alternate synchronously share one thread and nothing blocks
across threads.

### Subsidiary design decisions

- **SD1 — The worker owns the cadence.** The Go module is built as a wasip1
  reactor: the worker calls a setup export once and a frame export per
  tick, and yields between ticks, which is when the page's input reaches
  it. On the Go side this is `Application.Begin`, `Step` and `CloseAll`,
  the pieces `Run` is composed of. The command-mode module, which runs its
  loop in `main` and never yields, remains a measurement arm only.
- **SD2 — The Rust module is a C-ABI host, not a wasm-bindgen crate.** Its
  exports take and return bytes in linear memory; its only import is a
  clock. The worker hands it Go's fd 1 writes and calls a step export when
  Go blocks on a read; the lock-step protocol guarantees that the unread
  bytes are then one fetch, or a whole frame followed by its first fetch,
  so a step is either an egui pass or a fetch answered from the
  interpreter's registers. Dependencies that expect wasm-bindgen glue are
  pointed at the crate's own generator or left unreached, so the module is
  loadable from plain JavaScript with no bundler.
- **SD3 — One wire, one painter.** The worker posts the same bytes the mesh
  appliance sends on its socket, prefixed as the carrier prefixes them,
  and the page's `?worker=` mode substitutes a port for the socket behind
  the same hello, painter and input path. A change to the wire is a change
  to both modes by construction.
- **SD4 — Input crosses as the carrier's protobuf.** The page encodes DOM
  events as it does for the appliance; the worker forwards them to the Rust
  module, which translates them with the headless host's translator. No
  second input vocabulary.
- **SD5 — ADR-0077's prerequisites stand.** Fonts as bytes (its SD5), the
  reactive cadence (SD6), the Go build-tag sweep (SD8) and the data plane
  (SD9) are unchanged in substance; this ADR changes where the bridge
  runs, not what it needs. Its SD3, fetch coalescing, landed as
  pipelining: `Sync` issues every fetch of a frame before it reads the
  first reply, so the worker steps the host once a frame instead of once
  a fetch, with no change to the wire; the trial's logbook records what
  that is worth (small on a loaded handheld).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `rust/imzero2` feature set | added: `browser`, a wasm32 cdylib with a C ABI | `build_rust_browser.sh`; the getrandom rustflag it sets; protos generated for the feature as for `headless` |
| Interpreter pull loop | reshaped: a reader that is empty at a message boundary ends the step instead of the session | the browser host's inbox is the only such reader; the pipe and socket readers block as before |
| Viewer page (`viewer/index.html`) | added: `?worker=` mode | none — the painter, hello and input path are shared |
| `application.Application` | added: `Begin`, `Step`, `CloseAll`; `Run` is their composition | a reactor build's exports call the three |
| Mesh wire prefix | reshaped: available without the headless input module | the headless build asserts the two definitions agree |

## Alternatives

- **O1 — the main-thread sandwich.** Rejected on C2 and C5: every Go frame
  would run on the main thread, and the re-entrant plumbing it needs is
  the largest interpreter change of any option; kept in ADR-0077 as the
  shape this one amends.
- **O2 — split threads over `SharedArrayBuffer`.** Rejected on C1, as in
  ADR-0077; what it offered, the Go frame off the main thread, O3 gives
  without the headers.
- **O4 — `OffscreenCanvas` in the worker.** Rejected on C3 and C4: a second
  rendering path and eframe's GL painter inside the wasm module, for no
  gain the mesh wire does not already give. It becomes the right choice
  only if the mesh seam is ever abandoned natively.
- **Go on a server, only egui in the tab.** Stays rejected as in
  [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) O5 and
  ADR-0128; the trial did not change the protocol argument against it.

## Consequences

### Positive

- The tab needs no isolation headers, and a slow application frame costs
  frame rate rather than page responsiveness.
- The appliance and the tab are one display path; the viewer page did not
  change to paint the worker's frames.
- The Rust module in the tab has no GPU, socket, thread or file
  dependency, which is also the smallest surface to keep compiling for
  wasm32.
- The trial's demonstrator exists in this shape, so the gate figures in
  its §0 describe the decided topology and not a proxy for it; three real
  apps — taskdemo, mdedit and play, the last querying ClickHouse through
  the page's origin — run in it from the existing viewer page.

### Negative

- Every frame's mesh crosses a thread boundary as a copy or a transfer; on
  dense scenes the mesh is larger than the FFFI2 stream that produced it
  (the multi-tenant design-space page's §6 has the comparison), and the
  worker pays tessellation and serialisation on the same thread as Go.
- Input has a frame of latency by construction: it reaches the worker only
  when the worker yields.
- Two more build products to keep green — the wasm32 cdylib and the wasip1
  reactor — with no CI lane running them at the time of writing.

### Neutral

- The page's `?worker=` mode is a transport swap on the viewer and stays
  small; it does not make the viewer a host of its own.
- Whether the Go module lives in the same worker or a second one is left
  open by the bridge but not by the measurements: the synchronous bridge
  requires one thread, and the worker's only other job is posting bytes.

## Migration — Tier 1

- **Breaks.** Nothing: every surface above is additive, and the native and
  headless hosts read the same interpreter with the same blocking readers.
- **Path.** None to walk.
- **Regeneration.** None; the protos are generated per build as before.
- **Old shape.** ADR-0077 SD1's main-thread sandwich was never built; it is
  superseded by this ADR and not deprecated.

## Verification plan — Tier 1

- **Lane.** The trial's `measure.sh` under
  [doc/trials/keelson-wasm-frame-cost](../trials/keelson-wasm-frame-cost/README.md):
  its Node arms load the reactor module against the browser host and
  report per-frame cost, and its browser arms paint the worker's mesh
  through the viewer page. Both are run by hand and recorded under
  `runs/`; neither is a CI lane.
- **What would fail.** A Node reactor arm that produces no frames, or a
  gallery frame on the handheld outside ADR-0077's gate, would mean the
  topology or the bridge regressed; a browser arm whose screenshot is
  blank would mean the wire or the `?worker=` mode did.
- **Gap.** No default-lane test covers the wasm32 module or the reactor;
  the cost is a toolchain (wasm32 target, Node, a headless browser) the
  default lane does not carry. The interpreter change is covered
  indirectly by every headless scene, which would hang or fail if a
  blocking reader started returning early.

## Status

Accepted 2026-09-27.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-27 — accepted; the demonstrator becomes a package, a binary and a bundle

Accepted the day the trial's step 8 closed. What shipped on acceptance,
so that the shape is a citizen of the tree rather than a trial harness:

- `rust/imzero2/browser` — the browser host's C ABI as its own cdylib crate
  over `imzero2`'s `browser` feature. The main crate is an rlib again; the
  native hosts no longer link a shared library they never load. The
  getrandom backend cfg stays in `build_rust_browser.sh`, because the repro
  environment's `RUSTFLAGS` would replace any cargo config that set it.
- `public/thestack/imzero2/browserhost` — the Go side: `Mount` (a
  registered app over an in-process bus, the window host's shape minus the
  window and the services), the wasip1 reactor exports behind `SetMain`,
  `StepLoop`, and `InstallHostTransport`, the process-wide HTTP swap taken
  deliberately by the binary rather than by an `init`. Its `web/` holds
  the worker, the WASI shim and the dev server the page loads.
- `public/thestack/cmd/imzero2tab` — the tab binary: the apps a tab may
  open, one mounted by `-app`, the pipe natively, the reactor under wasm.
- `scripts/dev/build_tab_bundle.sh` — a servable directory from all of the
  above plus the viewer page and the fonts; the how-to is
  [imzero2-in-the-browser](../howto/imzero2-in-the-browser.md).
- The trial's spike is measurement again: scenes, consumers, the stub's
  tables, and the reactor arm through `browserhost`.

Two things the implementation taught, both now in the design: the render
loop must yield to the scheduler around a frame on wasm (SD1 above says
the worker owns the cadence; it also owns every other goroutine's chance
to run, since the shim answers reads synchronously and nothing parks), and
the host skips a pass whose mesh equals the last one posted, as the
carrier does. What the tab still lacks is unchanged: the runtime services
on the bus, live query progress, sealed files.

## References

- [ADR-0077](./0077-keelson-browser-wasm-execution.md) — the two-module decision, SD1 amended and O3's kill reason partly reversed here.
- [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) — the mesh wire and the viewer painter this ADR reuses.
- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — remote access to a server-resident instance; unaffected.
- [ADR-0087](./0087-imzero2-client-compositor-compartmentalization.md) — why a tab earns no isolation credit.
- [imzero2-in-browser-worker-mesh](../adr-background-work/imzero2-in-browser-worker-mesh.md) — the design dialogue.
- [keelson-wasm-frame-cost](../trials/keelson-wasm-frame-cost/README.md) — the trial; quote figures from its §0 only.
- [imzero2-multi-tenant-display-design-space](../adr-background-work/imzero2-multi-tenant-display-design-space.md) — §6, mesh bytes against command bytes.
