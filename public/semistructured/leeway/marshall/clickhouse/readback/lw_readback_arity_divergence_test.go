package readback

// The artefacts' arity must equal the write path's, which
// mappingplan.DeriveReadContract states (ADR-0146 D1). The case that used to
// disagree is a non-Option CONTAINER: marshalContainer splices an empty []T /
// *roaring.Bitmap to ZERO attributes, so the slot is [0..1], while this
// generator treated every non-Option field as mandatory and emitted a presence
// literal plus `countEqual(...) = 1`. A row whose container was legitimately
// empty then passed both Go read paths and failed the Presence and Validator
// its own kind generates.
//
// This file pins the agreement in the direction that matters: what the
// generator emits, against what the contract says.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

func containerOnlyPlan() *mappingplan.Plan {
	return &mappingplan.Plan{
		KindName: "arityProbe",
		KindType: "ArityProbe",
		PlainCols: []mappingplan.PlainCol{
			{GoField: "Id", Column: "id", Canonical: ctabb.U64},
		},
		Fields: []mappingplan.TaggedField{{
			GoFieldName:  "Nums",
			Canonical:    canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray),
			LWMembership: "myNums",
			LWSection:    "u64Array",
			Flags:        mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardRef},
		}},
	}
}

func TestGenerator_ContainerSlotIsNotRequired(t *testing.T) {
	plan := containerOnlyPlan()

	contract, err := mappingplan.DeriveReadContract(plan)
	if err != nil {
		t.Fatalf("DeriveReadContract: %v", err)
	}
	slot, ok := contract.Slot("u64Array", "myNums")
	if !ok {
		t.Fatalf("contract has no slot for u64Array@myNums:\n%s", contract)
	}
	if slot.MinAttrs != 0 || slot.MaxAttrs != 1 {
		t.Fatalf("container slot arity = [%d..%d], want [0..1]", slot.MinAttrs, slot.MaxAttrs)
	}

	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"myNums": 42}))
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// At most one attribute, not exactly one. Checked on the whole term, since
	// "= 1" is a substring of "<= 1" and asserting the substring alone would
	// pass either way — the mistake this test previously made.
	if !strings.Contains(a.Validator, "<= 1") {
		t.Errorf("validator must admit zero attributes for a container slot:\n%s", a.Validator)
	}
	if strings.Contains(strings.ReplaceAll(a.Validator, "<= 1", ""), "= 1") {
		t.Errorf("validator still requires exactly one attribute somewhere:\n%s", a.Validator)
	}

	// The container is the kind's ONLY slot, so nothing is required and the
	// conjunctive presence is empty. Presence is then the disjunction — the row
	// carries at least one of the kind's slots — so a container-only kind still
	// detects instead of matching every row. With one slot the disjunction is
	// just that slot's has() term.
	if !strings.Contains(a.Presence, "42") {
		t.Errorf("a container-only kind must still detect via the disjunction:\n%s", a.Presence)
	}
	if a.Presence == "1" {
		t.Errorf("presence must not degrade to the trivial term — the Filter would match every row")
	}
}

// A mandatory scalar alongside the container still carries the prefilter, so
// dropping the container's presence term does not cost detection for kinds
// that have any always-emitted field.
func TestGenerator_MandatoryScalarStillCarriesPresence(t *testing.T) {
	plan := containerOnlyPlan()
	plan.Fields = append(plan.Fields, mappingplan.TaggedField{
		GoFieldName:  "Sym",
		Canonical:    ctabb.S,
		LWMembership: "mySym",
		LWSection:    "symbol",
		Flags:        mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim},
	})

	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"myNums": 42}))
	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(a.Presence, "'mySym'") {
		t.Errorf("the mandatory scalar must still contribute a presence literal:\n%s", a.Presence)
	}
	if strings.Contains(a.Presence, "42") {
		t.Errorf("the container must still not contribute one:\n%s", a.Presence)
	}
}
