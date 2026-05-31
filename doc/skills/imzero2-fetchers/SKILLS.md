---
type: explanation
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# imzero2 fetcher discipline

ImZero2 fetchers (`Fetcher.Fetch*` opcodes — every method on the `Fetcher`
type in `public/thestack/imzero2/egui2/bindings/fetchers.out.go`)
must run only from `StateManager.Sync()` at frame end, after every
deferred-block capture buffer has flushed. Calling a fetcher anywhere
else — typically "inline" from a widget render body — is silent at top
scope but **deadlocks the render loop** as soon as the calling code
ends up inside a deferred-block capture (a `dock.Tab` body, an
`etable` row body, any future `iter`-based wrapper that uses
`BeginCapture`).

This skill explains why the rule exists, how the wire protocol drifts
out of sync when it's violated, and what to do instead.

## Background

The FFFI2 transport is a single bidirectional byte pipe between Go and
Rust. Go writes a stream of opcodes; Rust reads, interprets, and
writes responses back. The Go side has two write paths:

- **Direct send** — `Fffi2.SendIntermediate(buf)` writes `buf` to the
  pipe and flushes. Rust reads it on the other end and dispatches.
- **Capture send** — also `Fffi2.SendIntermediate(buf)`, but when the
  current goroutine has pushed a capture frame via `BeginCapture` (as
  every deferred-block iter does in its prologue), the bytes go into
  the *innermost capture buffer* instead. The capture frame is
  finalised later (in the iter's `defer EndCapture()` body) and the
  buffered bytes are then re-emitted into the surrounding scope.

Capture is what makes nested deferred-block iters work — `dock.Tab`'s
body needs to be serialised into the dock area's per-tab byte map,
not interleaved with the surrounding stream. So the `SendIntermediate`
*dispatch* (pipe vs capture buffer) is implicit, switched by
`captureStack` depth.

For widgets that **only emit** (Button, Label, c.Window…) this works
transparently — they `Send()` opcodes and never expect a response.
The bytes land in whatever buffer is active; everything ends up at
Rust in the right order by the time the outermost scope flushes.

Fetchers are different. A fetcher writes one opcode (the fetch
request) and then **immediately reads a response**. The read is
synchronous on the pipe. So a fetcher needs its request to be
**flushed to Rust before it reads** — and a request that landed in a
capture buffer is not flushed yet. The pipe is empty from Rust's
perspective; Rust blocks waiting for more bytes; Go blocks waiting
for the response. Mutual deadlock.

## The rule

> **Every `Fetcher.Fetch*` call must originate from
> `StateManager.Sync()` at frame end.**

`Sync()` runs in `FinishServersideFrame` (see
`bindings/egui2_lifecycle.go`), which is invoked *after* the
top-level render closure returns and *after* every deferred-block
iter has finalised. At that point `captureStack` is empty,
`SendIntermediate` writes straight to the pipe, and fetchers work.

Concretely, the body of `Sync()` drains a fixed set of fetchers and
caches the results in typed fields on the `StateManager`. Widget
render code reads from the cache via `Get*` methods on the next
frame — with the documented one-frame lag that every fetcher already
imposed anyway, because the data being fetched is always *last
frame's* state.

The current cache surface (extend it when adding a new fetcher):

| Getter                                | Cached register / event source | Last-frame source |
|---------------------------------------|--------------------------------|-------------------|
| `GetCanvasPointer() CanvasPointerValue` | `FetchR14CanvasPointer`       | egui `PaintCanvas` hover/click |
| `GetPlotPointer() PlotPointerValue`     | `FetchR15PlotPointer`         | latest egui_plot click |
| `GetWalkersCamera() WalkersCameraValue` | `FetchR15WalkersCamera`       | latest `WalkersMap` camera |
| `GetSnarlEvents() []SnarlEvent`         | `FetchSnarlEvents`            | drained queue from snarl editors |
| `GetGraphEvents() []GraphEvent`         | `FetchGraphEvents`            | drained queue from egui_graphs |
| `GetGraphSelection() []GraphSelectedItem` | `FetchGraphSelection`       | snapshot of all selections |
| `GetGraphMetrics() []GraphMetrics`      | `FetchGraphMetrics`           | per-graph counters |

Plus the broader-purpose families that `Sync` was already managing:
response flags (`FetchR7`), databindings (`FetchR9*`, `FetchR10`),
etable prefetch (`FetchR9EtPrefetch`), frame metrics
(`FetchFrameMetrics`).

## How to add a new fetcher

1. Define the fetcher opcode in
   `public/thestack/imzero2/egui2/definition/egui2_definition_d_fetchers.go`
   (or another `egui2_definition_d_*.go` if it's feature-scoped) and
   the Rust handler in `rust/imzero2/src/imzero2/interpreter.rs`.
2. Run `./generate.sh` to refresh `fetchers.out.go`.
3. In `bindings/egui2_statemanagement.go`:
   a. Add a value type — `XyzValue` — mirroring the fetcher's return
      tuple (for slice fetchers, alias `[]Xyz`).
   b. Add a field on `StateManager` (`xyz XyzValue`).
   c. In the `// Per-frame inline-fetcher snapshot.` block at the end
      of `Sync()`, call the new fetcher and store the result into the
      field. For slice fetchers, reuse the existing slice via
      `out := inst.xyz[:0]; ...; inst.xyz = out` to keep the hot
      path allocation-free.
   d. Add a `GetXyz() XyzValue` method.
4. Document the cache in the table above when you flip this skill's
   front-matter to `stable`.

That's it. No widget code calls the fetcher directly.

## Failure signature

When the rule is broken the symptoms are distinctive, learn them
once:

- **Go goroutine 1** is in `IO wait` deep inside
  `fffi2/runtime.Unmarshaller.readBuf` →
  `bindings.Fetcher.readB/readU32/readU64/…` → the offending
  `FetchXyz()` call. Get the dump via
  `curl -s http://localhost:6060/debug/pprof/goroutine?debug=2`.
- **Rust main thread** (`pebble2_rust` TID) is sleeping in
  `anon_pipe_read`. Check via
  `for t in /proc/<rust_pid>/task/*; do cat $t/wchan; done`.
- The `1 minutes` annotation in the Go stack header confirms the
  block has been static for a long time — not a slow operation.

If you see this signature, find the inline `.Fetch*` call in the
stack and migrate it to a `StateManager.Sync` cache.

## Enforcement

Three independent defences guard the rule:

1. **This document.** The first line of defence — explains the rule
   before someone writes new code.
2. **`scripts/ci/fetcher-discipline.sh`** (called from
   `scripts/ci/lint.sh`). A grep-based check that fails if `.Fetch*`
   is referenced outside an allowlist of legitimate sites
   (`egui2_statemanagement.go`, the generated
   `fetchers.out.go`, `definition/` IDL files, and tests). Catches
   drift in PRs before merge.
3. **Runtime guard in `Fetcher.invoke`.** Panics if the active
   `Fffi2[U].captureStack` is non-empty when a fetcher opcode is
   about to write. Triggers deterministically the first time the
   offending widget renders inside a deferred-block scope. Crash
   message names the fetcher and points back at this skill.

The three layers compose: the lint catches PR-time drift in
controlled environments; the runtime guard catches missed paths in
dev / hmi.sh runs; the doc explains the why so neither check is
mysterious to debug.

## Further reading

- The original deadlock investigation that motivated this rule lives
  in the `2231cae1` commit message (the eight migrated call sites,
  the protocol diagnosis, and the verification path).
- ADR-0026 Amendments 2026-05-12 (M3 dock host) and 2026-05-13 (host
  reverted to per-app egui::Window) — explain why the
  M3 work exposed this latent violation across the codebase.
- `bindings/egui2_statemanagement.go` is the canonical home for the
  fetch+cache pattern; new fetchers extend the `// Per-frame
  inline-fetcher snapshot.` block there.
- `fffi2/runtime/fffi2_rt_impl.go::SendIntermediate` is the
  branching site (capture vs pipe).
