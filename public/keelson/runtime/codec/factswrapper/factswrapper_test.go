package factswrapper

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

func importsHave(imports []string, substr string) bool {
	for _, imp := range imports {
		if strings.Contains(imp, substr) {
			return true
		}
	}
	return false
}

// TestImports_DeclaresOwnNeeds pins the post-2026-06-14 contract: the wrapper
// declares the imports its OWN emitted code uses and leaves overlap to the
// marshallgen import set's dedup. eh + eb back the wrapper's Marshal error wrap
// and codec Decode row-count check, so they are declared unconditionally — the
// wrapper no longer mirrors the core's eb gating (the earlier seam). vdd is
// still conditional: Init's per-membership `vdd.MembXxx.GetId()` lookups are
// its only use, so a kind with no ref-channel membership must omit it.
func TestImports_DeclaresOwnNeeds(t *testing.T) {
	// Tagged: one non-const ref-channel field (LowCardRef is the zero value and
	// NeedsKindVar).
	tagged := &mappingplan.Plan{Fields: []mappingplan.TaggedField{
		{GoFieldName: "X", LWMembership: "x", LWSection: "symbol"},
	}}
	ti := FactsWrapper{}.Imports(tagged)
	if !importsHave(ti, "observability/eh/eb") {
		t.Error("tagged: eb must be declared unconditionally (the import set dedups the core's copy)")
	}
	if !importsHave(ti, "keelson/vdd") {
		t.Error("tagged (ref membership): vdd is needed for Init's lookups")
	}

	// Plain-only: no fields → no memberships. eb still declared (the wrapper
	// uses it, and the core omits it here); vdd omitted (unused → build error).
	plain := &mappingplan.Plan{}
	pi := FactsWrapper{}.Imports(plain)
	if !importsHave(pi, "observability/eh/eb") {
		t.Error("plain-only: eb must still be declared (the wrapper uses it)")
	}
	if importsHave(pi, "keelson/vdd") {
		t.Error("plain-only: vdd is unused (no memberships) and must be omitted")
	}
}

// TestCheckRefMembershipsAreIdentifiers pins the facts-target rule that every
// ref-channel membership (value or const) must be a Go identifier — Init
// resolves each via vdd.Memb<Name>. This is stricter than the shared builder
// (which rejects only const ref non-identifiers): a value ref field with a
// hyphenated membership is valid for the anchor target but rejected here,
// because the facts target needs a vdd symbol. Verbatim memberships are wire
// labels, never vdd-resolved, so they stay unrestricted.
func TestCheckRefMembershipsAreIdentifiers(t *testing.T) {
	refPlan := func(memb string, ch mappingplan.MembershipChannel) *mappingplan.Plan {
		return &mappingplan.Plan{Fields: []mappingplan.TaggedField{
			{GoFieldName: "F", LWMembership: memb, LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: ch}},
		}}
	}
	if checkRefMembershipsAreIdentifiers(refPlan("my-app", mappingplan.MembershipChannelHighCardRef)) == nil {
		t.Error("value ref membership 'my-app' must be rejected (facts would emit vdd.MembMy-app)")
	}
	if err := checkRefMembershipsAreIdentifiers(refPlan("myApp", mappingplan.MembershipChannelHighCardRef)); err != nil {
		t.Errorf("identifier ref membership must be accepted: %v", err)
	}
	if err := checkRefMembershipsAreIdentifiers(refPlan("my-label", mappingplan.MembershipChannelLowCardVerbatim)); err != nil {
		t.Errorf("verbatim membership (not vdd-resolved) must be accepted: %v", err)
	}
}
