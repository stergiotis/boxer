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

// Kind classifies a package by its primary role — what the package *is* — when
// it is not ordinary library/production code (ADR-0080 §SD4, 2026-07-02 Update).
// Unlike the WASM* verdicts there is no survey that computes it, so it is
// human-curated and `wasmsurvey props verify` does not reconcile it. The zero
// value is KindUnspecified, so an unset field asserts nothing — the common case
// being ordinary library code that carries no special role.
type Kind uint8

const (
	KindUnspecified     Kind = iota // ordinary library code / not classified
	KindDemo                        // a runnable demonstration (e.g. a keelson demo scene)
	KindExample                     // illustrative usage code, not part of the shipped surface
	KindIntegrationTest             // a package whose primary role is integration testing
)

// String renders the kind as a stable lowercase token (for harvest tables and
// diagnostics). KindUnspecified renders "unspecified"; display sites that want a
// blank cell for the common case should special-case it.
func (k Kind) String() (str string) {
	switch k {
	case KindDemo:
		str = "demo"
	case KindExample:
		str = "example"
	case KindIntegrationTest:
		str = "integration-test"
	default:
		str = "unspecified"
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

	// Kind classifies the package's primary role (demo / example /
	// integration-test) when it is not ordinary library code. Human-curated;
	// the zero value KindUnspecified asserts nothing (ADR-0080 §SD4).
	Kind Kind
}
