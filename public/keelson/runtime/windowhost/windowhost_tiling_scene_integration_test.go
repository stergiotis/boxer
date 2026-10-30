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

// The tiled desktop (ADR-0346) on a running host, asserted on the window
// rects the accessibility tree reports, in the shape of
// TestSceneWindowArrangements. The layout is seeded by the env var, switched
// through the Window menu's Desktop layout submenu and by a chord, and the
// set of windows changes under it. As for the arrangements, the assertions
// are shapes rather than exact rects: a window's minimum is its content's,
// and that moves with the apps.

// tileSettleMs covers a settling run: one frame per pass, at most windows+1
// of them, at the host's 30 fps, plus the frame a new window waits for its
// handle.
const tileSettleMs = 1500

// layoutRadio switches the desktop's layout through the submenu, opening
// the Window menu and the submenu first when they are not open.
func layoutRadio(t *testing.T, s *scene.Session, layout string) {
	t.Helper()
	snap, err := s.Client.Tree(10 * time.Second)
	require.NoError(t, err)
	has := func(role, name string) bool {
		return slices.ContainsFunc(snap.Nodes, func(n *carrierclient.TreeNode) bool {
			return n.Role == role && strings.Contains(n.Name, name)
		})
	}
	if !has("radio_button", layout) {
		if !has("button", "Desktop layout") {
			scenetest.Run(t, s, `{"do":"click","name":"Window","role":"button"}`)
		}
		scenetest.Run(t, s, `{"do":"click","contains":"Desktop layout","role":"button"}`)
	}
	scenetest.Run(t, s, fmt.Sprintf(`{"do":"click","name":%q,"role":"radio_button","settleMs":%d}`, layout, tileSettleMs))
}

// layoutButton clicks a button of the submenu (the float toggle), opening
// the Window menu and the submenu first when they are not open.
func layoutButton(t *testing.T, s *scene.Session, name string) {
	t.Helper()
	snap, err := s.Client.Tree(10 * time.Second)
	require.NoError(t, err)
	has := func(role, n string) bool {
		return slices.ContainsFunc(snap.Nodes, func(node *carrierclient.TreeNode) bool {
			return node.Role == role && strings.Contains(node.Name, n)
		})
	}
	if !has("button", name) {
		if !has("button", "Desktop layout") {
			scenetest.Run(t, s, `{"do":"click","name":"Window","role":"button"}`)
		}
		scenetest.Run(t, s, `{"do":"click","contains":"Desktop layout","role":"button"}`)
	}
	scenetest.Run(t, s, fmt.Sprintf(`{"do":"click","name":%q,"role":"button","settleMs":%d}`, name, tileSettleMs))
}

// sameRect is whether two rects agree within the slack egui's rounding
// leaves.
func sameRect(a, b winRect) bool {
	return math.Abs(a.minX-b.minX) <= arrangeSlack && math.Abs(a.minY-b.minY) <= arrangeSlack &&
		math.Abs(a.maxX-b.maxX) <= arrangeSlack && math.Abs(a.maxY-b.maxY) <= arrangeSlack
}

func byTitle(ws []winRect, title string) winRect {
	for _, w := range ws {
		if w.title == title {
			return w
		}
	}
	return winRect{}
}

// assertFullHeightColumns is Columns' shape: every window starts at the top
// of the work area and the columns are evenly spaced, overlapping evenly
// where the minimums do not fit.
func assertFullHeightColumns(t *testing.T, what string, ws []winRect, menuBottom float64) {
	t.Helper()
	assertInside(t, what, ws, menuBottom)
	assertEvenLine(t, what, ws, true)
	for _, w := range ws {
		assert.InDelta(t, menuBottom+4, w.minY, arrangeSlack+2, "%s: %s does not start at the top of the work area", what, w.title)
	}
}

