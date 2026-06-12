package packageprops

// WASMState is a package's TinyGo/WebAssembly compile state for one target —
// the verdict computed by the wasmsurvey (ADR-0078) and reconciled against the
// declaration by `wasmsurvey props verify`. The zero value is WASMUnknown, so
// an unset field asserts nothing.
type WASMState uint8

const (
	WASMUnknown  WASMState = iota // not asserted / not yet determined
	WASMCompiles                  // verified to compile under TinyGo for the target
	WASMBlocked                   // does not compile (a transitive blocker — see the survey for blame)
)

// String renders the state as a stable lowercase token (for harvest tables and
// diagnostics).
func (s WASMState) String() (str string) {
	switch s {
	case WASMCompiles:
		str = "compiles"
	case WASMBlocked:
		str = "blocked"
	default:
		str = "unknown"
	}
	return
}

// Props is the curated, co-located property record of a Go package (ADR-0080).
// It is a plain typed struct — refactor-safe and IDE-navigable — read at runtime
// as the package's PackageProps value and statically harvestable into an
// overview table. The zero value asserts nothing. New properties are added as
// fields over time (purity, determinism, ownership, stability, …); wasm
// amenability is the first.
type Props struct {
	// WASM* record the TinyGo/wasm compile state per target (ADR-0078):
	// wasi (GOOS=wasip1), js (browser), and freestanding wasm-unknown.
	WASMWASI         WASMState
	WASMJS           WASMState
	WASMFreestanding WASMState
}
