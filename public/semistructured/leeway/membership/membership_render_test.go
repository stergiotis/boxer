package membership

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression for review C-11: the membership package had zero tests, yet
// IsPlaceholder's per-kind matrix guards the documented "every attribute
// resolves to ref:0" failure (card keying) and Render's kind dispatch feeds
// every card emitter.
func TestIsPlaceholder_PerKindMatrix(t *testing.T) {
	cases := []struct {
		name string
		mv   MembershipValue
		want bool
	}{
		// Ref kinds: placeholder iff Ref==0 && Params=="".
		{"ref zero", MembershipValue{Kind: IdentityRef}, true},
		{"ref nonzero", MembershipValue{Kind: IdentityRef, Ref: 7}, false},
		{"perRowId zero", MembershipValue{Kind: IdentityPerRowId}, true},
		{"perRowId with params", MembershipValue{Kind: IdentityPerRowId, Params: "p"}, false},
		{"perRowBlob zero", MembershipValue{Kind: IdentityPerRowBlob}, true},
		{"perRowBlob with params", MembershipValue{Kind: IdentityPerRowBlob, Params: "p"}, false},
		// Verbatim kind: placeholder iff Verbatim=="".
		{"verbatim empty", MembershipValue{Kind: IdentityVerbatim}, true},
		{"verbatim set", MembershipValue{Kind: IdentityVerbatim, Verbatim: "v"}, false},
		// PerRowName: placeholder iff Verbatim=="" && Params=="".
		{"perRowName empty", MembershipValue{Kind: IdentityPerRowName}, true},
		{"perRowName with verbatim", MembershipValue{Kind: IdentityPerRowName, Verbatim: "v"}, false},
		{"perRowName with params", MembershipValue{Kind: IdentityPerRowName, Params: "p"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsPlaceholder(tc.mv))
		})
	}
}

func TestRender_KindDispatch(t *testing.T) {
	r := DefaultRenderer()

	// Ref kinds render the ref id via the ref formatter.
	require.Equal(t, r.RenderRef(42), r.Render(MembershipValue{Kind: IdentityRef, Ref: 42}))
	require.Equal(t, r.RenderRef(7), r.Render(MembershipValue{Kind: IdentityPerRowId, Ref: 7}))
	// Verbatim kinds render the verbatim string.
	require.Equal(t, "in_transit", r.Render(MembershipValue{Kind: IdentityVerbatim, Verbatim: "in_transit"}))
	require.Equal(t, "edge", r.Render(MembershipValue{Kind: IdentityPerRowName, Verbatim: "edge"}))
	// IdentityNone (zero value) renders empty.
	require.Equal(t, "", r.Render(MembershipValue{}))
}

// C-14 trap: IdentityPerRowBlob carries its identity in Params (Ref is
// contractually 0), but Render dispatches it through RenderRef(mv.Ref), so its
// primary display string is whatever RenderRef(0) is — "0x0" with the default
// formatter. This pins that behavior so a card emitter using Render as the
// label key has a defined (if surprising) value, and a change is caught.
func TestRender_PerRowBlobRendersZeroRef(t *testing.T) {
	r := DefaultRenderer()
	require.Equal(t, r.RenderRef(0), r.Render(MembershipValue{Kind: IdentityPerRowBlob, Params: "anything"}),
		"per-row-blob renders RenderRef(0) regardless of Params (review C-14)")
}
