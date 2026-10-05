//go:build integration

package windowhost_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Window menu's arrangements (ADR-0275) on a running host, asserted on the
// window rects the accessibility tree reports. A scene of the computed kind
// (ADR-0248 §SD5): the oracle is geometry over the rects, which no declarative
// step can read. The assertions are the arrangements' shapes rather than
// their exact rects, because the rects depend on what each app's content
// needs (ADR-0275 §SD4) and that moves with the apps.
//
// A placement fixes a window's position and its extent only where the
// content fills it: egui sizes a window to its content on an axis the
// content does not fill, so a window with short content ends shorter than
// its cell. The assertions allow for that — leading edges align, trailing
// edges stay inside the cell.

const (
	arrangeViewportW float64 = 1400
	arrangeViewportH float64 = 900
	// arrangeSlack absorbs egui's pixel rounding of window positions.
	arrangeSlack float64 = 2
	// arrangeSettleMs covers an arrangement's passes: one frame per pass,
	// at most windows+1 of them, at the host's 30 fps.
	arrangeSettleMs = 1200
)

// arrangeTitles names the windows the scene opens, by a substring of each
// title; titles carry an icon glyph in front.
var arrangeTitles = []string{"Apps — launcher", "Widget gallery", "Operations demo"}

type winRect struct {
	title                  string
	minX, minY, maxX, maxY float64
}

// windows reads the three windows' rects, and the bottom of the menu bar,
// which is the top of the work area.
func windows(t *testing.T, s *scene.Session) (ws []winRect, menuBottom float64) {
	t.Helper()
	ws, menuBottom, _ = windowsAndStatus(t, s)
	return
}

// windowsAndStatus is windows plus the top of the status bar's first segment
// (the run id), which lies inside the bottom panel and so below the work
// area.
func windowsAndStatus(t *testing.T, s *scene.Session) (ws []winRect, menuBottom float64, statusTop float64) {
	t.Helper()
	snap, err := s.Client.Tree(10 * time.Second)
	require.NoError(t, err)
	for _, title := range arrangeTitles {
		found := false
		for _, n := range snap.Nodes {
			if n.Role != "window" || !strings.Contains(n.Name, title) {
				continue
			}
			require.False(t, found, "two windows named %q", title)
			found = true
			ws = append(ws, winRect{title: title,
				minX: float64(n.X), minY: float64(n.Y),
				maxX: float64(n.X + n.W), maxY: float64(n.Y + n.H)})
		}
		require.True(t, found, "window %q not in the tree", title)
	}
	for _, n := range snap.Nodes {
		if n.Role == "button" && n.Name == "Window" {
			menuBottom = float64(n.Y + n.H)
		}
		if n.Role == "button" && strings.HasPrefix(n.Name, "run:") {
			statusTop = float64(n.Y)
		}
	}
	require.Positive(t, menuBottom, "the Window menu is not in the tree")
	require.Positive(t, statusTop, "the status bar's run segment is not in the tree")
	return
}

// arrange runs a Window menu command, opening the menu first when it is not
// open: a press elsewhere may or may not have closed it.
func arrange(t *testing.T, s *scene.Session, cmd string) {
	t.Helper()
	snap, err := s.Client.Tree(10 * time.Second)
	require.NoError(t, err)
	if !slices.ContainsFunc(snap.Nodes, func(n *carrierclient.TreeNode) bool {
		return n.Role == "button" && n.Name == cmd
	}) {
		scenetest.Run(t, s, `{"do":"click","name":"Window","role":"button"}`)
	}
	scenetest.Run(t, s, fmt.Sprintf(`{"do":"click","name":%q,"role":"button","settleMs":%d}`, cmd, arrangeSettleMs))
}

func assertInside(t *testing.T, cmd string, ws []winRect, menuBottom float64) {
	t.Helper()
	for _, w := range ws {
		assert.GreaterOrEqual(t, w.minX, -arrangeSlack, "%s: %s left of the viewport", cmd, w.title)
		assert.GreaterOrEqual(t, w.minY, menuBottom-arrangeSlack, "%s: %s over the menu bar", cmd, w.title)
		assert.LessOrEqual(t, w.maxX, arrangeViewportW+arrangeSlack, "%s: %s right of the viewport", cmd, w.title)
		assert.LessOrEqual(t, w.maxY, arrangeViewportH+arrangeSlack, "%s: %s below the viewport", cmd, w.title)
	}
}

