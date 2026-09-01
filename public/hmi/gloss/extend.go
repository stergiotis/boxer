package gloss

import (
	"slices"
	"sync"
)

// The extension point: how a repository that consumes boxer adds a gloss its
// own domain needs to the catalog every host builds.
//
// Until this existed, [Default] was closed — the two built-in families are
// unexported and every host calls Default, so a consumer could build its own
// catalog with [NewCatalog] but nothing that renders a result column would
// ever look in it. A domain gloss was therefore only reachable by changing
// this package, which is the wrong shape for a vocabulary that is
// deliberately open-ended (ADR-0186 §SD3: the catalog is a set of names, not
// a closed enum).
//
// A consumer registers in an init function of a package its binary imports:
//
//	func init() {
//		gloss.RegisterDefault(fathomCurveGloss{})
//	}
//
// Naming: a media type is the catalog key, so a consumer must pick one that
// boxer will not later take. `gloss/<name>` is boxer's own space; a consumer
// should use a prefix it owns — `sailing/tidestate`, `acme/x-widget` — and
// that is a convention here rather than a rule, because refusing an unknown
// prefix would also refuse a gloss that later moves upstream unchanged.
//
// Ordering: extras are appended after the built-ins, so a built-in affinity
// rule still wins a contested column (registration order is affinity order).
// That is deliberate — an extension should be able to add a rendering without
// silently changing how an existing column renders.

var (
	extraMu sync.Mutex
	// extras holds what consumers registered, in registration order.
	extras []GlossI
)

// RegisterDefault adds g to every catalog [Default] builds from here on.
//
// It panics on a media type that a built-in or an earlier registration
// already holds: two packages claiming one name would otherwise resolve by
// import order, which is not something either of them can see. Registration
// happens in init, so the panic lands at startup with the duplicate named,
// rather than as a rendering that changes depending on the link order.
//
// Safe for concurrent use, though the intended caller is an init function.
func RegisterDefault(g GlossI) {
	mt := g.MediaType()
	extraMu.Lock()
	defer extraMu.Unlock()
	if builtinMediaTypes()[mt] {
		panic("gloss: RegisterDefault(" + mt + "): a built-in gloss already holds that media type")
	}
	for _, e := range extras {
		if e.MediaType() == mt {
			panic("gloss: RegisterDefault(" + mt + "): already registered by another package")
		}
	}
	extras = append(extras, g)
}

// RegisteredDefaults are the glosses consumers have registered, in
// registration order. It is what a listing surface shows to distinguish an
// extension from a built-in, and what a test asserts against.
func RegisteredDefaults() []GlossI {
	extraMu.Lock()
	defer extraMu.Unlock()
	return slices.Clone(extras)
}

// builtinMediaTypes is the set Default would build without extras. Computed
// rather than listed so a new built-in cannot be forgotten here.
func builtinMediaTypes() map[string]bool {
	out := make(map[string]bool, 24)
	for _, g := range contentFamily() {
		out[g.MediaType()] = true
	}
	for _, g := range presentationFamily() {
		out[g.MediaType()] = true
	}
	return out
}
