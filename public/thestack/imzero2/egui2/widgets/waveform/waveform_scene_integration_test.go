//go:build integration

package waveform_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The waveform player's pointer and transport paths (ADR-0208's verification
// plan): seek by click, pan by drag, edit a region, play on the null sink,
// asserted through the readout labels. The interesting failures are the silent
// ones every painter widget is exposed to — a sense region that does not win
// the hit test, a drag summed from deltas that lands short, a click whose id
// never reads back. A capture would look right through all of them.
//
// A scene of the computed kind (ADR-0248 §SD5): the canvas is painter-only, so
// the pointer is aimed between two nodes, and the region press is placed from
// a hover readout and the demo's burst period.

const (
	// The demo's regions are its tone bursts: 0.35 s every burstPeriod, and
	// burstEntry into one is well inside it.
	burstPeriod = 0.9
	burstEntry  = 0.17
	// secPerPx is the zoom the scene pins, so pixel offsets are known durations.
	secPerPx = 0.010
)

func roundHalfEven(v float64) int { return int(math.RoundToEven(v)) }

// regionPressX is where to press so the pointer is burstEntry seconds into the
// next burst after the time under canvasX.
func regionPressX(canvasX int, hoverMin, hoverSec float64) int {
	at := hoverMin*60 + hoverSec
	target := math.Ceil(at/burstPeriod)*burstPeriod + burstEntry
	return roundHalfEven(float64(canvasX) + (target-at)/secPerPx)
}

func TestSceneRegionPressLandsInsideTheNextBurst(t *testing.T) {
	// 0:02.000 under x=500: the next burst starts at 2.7 s, so 0.87 s = 87 px on.
	assert.Equal(t, 587, regionPressX(500, 0, 2.0))
	// Exactly on a burst boundary the press goes into that burst.
	assert.Equal(t, 517, regionPressX(500, 0, 1.8))
}

const expandDemo = `
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"waveform","comment":"narrow the gallery to the demo"}
{"do":"wait","contains":"waveform player","settleMs":400}
{"do":"click","contains":"waveform player","comment":"expand the demo's section"}
{"do":"wait","name":"10 ms/px","settleMs":600,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"10 ms/px","settleMs":600}`

