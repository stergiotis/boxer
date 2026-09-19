---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — 2026-09-19, after the bulk slice marshaller

- **Machine, toolchains, canvas, hosts:** as the [first run](../2026-09-19-first-run/environment.md).
  The Rust hosts were not rebuilt: the change is on the Go side and the bytes
  on the wire are the same.
- **Build under test:** boxer at the commit that adds this run, which is the
  one that makes the FFFI runtime write a slice argument's elements in one
  call.
- **Governor:** the system's default, `powersave`; the
  [governor run](../2026-09-19-governor/environment.md) found it makes no
  difference.
- **Not idle:** a browser was open; one-minute load average about 3.6.
- **Cells:** both hosts at 5 000 particles in all three arms and at 20 000 in
  the mesh arm, plus the floor; two launches per cell.

## The Go side under `go test`

`BenchmarkDraw` in the layer's package — one frame with a tick, paint
commands discarded, so no pipe — six runs of 300 frames each, before and
after the change, same machine, minutes apart:

```
before (element-wise slice marshalling):
  BenchmarkDraw/1000-8 713559 ns/op
  BenchmarkDraw/1000-8 711396 ns/op
  BenchmarkDraw/1000-8 714463 ns/op
  BenchmarkDraw/1000-8 709628 ns/op
  BenchmarkDraw/1000-8 713271 ns/op
  BenchmarkDraw/1000-8 718228 ns/op
  BenchmarkDraw/10000-8 7288851 ns/op
  BenchmarkDraw/10000-8 7232708 ns/op
  BenchmarkDraw/10000-8 7274501 ns/op
  BenchmarkDraw/10000-8 7209176 ns/op
  BenchmarkDraw/10000-8 7366789 ns/op
  BenchmarkDraw/10000-8 7228224 ns/op
after (bulk slice marshalling):
  BenchmarkDraw/1000-8 245712 ns/op
  BenchmarkDraw/1000-8 245004 ns/op
  BenchmarkDraw/1000-8 243713 ns/op
  BenchmarkDraw/1000-8 239451 ns/op
  BenchmarkDraw/1000-8 240137 ns/op
  BenchmarkDraw/1000-8 243270 ns/op
  BenchmarkDraw/10000-8 2708558 ns/op
  BenchmarkDraw/10000-8 2592504 ns/op
  BenchmarkDraw/10000-8 2569877 ns/op
  BenchmarkDraw/10000-8 2550409 ns/op
  BenchmarkDraw/10000-8 2592558 ns/op
  BenchmarkDraw/10000-8 2590322 ns/op
```

`BenchmarkPutFloat32Slice` in the FFFI runtime, 100 000 elements, 200 runs:
element by element 1.48 ms; in one write, wire order the machine's own,
15 µs; in chunks, wire order the other one, 0.34 ms.

After the change the benchmark's profile has no marshalling in it beyond a
`memmove` at 7 %: the layer's `paint` is 54 % and `sim.tick` 40 %.
