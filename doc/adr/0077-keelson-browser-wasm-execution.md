---
type: adr
status: accepted
date: 2026-06-12
reviewed-by: "@spx"
reviewed-date: 2026-06-21
---

# ADR-0077: Keelson in the browser — dual-module wasm execution with an in-page FFFI2 bridge

## Context

Keelson today is a desktop shape: the Go host (`public/thestack/cmd/imzero2/`, built CGO-free per `rust/imzero2/build_go.sh`) spawns the Rust egui renderer (`rust/imzero2/`, eframe 0.34.1 + wgpu) as a child process and speaks FFFI2 over stdin/stdout (`public/thestack/imzero2/application/application.go:130-184`). [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) addresses *remote access to a server-resident instance* (headless render + ffmpeg + WebCodecs pixel streaming). This ADR answers a different question: **executing keelson itself inside a browser tab** — Go host, platform spine, and renderer all client-side. The two are complementary; pixel streaming remains the answer for server-resident access and is unaffected here.

Drivers:

- **Zero-install delivery.** On ultra-locked-down enterprise machines (no admin rights, software allowlisting), a browser tab is the only deployment channel that exists. A static URL is not an "installation."
- **Conservative-customer trial tier.** A fully static, no-backend demo (embedded data, no egress — verifiable with devtools open) lets a prospective user evaluate on the hardware they have: aging mobile i7, pinned/ESR browser, SSL-inspecting proxy, possibly VDI. This tier decides several design points below; it is a first-class deployment shape, not an afterthought.
- **LAN-served, cacheable; bundle size explicitly not a constraint** (content-hashed immutable assets, brotli, wasm streaming compilation + browser code cache).

Mechanical facts the design rests on (verified in-tree):

