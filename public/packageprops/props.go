package packageprops

// WASMState is a package's TinyGo/WebAssembly compile state for one target —
// the verdict computed by the wasmsurvey (ADR-0078) and reconciled against the
// declaration by `props verify`. The zero value is WASMUnknown, so
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
// human-curated and `props verify` does not reconcile it. The zero
// value is KindUnspecified, so an unset field asserts nothing — the common case
// being ordinary library code that carries no special role.
type Kind uint8

const (
	KindUnspecified     Kind = iota // ordinary library code / not classified
	KindDemo                        // a runnable demonstration (e.g. a keelson demo scene)
	KindExample                     // illustrative usage code, not part of the shipped surface
	KindIntegrationTest             // a package whose primary role is integration testing
	KindTestSupport                 // helpers meant to be imported by other packages' tests (fakes, assertions, fixtures)
	KindTool                        // a command or governance package of this repository's own tooling (CLI wiring, linters, generators)
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
	case KindTestSupport:
		str = "test-support"
	case KindTool:
		str = "tool"
	default:
		str = "unspecified"
	}
	return
}

// StabilityE states what a consumer outside this module may rely on in a
// package's exported Go API (ADR-0389). It is a promise, not an observation:
// human-curated like Kind, and not reconciled against any survey. `props
// verify` checks only that a Stable package keeps to its own side of the
// promise — it imports no Experimental or Internal package, no demo, example,
// test-support or tool package, and it carries a package doc comment.
//
// The zero value StabilityUnspecified asserts nothing, and the compatibility
// promise in VERSIONING.md covers StabilityStable packages only, so an
// unclassified package is outside it.
type StabilityE uint8

const (
	StabilityUnspecified  StabilityE = iota // not classified; outside the compatibility promise
	StabilityStable                         // covered by the compatibility promise: an incompatible change needs a deprecation window
	StabilityExperimental                   // public and usable, but may change in any MINOR without a deprecation window
	StabilityInternal                       // an implementation detail that Go's import rules happen to expose; not for use outside this module
)

// AllStabilities lists the stability tiers in declaration order.
var AllStabilities = []StabilityE{StabilityUnspecified, StabilityStable, StabilityExperimental, StabilityInternal}

// String renders the tier as a stable lowercase token (for harvest tables,
// the surface document and diagnostics).
func (inst StabilityE) String() (str string) {
	switch inst {
	case StabilityStable:
		str = "stable"
	case StabilityExperimental:
		str = "experimental"
	case StabilityInternal:
		str = "internal"
	default:
		str = "unspecified"
	}
	return
}

// Props is the curated, co-located property record of a Go package (ADR-0080).
// It is a plain typed struct — refactor-safe and IDE-navigable — read at runtime
// as the package's PackageProps value and statically harvestable into an
// overview table. The zero value asserts nothing. New properties are added as
// fields over time (purity, determinism, ownership, …); wasm amenability was
// the first, then Kind, then Stability.
type Props struct {
	// WASM* record the TinyGo/wasm compile state per target (ADR-0078):
	// wasi (GOOS=wasip1), js (browser), and freestanding wasm-unknown.
	WASMWASI         WASMState
	WASMJS           WASMState
	WASMFreestanding WASMState

	// Kind classifies the package's primary role (demo / example /
	// integration-test / test-support / tool) when it is not ordinary library
	// code. Human-curated; the zero value KindUnspecified asserts nothing
	// (ADR-0080 §SD4).
	Kind Kind

	// Stability is the package's tier in the compatibility promise
	// (ADR-0389). Human-curated; the zero value StabilityUnspecified asserts
	// nothing and is outside the promise. Kind and Stability are separate
	// axes: Kind says what a package is, Stability what a consumer may rely
	// on, so a test-support package can be Stable when other modules' tests
	// are meant to import it.
	Stability StabilityE
}
