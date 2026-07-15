package wasmsurvey

// This file defines the value model the survey produces. Everything is plain
// data so a report can be rendered, JSON-encoded, or (a deferred follow-up)
// persisted as leeway facts without reaching back into the analysis.

// Tier is a package's TinyGo/wasm amenability verdict. The numeric order is
// load-bearing: it runs best→worst, so `max(a, b)` yields the more
// restrictive of two tiers (the propagation rule — a package is only as
// amenable as its least-amenable import). TierUnknown sorts below TierGreen
// so an as-yet-unclassified node never dominates a real verdict.
type Tier uint8

const (
	TierUnknown Tier = iota // not yet classified
	TierGreen               // whole closure is TinyGo-supported; expected to compile
	TierYellow              // reaches a partial capability (reflect/unsafe/json-v2/unknown external) — may compile; the empirical probe decides
	TierRed                 // reaches a known-unsupported leaf — not expected to compile
)

// String renders the tier as a lowercase word for reports and JSON.
func (t Tier) String() (s string) {
	switch t {
	case TierGreen:
		s = "green"
	case TierYellow:
		s = "yellow"
	case TierRed:
		s = "red"
	default:
		s = "unknown"
	}
	return
}

// worstTier returns the more restrictive (higher) of two tiers. It is the
// monotone combinator the static propagation folds over a package's imports.
func worstTier(a Tier, b Tier) (w Tier) {
	if a > b {
		return a
	}
	return b
}

// TargetID identifies one TinyGo wasm target. The three targets differ in
// both file selection (GOOS) and the supported-stdlib surface, so the survey
// re-collects and re-classifies the closure once per target.
type TargetID uint8

const (
	TargetWASI        TargetID = iota // server/sandbox wasm: GOOS=wasip1, `tinygo -target=wasi`
	TargetJS                          // browser wasm: GOOS=js, `tinygo -target=wasm`
	TargetWasmUnknown                 // freestanding wasm, no host: `tinygo -target=wasm-unknown`
)

// AllTargets is the canonical target order used across the survey and report.
var AllTargets = []TargetID{TargetWASI, TargetJS, TargetWasmUnknown}

// String is the short flag/report name of the target.
func (t TargetID) String() (s string) {
	switch t {
	case TargetWASI:
		s = "wasi"
	case TargetJS:
		s = "js"
	case TargetWasmUnknown:
		s = "wasm-unknown"
	default:
		s = "?"
	}
	return
}

// GOOS reports the GOOS the upstream toolchain uses for this target's build
// constraints. wasm-unknown has no upstream GOOS of its own, so it reuses
// wasip1 file selection and leans on the stricter support set plus the
// empirical probe to find what the freestanding target actually drops.
func (t TargetID) GOOS() (goos string) {
	switch t {
	case TargetJS:
		goos = "js"
	default:
		goos = "wasip1"
	}
	return
}

// TinyGoTarget is the `-target=` value passed to `tinygo build`.
func (t TargetID) TinyGoTarget() (name string) {
	switch t {
	case TargetWASI:
		name = "wasi"
	case TargetJS:
		name = "wasm"
	case TargetWasmUnknown:
		name = "wasm-unknown"
	}
	return
}

// ParseTargetE resolves a flag string ("wasi"|"js"|"wasm-unknown") to a
// TargetID. ok is false for an unrecognized name.
func ParseTargetE(s string) (t TargetID, ok bool) {
	for _, c := range AllTargets {
		if c.String() == s {
			return c, true
		}
	}
	return 0, false
}

// ReasonKind classifies why a package is not Green. It is the bucket a
// report groups by and the key the static seeds and the empirical
// stderr-classifier both emit.
type ReasonKind uint8

const (
	ReasonNone                ReasonKind = iota
	ReasonUnsupportedStdlib              // imports a stdlib package TinyGo does not provide on wasm (os/exec, plugin, net…)
	ReasonReflect                        // uses reflect — TinyGo implements only a subset
	ReasonUnsafe                         // uses unsafe — portability/ABI risk under wasm
	ReasonGoexperimentJSONv2             // reaches encoding/json/v2 (the goexperiment.jsonv2 surface)
	ReasonUnknownExternal                // depends on an external module not on the allow/deny list — unverified
	ReasonUnsupportedExternal            // depends on an external module known not to build under TinyGo/wasm
	ReasonSyscall                        // empirical: missing/again unsupported syscall surface
	ReasonCgo                            // empirical: cgo reached (no cgo for wasm)
	ReasonLinker                         // empirical: link-stage failure (missing symbol/intrinsic)
	ReasonToolchain                      // empirical: tinygo cannot use the active Go toolchain (version ceiling)
	ReasonProbeOther                     // empirical: a build failure that did not match a known bucket
)

