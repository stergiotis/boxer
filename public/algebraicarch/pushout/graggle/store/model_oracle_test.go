// Semantic model oracle: a single-actor edit/undo session where the
// graggle's rendered output must equal an independently tracked list of
// lines at every step. Unlike the structural invariants (which accept
// any self-consistent graph) and the property tests (which compare two
// graggles), this pins the engine's MEANING against a trivially correct
// model: a stack of file snapshots. LineDiff, Apply, Unapply, pseudo-edge
// resolution, and Render all sit on the verified path.
package store

import (
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func TestModelOracle_EditUndoSession(tt *testing.T) {
	lineVals := []string{"alpha\n", "beta\n", "gamma\n", "delta\n", "beta\n"} // duplicate on purpose
	rapid.Check(tt, func(rt *rapid.T) {
		g := New()
		var patches []*patch.Patch
		snapshots := []string{""} // snapshots[i] = expected render after patches[:i]

		expectRender := func(rt *rapid.T) {
			want := snapshots[len(snapshots)-1]
			if got := string(g.Render()); got != want {
				rt.Fatalf("render diverged from model:\n got: %q\nwant: %q", got, want)
			}
			if errs := qc.CheckInvariants(g); len(errs) != 0 {
				rt.Fatalf("invariants violated: %v", errs)
			}
		}

		rt.Repeat(map[string]func(*rapid.T){
			"edit": func(rt *rapid.T) {
				target := rapid.SliceOfN(rapid.SampledFrom(lineVals), 0, 6).Draw(rt, "target")

				order := mustLinearOrder(rt, g)
				var oldIDs []t.NodeID
				var oldContents [][]byte
				for _, n := range order {
					if n == t.RootNodeID {
						continue
					}
					oldIDs = append(oldIDs, n)
					oldContents = append(oldContents, g.NodeContent(n))
				}
				newLines := make([][]byte, len(target))
				for i, l := range target {
					newLines[i] = []byte(l)
				}
				diff, derr := patch.LineDiff(oldIDs, oldContents, newLines)
				if derr != nil {
					rt.Fatalf("LineDiff: %v", derr)
				}
				if len(diff.Changes) == 0 {
					return // target equals current state; nothing to record
				}
				// Identity collision: editing back to a previously
				// recorded shape (edit T, delete all, edit T again)
				// rebuilds the exact {deps, changes} of a still-applied
				// patch whose nodes are tombstoned. Same patch ⇒ cannot
				// re-apply; the recording client disambiguates node
				// identity, mirroring the backend's
				// shiftPlaceholderIndexes.
				deps := patch.ComputeDependencies(diff.Changes)
				p := patch.NewPatch("model", "edit", deps, diff.Changes)
				applied := make(map[t.PatchHash]struct{}, len(patches))
				for _, q := range patches {
					applied[q.Hash] = struct{}{}
				}
				for attempt := uint64(1); ; attempt++ {
					if _, clash := applied[p.Hash]; !clash {
						break
					}
					if attempt > 8 {
						rt.Fatalf("could not disambiguate colliding patch %s", p.Hash)
					}
					shifted := make([]patch.Change, len(diff.Changes))
					for i, c := range diff.Changes {
						shifted[i] = c
						if c.NodeID.Patch.IsPlaceholder() {
							shifted[i].NodeID.Index += attempt << 32
						}
						shifted[i].UpContext = shiftPlaceholders(c.UpContext, attempt<<32)
						shifted[i].DownContext = shiftPlaceholders(c.DownContext, attempt<<32)
					}
					p = patch.NewPatch("model", "edit", deps, shifted)
				}
				if err := p.Apply(g); err != nil {
					rt.Fatalf("apply: %v", err)
				}
				patches = append(patches, p)
				snapshots = append(snapshots, strings.Join(target, ""))
			},
			"undo": func(rt *rapid.T) {
				if len(patches) == 0 {
					return
				}
				p := patches[len(patches)-1]
				if err := p.Unapply(g); err != nil {
					rt.Fatalf("unapply: %v", err)
				}
				patches = patches[:len(patches)-1]
				snapshots = snapshots[:len(snapshots)-1]
			},
			"": expectRender,
		})
	})
}

// mustLinearOrder: the single-actor session must stay linear at all
// times — a fork here would itself be a finding.
func mustLinearOrder(rt *rapid.T, g *Graggle) []t.NodeID {
	g.ResolvePseudoEdges()
	order := algo.LinearOrder(g)
	if order == nil {
		rt.Fatalf("single-actor session lost its linear order:\n%s", g.Debug())
	}
	return order
}

func shiftPlaceholders(ids []t.NodeID, offset uint64) (out []t.NodeID) {
	if ids == nil {
		return nil
	}
	out = make([]t.NodeID, len(ids))
	for i, id := range ids {
		out[i] = id
		if id.Patch.IsPlaceholder() {
			out[i].Index += offset
		}
	}
	return
}
