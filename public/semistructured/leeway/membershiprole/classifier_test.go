package membershiprole

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

func mustEncode(t *testing.T, aspects ...useaspects.AspectE) (set useaspects.AspectSet) {
	t.Helper()
	set, err := useaspects.EncodeAspects(aspects...)
	if err != nil {
		t.Fatalf("EncodeAspects: %v", err)
	}
	return
}

func TestDefaultClassifier_UseAspectShortCircuits(t *testing.T) {
	cls := DefaultClassifier{}

	cases := []struct {
		name    string
		aspects []useaspects.AspectE
		mv      MembershipValue
		want    MembershipRoleE
	}{
		{
			name:    "AllPrimary forces primary even on plain identifier",
			aspects: []useaspects.AspectE{useaspects.AspectSectionMembershipsAllPrimary},
			mv:      MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "errormsg"},
			want:    MembershipRolePrimary,
		},
		{
			name:    "AllSecondary forces secondary even on path-shaped",
			aspects: []useaspects.AspectE{useaspects.AspectSectionMembershipsAllSecondary},
			mv:      MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "/hostname"},
			want:    MembershipRoleSecondary,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sec := SectionContext{UseAspects: mustEncode(t, tc.aspects...)}
			got, _ := cls.Classify(sec, tc.mv)
			if got != tc.want {
				t.Fatalf("role=%d want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultClassifier_VerbatimNamingConvention(t *testing.T) {
	cls := DefaultClassifier{}

	cases := []struct {
		name string
		mv   MembershipValue
		want MembershipRoleE
	}{
		{
			name: "leading slash → primary",
			mv:   MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "/hostname"},
			want: MembershipRolePrimary,
		},
		{
			name: "deep path → primary",
			mv:   MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "/metrics/cpu"},
			want: MembershipRolePrimary,
		},
		{
			name: "plain identifier → secondary",
			mv:   MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "errormsg"},
			want: MembershipRoleSecondary,
		},
		{
			name: "mixed-low-verbatim with path skeleton → primary",
			mv:   MembershipValue{Kind: MembershipKindMixedLowCardVerbatimHighCardParam, Verbatim: "/tags/_", Params: "0"},
			want: MembershipRolePrimary,
		},
		{
			name: "mixed-low-verbatim with plain skeleton → secondary",
			mv:   MembershipValue{Kind: MembershipKindMixedLowCardVerbatimHighCardParam, Verbatim: "annotations", Params: "severity"},
			want: MembershipRoleSecondary,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := cls.Classify(SectionContext{}, tc.mv)
			if got != tc.want {
				t.Fatalf("role=%d want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultClassifier_RefShapedDefaultPrimary(t *testing.T) {
	cls := DefaultClassifier{}

	for _, kind := range []MembershipKindE{
		MembershipKindRef,
		MembershipKindRefParametrized,
		MembershipKindMixedLowCardRefHighCardParam,
	} {
		mv := MembershipValue{Kind: kind, Ref: 42}
		got, _ := cls.Classify(SectionContext{}, mv)
		if got != MembershipRolePrimary {
			t.Fatalf("kind=%d role=%d want %d", kind, got, MembershipRolePrimary)
		}
	}
}

func TestDefaultClassifier_ParamTreatment(t *testing.T) {
	cls := DefaultClassifier{}

	cases := []struct {
		name string
		kind MembershipKindE
		want ParamTreatmentE
	}{
		{"ref → none", MembershipKindRef, ParamTreatmentNone},
		{"verbatim → none", MembershipKindVerbatim, ParamTreatmentNone},
		{"refParametrized → identity", MembershipKindRefParametrized, ParamTreatmentIdentity},
		{"mixedRef → identity", MembershipKindMixedLowCardRefHighCardParam, ParamTreatmentIdentity},
		{"mixedVerbatim → identity", MembershipKindMixedLowCardVerbatimHighCardParam, ParamTreatmentIdentity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mv := MembershipValue{Kind: tc.kind, Verbatim: "/tags/_"}
			_, got := cls.Classify(SectionContext{}, mv)
			if got != tc.want {
				t.Fatalf("paramTreatment=%d want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultClassifier_PathPrefixOverride(t *testing.T) {
	cls := DefaultClassifier{PathPrefix: "tag:"}

	primary := MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "tag:hostname"}
	secondary := MembershipValue{Kind: MembershipKindVerbatim, Verbatim: "/hostname"}

	if got, _ := cls.Classify(SectionContext{}, primary); got != MembershipRolePrimary {
		t.Fatalf("custom prefix not honoured for primary input: got %d", got)
	}
	if got, _ := cls.Classify(SectionContext{}, secondary); got != MembershipRoleSecondary {
		t.Fatalf("non-prefix input should be secondary under custom prefix: got %d", got)
	}
}

func TestDefaultClassifier_NoneKindReturnsNoneRole(t *testing.T) {
	cls := DefaultClassifier{}
	got, pt := cls.Classify(SectionContext{}, MembershipValue{})
	if got != MembershipRoleNone {
		t.Fatalf("zero MembershipValue should classify as None: got %d", got)
	}
	if pt != ParamTreatmentNone {
		t.Fatalf("zero MembershipValue should have no param treatment: got %d", pt)
	}
}

func TestSectionContext_HasUseAspect(t *testing.T) {
	sec := SectionContext{UseAspects: mustEncode(t, useaspects.AspectSectionMembershipsAllPrimary)}
	if !sec.HasUseAspect(useaspects.AspectSectionMembershipsAllPrimary) {
		t.Fatalf("encoded aspect not detected")
	}
	if sec.HasUseAspect(useaspects.AspectSectionMembershipsAllSecondary) {
		t.Fatalf("absent aspect reported present")
	}
	empty := SectionContext{}
	if empty.HasUseAspect(useaspects.AspectSectionMembershipsAllPrimary) {
		t.Fatalf("empty UseAspects should not report any aspect present")
	}
}
