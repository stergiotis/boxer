package cardgrid

import "testing"

func TestPlanColumns(t *testing.T) {
	cases := []struct {
		paneW float32
		d     DensityE
		cols  int
	}{
		{100, DensityMedium, 1}, // narrower than one card: still one column
		{260, DensityMedium, 1},
		{531, DensityMedium, 1}, // 2*260+12 = 532 is the two-column threshold
		{532, DensityMedium, 2},
		{1100, DensityMedium, 4},
		{1100, DensitySmall, 5},
		{1100, DensityLarge, 2},
	}
	for _, tc := range cases {
		l := Plan(tc.paneW, tc.d, Aspect16x9, SlotsTitle)
		if l.Cols != tc.cols {
			t.Errorf("Plan(%v, %v): %d columns, want %d", tc.paneW, tc.d, l.Cols, tc.cols)
		}
		if tc.paneW >= tc.d.spec().minW {
			if w := l.GridWidth(); w > tc.paneW {
				t.Errorf("Plan(%v, %v): grid %v wider than the pane", tc.paneW, tc.d, w)
			}
			if l.CardW < tc.d.spec().minW {
				t.Errorf("Plan(%v, %v): card %v under the density's minimum", tc.paneW, tc.d, l.CardW)
			}
		}
	}
}

// The card height is a function of the declared slots, the density and the
// aspect — and of nothing a card carries (ADR-0245 §SD4).
func TestPlanHeightFollowsSlots(t *testing.T) {
	all := SlotsHero | SlotsOverline | SlotsTitle | SlotsSubtitle | SlotsBody | SlotsFacts | SlotsTags | SlotsFooter
	full := Plan(1000, DensityMedium, Aspect16x9, all)
	prev := float32(0)
	for _, s := range []Span{full.Hero, full.Head, full.Body, full.Facts, full.Tags, full.Footer} {
		if s.H <= 0 {
			t.Fatalf("a declared slot has no room: %+v", full)
		}
		if s.Y < prev {
			t.Fatalf("slots overlap: %+v", full)
		}
		prev = s.Y + s.H
	}
	if full.CardH < prev {
		t.Fatalf("card %v shorter than its last slot's end %v", full.CardH, prev)
	}

	for slot := SlotsHero; slot <= SlotsFooter; slot <<= 1 {
		without := Plan(1000, DensityMedium, Aspect16x9, all&^slot)
		if without.CardH >= full.CardH {
			t.Errorf("dropping slot %#x did not shorten the card (%v vs %v)", slot, without.CardH, full.CardH)
		}
	}

	heroOnly := Plan(1000, DensityMedium, Aspect1x1, SlotsHero)
	if heroOnly.CardH != heroOnly.Hero.H || heroOnly.Hero.H != heroOnly.CardW {
		t.Errorf("a hero-only 1:1 card should be its hero: %+v", heroOnly)
	}
	if w := Plan(1000, DensityMedium, Aspect16x9, SlotsHero); w.Hero.H >= heroOnly.Hero.H {
		t.Errorf("16:9 hero %v not shorter than 1:1 %v", w.Hero.H, heroOnly.Hero.H)
	}
	if Plan(1000, DensityLarge, Aspect16x9, SlotsBody).Body.H <= Plan(1000, DensitySmall, Aspect16x9, SlotsBody).Body.H {
		t.Errorf("a large card's body budget should exceed a small card's")
	}
}

func TestGridGeometry(t *testing.T) {
	l := Plan(1100, DensityMedium, Aspect16x9, SlotsTitle) // 4 columns
	if l.Rows(0) != 0 || l.Rows(1) != 1 || l.Rows(4) != 1 || l.Rows(5) != 2 {
		t.Fatalf("Rows: %d %d %d %d", l.Rows(0), l.Rows(1), l.Rows(4), l.Rows(5))
	}
	if h := l.GridHeight(5); h != 2*l.CardH+cardGap {
		t.Errorf("GridHeight(5) = %v", h)
	}
	x, y := l.CardOrigin(5)
	if x != l.CardW+cardGap || y != l.CardH+cardGap {
		t.Errorf("CardOrigin(5) = %v, %v", x, y)
	}
}

func TestNeighbour(t *testing.T) {
	l := Plan(1100, DensityMedium, Aspect16x9, SlotsTitle) // 4 columns
	const n = 10                                           // rows of 4, 4, 2
	cases := []struct{ i, dx, dy, want int }{
		{-1, 1, 0, 0}, // nothing selected: the first key selects the first card
		{0, -1, 0, 0},
		{3, 1, 0, 4}, // → at a row's end walks on in reading order
		{4, -1, 0, 3},
		{9, 1, 0, 9},
		{1, 0, 1, 5},
		{5, 0, -1, 1},
		{1, 0, -1, 1}, // top row stays put
		{7, 0, 1, 9},  // down onto a short last row lands on its last card
		{9, 0, 1, 9},
	}
	for _, tc := range cases {
		if got := l.Neighbour(tc.i, n, tc.dx, tc.dy); got != tc.want {
			t.Errorf("Neighbour(%d, dx %d, dy %d) = %d, want %d", tc.i, tc.dx, tc.dy, got, tc.want)
		}
	}
	if got := l.Neighbour(0, 0, 1, 0); got != -1 {
		t.Errorf("Neighbour over no cards = %d", got)
	}
}

func TestFit(t *testing.T) {
	box := Box{W: 320, H: 180}
	cases := []struct {
		name         string
		sw, sh, w, h float32
	}{
		{"same aspect scales down", 1920, 1080, 320, 180},
		{"panorama becomes a band", 8000, 1000, 320, 40},
		{"strip becomes a sliver", 100, 6000, 3, 180},
		{"icon is not scaled up", 16, 16, 16, 16},
		{"one pixel wide keeps a point", 1, 9000, 1, 180},
		{"degenerate takes the box", 0, 10, 320, 180},
	}
	for _, tc := range cases {
		w, h := Fit(tc.sw, tc.sh, box)
		if w != tc.w || h != tc.h {
			t.Errorf("%s: Fit(%v, %v) = %v × %v, want %v × %v", tc.name, tc.sw, tc.sh, w, h, tc.w, tc.h)
		}
		if w > box.W || h > box.H {
			t.Errorf("%s: %v × %v escapes the box", tc.name, w, h)
		}
	}
}
