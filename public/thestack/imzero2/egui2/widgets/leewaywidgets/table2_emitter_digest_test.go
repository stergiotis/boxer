package leewaywidgets

import "testing"

// TestSectionDigests verifies the digest accessor lifts the primary / secondary
// membership chips and every value pair out of the buffered tagged-section rows,
// and excludes plain / backbone rows (which carry no memberships and fan out per
// column). This is the content the Detail timeline reuses to label its flags.
func TestSectionDigests(t *testing.T) {
	e := &Table2CardEmitter{
		unified: []table2UnifiedRow{
			{kind: rowKindSectionHeader, sectionName: "obs"},
			{
				kind:        rowKindData,
				sectionType: sectionTypeTagged,
				sectionName: "obs",
				primary:     []table2Tag{{display: "sensorA"}},
				secondary:   []table2Tag{{display: "region:eu"}},
				valuePairs:  []table2NamedValue{{name: "seen", value: "2025-12-06"}, {name: "seq", value: "42"}},
			},
			{
				kind:        rowKindData,
				sectionType: sectionTypePlain,
				sectionName: "entity-timestamp",
				primary:     []table2Tag{{display: "event-time"}},
				valuePairs:  []table2NamedValue{{value: "1969-12-31"}},
			},
		},
	}

	digs := e.SectionDigests()
	if len(digs) != 1 {
		t.Fatalf("want exactly one digest (the tagged section), got %d: %+v", len(digs), digs)
	}
	d := digs[0]
	if d.SectionName != "obs" {
		t.Errorf("SectionName: got %q, want \"obs\"", d.SectionName)
	}
	if len(d.Primary) != 1 || d.Primary[0] != "sensorA" {
		t.Errorf("Primary: got %v, want [sensorA]", d.Primary)
	}
	if len(d.Secondary) != 1 || d.Secondary[0] != "region:eu" {
		t.Errorf("Secondary: got %v, want [region:eu]", d.Secondary)
	}
	want := []SectionValue{{Name: "seen", Value: "2025-12-06"}, {Name: "seq", Value: "42"}}
	if len(d.Values) != len(want) {
		t.Fatalf("Values: got %v, want %v", d.Values, want)
	}
	for i := range want {
		if d.Values[i] != want[i] {
			t.Errorf("Values[%d]: got %v, want %v", i, d.Values[i], want[i])
		}
	}
}