- **Transport is already interface-shaped on both sides.** Go's `InlineIoChannel` holds plain `io.Reader/Writer` (`public/thestack/fffi2/runtime/fffi2_rt_channel.go:12-22`, `SetInOut` :70); a file-based alternative mode already exists (`application.go:65-87`). Rust's `ImZeroFffiIo<R: BufRead, W: Write>` is generic (`rust/imzero2/src/fffi/io.rs:76`), bound to stdin/stdout only at `src/imzero2/entry.rs:22-23`. Swapping the byte carrier is cheap; **the blocking semantics are the actual problem**.
- **FFFI2 is a synchronous lockstep protocol.** Hand-rolled little-endian binary, 4-byte length prefix, flush per message; ~250–330 KB/frame Go→Rust, ~10–20 KB back. Rust paces frames (eframe vsync; cadence per [ADR-0062](./0062-imzero2-render-cadence.md)); the interpreter (`src/imzero2/interpreter.rs:2408`) pulls messages inside `update()` and dispatches against the live egui pass. At frame end, Go's `Sync()` (`public/thestack/imzero2/egui2/bindings/egui2_statemanagement.go:533-753`) issues ~14 *sequential blocking* fetches (response flags, databind values, pointer/plot/canvas state, modifiers, rects, frame metrics), each answered mid-interpretation. Natively each costs microseconds; over any real network each costs an RTT — which is why ADR-0024 ruled its O5 out *for the remote problem*. In-page, a "round-trip" is a function call, which voids that objection for *this* problem.
- **Single eframe viewport; egui-internal windows only** (windowhost manages `egui::Window`s, not OS windows) — exactly the single-canvas shape web eframe supports.
- **State residency:** egui Context (Rust) owns widget state; app/platform state is Go-side; persist + factsstore default to in-memory; eframe persistence is disabled.
- **Empirical priors.** [egui.rs](https://www.egui.rs/) (eframe web — our exact renderer) and [imgui_explorer](https://pthom.github.io/imgui_explorer/) (Dear ImGui Bundle via emscripten, WebGL-only) run well in browsers, including on modest hardware. This validates the renderer half and WebGL2-only delivery. It deliberately does **not** validate the Go half or the bridge — both demos are single native-language modules; the unvalidated cost is Go-wasm + boundary crossing, which is what the Phase-0 spike measures.
- **In-repo precedent for wasm guests.** `public/science/geo/h3/` executes a Rust wasm32 build of h3o under wazero (`rust/h3bridge/`, parity-gated by `scripts/ci/h3_wasm_parity.sh`). The wasm-guest seam is established practice here for leaf libraries; §Alternatives O4 explains why the same pattern does *not* fit the frame-hot GUI layer in a browser.

Forces:

- **Locked-down-fleet constraints.** SSL-inspecting middleboxes strip/mangle headers → no reliance on COOP/COEP (hence no SharedArrayBuffer, hence no wasm threads in any option); group policy disables WebGPU or hardware acceleration → WebGL2 (incl. SwiftShader software rasterization) is the compatibility floor; protocol-inspecting proxies mistreat WebSockets → the static demo tier is fetch-only; CSP must allow `'wasm-unsafe-eval'`; AV scanning makes first load slow (one-time; progress UI).
- **Aging-i7 budget.** Go on `js/wasm` is single-threaded, GC shares the thread, codegen ~1.5–3× slower than native. The design must keep the CPU-heavy GUI work (layout, text galleys, hit-testing, tessellation) in the Rust module and pay the Go tax only on app logic + marshalling.
- **Isomorphism principle.** ImZero mirrors upstream GUI *APIs* mechanically; it does not fork *implementations*. ADR-0024's context records the confirmed ImZero1 lesson: the brittle parts were the patched-ImGui fork and the middle layer. This force excludes one idea from the option set a priori (an egui-logic port to Go; see §Design space) and weighs against any option that hand-owns an upstream-tracking surface.
- **Data plane.** A tab cannot spawn `clickhouse-local` (`public/keelson/data/chlocalpool/worker.go:66`) and has no /proc, no inotify. ClickHouse is reachable only over HTTP; heavy data work already belongs server-side.

## Design space (QOC)

**Question.** How should the Go keelson host and the Rust egui renderer execute and communicate inside a browser tab so that the platform runs fully client-side with acceptable interactivity on constrained, policy-locked enterprise hardware?

**Options.**

- **O1 — Dual wasm modules, re-entrant synchronous in-page bridge, Go as `GOOS=js`.** Both halves compiled to wasm as sibling modules under the browser's JIT; FFFI2 messages cross via synchronous JS calls; the lockstep is preserved by a re-entrant call-stack "sandwich" (§SD1). Go runs under `wasm_exec.js`; `net/http` rides fetch for free.
- **O2 — Same bridge, Go as `GOOS=wasip1`.** Go compiled to wasip1 (reactor mode via `go:wasmexport`, Go 1.24+); its small WASI import surface is satisfied by `browser_wasi_shim`-style JS forwarding into the renderer module's exports. FFFI2 rides fd 0/1 **byte-identically** — `InlineIoChannel` does not learn it left the desktop. No `wasm_exec.js`, no JS event-loop integration: the event-loop-deadlock hazard class of O1 is eliminated structurally. Cost: wasip1 has no networking — chclient needs a custom `go:wasmimport` http host call fulfilled by the host with fetch.
- **O3 — Dual modules, Go in a Web Worker, SharedArrayBuffer ring + Atomics.** Both sides keep blocking-style control flow: Go blocks via `Atomics.wait` in the worker; Rust on the main thread polls/spins briefly when starved. True parallelism between the halves.
- **O4 — Nested engine: Go wasm interpreted by a Rust-hosted wasm engine inside the renderer module** (wasmi / wasmtime-Pulley class). Single deployable module; host functions instead of JS glue; full scheduling control (fuel, resumable calls); FFFI2 as WASI fds.
- **O5 — Build-time fusion via the RLBox mechanism**: Go → wasip1 wasm → `wasm2c`/`w2c2` → C → compiled back to wasm32 objects and statically linked into the renderer crate. One module, no JS bridge, host functions as direct calls. (RLBox's `tainted<T>` validation layer is not adopted — the guest is first-party and FFFI2's serialized protocol already is the boundary discipline.)
- **O6 — Relocate the boundary down: egui's logic layer compiled to wasm32, embedded in Go** (wazero natively; necessarily a sibling module in the browser), apps calling a flattened egui ABI per widget call.
- **O7 — Eliminate the boundary: Rust as keelson's host language** (scoped: Rust spine twin + Rust apps; leeway stays Go server-side; Arrow on the wire). Apps call egui directly; FFFI2 and the bridge cease to exist for ported apps.

Remote shapes — renderer-only-in-browser with FFFI2 over WebSocket (ADR-0024's O5) and pixel streaming (ADR-0024's O1) — are excluded from this design space by the question's definition (they do not execute keelson client-side); see §Alternatives for their disposition. A second a-priori exclusion: *porting* egui's logic layer to Go was raised in the design dialogue and discarded without entering the option set — an implementation fork of a fast-moving ~50k-line core plus the widget ecosystem in active use (egui_dock/table/plot/graphs, egui-snarl, egui_ltreeview, walkers; egui-snarl is already pinned to a git rev because of 0.34 churn), which the isomorphism force above rules out outright (the ImZero1 patched-ImGui lesson), and which would additionally move layout/text/hit-testing into the slow runtime. Recorded here only so it is not re-derived.

**Criteria.**

- **C1 — Client-side execution.** Keelson (spine + apps) runs in the tab; static hosting possible; no per-session server process.
- **C2 — Frame cost on constrained single-thread hardware.** Aging mobile i7, no SAB ⇒ single-threaded in every option; the CPU-heavy GUI layer must not land in the slow runtime.
- **C3 — Locked-down-fleet deliverability.** No COOP/COEP reliance, WebGL2 floor, fetch-only demo path, proxy/AV tolerance.
- **C4 — Preservation of FFFI2 semantics and investment.** ADR-0013 stateful-widget contract, deferred-response registers, egui2gen codegen, record/replay-shaped stream.
- **C5 — Upstream-churn exposure / fork risk.** The isomorphism principle; who tracks egui releases, and in what form (regenerate vs. re-port).
- **C6 — Implementation cost to a working v1.**
- **C7 — Reversibility and option preservation.** Native path unaffected; future shapes (native single-binary, Rust-host, untrusted-app sandboxing) stay open.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 js | O2 wasip1 | O3 SAB | O4 nested | O5 fusion | O6 embed | O7 Rust host |
|----|:--:|:--:|:--:|:--:|:--:|:--:|:--:|
| C1 | ++ | ++ | ++ | ++ | ++ | ++ | ++ |
| C2 | +  | +  | +  | −− | −  | +  | ++ |
| C3 | ++ | ++ | −− | ++ | ++ | ++ | ++ |
| C4 | ++ | ++ | ++ | ++ | ++ | −− | −− |
| C5 | ++ | ++ | ++ | +  | +  | −  | +  |
| C6 | +  | +  | −  | −  | −− | −− | −− |
| C7 | ++ | ++ | +  | +  | +  | −  | −  |

**Assessment notes.** O1/O2 are the chosen family; they are the only options that simultaneously keep the CPU-heavy layer in Rust-wasm (C2), need no fragile headers (C3), and reuse the protocol + codegen unchanged (C4, C5). O2 is the working hypothesis over O1: eliminating the `wasm_exec.js` event-loop discipline ("nothing inside a frame may depend on the JS event loop") removes a whole invariant class, and FFFI2 over synthetic fds is byte-identical — at the bounded cost of a DIY http host import; the Phase-0 spike (§SD2) decides. O3 fails C3 outright (COOP/COEP through SSL-inspection middleboxes) and has worse semantics than it looks: `Atomics.wait` stalls the *entire* Go runtime (all goroutines, timers), not one goroutine, and the main thread must spin when starved. O4 fails C2 decisively: browsers forbid nested JIT (no executable memory inside wasm), so the embedded engine is necessarily an interpreter (~5–15× behind JIT even for wasmi/Pulley) applied to the already-slowest half — a 4–10 ms Go frame becomes 40–100 ms on the target hardware. O5 is the same direction milder (no guard pages on wasm32 ⇒ explicit bounds checks compounding with the outer module's; plus a real toolchain gap: `wasm2c` traps via setjmp/longjmp, absent on `wasm32-unknown-unknown`) and is strictly dominated in-browser by letting the browser JIT the Go module directly. O6 founders on a missing artifact: egui has no C ABI (no cimgui equivalent); authoring one means re-solving closure flattening and response deferral — converging back into what FFFI2 already is, one level down, with the proven generated interpreter discarded ("conservation of churn"); in the browser it degenerates into O1 anyway, and natively it still needs a painter process for window/input/GPU. O7 is not a v1 option but a live strategic fork — deferred to its own ADR with an explicit sequencing gate (§SD11), because a Rust-host decision would demote this ADR's Go-side bridge to transition scaffolding.

The boundary-altitude comparison underlying O6's rejection (and the a-priori exclusion of an epaint-level Go port), kept for reuse:

| Boundary altitude | Per-frame traffic | Response semantics | Who tracks egui churn | Heavy CPU runs in |
|---|---|---|---|---|
| Widget/IDL (today; O1/O2) | ~0.3 MB batched | deferred registers → in-page function calls | generated interpreter (egui2gen) | Rust |
| egui API (O6) | thousands of fine-grained crossings unless re-flattened | immediate, but deferral must be rebuilt | new hand-owned flat-ABI crate | Rust |
| epaint shapes/primitives (ADR-0024's O6 wire) | ~1–3 MB shapes/vertices | none needed | the logic-layer owner | wherever the logic layer runs |

The widget-IDL altitude is the highest at which the stream stays batched and semantic, and the lowest at which egui does not have to be reimplemented. It is the asset, not the liability.

## Decision

Adopt **O1/O2**: both halves compiled to WebAssembly as sibling modules under the browser's JIT, FFFI2 unchanged at the protocol layer, carried by a synchronous in-page bridge. The guest target (`js` vs `wasip1`) is settled by the Phase-0 spike, with **wasip1 as the working hypothesis**.

### Subsidiary design decisions

- **SD1 — Re-entrant sandwich bridge.** Per frame: `requestAnimationFrame` → eframe `update()` opens the egui pass → Rust calls out (JS import) "run the Go frame" → the host calls Go's exported frame function → Go streams FFFI2 messages, each `Flush` a *synchronous* call into the renderer's exported `consume(bytes)`, dispatched immediately against the open pass; fetch responses are queued by `consume` and are therefore always already present when Go's blocking read executes (the same alternation order as the native pipes — by protocol construction, never an actual wait) → Go returns → pass closes → tessellate → paint. No threads, no SAB, no headers, zero RTT. Go-side: custom `io.Reader/Writer` doing synchronous host calls; `InlineIoChannel` untouched. Rust-side (the long pole): convert the interpreter's pull-loop (`begin_consume_message`) to a per-message push entry feeding the existing `BufRead` machinery, plus re-entrancy plumbing (`thread_local`/`RefCell` around the app state) since `consume` runs while `update()` is on the stack. The deferred-block replay mechanism (`replay_depth` in `io.rs`) is internal buffering and survives unchanged.
- **SD2 — Phase-0 guest-target spike is the gating number.** Harness: a minimal Go frame producer emitting a representative opcode mix (~0.3 MB/frame + the 14 Sync fetches) into a stub Rust consumer; measure µs/frame on a beefy desktop **and** the oldest available laptop. Per-target verification: `js` — the event-loop discipline holds under real app patterns (no fetch/promise dependence inside a frame); `wasip1` — `go:wasmexport` blocking semantics, timer delivery through a `poll_oneoff` shim, and the custom http host import. **Acceptance gate: Go frame + bridge ≤ ~half the frame budget on the old machine.** If neither target passes, this ADR's recommendation reverts to the remote shapes (ADR-0024) and the O7 strategy question gains urgency.
- **SD3 — Sync fetch coalescing ships as an independent protocol improvement.** Collapse `Sync()`'s ~14 sequential fetches into one combined request/response message. In-page it is hygiene; natively it removes per-fetch flush/wakeup churn; for any future socket transport it is the difference between viable and not. Land it on the native path first, before the bridge.
- **SD4 — Renderer web build gates.** cfg-gate to native: `mimalloc` global allocator (does not build for wasm32-unknown-unknown), `memmap2`, `puffin_http`, `std::process::exit` (`src/main.rs:56`), filesystem font loading (`src/imzero2/app.rs:27`), screenshot PNG writing (`interpreter.rs:2493`), the IPC test harness. Swap `std::time::Instant` uses (debugtools, interpret timing) to `web-time`. Feature plumbing: getrandom `wasm_js` backend, `jiff` `js` feature, `wayland`/`x11` eframe features native-only. wgpu ships with **WebGPU and the WebGL2 fallback both enabled — WebGL2 is the compatibility floor** (policy-disabled WebGPU, Firefox/ESR, VDI/SwiftShader). Single canvas is already satisfied (one viewport; `ViewportCommand` usage is Close + Screenshot only — the latter gated by SD12).
- **SD5 — Fonts cross as bytes, not paths.** Today Go passes TTF *paths* as child-process flags (`application.go:96-107`) and Rust reads them from disk. The web target embeds Phosphor (already vendored at `assets/fonts/phosphor/`) and fetches/embeds the Noto set; extending the launch path to carry font bytes is also the cleaner native design. **PragmataPro is never bundled into served assets** (commercial license); `hmi-fonts-pragmatapro.sh` remains a native, local opt-in.
- **SD6 — Reactive render cadence (ADR-0062) is the web default.** Continuous redraw remains a debug switch. On battery-powered, thermally limited laptops, idle cost is the difference between "demoable" and "fan noise"; dashboard usage is mostly idle.
- **SD7 — Hostile-environment requirements are acceptance criteria, not advice.** (a) No COOP/COEP/SAB anywhere at v1. (b) The static demo tier uses no WebSocket — fetch-only. (c) CSP requirement (`'wasm-unsafe-eval'`) documented for embedders. (d) Content-hashed immutable assets + brotli. (e) First-load progress UI (AV/proxy scan latency is real and one-time). (f) The zero-cost fleet probe precedes engineering: egui.rs and imgui_explorer opened on an actual target notebook answer "wasm allowed / WebGL allowed / bundle survives the middlebox / ballpark fps" before any code is written.
- **SD8 — Go-side build-tag inventory for the wasm target.** Excluded via build tags with capability-absent or stub paths: `chlocalpool` (`worker.go:66` spawns clickhouse-local), the fsbroker inotify watcher (`fsbroker/watcher.go:197-278`), the `sysmetrics` tree (/proc, /sys, GPU), signal handling, the pprof listener, the flight recorder, debug-profiler wrappers. The app-launch resolution path that shells out to clickhouse-local (`imzero2_demo_list.go:178`) gains a pure-Go alias/ID match for the wasm build. `imztop` is excluded from the web app set (it is a /proc monitor; a tab has no /proc).
- **SD9 — Data plane.** `chclient` works unchanged on `js/wasm` (`net/http` rides fetch); under `wasip1` it uses the SD2 http host import. Deployment serves the bundle and reverse-proxies the ClickHouse endpoint from the **same origin** (no CORS surface, no second TLS authority). chlocal-shaped SQL routes to server-side CH. `persist`/`factsstore` remain in-memory at v1 — tab discard loses state (recorded; OPFS-backed persist is the named follow-up). The static demo tier ships embedded/pre-baked data and only CH-independent apps.
- **SD10 — The static demo tier is a first-class deployment shape.** Single static origin, no backend, embedded data, no egress. It is the trial channel for locked-down customers, and it is what *decides* SD1-over-O3, the WebGL2 floor, fetch-only transport, the cadence default, and the persistence priority. Treating it as a named deliverable keeps those decisions from being silently relaxed.
- **SD11 — Sequencing and the host-language gate.** Phase 1 (renderer web build, SD4/SD5) is invariant under every strategic branch — including a future Rust-host decision — and proceeds unconditionally. Phase 0 (SD2 spike + SD7 probe) is cheap and informs everything. **The full Go-side bridge build (SD1 at scale + the SD8 sweep) is explicitly gated on the host-language strategy ADR** (follow-up; §Alternatives O7): under a Rust-host decision the bridge is transition scaffolding whose scale should match the number of Go apps that must reach the tab before their port.
- **SD12 — Out of scope at v1.** In-browser screenshot/tour capture (the ADR-0057 tour stays a native CI concern; canvas capture is a future option); eframe persistence/localStorage; OPFS; multi-session, auth, serving infrastructure; wasm threads (blocked by SD7(a) regardless of language); WebSocket carriage of FFFI2 (remote shapes belong to ADR-0024); audio; clipboard sync. Each named so escape hatches are explicit.

## Alternatives

Recorded with kill-reasons and salvage, so these are not re-derived when they resurface (each is individually plausible-sounding).

- **O3 — Worker + SharedArrayBuffer lockstep.** Both codebases keep blocking-style control flow, and the halves run truly in parallel. Killed by C3: COOP/COEP headers through SSL-inspecting middleboxes are exactly the fragile dependency the target fleet punishes; additionally `Atomics.wait` stalls the whole Go runtime rather than one goroutine, and the main-thread side must spin when starved. **Retained as the fallback** if SD1's re-entrancy plumbing proves unworkable in practice.
- **O4 — Nested wasm engine.** The single-artifact, host-function-bridge, FFFI2-as-fds properties are genuinely attractive — but browsers forbid nested JIT, so the engine is an interpreter, and the ~order-of-magnitude tax lands on the half that was already the concern. Where the pattern *is* right: (a) **natively**, where real JIT exists — wasmtime-class embedding would merge keelson + renderer into one distributable binary at ~1.2–2× cost (it currently solves a non-problem; the pipes work); the in-repo h3o-under-wazero precedent (`rust/h3bridge/`, `h3o_wasm`) shows the same seam succeeding for a leaf compute library; (b) **future untrusted third-party apps**, where an engine with capability-scoped host imports is the dynamic counterpart to capslock's static checking (ADR-0026 §SD10) — a separate ADR if it ever materialises. Salvage: O2's WASI host-import seam is the same seam both of these reuse.
- **O5 — RLBox-mechanism fusion (wasm2c / w2c2).** Same technology family as [rlbox.dev](https://rlbox.dev/) minus its tainted-type layer (first-party guest; FFFI2 is already the boundary discipline). For **native** packaging the mapping is exact and production-proven (Firefox ships graphite/hunspell/expat/ogg/woff2 this way) — shelf-ready if a one-file desktop distribution ever becomes a goal. For the **browser** the idea adds a back-to-wasm32 recompilation RLBox never does: explicit bounds checks (no guard pages in wasm32) compounding with the outer module's, plus the setjmp/longjmp trap-handling gap on `wasm32-unknown-unknown`. Purpose inversion noted for honesty: RLBox buys *security* for memory-unsafe guests; we would buy *packaging* for a memory-safe one — and pay with speed that the dual-module design gets for free from the browser's JIT. Retained as the native consolidation option only.
- **O6 — Embed egui's logic layer as a wasm module inside Go.** No upstream C ABI for egui exists (there is no cimgui equivalent); authoring one means flattening a closure-heavy, per-call-response API into begin/end opcodes with deferred responses — i.e., rebuilding FFFI2 one level down while discarding the generated interpreter that already does this ("conservation of churn": the egui-version-tracking burden moves from a *regenerated* artifact into a *hand-owned* one). In the browser the module cannot be embedded (O4's nested-interpretation bar) and becomes a sibling module — O1 with more work. Natively the painter process still owns window/input/GPU, so the process count does not even drop. The two genuine kernels in the idea are served elsewhere: "shrink the Rust maintenance surface" → invest in egui2gen generator coverage (churn → regeneration, the right mitigation); "immediate widget responses in Go" → in-page A1 already makes Sync a function call, and natively it costs microseconds.
- **O7 — Rust as keelson's host language.** **Deferred, not rejected** — the one live strategic fork. The decomposition recorded from the design dialogue: "host language" bundles three separable roles — the spine (small, contract-shaped, cheap to twin in either language), the app authoring language (the per-frame hot path, where Rust-host pays off in the tab), and the data plane (leeway: the expensive port, and the part that least needs to be in a tab at all — heavy data work belongs to server-side ClickHouse, leeway already speaks Arrow IPC in places, arrow-rs is first-class, so a tab consuming Arrow may need no leeway port whatsoever). A strangler path exists because the bus is message-shaped: a Rust `AppI` twin, Rust apps hosted natively by the renderer, the CBOR buscodec bridging across, Go apps continuing over FFFI2 — which then retires to a legacy-app adapter rather than the architecture. Costs that keep this open rather than decided: leeway's reflection-based machinery and its accumulated validation surface; Go-runtime observability as part of the closed-loop value proposition (pprof/trace/flight recorder; imzrt loses its subject matter); capslock has no Rust off-the-shelf equivalent (type-level caps + custom lints have a higher ceiling but no present value); iteration velocity (whole-monolith builds in seconds; LLM-generated code lands correct more often in Go). The pivot is delivery-channel strategy, not technique: if the browser tier becomes *the* primary channel, scoped Rust-host wins and the SD1 bridge should be built only to transition scale (§SD11). One property to preserve under any Rust-host future: the serialized command stream's record/replay affordance (ADR-0057 capture) — keep an optional tap or move capture to the epaint layer; do not lose it silently.
- **Remote shapes (out of this design space).** *Renderer-only in the browser, FFFI2 over WebSocket to a native keelson host* (ADR-0024's O5): mechanically cheap given this ADR's Phase 1 (the transports are already interface-shaped; SD3 coalescing makes the per-frame RTT viable at LAN grade), but it executes keelson server-side — for that problem the project's chosen answer is **pixel streaming (ADR-0024 O1)**, reaffirmed during this design dialogue; carrying both remote shapes is redundant. *Pixel streaming* itself: complementary, not competing — zero wasm, full keelson semantics, per-session GPU/encoder cost; the right tool for many weak/heterogeneous clients against one server-resident instance.
- **TinyGo guest.** Better codegen and far smaller modules, attractive under several options above — ruled out by its reflection and GC limitations: leeway's `marshallreflect`, the jsonv2 paths, and gofakeit-driven testing are reflection-heavy.

## Consequences

### Positive

- **The browser becomes a delivery channel on machines where no other channel exists.** Static URL, no install, no admin rights, sandboxed by construction; the demo tier additionally has no backend and no egress — properties a security officer can verify directly, aligned with the sovereignty posture.
- **The plan is no-regret under strategic uncertainty.** Phase 0/1 (spike, probe, renderer web build) are correct under every branch including a later Rust-host decision; SD3 (fetch coalescing) and SD5 (fonts-by-bytes) improve the native path regardless.
- **Protocol, codegen, and contracts are untouched.** FFFI2 framing, the ADR-0013 widget contract, egui2gen, and the native process model carry over unchanged; the new code is a bounded bridge + shims + build gates.
- **The renderer half is de-risked by direct evidence** (egui.rs, imgui_explorer) before any investment; the remaining risk is concentrated into one measurable number (SD2).

### Negative

- **The Go half pays the wasm tax until the spike says otherwise.** Single thread, shared GC, 1.5–3× codegen. The SD2 gate exists because this can fail on the target hardware; the documented fallbacks are the remote shapes and an accelerated O7 decision.
- **Reduced in-tab app surface.** No imztop, no fsbroker file-watching, no chlocal-backed SQL without a reachable server CH; apps degrade per SD8/SD9 rather than port silently.
- **A new artifact class to maintain**: two wasm modules, JS shell/glue, WASI shims, web build lanes — alongside, not replacing, the native build.
- **First load through scanning middleboxes is slow** (one-time per cache; SD7(e) mitigates perception, not the scan).
- **Tab discard loses in-memory state at v1** (SD9; OPFS follow-up named).
- **Per-guest-target residual costs**: `wasip1` — a DIY http host import and `go:wasmexport`/timer semantics to verify; `js` — a standing event-loop discipline invariant on all frame-path code.

### Neutral

- **ADR-0024 stands as the server-resident sibling**; this ADR neither supersedes nor competes with it (an `## Updates` cross-reference is added there).
- **Bundle size is irrelevant by premise** (LAN, immutable caching) — the criterion that helped rule out wasm-in-browser in ADR-0024's framing does not apply here.
- **Single-canvas constraint is moot**: the desktop app is already single-viewport.

### Derived practices

- **Probe before building**: for browser-delivery questions, a public URL running the same renderer class (egui.rs) answers fleet-policy questions for free; engineering starts after the probe, not before.
- **The boundary-altitude table is a reusable heuristic**: when a cross-language seam feels heavy, compare altitudes before moving it — the cheapest seam is usually the highest one that still batches and the lowest one that avoids reimplementing a dependency.
- **wasm-guest seams are this repo's standard pattern for native-code reuse without CGO** (h3o-under-wazero precedent; O2's WASI host imports; the prospective untrusted-app sandbox) — preferred over linking, matching the encoder-as-subprocess practice of ADR-0024.
- **Platform-surface exclusions ride build tags** with capability-absent paths, mirroring the existing `gpu_intel`/`gpu_nvml`/`gpu_rocm` pattern, rather than runtime feature probing.

## Status

Accepted — 2026-06-21 (reviewed by @spx). Phasing: **Phase 0** — SD7(f) fleet probe + SD2 guest-target spike (gating number) → **Phase 1** — renderer web build (SD4, SD5), boots in a tab against a synthetic stream → **Phase 2** — SD3 fetch coalescing on the native path → **host-language strategy ADR** (SD11 gate; §Alternatives O7) → **Phase 3** — SD1 bridge at scale, SD8 build-tag sweep, SD9 data plane → **Phase 4** — SD10 static demo tier as a named deliverable.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. See boxer's `DOCUMENTATION_STANDARD.md` §1 ADR for the edit-policy tiers (Tier 1 in-place / Tier 2 `## Updates` H3 / Tier 3 superseding ADR).

## References

- [ADR-0024](./0024-imzero2-remote-access-browser-viewer.md) — remote access via pixel streaming; the server-resident sibling; its O5 is the nearest prior evaluation of wasm-in-browser, under different premises.
- [ADR-0013](./0013-imzero2-stateful-widget-contract.md) — the deferred-response widget contract the bridge must preserve.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — AppI/Manifest/caps; §SD10 capslock (static counterpart to the prospective wasm-sandbox seam).
- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — chlocal pool (excluded in-tab per SD8; §Updates O4b purego is adjacent prior art on embedding-without-CGO).
- [ADR-0035](./0035-keelson-namespace-introduction.md) — keelson namespace and pillar layout.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — demo registry + capture drivers; the record/replay affordance to preserve under any future host-language change.
- [ADR-0062](./0062-imzero2-render-cadence.md) — reactive cadence; promoted to web default by SD6.
- Key code: `public/thestack/fffi2/runtime/fffi2_rt_channel.go` (transport interfaces, framing); `public/thestack/imzero2/egui2/bindings/egui2_statemanagement.go` (`Sync()` fetches); `public/thestack/imzero2/application/application.go` (spawn, fonts, file-mode transport); `rust/imzero2/src/fffi/io.rs`, `src/imzero2/entry.rs`, `src/imzero2/interpreter.rs` (Rust transport, entry, interpreter); `rust/imzero2/Cargo.toml` (eframe/wgpu features, native-only deps); `public/science/geo/h3/internal/h3o_wasm/` + `scripts/ci/h3_wasm_parity.sh` (wasm-guest precedent).
- [egui.rs](https://www.egui.rs/) — eframe web demo; exact-renderer probe. [imgui_explorer](https://pthom.github.io/imgui_explorer/) — Dear ImGui Bundle via emscripten; tested by @spx, runs well; single-native-module prior.
- [RLBox](https://rlbox.dev/) — wasm-SFI framework (USENIX Security 2020); the O5 mechanism, production-shipped in Firefox. [`wasm2c` (WABT)](https://github.com/WebAssembly/wabt/tree/main/wasm2c), [`w2c2`](https://github.com/turbolent/w2c2) — wasm→C AOT translators.
- [`wazero`](https://wazero.io/) — pure-Go wasm runtime (already in go.mod, v1.11.0). [`wasmi`](https://github.com/wasmi-labs/wasmi), [Wasmtime Pulley](https://docs.wasmtime.dev/) — interpreter-class engines considered for O4.
- [`browser_wasi_shim`](https://github.com/bjorn3/browser_wasi_shim) — wasip1-in-browser import shim pattern (O2). Go `GOOS=wasip1` port, `go:wasmimport`/`go:wasmexport` (Go ≥1.24 reactor mode) — O2 guest mechanics; blocking semantics to verify per SD2.
- [`web-time`](https://crates.io/crates/web-time), getrandom `wasm_js` backend — SD4 plumbing.
