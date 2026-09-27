---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** One run on each of two machines,
> one browser on each; §0 says what that is worth. Do not cite as
> authoritative.

# Keelson wasm frame cost — the Phase-0 spike of ADR-0077

## 0 The claim, and how to cite it

**On two machines a decade apart in class, one Go frame of the unmodified
imzero2 application layer costs about four times its native time under
wasm — 3.6 to 4.4× across both machines, both Go targets and both browser
engines — and what the in-page bridge adds on top is set by how often Go
flushes, not by how many bytes cross: with a flush after every message the
`GOOS=js` bridge costs 9 to 30 µs a message and the `GOOS=wasip1` bridge
0.5 to 1.3 µs, and with flushes deferred to the next blocking read both
bridges cost under a millisecond a frame.** Under those conditions
[ADR-0077](../../adr/0077-keelson-browser-wasm-execution.md)'s acceptance
gate — Go frame plus bridge within about half of a 60 Hz budget — holds on
the handheld for a gallery-class frame (100 kB, 600 messages: 2.8 ms in
Chromium with deferred flushes) and fails there for a frame of 8 500
messages (300 kB: 12.8 ms), where the Go side's own wasm cost is the limit;
on the desktop-class machine the same 8 500-message frame passes at about
5 ms in Node and in Firefox.

| Median per frame, `wasip1` unless noted | handheld, gallery (99 kB, 579 msgs) | handheld, labels (302 kB, 8 533 msgs) | desktop, gallery | desktop, labels |
| --- | --- | --- | --- | --- |
| native, in-process (no transport) | 0.5 ms | 2.8 ms | 0.2 ms | 1.0 ms |
| native, pipe to the stub, flush per message / deferred | 2.2 / 0.7 ms | 13.9 / 2.7 ms | 0.5 / 0.3 ms | 3.2 / 1.2 ms |
| wasm in Node, in-process | 2.1 ms | 10.6 ms | 0.8 ms | 4.4 ms |
| wasm in Node, bridge, flush per message / deferred | 3.0 / 2.4 ms | 21.6 / 11.5 ms | 1.2 / 0.9 ms | 8.7 / 4.8 ms |
| wasm in a browser, bridge, flush per message / deferred | Chromium 4.1 / 2.8 ms | Chromium 29.5 / 12.8 ms | Firefox ≈1.9 / 1.5 ms | Firefox 12 / 5 ms |
| the same with `GOOS=js`, flush per message / deferred | Chromium 21.0 / 4.7 ms | Chromium 288.6 / 17.0 ms | Firefox 7 / ≈2 ms | Firefox 97 / 6 ms |

Handheld: an AMD custom APU, 8 threads, loaded, Chromium 144. Desktop: a
Ryzen AI MAX+ PRO 395, 32 threads, idle, Firefox 149. Firefox rounds the
in-worker clock to 1 ms, so its rows marked ≈ are read from the run's wall
time per frame instead. Both machines ran their CPUs under the `powersave`
governor of `amd-pstate-epp`.

**M1 — what a real frame is.** Counted between the Go host and the real
CPU-rasterizer client over 63 screens of play's maintained tour (2026-09-25,
handheld): a play frame is **about 180 messages** (p50 179, range 159–276
across screens), with **20 to 700 kB per frame of which 80 to 95 % is one
message**, the deferred dock body (`DockAreaRaw`), and the occasional
multi-megabyte image upload on a single frame. The Go side spends
**3 to 8 ms natively** on such a frame on the handheld (its own status bar,
four captures). So for real frames the message count is a non-issue at
under 4 % of the gate, the synthetic `labels` scene overstates it forty
times, and the wasm cost that matters is the 4× on Go's own per-frame work —
decoding, formatting and capturing the body — which puts a play frame at
roughly 12 to 30 ms of Go on the handheld under wasm and 3 to 8 ms on the
desktop.