func TestSceneWaveformPlayer(t *testing.T) {
	s := scenetest.Launch(t, scene.Spec{
		Launch: "widgets",
		Size:   "1400x1000",
		Needs:  []string{scene.NeedRaster},
		Env:    map[string]string{"BOXER_AUDIO_PEAKS_CACHE_DIR": t.TempDir()},
	})
	scenetest.Run(t, s, expandDemo+`
{"do":"wait","value":"position: 0:00.000 · paused","role":"label"}`)

	// The canvas is the 220 px strip between the button row and the first
	// readout line; their midpoint is inside it whatever the row heights are.
	snap, err := s.Client.Tree(30 * time.Second)
	require.NoError(t, err)
	button, err := carrierclient.Resolve(snap, carrierclient.Locator{Name: "10 ms/px"})
	require.NoError(t, err)
	readout, err := carrierclient.Resolve(snap, carrierclient.Locator{Value: "position: 0:00.000 · paused", Role: "label"})
	require.NoError(t, err)
	bx, by := carrierclient.Center(button)
	_, ry := carrierclient.Center(readout)
	cxi, cyi := roundHalfEven(float64(bx)), roundHalfEven(float64(by+ry)/2)
	cx, cy := strconv.Itoa(cxi), strconv.Itoa(cyi)

	// At 10 ms/px a 300 px drag is exactly 3.000 s; the pan is press-origin
	// plus offset, so anything else means the drag path lost or doubled moves.
	scenetest.Run(t, s, `
{"do":"click","name":"10 ms/px"}
{"do":"wait","valueContains":"· 10.000 ms/px","role":"label"}
{"do":"hover","x":`+cx+`,"y":`+cy+`,"settleMs":300}
{"do":"wait","valueContains":"hover: 0:0","role":"label","comment":"a time under the pointer, read back through R24"}
{"do":"click","x":`+cx+`,"y":`+cy+`,"settleMs":300}
{"do":"wait","valueContains":"clicked: 0:0","role":"label","comment":"the sense region wins the hit test and its click reads back"}
{"do":"click","name":"\ue5a4 start","comment":"back to frame 0 so the view starts at 0:00.000"}
{"do":"wait","value":"position: 0:00.000 · paused","role":"label"}
{"do":"drag","x":`+cx+`,"y":`+cy+`,"toX":`+strconv.Itoa(cxi-300)+`,"toY":`+cy+`,"steps":16,"durationMs":320,"settleMs":400}
{"do":"wait","valueContains":"view: 0:03.000 –","role":"label","comment":"300 px at 10 ms/px, from a view that started at 0"}`)

	// Measure the time under the pointer, to land a press inside a region.
	scenetest.Run(t, s, `
{"do":"hover","x":`+cx+`,"y":`+cy+`,"settleMs":300}
{"do":"read","valueContains":"hover: ","role":"label","pattern":"hover: (?P<hm>\\d+):(?P<hs>[\\d.]+)"}`)
	exi := regionPressX(cxi, scenetest.Number(t, s, "hm"), scenetest.Number(t, s, "hs"))
	ex := strconv.Itoa(exi)

	scenetest.Run(t, s, `
{"do":"click","name":"edit regions"}
{"do":"drag","x":`+ex+`,"y":`+cy+`,"toX":`+strconv.Itoa(exi+200)+`,"toY":`+cy+`,"steps":16,"durationMs":320,"settleMs":400}
{"do":"wait","valueContains":"edit: region","role":"label","comment":"an editable region under the press took the drag and reported the new bounds (SD8)"}
{"do":"click","name":"edit regions","comment":"back to pan-by-drag"}
{"do":"click","name":"wall clock"}
{"do":"wait","valueContains":"position: 09:00:0","role":"label","comment":"the synthetic track has an epoch of 09:00:00 (SD9)"}
{"do":"click","name":"wall clock"}
{"do":"wait","valueContains":"position: 0:0","role":"label"}
{"do":"click","name":"\ue3d0 play"}
{"do":"wait","valueContains":"· playing","role":"label"}
{"do":"sleep","settleMs":1000}
{"do":"click","name":"\ue39e pause"}
{"do":"wait","valueContains":"· paused","role":"label"}
{"do":"wait","valueContains":"position: 0:01.","role":"label","comment":"between one and two seconds elapsed on the null sink's clock"}
{"do":"capture","text":"waveform-player"}`)
	requireCapture(t, s, "waveform-player")
}

// The demo's path field is not reachable through the accessibility tree, so
// the file goes in through BOXER_WAVEFORM_DEMO_FILE at mount — launch state,
// hence a scene of its own.
func TestSceneWaveformFile(t *testing.T) {
	if _, ok := extbin.Ffmpeg.Resolve(); !ok {
		t.Skip("ffmpeg is not installed")
	}
	fixture := filepath.Join(t.TempDir(), "tone.flac")
	out, err := extbin.Ffmpeg.CombinedOutput(context.Background(), extbin.Opts{}, "-y", "-v", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=30", "-ac", "2", "-ar", "48000", "-c:a", "flac", fixture)
	require.NoError(t, err, string(out))

	s := scenetest.Launch(t, scene.Spec{
		Launch: "widgets",
		Size:   "1400x1000",
		Needs:  []string{scene.NeedRaster},
		Env: map[string]string{
			// A per-run cache, so the readout says "built", not "from cache".
			"BOXER_AUDIO_PEAKS_CACHE_DIR": t.TempDir(),
			"BOXER_WAVEFORM_DEMO_FILE":    fixture,
		},
	})
	scenetest.Run(t, s, expandDemo+`
{"do":"wait","valueContains":"track: ffmpeg file","role":"label","comment":"the sniffing opener routed a FLAC to ffmpeg"}
{"do":"wait","valueContains":"peaks built","role":"label","comment":"the background build over the ffmpeg decoder completed"}
{"do":"wait","valueContains":"· task ","role":"label","comment":"the build was reported as a keelson task through the gallery host's bus (ADR-0038)"}
{"do":"wait","valueContains":"view: 0:00.000 – 0:30.000","role":"label","comment":"the declared length is the file's"}
{"do":"capture","text":"waveform-file"}`)
	requireCapture(t, s, "waveform-file")
}

func requireCapture(t *testing.T, s *scene.Session, name string) {
	t.Helper()
	st, err := os.Stat(filepath.Join(s.OutDir(), name+".png"))
	require.NoError(t, err, "the capture was written")
	assert.NotZero(t, st.Size())
}
