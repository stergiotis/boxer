//go:build integration

package play

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
)

// The Detail pane's leeway card derives every widget id once per frame: the
// host log carries no "id has already been used" line from the card emitter.
//
// The regression this guards: the card table's own id was an ordinal from a
// counter based at 0x30000, and each section-header cell's was the packed
// accentIdx<<16|col in the same scope, so section 3's column 1 derived the
// table's id on every frame of any card with three sections. Keying the
// column as a second ordinal under the section's is no fix either — the XOR
// stack commutes, so (1,2) and (2,1) derive one id.
//
// A scene of the computed kind (ADR-0248 §SD5): the oracle is the host log,
// which no trace step reads.
func TestSceneDetailCardIdsAreUnique(t *testing.T) {
	s := scenetest.Launch(t, scene.Spec{
		Launch:   "play",
		Size:     "1920x1200",
		Requires: []string{scene.RequireClickHouse, "table:anchor.facts"},
		Env: map[string]string{
			"BOXER_PLAY_WINDOW_SIZE": "1888x1100",
			"BOXER_PLAY_AUTORUN":     "1",
			"BOXER_PLAY_FOCUS_TABLE": "1",
			"BOXER_PLAY_SQL":         "SET param_selection = 0;\nSELECT * FROM anchor.facts WHERE `id:id` = 10005",
		},
	})
	scenetest.Run(t, s, `
{"do":"wait","name":"Run","role":"button"}
{"do":"wait","valueContains":"detail · row 1 / 1","role":"label"}
{"do":"sleep","settleMs":1500}`)

	b, err := os.ReadFile(filepath.Join(s.OutDir(), "logs", s.Name()+".host.log"))
	require.NoError(t, err)
	n, first := 0, ""
	for line := range strings.Lines(string(b)) {
		if strings.Contains(line, "id has already been used") && strings.Contains(line, "leewaywidgets") {
			if n == 0 {
				first = strings.TrimSpace(line)
			}
			n++
		}
	}
	require.Zero(t, n, "the card derived a widget id twice in a frame; first of %d: %s", n, first)
}
