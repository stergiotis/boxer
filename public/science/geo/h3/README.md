---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

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
- **CSR for variable-arity outputs.** Operations that emit a variable number
  of results per input row (children, gridDisk, string encoding) return a flat
  values slice plus a `[]int32` offsets slice with `len(offsets) == N+1`,
  `offsets[0] == 0`, monotone non-decreasing.
- **Per-element `StatusE`.** Bulk-level `error` is reserved for WASM traps and
  I/O; per-element failures set a `StatusE` byte so partial batches survive.
- **Pool of WASM modules.** `wazero.Runtime` is shared across goroutines;
  instantiated modules are not — check one out via [`Runtime.AcquireE`] and
  return it via [`Handle.Release`].

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
