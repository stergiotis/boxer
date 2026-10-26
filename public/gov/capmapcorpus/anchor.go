package capmapcorpus

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Anchors bind a competence to the things in the tree that realise it
// (ADR-0337 §SD1). An anchor is a typed string, `kind:key`, read by a resolver
// for its kind and by nothing else — it is not an Obsidian link and must not
// dangle as one.
//
// The key is a symbol, an id or a registry name, never a path with a line
// number: a refactor that keeps the decision keeps the anchor. What each kind's
// key is, and what it resolves against, is the table in [AnchorKindE].

// AnchorKindE names what an anchor points at. The set is closed: an anchor of
// a kind this build does not know is an authoring error, not an unresolved
// link, because no resolver could ever vouch for it.
type AnchorKindE string

const (
	// AnchorKindAdr is a decision record, keyed by its four-digit number.
	AnchorKindAdr AnchorKindE = "adr"
	// AnchorKindPkg is a Go package, keyed by its repository-relative import
	// path (`public/streaming/watchbill`).
	AnchorKindPkg AnchorKindE = "pkg"
	// AnchorKindApp is an app, keyed by the last segment of its manifest id
	// (`watchbill`).
	AnchorKindApp AnchorKindE = "app"
	// AnchorKindTable is a table, `boxer.<name>` for a facts-schema table or
	// `keelson/<name>` for an introspection table.
	AnchorKindTable AnchorKindE = "table"
	// AnchorKindHowto, AnchorKindExplanation, AnchorKindPositioning and
	// AnchorKindTrial are pages of the doc tree, keyed by the page's slug.
	AnchorKindHowto       AnchorKindE = "howto"
	AnchorKindExplanation AnchorKindE = "explanation"
	AnchorKindPositioning AnchorKindE = "positioning"
	AnchorKindTrial       AnchorKindE = "trial"
	// AnchorKindEnv is an environment variable in the ADR-0009 registry.
	AnchorKindEnv AnchorKindE = "env"
	// AnchorKindApplet is a SQL applet, keyed by its minted slug.
	AnchorKindApplet AnchorKindE = "applet"
	// AnchorKindScene is a scene document, keyed by its file stem without
	// the `.scene` suffix.
	AnchorKindScene AnchorKindE = "scene"
	// AnchorKindSkill is an agent skill under doc/skills, keyed by its name.
	AnchorKindSkill AnchorKindE = "skill"
)

// AllAnchorKinds lists the kinds in the order the lint lens presents them.
func AllAnchorKinds() (kinds []AnchorKindE) {
	return []AnchorKindE{
		AnchorKindAdr, AnchorKindPkg, AnchorKindApp, AnchorKindTable,
		AnchorKindHowto, AnchorKindExplanation, AnchorKindPositioning, AnchorKindTrial,
		AnchorKindEnv, AnchorKindApplet, AnchorKindScene, AnchorKindSkill,
	}
}

// IsAnchorKind reports whether k is one of the registered kinds.
func IsAnchorKind(k AnchorKindE) (ok bool) {
	for _, known := range AllAnchorKinds() {
		if known == k {
			return true
		}
	}
	return false
}

// Anchor is one parsed `kind:key`.
type Anchor struct {
	Kind AnchorKindE
	Key  string
}

// String renders the anchor the way the vault spells it, which is also what a
// relation of the anchor kinds carries as its target.
func (inst Anchor) String() (s string) {
	return string(inst.Kind) + ":" + inst.Key
}

// ParseAnchor reads one `kind:key`. The key is kept verbatim after trimming,
// because it is read by the kind's resolver and the resolver owns its spelling.
func ParseAnchor(raw string) (a Anchor, err error) {
	kind, key, found := strings.Cut(strings.TrimSpace(raw), ":")
	if !found {
		return a, eb.Build().Str("anchor", raw).Errorf("an anchor is written kind:key")
	}
	a = Anchor{Kind: AnchorKindE(strings.TrimSpace(kind)), Key: strings.TrimSpace(key)}
	if !IsAnchorKind(a.Kind) {
		return a, eb.Build().Str("anchor", raw).Str("kind", string(a.Kind)).Errorf("unknown anchor kind")
	}
	if a.Key == "" {
		return a, eb.Build().Str("anchor", raw).Errorf("an anchor's key is empty")
	}
	return a, nil
}

// AnchorResolution is what a resolver reports about one anchor.
type AnchorResolution struct {
	// Resolution is [ResolutionDirect] when the target exists,
	// [ResolutionUnresolved] when the corpus was consulted and does not hold
	// it, and [ResolutionUnchecked] when the corpus could not be reached.
	Resolution ResolutionE
	// Status is what the target's own corpus says about it, when it says
	// anything: a decision's lifecycle status, for an `adr` anchor. Empty for
	// kinds that carry none. An anchor to a retired decision resolves — the
	// competence may describe something the tree no longer does — and the
	// lint lens lists it by this field.
	Status string
}

// AnchorResolverI resolves anchors against the tree. One implementation
// covers every kind; a kind it does not reach reports [ResolutionUnchecked],
// on the providers' empty-rather-than-erroring convention.
//
// The implementation lives outside this package
// ([github.com/stergiotis/boxer/public/gov/capmapanchors]) because it reads
// the decision corpus, the app registry and the doc tree, and this package is
// a pure library over markdown.
type AnchorResolverI interface {
	ResolveAnchor(a Anchor) (res AnchorResolution)
}

// uncheckedResolver is what a parse runs with when no resolver was given.
type uncheckedResolver struct{}

func (uncheckedResolver) ResolveAnchor(Anchor) (res AnchorResolution) {
	return AnchorResolution{Resolution: ResolutionUnchecked}
}

// RetiredStatuses are the decision statuses an anchor may resolve to while
// naming something the tree no longer does. The lint lens lists anchors whose
// [Relation.TargetStatus] is one of these.
var RetiredStatuses = []string{"superseded", "withdrawn", "deprecated", "deferred"}

// IsRetiredStatus reports whether a resolved anchor's status is one of
// [RetiredStatuses].
func IsRetiredStatus(status string) (retired bool) {
	for _, s := range RetiredStatuses {
		if strings.EqualFold(s, status) {
			return true
		}
	}
	return false
}

// AnchorRelations filters the relations of the two anchor kinds — the
// `realised_by` list and the claims' bases — which is what the lint lens and
// the evidence table read.
func AnchorRelations(rels []Relation) (anchors []Relation) {
	for _, r := range rels {
		if r.Kind == RelationKindRealisedBy || r.Kind == RelationKindBasis {
			anchors = append(anchors, r)
		}
	}
	return anchors
}
