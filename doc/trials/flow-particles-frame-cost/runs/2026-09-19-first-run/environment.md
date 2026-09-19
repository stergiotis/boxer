---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — 2026-09-19 first run

- **Machine:** a handheld-class AMD custom APU, 8 hardware threads, integrated
  GPU, 16 GB of memory shared with it; Linux 6.11, x86-64.
- **Toolchains:** Go 1.27.0; rustc 1.96.1; egui 0.35.0.
- **Build under test:** boxer at the commit that adds this run (the harness and
  `Options.LinePerSegment`), over `5c5d4a1d`. Both hosts built from that tree:
  `build_rust_headless_soft.sh` (cpu) and `build_rust_headless.sh` (wgpu).
- **Canvas:** host window 1100 × 900; the demo's map 960 × 600, clipped by the
  gallery window to its upper part — the same clip in every cell.
- **Not idle.** Other work ran on the machine during the run, including at
  least one compile. One `go vet` was started by the operator during the
  second cell. The scene runner rebuilds the Go app before every cell; an edit
  to the layer landed between the second and third cells (a 48-byte per-frame
  allocation removed from `paint`), so cells from the third on ran that build.
- **One launch per cell**, `REPEATS=1`.

`results.tsv` holds the measurements; `raw/` the lines they were read from.

## Profile of the Go side

`go test -bench BenchmarkDraw -cpuprofile` on the layer's package, 10 000
particles, a tick per frame, paint commands discarded. Flat and cumulative
share of 2.94 s sampled:

```
 flat%   cum%
20.07%  44.56%  fffi2/runtime.(*Marshaller).WriteUint32
13.27%  17.01%  bytes.(*Buffer).Write
11.56%  76.53%  flowoverlay.(*Layer).paint
 7.14%  24.49%  fffi2/runtime.(*Marshaller).writeBuf
 4.76%  46.94%  fffi2/runtime.PutFloat32SliceArg
 3.06%  12.93%  flowoverlay.(*sim).tick
        13.27%  fffi2/runtime.PutUint32SliceArg
```

`PutFloat32SliceArg` and `PutUint32SliceArg` together are about 60 % of the
frame: a slice argument is written one element at a time, each through a
four-byte `io.Writer.Write`.
