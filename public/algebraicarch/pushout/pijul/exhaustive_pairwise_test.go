// Small-scope exhaustive enumeration: EVERY pair of single-cell edits
// two actors can make from a common base is merged in both directions
// and checked. Randomized harnesses sample this space; this walks it
// completely (the "small scope hypothesis": most semantic bugs already
// show up in minimal configurations). Universal post-conditions:
//
//   - both repos converge to the same observable state
//   - structural invariants hold on both
//   - identical ops converge on ONE patch (identity dedup through the
//     full push/pull path), distinct ops yield two
//   - ops on different paths never conflict
//   - same-path distinct surviving values conflict, with both sides
//     present in the ConflictData
package pijul

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type pairOp struct {
	name  string
	cells []KVLine // full desired cell list, edited from base {a:1, b:1}
}

func pairwiseOps() []pairOp {
	base := func(mut func(m map[string]string)) []KVLine {
		m := map[string]string{"a": "1", "b": "1"}
		mut(m)
		paths := make([]string, 0, len(m))
		for p := range m {
			paths = append(paths, p)
		}
		slices.Sort(paths)
		out := make([]KVLine, 0, len(m))
		for _, p := range paths {
			out = append(out, KVLine{Path: p, Value: m[p]})
		}
		return out
	}
	return []pairOp{
		{"set_a2", base(func(m map[string]string) { m["a"] = "2" })},
		{"set_a3", base(func(m map[string]string) { m["a"] = "3" })},
		{"del_a", base(func(m map[string]string) { delete(m, "a") })},
		{"set_b2", base(func(m map[string]string) { m["b"] = "2" })},
		{"new_c", base(func(m map[string]string) { m["c"] = "1" })},
		{"noop", base(func(m map[string]string) {})},
	}
}

// editedPath returns which of a/b/c the op touches, or "" for noop.
func (op pairOp) editedPath() string {
	switch {
	case op.name == "noop":
		return ""
	case strings.Contains(op.name, "_a"):
		return "a"
	case strings.Contains(op.name, "_b"):
		return "b"
	default:
		return "c"
	}
}

func TestExhaustive_TwoActorMergeMatrix(tt *testing.T) {
	ops := pairwiseOps()
	for _, opA := range ops {
		for _, opB := range ops {
			tt.Run(fmt.Sprintf("%s_vs_%s", opA.name, opB.name), func(tt *testing.T) {
				ctx := context.Background()
				b := NewPushoutBackend()
				alice := newTestRepo(tt, b, "alice")
				mustRecord(tt, alice, []KVLine{{Path: "a", Value: "1"}, {Path: "b", Value: "1"}}, "alice", "base")
				bobI, _, err := b.Clone(ctx, alice, tt.TempDir(), "bob")
				if err != nil {
					tt.Fatal(err)
				}
				bob := bobI.(*PushoutRepo)

				idA, _, err := alice.SetAndRecord(ctx, opA.cells, "alice", "opA "+opA.name)
				if err != nil {
					tt.Fatal(err)
				}
				idB, _, err := bob.SetAndRecord(ctx, opB.cells, "bob", "opB "+opB.name)
				if err != nil {
					tt.Fatal(err)
				}

				// Exchange both ways.
				if _, _, err := bob.Pull(ctx, alice); err != nil {
					tt.Fatal(err)
				}
				if _, _, err := alice.Pull(ctx, bob); err != nil {
					tt.Fatal(err)
				}

				// Universal: convergence + invariants.
				aCells, aLog := stateCells(tt, alice)
				bCells, bLog := stateCells(tt, bob)
				if got, want := observableString(aCells), observableString(bCells); got != want {
					tt.Fatalf("divergence after full exchange:\nalice: %s\nbob:   %s", got, want)
				}
				if len(aLog) != len(bLog) {
					tt.Fatalf("log length divergence: alice=%d bob=%d", len(aLog), len(bLog))
				}
				assertRepoInvariants(tt, alice)
				assertRepoInvariants(tt, bob)

				// Identity dedup: identical resulting patches converge on
				// one history entry, distinct ones on two. Derived from
				// the recorded ids, valid even when an op was a no-op.
				wantPatches := 1 // base
				switch {
				case idA.Empty() && idB.Empty():
				case idA.Empty() || idB.Empty():
					wantPatches++
				case idA == idB:
					wantPatches++
				default:
					wantPatches += 2
				}
				if len(aLog) != wantPatches {
					tt.Fatalf("expected %d patches after merge (idA=%q idB=%q), got %d", wantPatches, idA.Short(), idB.Short(), len(aLog))
				}

				// Different (or no) paths touched ⇒ never a conflict.
				pa, pb := opA.editedPath(), opB.editedPath()
				if pa != pb || pa == "" {
					for _, c := range aCells {
						if c.Conflict != nil {
							tt.Fatalf("unexpected conflict on %s for disjoint ops: %+v", c.Path, c)
						}
					}
				}

				// Same path, both SET distinct values ⇒ conflict carrying
				// both sides. (Delete-vs-set pairs are checked only by the
				// universal conditions: the surviving-node outcome is the
				// engine's call, convergence is not.)
				bothSet := strings.HasPrefix(opA.name, "set_") && strings.HasPrefix(opB.name, "set_")
				if bothSet && pa == pb && idA != idB {
					var found *ConflictData
					for _, c := range aCells {
						if c.Path == pa {
							found = c.Conflict
						}
					}
					if found == nil {
						tt.Fatalf("expected conflict on %s for %s vs %s", pa, opA.name, opB.name)
					}
					sides := found.AllValues()
					wantA := opA.cells[indexOfPath(opA.cells, pa)].Value
					wantB := opB.cells[indexOfPath(opB.cells, pa)].Value
					if !slices.Contains(sides, wantA) || !slices.Contains(sides, wantB) {
						tt.Fatalf("conflict sides %v missing %q or %q", sides, wantA, wantB)
					}
				}
			})
		}
	}
}

func observableString(cells []KVLine) string {
	var sb strings.Builder
	for _, c := range cells {
		if c.Conflict != nil {
			sides := c.Conflict.AllValues()
			slices.Sort(sides)
			fmt.Fprintf(&sb, "%s?%q ", c.Path, sides)
		} else {
			fmt.Fprintf(&sb, "%s=%q ", c.Path, c.Value)
		}
	}
	return sb.String()
}

func indexOfPath(cells []KVLine, path string) int {
	for i, c := range cells {
		if c.Path == path {
			return i
		}
	}
	return -1
}
