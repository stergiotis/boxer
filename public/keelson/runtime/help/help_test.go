package help

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestRefT_String(t *testing.T) {
	cases := []struct {
		name string
		ref  RefT
		want string
	}{
		{
			name: "doc only",
			ref:  RefT{AppId: "github.com/foo/bar", Doc: "overview"},
			want: "github.com/foo/bar/overview",
		},
		{
			name: "doc + section",
			ref:  RefT{AppId: "github.com/foo/bar", Doc: "howto/replay", Section: "sticky-caps"},
			want: "github.com/foo/bar/howto/replay#sticky-caps",
		},
		{
			name: "zero",
			ref:  RefT{},
			want: "/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.ref.String()
			if got != tc.want {
				t.Errorf("String: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRefT_IsZero(t *testing.T) {
	if !(RefT{}).IsZero() {
		t.Errorf("zero RefT should report IsZero")
	}
	cases := []RefT{
		{AppId: "x"},
		{Doc: "x"},
		{Section: "x"},
		{AppId: "a", Doc: "b", Section: "c"},
	}
	for i, r := range cases {
		if r.IsZero() {
			t.Errorf("case %d: non-zero RefT %+v reported IsZero", i, r)
		}
	}
}

// TestRefT_AppIdSubjectAlias verifies the AppId carried in RefT round-trips
// through the SubjectAlias function the bus layer will consume. This locks
// the assumption that RefT.AppId is the full dotted Id (not the short
// alias) — the wikilink resolver landing in a follow-up round will look up
// the full Id via app.LookupManifest, then build the RefT.
func TestRefT_AppIdSubjectAlias(t *testing.T) {
	r := RefT{AppId: "github.com/stergiotis/boxer/apps/capinspector"}
	alias := r.AppId.SubjectAlias()
	if alias != "capinspector" {
		t.Errorf("SubjectAlias: got %q, want %q", alias, "capinspector")
	}
	// Round-trip: full id stays intact in the ref even when an alias is
	// derived separately.
	if r.AppId != app.AppIdT("github.com/stergiotis/boxer/apps/capinspector") {
		t.Errorf("AppId mutated unexpectedly: %q", r.AppId)
	}
}
