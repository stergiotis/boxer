package goplan_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// The `[]S` routing rule's whole truth table (goplan.ClassifySliceSection).
// Both front-ends read it, so the rule is pinned here rather than twice over in
// their own tests — a front-end that disagrees shows up in the parity corpus,
// but this is where the decision itself is stated.
func TestClassifySliceSection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tag     string
		marker  bool // element has a lw.* membership marker field
		atMemb  bool // element has an `@membership`-tagged field
		want    goplan.SliceSectionKindE
		because string
	}{
		{
			name: "marker beats everything", tag: "text", marker: true, atMemb: true,
			want:    goplan.SliceSectionKindNested,
			because: "the lw.* markers are the typed spelling of @membership; a struct carrying both is still nested",
		},
		{
			name: "marker with a static tag stays nested", tag: "memb,text", marker: true,
			want:    goplan.SliceSectionKindNested,
			because: "per-attribute markers make it dynamic; the tag's membership is redundant, not decisive",
		},
		{
			name: "@membership is the flat tuple", tag: "text", atMemb: true,
			want:    goplan.SliceSectionKindFlatTuple,
			because: "the original ADR-0103 grammar",
		},
		{
			name: "@membership beats a static tag", tag: "memb,text", atMemb: true,
			want:    goplan.SliceSectionKindFlatTuple,
			because: "per-element memberships win; AddTupleSliceField rejects the tag's flags",
		},
		{
			name: "static membership on the tag", tag: "memb,text",
			want:    goplan.SliceSectionKindNested,
			because: "static-Many: N attributes per row under one membership",
		},
		{
			name: "bare section, no membership anywhere", tag: "text",
			want:    goplan.SliceSectionKindFlatTuple,
			because: "a flat tuple missing its @membership field — routed so the error names that",
		},
		{
			name: "sub-column tag does not name a membership", tag: "text:value",
			want:    goplan.SliceSectionKindFlatTuple,
			because: "SplitLW reads `text:value` as membership `text` with no section slot filled",
		},
		{
			name: "trailing comma is not a membership", tag: "text,",
			want:    goplan.SliceSectionKindFlatTuple,
			because: "the section slot is empty — the crude strings.Contains(\",\") test this replaced got it wrong",
		},
		{
			name: "unparseable tag routes on the element signals", tag: "memb,text,bogusFlag", atMemb: true,
			want:    goplan.SliceSectionKindFlatTuple,
			because: "the chosen builder re-parses the tag and reports the bad flag",
		},
		{
			name: "unparseable tag with no element signal", tag: "memb,text,bogusFlag",
			want:    goplan.SliceSectionKindFlatTuple,
			because: "a tag that does not parse cannot name a membership; the builder reports the flag",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := goplan.ClassifySliceSection(tc.tag, tc.marker, tc.atMemb)
			if got != tc.want {
				t.Errorf("ClassifySliceSection(%q, marker=%v, atMemb=%v) = %v, want %v — %s",
					tc.tag, tc.marker, tc.atMemb, got, tc.want, tc.because)
			}
		})
	}
}