func TestSceneTiledDesktop(t *testing.T) {
	s := scenetest.Launch(t, scene.Spec{
		Launch:   "subject_alias IN ('launcher','widgets','opsdemo')",
		Size:     fmt.Sprintf("%.0fx%.0f", arrangeViewportW, arrangeViewportH),
		SettleMs: 2500,
		Env:      map[string]string{"IMZERO2_DESKTOP_LAYOUT": "columns"},
	})
	scenetest.Run(t, s, `{"do":"wait","name":"Window","role":"button"}`)
	scenetest.Run(t, s, fmt.Sprintf(`{"do":"sleep","settleMs":%d}`, tileSettleMs))

	t.Run("the env var seeds columns", func(t *testing.T) {
		ws, menuBottom := windows(t, s)
		assertFullHeightColumns(t, "seeded columns", ws, menuBottom)
	})

	t.Run("the submenu switches to rows", func(t *testing.T) {
		layoutRadio(t, s, "Rows")
		ws, menuBottom := windows(t, s)
		assertInside(t, "Rows", ws, menuBottom)
		assertEvenLine(t, "Rows", ws, false)
		for _, w := range ws {
			assert.InDelta(t, 4, w.minX, arrangeSlack+2, "Rows: %s does not start at the left edge", w.title)
		}
	})

	t.Run("tile stretches a short last row", func(t *testing.T) {
		layoutRadio(t, s, "Tile")
		ws, menuBottom := windows(t, s)
		assertInside(t, "Tile", ws, menuBottom)
		// Three windows make two rows; the lone window of the second row
		// is stretched across the width, so it is wider than either above
		// it whatever their minimums are.
		slices.SortFunc(ws, func(a, b winRect) int { return int(a.minY - b.minY) })
		last := ws[2]
		assert.Greater(t, last.minY, ws[1].minY+arrangeSlack, "Tile: no second row")
		for _, w := range ws[:2] {
			assert.Greater(t, last.maxX-last.minX, w.maxX-w.minX, "Tile: %s is wider than the stretched last row", w.title)
		}
	})

	t.Run("a floated window leaves the layout and the rest re-tile", func(t *testing.T) {
		layoutRadio(t, s, "Columns")
		before, _ := windows(t, s)
		layoutButton(t, s, "Float active window")
		after, menuBottom := windows(t, s)
		// Exactly one window kept its rect while the other two spread.
		var kept []winRect
		var tiled []winRect
		for _, w := range after {
			if sameRect(w, byTitle(before, w.title)) {
				kept = append(kept, w)
			} else {
				tiled = append(tiled, w)
			}
		}
		require.Len(t, kept, 1, "one window floats where it was")
		require.Len(t, tiled, 2)
		assertFullHeightColumns(t, "two columns beside a float", tiled, menuBottom)
		layoutButton(t, s, "Tile active window")
		ws, menuBottom := windows(t, s)
		assertFullHeightColumns(t, "three columns again", ws, menuBottom)
	})

	t.Run("a chord cycles the layout", func(t *testing.T) {
		// Alt+Space from Columns is Rows (menu order).
		scenetest.Run(t, s, fmt.Sprintf(`{"do":"key","text":"Space","modifiers":1,"settleMs":%d}`, tileSettleMs))
		ws, menuBottom := windows(t, s)
		assertInside(t, "Alt+Space", ws, menuBottom)
		assertEvenLine(t, "Alt+Space", ws, false)
	})

	t.Run("a window whose content grows after launch gets a larger row", func(t *testing.T) {
		// Still Rows. Typing into the operations demo's note grows that
		// window's content well past its row; the layout follows (ADR-0346
		// §SD2, update of 2026-10-30): the window's row grows to its new
		// need and the rows stay an even line inside the work area.
		before, _ := windows(t, s)
		grown := byTitle(before, arrangeTitles[2])
		words := make([]string, 60)
		for i := range words {
			words[i] = fmt.Sprintf("word%02d", i)
		}
		scenetest.Run(t, s, fmt.Sprintf(`{"do":"type","valueContains":"A note both","role":"multiline_text_input","text":%q,"settleMs":%d}`,
			strings.Join(words, " "), tileSettleMs))
		after, menuBottom := windows(t, s)
		now := byTitle(after, arrangeTitles[2])
		assert.Greater(t, now.maxY-now.minY, grown.maxY-grown.minY+100, "the note did not grow %s", grown.title)
		assertInside(t, "grown row", after, menuBottom)
		assertEvenLine(t, "grown row", after, false)
	})

	t.Run("closing a window re-tiles the rest", func(t *testing.T) {
		layoutRadio(t, s, "Columns")
		ws, _ := windows(t, s)
		gone := byTitle(ws, arrangeTitles[2])
		// The title-bar X of that window: the close button nearest its
		// top-right corner — columns that overlap put another window's X
		// inside its rect too.
		snap, err := s.Client.Tree(10 * time.Second)
		require.NoError(t, err)
		var closeX, closeY, best float64 = 0, 0, -1
		for _, n := range snap.Nodes {
			if n.Role != "button" || n.Name != "Close window" {
				continue
			}
			cx, cy := float64(n.X+n.W/2), float64(n.Y+n.H/2)
			d := math.Abs(cx-gone.maxX) + math.Abs(cy-gone.minY)
			if best < 0 || d < best {
				closeX, closeY, best = cx, cy, d
			}
		}
		require.Positive(t, closeX, "no close button in the tree")
		require.Less(t, best, 60.0, "no close button near %s's top-right corner", gone.title)
		scenetest.Run(t, s, fmt.Sprintf(`{"do":"click","x":%.0f,"y":%.0f,"settleMs":%d}`, closeX, closeY, tileSettleMs))
		snap, err = s.Client.Tree(10 * time.Second)
		require.NoError(t, err)
		var rest []winRect
		for _, n := range snap.Nodes {
			if n.Role != "window" {
				continue
			}
			for _, title := range arrangeTitles[:2] {
				if strings.Contains(n.Name, title) {
					rest = append(rest, winRect{title: title, minX: float64(n.X), minY: float64(n.Y), maxX: float64(n.X + n.W), maxY: float64(n.Y + n.H)})
				}
			}
			assert.NotContains(t, n.Name, arrangeTitles[2], "the closed window is gone")
		}
		require.Len(t, rest, 2)
		var menuBottom float64
		for _, n := range snap.Nodes {
			if n.Role == "button" && n.Name == "Window" {
				menuBottom = float64(n.Y + n.H)
			}
		}
		assertFullHeightColumns(t, "two columns after a close", rest, menuBottom)
	})
}