// String renders the reason kind as a stable kebab token for reports/JSON.
func (r ReasonKind) String() (s string) {
	switch r {
	case ReasonUnsupportedStdlib:
		s = "unsupported-stdlib"
	case ReasonReflect:
		s = "reflect"
	case ReasonUnsafe:
		s = "unsafe"
	case ReasonGoexperimentJSONv2:
		s = "goexperiment-jsonv2"
	case ReasonUnknownExternal:
		s = "unknown-external"
	case ReasonUnsupportedExternal:
		s = "unsupported-external"
	case ReasonSyscall:
		s = "syscall"
	case ReasonCgo:
		s = "cgo"
	case ReasonLinker:
		s = "linker"
	case ReasonToolchain:
		s = "toolchain"
	case ReasonProbeOther:
		s = "probe-other"
	default:
		s = "none"
	}
	return
}

// Reason is one cause attached to a verdict: the offending leaf package and,
// for static reasons, the shortest import path that reaches it (the blame
// trail root→…→leaf). Detail carries free-text (e.g. a parsed tinygo stderr
// line) when present.
type Reason struct {
	Kind   ReasonKind `json:"kind"`
	Leaf   string     `json:"leaf,omitempty"`   // offending package import path (e.g. "os/exec")
	Detail string     `json:"detail,omitempty"` // free-text elaboration
	Path   []string   `json:"path,omitempty"`   // shortest import path root→…→leaf
}

// TargetVerdict is the outcome for one package on one target. Static is the
// graph-only triage; Empirical is the TinyGo build result (TierUnknown when
// the package was not probed). Tier() reports the verdict that stands.
type TargetVerdict struct {
	Target      TargetID `json:"target"`
	Static      Tier     `json:"static"`
	Empirical   Tier     `json:"empirical"`             // TierUnknown ⇒ not probed
	Probed      bool     `json:"probed"`                // whether the empirical TinyGo build ran
	BuildMillis int64    `json:"buildMillis,omitempty"` // wall time of the probe build
	Reasons     []Reason `json:"reasons,omitempty"`     // why not Green (static seeds + empirical findings)
}

// Tier is the verdict that stands: the empirical result when the package was
// probed (ground truth overrides the static guess), else the static triage.
func (v TargetVerdict) Tier() (t Tier) {
	if v.Probed && v.Empirical != TierUnknown {
		return v.Empirical
	}
	return v.Static
}

// Disagrees reports whether the empirical probe overturned the static guess
// — the rows worth a human's attention (a curated heuristic the real
// compiler refuted, in either direction).
func (v TargetVerdict) Disagrees() (b bool) {
	return v.Probed && v.Empirical != TierUnknown && v.Empirical != v.Static
}

// PackageReport is the per-package result across all surveyed targets, in
// AllTargets order.
type PackageReport struct {
	ImportPath string          `json:"importPath"`
	Name       string          `json:"name"`          // package-clause name (for props codegen)
	Dir        string          `json:"dir,omitempty"` // on-disk dir (for props file writes)
	Class      string          `json:"class"`         // godep class: internal | external | stdlib
	NumExports int             `json:"numExports"`
	Targets    []TargetVerdict `json:"targets"`
}

// Survey is the whole result set plus the run metadata needed to read it.
type Survey struct {
	RootModule string          `json:"rootModule"`
	Tags       []string        `json:"tags"`
	Mode       string          `json:"mode"`               // static | empirical | both
	TinyGoVer  string          `json:"tinyGoVer"`          // empty when no probe ran
	Targets    []string        `json:"targets"`            // surveyed target names
	Warnings   []string        `json:"warnings,omitempty"` // e.g. "probe skipped: tinygo not found"
	Packages   []PackageReport `json:"packages"`
}
