package readback

// ADR-0146 M1 found that this generator's arity for a non-Option CONTAINER
// field disagrees with what the write path emits. This test pins the current
// behaviour so the gap is visible rather than rediscovered; it is expected to
// change when the deferred "re-derive the artefacts from ReadContract" work
// lands, and changing it should be deliberate.
//
// The disagreement: `marshalContainer` splices an empty []T / *roaring.Bitmap
// to ZERO attributes, so mappingplan.DeriveReadContract gives such a slot
// [0..1]. Generate treats every non-Option field as mandatory — a presence
// literal plus `countEqual(...) = 1`, i.e. [1..1]. A row whose container field
// is legitimately empty passes the Go read paths and fails the Presence and
// Validator this generator emits for the same kind.

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

func TestGenerator_ContainerArityDivergesFromReadContract(t *testing.T) {
	g := NewGenerator(buildTestIR(t), NewLookupResolver(mapLookup{"myNums": 42}))
	plan := &mappingplan.Plan{
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

	contract, err := mappingplan.DeriveReadContract(plan)
	if err != nil {
		t.Fatalf("DeriveReadContract: %v", err)
	}
	slot, ok := contract.Slot("u64Array", "myNums")
	if !ok {
		t.Fatalf("contract has no slot for u64Array@myNums:\n%s", contract)
	}
	// The write path's guarantee: an empty container emits nothing.
	if slot.MinAttrs != 0 || slot.MaxAttrs != 1 {
		t.Fatalf("container slot arity = [%d..%d], want [0..1]", slot.MinAttrs, slot.MaxAttrs)
	}
	if slot.Required() {
		t.Fatalf("a container slot must not be Required — an empty container splices")
	}

	a, err := g.Generate(plan)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// KNOWN DIVERGENCE, pinned. The generator demands the slot be present
	// exactly once, which the contract above says is not guaranteed.
	if !strings.Contains(a.Validator, "= 1") {
		t.Errorf("expected the pinned (divergent) `= 1` validator for a container field; "+
			"if this now emits `<= 1`, the ADR-0146 deferred fix has landed — update this test "+
			"and the ADR's Scope note:\n%s", a.Validator)
	}
	if a.Presence == "" {
		t.Errorf("expected the pinned (divergent) presence term for a container field; " +
			"an empty presence means the deferred fix has landed")
	}
}