| Play screen (handheld, native, from the app's status bar) | messages / frame | bytes / frame | Go | Rust |
| --- | --- | --- | --- | --- |
| Table, leeway result, 100 rows a page | 179 | 67 kB | 2.9 ms | 3.2 ms |
| Projection, 1 500 entities | 179 | 142 kB | 4.9 ms | 1.5 ms |
| Map, raster, 500 rows | 179 | 144 kB | 5.1 ms | 1.5 ms |
| Table, tagged ids, 8 rows, just after the query | 180 | 34 kB | 7.6 ms | 4.8 ms |

**Why the 4×, and what moved it** (2026-09-25, handheld): not lost GC
parallelism (one processor costs nothing natively), not bounds checks or the
wasm feature flags (under 10 % together); the in-process profile is flat,
the same shape as native — Go's wasm code generation. Two frame-shrinking
changes followed. Deferring the channel's flushes to the next blocking read
is now the default (no change on play's 180-message frame, the gain is for
message-heavy widgets and the bridge). Replaying play's table cells while
nothing they depend on changes cut the Table frame from 2.8 to 2.1 ms of Go
in an A/B on a quiet machine, with clicks still taking the live path.

**M2 — the Rust half, and a whole frame** (2026-09-26, handheld, Firefox
and Chromium as flatpaks): the shared interpreter and egui compiled to
wasm32 as the browser host (`rust/imzero2 --features browser`) answer the
same Go module through the same shim, and the whole gallery frame — Go plus
the Rust pass, tessellation and mesh serialization — is **5.1 ms in Node,
6.1 ms in Chromium and 6.0 ms in Firefox on wasip1**, 8 ms on js; within
ADR-0077's gate on the handheld. Of the Rust side's 3.0 to 3.7 ms, the pass
is 1.8 to 2.3 ms and about 0.7 ms is the 24 fetch round trips, the first
figure that argues for fetch coalescing on time rather than hygiene. The
same worker's mesh, posted to the existing viewer page in a new `?worker=`
mode, was painted by the page's own painter with no change to it. The
synthetic `labels` scene costs the Rust side 29 to 39 ms because egui lays
out its 6 800 widgets without virtualization; play's tables are virtualized.

| Whole frame, handheld, gallery (Go + Rust) | Node | Chromium | Firefox |
| --- | --- | --- | --- |
| wasip1 | 5.1 ms | 6.1 ms | 6.0 ms |
| js | 6.7 ms | 8.2 ms | 8.0 ms |

**Input, and a real application** (2026-09-26): with the Go module built as
a wasip1 reactor the worker owns the cadence and yields between frames, so
the page's input reaches the host; a click on fibscope's "invalid" button,
painted by the viewer page from the worker, changed the app's state in the
next frames. fibscope's explore frame costs about 1.5 ms whole in the tab
on the handheld. The reactor costs nothing over command mode (paired: Go
5.7 against 5.6 ms, host 5.4 against 5.5 ms on a loaded machine).

**The fetch batch** (2026-09-26, handheld, loaded): with `Sync` issuing its
24 fetches before reading the first reply, the host is stepped once a frame
instead of 24 times; paired against the same tree without the change, that
is worth about 0.2 ms a frame in Node and 1 to 2 ms of a 45 ms gallery
frame in headless Chromium, less than the 0.7 ms M2 attributed to the
steps by subtraction. The logbook entry has the pairs.

**Real apps in the tab** (2026-09-27, handheld, functional, no numbers):
with the four Linux-only corners behind build tags and one host import for
HTTP, taskdemo, mdedit and play mount in the browser worker on an
in-process bus, and play's Run returned 50 rows in 74 ms from ClickHouse
through a same-origin proxy. The logbook entry lists what the tab still
lacks.

**Both machines on one pack** (2026-09-27, `powersave`, gallery of eleven
demos, 129 kB a frame): Go in-process 0.85 ms native / 4.2 ms wasm on the
handheld and 0.41 / 1.54 ms on the desktop; a whole frame through the real
host 8.2 ms in Node, 10.1 in Chromium and 10.0 in Firefox on the handheld,
3.2 and 4 ms on the desktop. The `performance` governor rerun is still
open. Mesh parity against the native
appliance: the first full frame is 1.40 MB native against 1.36 MB in the
browser host; a settled scene posts nothing on either. The gallery that
would not settle in the command-mode arms turned out to be goroutine
starvation under the synchronous shim — a query lane's spinner that never
got to fail — now yielded around each frame in the render loop.

**What this trial does not say.**

- **Not "the browser tier costs 4× the desktop."** The stub interprets
  nothing: no egui pass, no layout, no tessellation ran in the spike's
  arms; those figures are the Go frame producer and the transport alone,
  which is what ADR-0077 SD2 asked to measure. Only the `host` arms carry
  the Rust half, and they carry it without input, fonts as bytes or the
  wasm-bindgen glue a few dependencies expect.
- **Not "300 kB a frame is the app."** The `labels` scene is synthetic — a
  grid sized to reach the frame ADR-0077 quotes — and its 8 533 messages are
  35 bytes each. M1 measured real play frames at about 180 messages, so
  the `labels` rows describe a message-heavy frame play never produces; they
  bound the bridge, not the app.
- **Not "a play frame costs 12 to 30 ms in wasm."** That range is the
  handheld's native Go time multiplied by the in-process wasm ratio of this
  trial; no play frame has run under wasm (M2 and ADR-0077's build-tag sweep
  come first), and the native times are the app's status-bar figures from
  four captures on a loaded, `powersave`-governed machine.
- **Not a native regression claim.** The deferred-flush native row measures
  a channel option that was off by default when the row was taken and has
  since become the default; its effect on the Rust side's overlap with Go
  production is untested.
- **Not one browser against the other.** Chromium ran only on the handheld
  and Firefox only on the desktop; the engines were not compared on one
  machine. Not reviewed, not replicated: one run per machine, one of them
  not idle.

**If you need a number**, take it from `runs/<date>/results.tsv`. **No figure
from this trial travels without the pair of arms it compares.**

## 1 Question and scope

1. **The trial proper.** Can the unmodified Go application layer — the
   `application` package, the generated bindings, `StateManager.Sync`, real
   registry demos — run in a browser at all, and what does it cost there
   against native? Every escape hatch taken on the way is a finding.
2. **The side-product.** ADR-0077 SD2's gating number: Go frame plus bridge
   against the frame budget on the oldest available machine, per Go target,
   and the choice between `GOOS=js` and `GOOS=wasip1`.

In scope: the Go side and the fd 0/1 bridge. Out of scope: the Rust
interpreter and egui under wasm (ADR-0077 treats the renderer as validated
by egui's own web builds), ClickHouse access from a tab, bundle size.

## 2 The workload

Two scenes rendered by the
[`wasmspike`](../../../public/thestack/imzero2/egui2/demo/wasmspike/main.go)
command each frame:

- **gallery** — every registry demo linked into the binary, each on its own
  stage in one frame. Linked in: the demos that compile for both wasm
  targets *and* emit the same frame on every target (`idsshowcase`,
  `leewaywidgets`, `logdemo`); `sccmap` compiles but reads the filesystem, so
  its frame differs and it is left out.
- **labels** — a synthetic grid of `ROWS` rows, each a label, a button, a
  checkbox and a slider with a bound value. `ROWS=1700` gives about 300 kB a
  frame, the figure ADR-0077 quotes for a real app frame.

**M1** counts real frames instead: `fffi2stub tee` sits between the Go host
and the real CPU-rasterizer client, forwards everything untouched, and
records messages and bytes per frame and per opcode. The scene runner
launches play with `--clientBinary` pointing at a wrapper that runs the tee
around the client (`scenes/` holds the two M1 table scenes; the rest of the
figures come from `apps/play/scenes`, the maintained tour). Frames are
delimited by `FetchFrameMetrics`, the last fetch of every Sync.

The peer of the spike is [`fffi2stub`](../../../rust/fffi2stub/src/lib.rs): it walks the
`u32 len, u32 opcode, payload` framing, counts, and answers every fetcher
with zero scalars and empty slices. Its table of fetcher opcodes and reply
shapes is derived from the generated bindings at run time (`-dumpFetchTable`),
so the stub cannot drift from the tree.

## 3 System under test and arms

Every arm runs the same Go binary source; they differ in target, consumer
and flush policy.

| Arm | Go runs as | Consumer | What it isolates |
| --- | --- | --- | --- |
| `native-inproc` | linux/amd64 | in-process channel answering from memory | Go emission cost, no transport |
| `native-pipe-{eager,lazy}` | linux/amd64 | the stub binary over a pipe (as on the desktop) | the pipe transport; flush per message vs deferred |
| `node-<target>-inproc` | wasm in Node's V8 | in-process | Go's wasm cost |
| `node-<target>-pipe-{eager,lazy}` | wasm in Node | the stub wasm module via the JS fd shim | the in-page bridge (ADR-0077 SD1) |
| `<browser>-<target>-pipe-{eager,lazy}` | wasm in a browser worker | same shim | the bridge in a real browser |
| `node-<target>-host`, `<browser>-<target>-pipe-host` | wasm, both modules | the real Rust host (`imzero2.wasm`) via the shim | the whole frame: Go, the pass, tessellation, mesh serialization |

`eager` flushes after every message, the shipped behaviour; `lazy` flushes
only when the writer's buffer fills or before a blocking read
(`InlineIoChannel.SetDeferFlush`), which the lock-step protocol permits.

The bridge is the same for both targets: fd 1 writes go synchronously into
the stub module, its replies are queued and served to fd 0 reads. For
`GOOS=js` that path is `syscall/js` and `wasm_exec.js`'s `fs` object; for
`GOOS=wasip1` it is a hand-written `wasi_snapshot_preview1` import object
(`harness/bridge.js`). Both arms run entirely inside one worker: no
`SharedArrayBuffer`, no COOP/COEP headers.

Standing hypotheses: **H1** the wasip1 fd path is byte-identical to the
desktop pipe and needs no Go-side change (ADR-0077 O2); **H2** Go under wasm
costs 1.5–3× native (ADR-0077 §Context); **H3** the js target's per-call
overhead makes per-message flushing the dominant bridge cost; **H4** the
browser engines differ materially from each other for this workload.

## 4 Method

- **Environment.** Recorded per run in `runs/<date>/environment.md`: CPU
  class, memory, kernel, the build's toolchain versions, Node and browser
  versions, load, and the run's settings. Never hostnames or personal paths.
- **Per run:** [`measure.sh`](./measure.sh) builds the stub natively and
  for wasm32 and the Go command for native, js and wasip1, derives the fetch
  table, and runs every arm with the same `FRAMES` after `WARMUP` discarded
  frames. `results.tsv` holds one row per arm: bytes and messages per
  frame, the Go side's render and Sync times at p50, p90 and max (measured
  by `metrics.Current`, the same clock the desktop overlay uses), the bridge's
  own time per frame, and wall time. Raw reports sit under `raw/`.
- **Another machine** runs the same arms from a pack: `measure.sh --pack`
  builds everything into a directory, and `PREBUILT=<dir> measure.sh` on a
  host with only Node and a browser runs it; native arms need the packing
  host's OS and architecture. The desktop run was made this way.
- **Idiom rule.** The pipe and bridge arms go through `application.Launch`
  and `Run` unchanged. The in-process arms bypass the transport by
  construction; they are reference points, not the product path.
- **Browser arms** load a page (`harness/index.html`) whose module worker
  runs the arm and POSTs its report to the serving host (`harness/serve.mjs`),
  which prints it and exits; no DOM dumping, no virtual time. A sandboxed
  browser must be able to see the throwaway profile directory
  (`PROFILE_DIR`); one that cannot never loads the page.
- **The demonstrator.** `harness/host_worker.mjs` runs both modules in a
  worker — the Go module as a wasip1 reactor whose `frame` export the worker
  calls per tick, yielding in between so the page's input arrives — and
  posts each mesh message; the viewer page in `?worker=` mode paints them
  and forwards its input. Serve a directory holding the page, the harness
  files, `imzero2.wasm` and `wasmspike_wasip1_reactor.wasm` with
  `serve.mjs`, open
  `index.html?worker=host_worker.mjs%3Fscene%3Dgallery%26demo%3Dfibscope-explore`,
  or screenshot it headless after real seconds with `cdp_shot.mjs`, which
  can click first (Chromium needs `--enable-unsafe-swiftshader` for WebGL2
  there).
- **Reporting.** Each run appends a [logbook](./logbook.md) entry.

## 5 Findings ledger

Findings follow the [directory convention](../README.md): competence ×
relation × ISO 25010 characteristic, severity, evidence in the run dir.
Pre-registered candidates: the per-Flush host call (H3), wasm codegen
slowness (H2), Go packages that fail to build for wasm, browser sandbox
friction.

## 6 Milestone cut

- **M0 — the spike ✓** (2026-09-25). Both targets, both consumers, both
  flush policies, native and Node and one browser per machine, two scenes,
  two machines. Gate: a RESULT line from every arm.
- **M1 — a real frame's message count ✓** (2026-09-25). 63 play screens
  counted through the tee, two M1 table scenes added; §0 re-read: message
  count is not the lever for real frames, Go's per-frame work is. The wasm
  arm of a real frame is M2's, since it needs the Rust half and the
  build-tag sweep.
- **M1b — why the 4×, and two levers ✓** (2026-09-25). GOMAXPROCS, GOGC
  and codegen-knob arms, profiles of both sides, deferred flush as default,
  the cells cache in play. Gate: the A/B.
- **M2 — the Rust half ✓** (2026-09-26). The browser host built from the
  shared interpreter, measured in Node and both browsers on both scenes,
  and painted by the existing viewer page from a worker. The gate was
  softened from wire parity to "the page paints the frame": the native mesh
  host's bytes were not captured for comparison.
- **M2b — input, and a real app ✓** (2026-09-26). The reactor entry, the
  input path through the carrier's translator, fibscope painted and
  clicked in the page. Gate: a click changes the app's state on screen.
- **M3 — a real app frame through the host.** The build-tag sweep of
  ADR-0077 SD8 so play links for wasm, then the Table scene through this
  host. Gate: the arm runs on wasip1.

## 7 Open questions

1. Does deferring flushes cost the native pipeline its overlap between Go
   production and Rust interpretation? The buffer still flushes every 4 kB,
   so probably little; unmeasured.
2. What fraction of the wasm cost is Go's codegen against the runtime's
   memory model (bounds checks, no register allocation)? A profile of the
   in-process arm would say.
3. Both engines on one machine: Chromium on the desktop and Firefox on the
   handheld (its flatpak sees only `~/Downloads`, so `PROFILE_DIR` must
   point there).
4. Every figure so far was taken under the `powersave` governor; a
   `performance` run on both machines would say how much of the handheld's
   frame is frequency ramp rather than work.
5. With the cells replayed, where do the remaining 2.1 ms of play's Table
   frame go? The profile charged more to the cells than the A/B recovered;
   the parameter extraction that parses the query every frame and the
   headers are the first suspects.

6. A reactive cadence for the worker: tick on input, on a timer the app
   asks for, and on animation, instead of a fixed rate that runs a Go frame
   whether or not anything changed.
7. Mesh-wire parity between the browser host and the native mesh appliance
   on the same scene: the bodies and textures should be byte-identical.
8. Fetch coalescing's effect on the host arm, where 24 steps are about
   0.7 ms of a 3 ms Rust frame.

Related: [ADR-0077](../../adr/0077-keelson-browser-wasm-execution.md),
[ADR-0128](../../adr/0128-imzero2-mesh-draw-stream-codec-lane.md), the
[worker-mesh design page](../../adr-background-work/imzero2-in-browser-worker-mesh.md).
