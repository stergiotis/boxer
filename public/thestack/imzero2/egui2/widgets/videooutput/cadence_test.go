package videooutput

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/videopipeline"
)

// TestHostReactive locks the contract decorateRenderer relies on: a live
// stream's cadence wins, and an absent/invalid stream reports ok=false so the
// caller keeps its launch-time default rather than trusting a stale Reactive
// flag with no geometry behind it.
func TestHostReactive(t *testing.T) {
	cases := []struct {
		name      string
		stream    videopipeline.StreamInfo
		wantReact bool
		wantOK    bool
	}{
		{"zero stream falls back", videopipeline.StreamInfo{}, false, false},
		// Reactive=true but no geometry: not a live stream, so it must NOT be
		// trusted — a stale flag would otherwise pin Go to reactive forever.
		{"reactive without geometry falls back", videopipeline.StreamInfo{Reactive: true}, false, false},
		{"live reactive", videopipeline.StreamInfo{Width: 1280, Height: 800, Reactive: true}, true, true},
		{"live continuous", videopipeline.StreamInfo{Width: 1280, Height: 800, Reactive: false}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := &State{}
			st.model.Stream = tc.stream
			gotReact, gotOK := st.HostReactive()
			if gotReact != tc.wantReact || gotOK != tc.wantOK {
				t.Fatalf("HostReactive() = (%v, %v), want (%v, %v)",
					gotReact, gotOK, tc.wantReact, tc.wantOK)
			}
		})
	}
}

// TestGalleryRendersOneFrameHeadless renders the gallery body and the
// dialog part over a fixed model under the discard channel (ADR-0267 W19).
// RenderStatus is not exercised: it fetches the host's capabilities
// synchronously, which only a live host can answer (imzero2-fetchers).
func TestGalleryRendersOneFrameHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	res := RenderGallery(Input{Ids: ids, ScopeKey: "t", State: NewGalleryState(videopipeline.Model{})})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res := RenderDialog(Input{Ids: ids, State: &State{}}); res.Err != nil {
		t.Fatal(res.Err)
	}
	if res := RenderDialog(Input{}); res.Err == nil {
		t.Fatal("a nil Ids and State must be reported")
	}
}
