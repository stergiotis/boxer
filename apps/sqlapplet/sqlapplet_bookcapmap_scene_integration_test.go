//go:build integration

package sqlapplet

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
)

// TestSceneCompBrowserWalksFromTheTable drives the competence browser in a
// headless host and checks the walk its document promises: a table row click
// re-focuses the family graph.
//
// The buffer-level test cannot see this. It runs the SQL with selection_key
// set by hand, so it stays green when nothing in the window ever publishes
// one — which is what happened while the list had no `key` column, the one
// column play's Table publishes. The oracle is the graph's own status line:
// the root's neighbourhood is 3 nodes, and observability's — its parent, its
// two children and the co-parent of the shared block — is 5.
//
// The window is opened at a workspace size because at the host's 900×640
// fallback the browser's panes have no height, and a row that is never drawn
// cannot be clicked.
func TestSceneCompBrowserWalksFromTheTable(t *testing.T) {
	s := scenetest.Launch(t, scene.Spec{
		Launch:   "subject_alias = 'comp-browser'",
		Size:     "1600x1100",
		Requires: []string{scene.RequireExePrefix + "clickhouse local"},
		Env: map[string]string{
			"BOXER_CAPMAP_VAULT":          writeCapmapFixtureVault(t),
			"BOXER_SQLAPPLET_WINDOW_SIZE": "1560x1020",
		},
	})
	scenetest.Run(t, s, `
{"do":"read","valueContains":"rows ·","role":"label","pattern":"(?P<rows>\\d+) rows","settleMs":3000}
{"do":"expect","of":"rows","eq":5}
{"do":"click","name":"Network *","role":"button","settleMs":1000}
{"do":"read","valueContains":"nodes ·","role":"label","pattern":"^(?P<before>\\d+) nodes"}
{"do":"expect","of":"before","eq":3,"comment":"unfocused: the root and its two children"}
{"do":"click","name":"Table *","role":"button","settleMs":1000}
{"do":"click","name":"observability","role":"button","nth":0,"pointer":true,"settleMs":2000}
{"do":"read","valueContains":"detail · row","role":"label","pattern":"row (?P<row>\\d+) /"}
{"do":"expect","of":"row","eq":3,"comment":"the Live re-run the click causes keeps the clicked row selected"}
{"do":"click","name":"Network *","role":"button","settleMs":1000}
{"do":"read","valueContains":"nodes ·","role":"label","pattern":"^(?P<after>5) nodes · 5 edges","comment":"polls until the graph has re-read the focus"}`)
}
