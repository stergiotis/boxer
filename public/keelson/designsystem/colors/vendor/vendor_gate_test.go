// SPDX-License-Identifier: MIT

package vendor

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/gen"
)

// batlowSFirst10 is the palette that shipped as the IDS qualitative cycle
// before ADR-0156 — Crameri batlowS's first-10 subset. It is the concrete
// regression this gate exists to catch, so it doubles as the fixture: if
// gateQualitative ever stops rejecting it, the gate has rotted.
var batlowSFirst10 = [][3]uint8{
	{1, 25, 89}, {250, 204, 250}, {130, 130, 49}, {34, 96, 97}, {241, 157, 107},
	{77, 115, 77}, {17, 67, 96}, {253, 180, 180}, {192, 144, 54}, {23, 82, 98},
}

func TestGateQualitativeRejectsBatlowS(t *testing.T) {
	findings := gateQualitative(lut{Name: "batlow_s", RGB: batlowSFirst10})
	if len(findings) == 0 {
		t.Fatal("gate accepted batlowS, the palette it exists to reject")
	}

	// The contrast arm must fire on the four entries measured below 3:1,
	// and specifically on slot 0, which sits at the background's own
	// luminance (1.00:1) — the defect that motivated ADR-0156.
	joined := strings.Join(findings, "\n")
	for _, want := range []string{
		"slot 0 (#011959): 1.00:1",
		"slot 3 (#226061): 2.27:1",
		"slot 6 (#114360): 1.56:1",
		"slot 9 (#175262): 1.89:1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing contrast finding %q in:\n%s", want, joined)
		}
	}

	// The separation arms must fire too — batlowS's slots 3/9 are
	// near-duplicate dark teals that the previous RGB-Euclidean floor
	// passed.
	if !strings.Contains(joined, "slots 3/9") {
		t.Errorf("gate missed the 3/9 near-duplicate pair in:\n%s", joined)
	}
}

func TestGateQualitativeAcceptsShippedPalette(t *testing.T) {
	luts, err := assemble(upstreamDirForTest(t))
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	seen := 0
	for _, l := range luts {
		if !l.Qualitative {
			continue
		}
		seen++
		findings := gateQualitative(l)
		if len(findings) > 0 {
			t.Errorf("%s fails its own gate:\n  %s", l.Name, strings.Join(findings, "\n  "))
		}
		gating, _, err := gen.CrossPalette("", l.RGB)
		if err != nil {
			t.Fatalf("CrossPalette: %v", err)
		}
		if len(gating) > 0 {
			t.Errorf("%s fails the cross-palette gate:\n  %s", l.Name, strings.Join(gating, "\n  "))
		}
	}
	if seen == 0 {
		t.Fatal("no palette is marked Qualitative — the gate would be vacuous")
	}
}

func upstreamDirForTest(t *testing.T) (dir string) {
	t.Helper()
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	dir = root + "/rust/imzero2/assets/colors/scientific/upstream"
	return
}

// TestOkabeItoCycleOrderIsASubsetPermutation holds the vendor rule of
// ADR-0156 §SD2: the shipped cycle is the publication's values, subset and
// re-ordered, never altered. Every published entry but black appears once.
func TestOkabeItoCycleOrderIsASubsetPermutation(t *testing.T) {
	seen := map[int]bool{}
	for _, i := range okabeItoCycleOrder {
		if i < 1 || i > 7 || seen[i] {
			t.Fatalf("okabeItoCycleOrder %v: index %d out of 1..7 or repeated", okabeItoCycleOrder, i)
		}
		seen[i] = true
	}
	if len(seen) != 7 {
		t.Fatalf("okabeItoCycleOrder %v drops a non-black entry", okabeItoCycleOrder)
	}
}

func TestPermuteRejectsBadOrder(t *testing.T) {
	rgb := [][3]uint8{{1, 1, 1}, {2, 2, 2}}
	for _, order := range [][]int{{0, 0}, {2}, {-1}} {
		_, err := permute(rgb, order)
		if err == nil {
			t.Errorf("permute accepted %v", order)
		}
	}
	out, err := permute(rgb, []int{1, 0})
	if err != nil || out[0] != rgb[1] || out[1] != rgb[0] {
		t.Errorf("permute([1 0]) = %v, %v", out, err)
	}
}