// assertEvenLine checks windows laid along one axis: starting together on the
// other axis and ending no further than the longest, and each overlapping (or
// separated from) the next by the same amount along the axis — the shape
// fitSpans gives whether or not the minimums fit. Along the axis a window's
// extent is the one placed, since a column's width (a row's height) is what
// the content was laid out to fill.
func assertEvenLine(t *testing.T, cmd string, ws []winRect, horizontal bool) {
	t.Helper()
	lo := func(w winRect) float64 { return w.minY }
	hi := func(w winRect) float64 { return w.maxY }
	across := func(w winRect) (float64, float64) { return w.minX, w.maxX }
	if horizontal {
		lo = func(w winRect) float64 { return w.minX }
		hi = func(w winRect) float64 { return w.maxX }
		across = func(w winRect) (float64, float64) { return w.minY, w.maxY }
	}
	ws = slices.Clone(ws)
	slices.SortFunc(ws, func(a, b winRect) int { return int(lo(a) - lo(b)) })
	a0, _ := across(ws[0])
	end := 0.0
	for _, w := range ws {
		_, b1 := across(w)
		end = math.Max(end, b1)
	}
	for _, w := range ws[1:] {
		b0, b1 := across(w)
		assert.InDelta(t, a0, b0, arrangeSlack, "%s: %s does not start with the others", cmd, w.title)
		assert.LessOrEqual(t, b1, end+arrangeSlack, "%s: %s ends past the others", cmd, w.title)
	}
	step := lo(ws[1]) - hi(ws[0])
	for i := 1; i < len(ws)-1; i++ {
		assert.InDelta(t, step, lo(ws[i+1])-hi(ws[i]), arrangeSlack, "%s: uneven spacing after %s", cmd, ws[i].title)
	}
}

func overlapArea(a, b winRect) float64 {
	w := math.Min(a.maxX, b.maxX) - math.Max(a.minX, b.minX)
	h := math.Min(a.maxY, b.maxY) - math.Max(a.minY, b.minY)
	if w <= arrangeSlack || h <= arrangeSlack {
		return 0
	}
	return w * h
}

func TestSceneWindowArrangements(t *testing.T) {
	s := scenetest.Launch(t, scene.Spec{
		Launch:   "subject_alias IN ('launcher','widgets','opsdemo')",
		Size:     fmt.Sprintf("%.0fx%.0f", arrangeViewportW, arrangeViewportH),
		SettleMs: 2000,
	})
	scenetest.Run(t, s, `{"do":"wait","name":"Window","role":"button"}`)

	t.Run("cascade steps down and right in one direction", func(t *testing.T) {
		arrange(t, s, "Cascade")
		ws, menuBottom := windows(t, s)
		assertInside(t, "Cascade", ws, menuBottom)
		slices.SortFunc(ws, func(a, b winRect) int { return int(a.minY - b.minY) })
		dy := ws[1].minY - ws[0].minY
		assert.Positive(t, dy)
		for i := 1; i < len(ws); i++ {
			assert.InDelta(t, dy, ws[i].minY-ws[i-1].minY, arrangeSlack, "uneven cascade step at %s", ws[i].title)
			assert.Greater(t, ws[i].minX, ws[i-1].minX, "%s not right of %s", ws[i].title, ws[i-1].title)
		}
	})

	t.Run("tile leaves no window over another", func(t *testing.T) {
		arrange(t, s, "Tile")
		ws, menuBottom := windows(t, s)
		assertInside(t, "Tile", ws, menuBottom)
		for i := range ws {
			for j := i + 1; j < len(ws); j++ {
				assert.Zero(t, overlapArea(ws[i], ws[j]), "Tile: %s over %s", ws[i].title, ws[j].title)
			}
		}
		minX, maxX := ws[0].minX, ws[0].maxX
		for _, w := range ws {
			minX, maxX = math.Min(minX, w.minX), math.Max(maxX, w.maxX)
		}
		assert.Greater(t, maxX-minX, 0.95*arrangeViewportW, "Tile does not span the work area")
	})

	t.Run("side by side spaces columns evenly", func(t *testing.T) {
		arrange(t, s, "Side by side")
		ws, menuBottom := windows(t, s)
		assertInside(t, "Side by side", ws, menuBottom)
		assertEvenLine(t, "Side by side", ws, true)
	})

	t.Run("stacked spaces rows evenly", func(t *testing.T) {
		arrange(t, s, "Stacked")
		ws, menuBottom := windows(t, s)
		assertInside(t, "Stacked", ws, menuBottom)
		assertEvenLine(t, "Stacked", ws, false)
	})

	t.Run("gather brings a window out from under the status bar", func(t *testing.T) {
		ws, _ := windows(t, s)
		w := ws[len(ws)-1]
		// egui keeps a window inside the viewport but not inside the work
		// area: dragged down by its title bar, it ends under the status bar.
		x, y := (w.minX+w.maxX)/2, w.minY+12
		scenetest.Run(t, s, fmt.Sprintf(`{"do":"drag","x":%.0f,"y":%.0f,"toX":%.0f,"toY":%.0f,"settleMs":500}`,
			x, y, x, arrangeViewportH-1))
		moved, _, statusTop := windowsAndStatus(t, s)
		require.Greater(t, moved[len(moved)-1].maxY, statusTop,
			"the drag did not move %s under the status bar", w.title)
		arrange(t, s, "Gather into view")
		ws, menuBottom, statusTop := windowsAndStatus(t, s)
		assertInside(t, "Gather into view", ws, menuBottom)
		for _, g := range ws {
			assert.LessOrEqual(t, g.maxY, statusTop, "Gather into view: %s still under the status bar", g.title)
		}
	})
}
