package readback

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// TestValidatePlanAgainstIR exercises the extracted conformance pass: a plan
// whose columns all exist validates, and each kind of dangling reference is
// reported — without generating any ClickHouse SQL.
func TestValidatePlanAgainstIR(t *testing.T) {
	ir := buildTestIR(t)

	conforming := &mappingplan.Plan{
		KindName:  "ok",
		PlainCols: []mappingplan.PlainCol{{Column: "id", GoField: "Id", Canonical: ctabb.U64}},
		Fields: []mappingplan.TaggedField{
			{GoFieldName: "Sym", Canonical: ctabb.S, LWMembership: "s", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
		},
	}
	if err := ValidatePlanAgainstIR(conforming, ir); err != nil {
		t.Fatalf("conforming plan should validate: %v", err)
	}

	cases := map[string]*mappingplan.Plan{
		"missing plain column": {
			KindName:  "p",
			PlainCols: []mappingplan.PlainCol{{Column: "nope", GoField: "X", Canonical: ctabb.U64}},
		},
		"missing section": {
			KindName: "s",
			Fields: []mappingplan.TaggedField{
				{GoFieldName: "X", Canonical: ctabb.S, LWMembership: "m", LWSection: "ghost", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelLowCardVerbatim}},
			},
		},
		"channel absent from section": {
			// symbol carries lr/lv/hr; highCardVerbatim's hv column is absent.
			KindName: "c",
			Fields: []mappingplan.TaggedField{
				{GoFieldName: "X", Canonical: ctabb.S, LWMembership: "m", LWSection: "symbol", Flags: mappingplan.FieldFlags{Channel: mappingplan.MembershipChannelHighCardVerbatim}},
			},
		},
	}
	for name, plan := range cases {
		if err := ValidatePlanAgainstIR(plan, ir); err == nil {
			t.Errorf("%s: expected a conformance error", name)
		}
	}
}
