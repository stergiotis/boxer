---
type: reference
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-23
---

# H3 geospatial indexing bridge

`github.com/stergiotis/boxer/public/science/geo/h3` exposes Uber H3 hierarchical
hexagonal indexing to Go through a Rust→WASM→wazero bridge. The Rust companion
crate at [`rust/h3bridge`](../../../../rust/h3bridge) wraps [`h3o`](https://github.com/HydroniumLabs/h3o)
and is compiled to `wasm32-unknown-unknown`; the resulting `.wasm` is embedded
into the Go package via `//go:embed`.

The design is fixed by [ADR-0003](../../../../doc/adr/0003-h3-wasm-bridge.md).

## Shape

- **Struct-of-Arrays API.** Inputs and outputs are parallel slices
  (`lats []float64`, `lngs []float64`, `cells []uint64`, ...). No `struct{Lat,Lng}` values on the hot path.
- **CSR for variable-arity outputs.** `CellsToChildren`, `GridDisks`,
  `CellsToStrings`, and `CellsToBoundaries` return a flat values slice
  plus a `[]int32` offsets slice with `len(offsets) == N+1`,
  `offsets[0] == 0`, monotone non-decreasing. `PolygonToCells` (SD11) and
  `UncompactCells` (SD14) are flat — see [`EXPLANATION.md`](EXPLANATION.md)
  for the deviations.
- **Per-element `StatusE`.** Bulk-level `error` is reserved for WASM traps,
  use-after-release, and whole-batch semantic errors (e.g., mixed-resolution
  input to `CompactCells` per SD13); per-element failures set a `StatusE`
  byte so partial batches survive.
- **Pool of WASM modules.** `wazero.Runtime` is shared across goroutines;
  instantiated modules are not — check one out via [`Runtime.Acquire`] and
  return it via [`Handle.Release`].

## Available bulk operations

| Method                   | In                                           | Out                                                         |
|--------------------------|----------------------------------------------|-------------------------------------------------------------|
| `LatLngsToCells`        | `[]float64` lats, lngs, `ResolutionE`         | `[]uint64` cells, `[]StatusE`                               |
| `LatLngsIterToCells`    | `iter.Seq2[int, LatLng]`, `n`, `ResolutionE`  | `[]uint64` cells, `[]StatusE` (SD16)                        |
| `CellsToLatLngs`        | `[]uint64` cells                              | `[]float64` lats, lngs, `[]StatusE`                         |
| `CellsToParents`        | `[]uint64` cells, `ResolutionE`               | `[]uint64` parents, `[]StatusE`                             |
| `CellsToChildren`       | `[]uint64` cells, `ResolutionE`               | CSR `[]uint64` + `[]int32` offsets, `[]StatusE`             |
| `GridDisks`             | `[]uint64` cells, `k`                         | CSR `[]uint64` + `[]int32` offsets, `[]StatusE`             |
| `CellsToStrings`        | `[]uint64` cells                              | CSR `[]byte` + `[]int32` offsets, `[]StatusE`               |
| `StringsToCells`        | CSR `[]byte` + `[]int32` offsets              | `[]uint64` cells, `[]StatusE`                               |
| `AreValidCells`         | `[]uint64` cells                              | `[]bool` valid                                              |
| `GetResolutions`        | `[]uint64` cells                              | `[]ResolutionE`, `[]StatusE`                                |
| `PolygonToCells`        | flat vert lats/lngs + ring offsets, `ResolutionE`, `ContainmentModeE` | flat `[]uint64` cells                  |
| `CellsToBoundaries`     | `[]uint64` cells                              | CSR `[]float64` lats + `[]float64` lngs + `[]int32` offsets, `[]StatusE` |
| `Dissolve`              | `[]uint64` same-resolution cells              | two-level CSR `[]float64` lats + `[]float64` lngs + `[]int32` ring offsets + `[]int32` polygon offsets (bulk error only, SD17) |
| `CompactCells`          | `[]uint64` same-resolution cells              | `[]uint64` compacted (bulk error only, SD13)                |
| `UncompactCells`        | `[]uint64` cells, target `ResolutionE`        | flat `[]uint64` expanded, `[]StatusE` (SD14)                |

## Arrow interop

The companion subpackage [`h3arrow`](h3arrow) provides zero-copy adapters
from this package's slice / CSR outputs to `arrow-go` arrays
(`CellsAsArrowUint64`, `Float64sAsArrowFloat64`, `CSRAsArrowListUint64`,
`CSRAsArrowListFloat64`). The returned arrow arrays wrap the caller's
`[]uint64` / `[]float64` / `[]int32` backing arrays directly, so the
caller must keep those slices reachable until the arrow arrays are
`Release()`d. Kept as a subpackage so consumers who do not use arrow-go
do not inherit its import graph.

## Building the wasm artifact

The embedded `internal/h3o_wasm/h3.wasm` is committed to the repository.
Contributors who change the Rust sources rebuild with:

```bash
scripts/dev/build_h3_wasm.sh
```

Prerequisites: `cargo` and the `wasm32-unknown-unknown` target
(`rustup target add wasm32-unknown-unknown` under rustup, or the Fedora
`rust-std-static-wasm32-unknown-unknown` package). CI enforces byte parity
between a fresh rebuild and the committed artifact via
[`scripts/ci/h3_wasm_parity.sh`](../../../../scripts/ci/h3_wasm_parity.sh).

## Further reading

- [ADR-0003](../../../../doc/adr/0003-h3-wasm-bridge.md) — design rationale and sub-decisions.
- [`EXPLANATION.md`](EXPLANATION.md) — H3 theory, pentagons, antimeridian, CSR invariants.
- [H3 specification](https://h3geo.org) — upstream documentation.
