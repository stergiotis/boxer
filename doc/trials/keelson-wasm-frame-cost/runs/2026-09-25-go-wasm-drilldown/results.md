---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Go-under-wasm drill-down, handheld, 2026-09-25

All arms in-process (no transport), `powersave` governor, machine not idle
(other sessions). Medians of the Go side's render time per frame.

## Is it lost GC parallelism? No.

| scene | GOMAXPROCS=8 | GOMAXPROCS=1 | GOGC=off (8) |
| --- | --- | --- | --- |
| gallery (579 msgs) | 0.53 / 0.55 ms | 0.50 / 0.45 ms | 0.30 ms |
| labels (8 533 msgs) | 2.21 / 2.05 ms | 2.15 / 2.17 ms | 2.04 ms |

One processor costs nothing natively, so the wasm ratio is not the GC
losing its helper threads. Turning the GC off takes 40 % off the gallery
frame: that frame is allocation-bound, and under wasm the collector runs
inline in the frame.

## Do the codegen knobs help? Under 10 %.

wasip1, Node 24, render p50 of two runs each:

| scene | base | GOWASM=satconv,signext | -gcflags=all=-B | both |
| --- | --- | --- | --- | --- |
| gallery | 2.22 / 2.29 ms | 2.04 / 2.25 ms | 1.98 / 2.09 ms | 1.97 / 1.91 ms |
| labels | 10.13 / 10.14 ms | 10.41 / 10.07 ms | 9.93 / 9.95 ms | 9.92 / 10.28 ms |

Bounds checks and the two wasm features together buy about 10 % on the
gallery frame and nothing on the message-heavy one. The remaining ratio is
Go's wasm code generation and runtime model (stack in linear memory,
no register allocation onto locals), not a knob.

## Where does the wasm time go? Everywhere at once.

`node --cpu-prof` of the wasip1 in-process labels arm, self time
(profile-wasip1-labels.txt): marshaller 9 %, bindings 8 %, builder 4 %,
`bytes.Buffer.Write` 4 %, allocation ~10 % across `mallocgc*`, span scan
3.5 %, `sync.Pool` pin/put 4 %, map access 4 %, `fmt` 3 %, memmove 3 %.
The native profile of the same scene (pprof-native-labels.txt) has the same
shape. A flat, uniform slowdown is the signature of a codegen tax, not of
one pathological function.

## Where does a real play frame go? The table's cells, then GC, then syscalls.

Native pprof of play on the Table tab, 25 s at 30 fps, 7.0 s of samples
(pprof-play-table.txt): `PlayApp.Frame` 1.95 s = 2.6 ms per frame, of
which `renderMasterTable` 1.49 s and `selectableCell` 1.17 s — about 2 000
visible cells at 0.8 µs each, spent emitting five opcodes and closures per
cell, not formatting strings. Process-wide: GC scan and allocation about a
third of CPU; `Syscall6` 11 % (pipe writes and reads); the SQL parser 20 %
(`codeview.PrepareSql` re-parsing off the frame path); system-metrics
sampling 12 %. The last two are findings, not frame costs.
