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

// TestImports_PlainOnlyVsTagged pins the 2026-06-14 review fix to the
// import gating. The marshallgen core imports eb only when the plan has a
// non-const field, and vdd is used only by Init's per-membership lookups —
// so for a plain-only kind the wrapper must supply eb itself (the core
// won't) and must NOT list vdd (unused → build error). For a tagged kind
// the inverse holds: the core supplies eb (the wrapper must not, to avoid a
// duplicate import) and vdd is needed.
func TestImports_PlainOnlyVsTagged(t *testing.T) {
	// Tagged: one non-const ref-channel field (LowCardRef is the zero value
	// and NeedsKindVar). Core imports eb; Init uses vdd.
	tagged := &mappingplan.Plan{Fields: []mappingplan.TaggedField{
		{GoFieldName: "X", LWMembership: "x", LWSection: "symbol"},
	}}
	ti := FactsWrapper{}.Imports(tagged)
	if importsHave(ti, "observability/eh/eb") {
		t.Error("tagged plan: eb comes from the core; wrapper listing it too duplicates the import")
	}
	if !importsHave(ti, "keelson/vdd") {
		t.Error("tagged plan: vdd is needed for Init's membership lookups")
	}

	// Plain-only: no fields. Core omits eb (no non-const field) so the wrapper
	// must supply it for the codec's row-count check; no memberships → no vdd.
	plain := &mappingplan.Plan{}
	pi := FactsWrapper{}.Imports(plain)
	if !importsHave(pi, "observability/eh/eb") {
		t.Error("plain-only plan: wrapper must supply eb (the core does not)")
	}
	if importsHave(pi, "keelson/vdd") {
		t.Error("plain-only plan: vdd is unused (no memberships) and must be omitted")
	}
}
