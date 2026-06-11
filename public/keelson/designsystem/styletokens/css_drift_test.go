package styletokens_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// CSS↔Go scalar drift (ADR-0076 §SD3). ids.css hand-mirrors the styletokens
// scalar ladders at Standard density, the same way palette_generated.go mirrors
// the Rust palette — this test is the CSS analogue of the Go↔Rust drift checks
// above. The colour tokens are NOT checked here: they are generated
// (ids-palette.css), so they cannot drift.

// idsCssPath resolves web/ids.css from this test file's location, regardless of
// where `go test` was invoked (web is a sibling of styletokens under
// designsystem/).
func idsCssPath(t *testing.T) (path string) {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path = filepath.Join(filepath.Dir(here), "..", "web", "ids.css")
	return
}

// cssScalar returns the numeric value of the first `--ids-<name>: <n><unit>;`
// custom-property definition in css, stripping the px/rem/ms unit.
func cssScalar(t *testing.T, css, name string) (v float64) {
	t.Helper()
	_, after, found := strings.Cut(css, "--ids-"+name+":")
	if !found {
		t.Fatalf("--ids-%s: not defined in ids.css", name)
	}
	val, _, found := strings.Cut(after, ";")
	if !found {
		t.Fatalf("--ids-%s: no terminating ';'", name)
	}
	raw := strings.TrimRight(strings.TrimSpace(val), "pxrems ")
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		t.Fatalf("--ids-%s: parse %q: %v", name, raw, err)
	}
	v = f
	return
}

func TestCSSScalarDrift(t *testing.T) {
	b, err := os.ReadFile(idsCssPath(t))
	if err != nil {
		t.Fatalf("read ids.css: %v", err)
	}
	css := string(b)

	// Spacing — --ids-space-N == PxTable[N][Standard].
	for i := range styletokens.PxTable {
		got := cssScalar(t, css, "space-"+strconv.Itoa(i))
		want := float64(styletokens.PxTable[i][styletokens.DensityStandard])
		if got != want {
			t.Errorf("space-%d: ids.css %vpx != PxTable[%d][Standard] %vpx", i, got, i, want)
		}
	}

	// Rounding / stroke / motion — direct px / ms scalars.
	for _, c := range []struct {
		name string
		want float64
	}{
		{"radius-none", float64(styletokens.RoundingNone)},
		{"radius-sm", float64(styletokens.RoundingSm)},
		{"radius-md", float64(styletokens.RoundingMd)},
		{"radius-lg", float64(styletokens.RoundingLg)},
		{"border-hair", float64(styletokens.StrokeHair)},
		{"border-regular", float64(styletokens.StrokeRegular)},
		{"border-strong", float64(styletokens.StrokeStrong)},
		{"motion-quick", float64(styletokens.MotionQuickMs)},
		{"motion-standard", float64(styletokens.MotionStandardMs)},
		{"motion-slow", float64(styletokens.MotionSlowMs)},
	} {
		if got := cssScalar(t, css, c.name); got != c.want {
			t.Errorf("%s: ids.css %v != styletokens %v", c.name, got, c.want)
		}
	}

	// Typography — CSS rem against a 16px root equals the <Name>Pt logical
	// points (ids.css comments record the same mapping).
	const rootPx = 16.0
	for _, c := range []struct {
		name string
		want float64 // logical points
	}{
		{"text-display", float64(styletokens.DisplayPt)},
		{"text-heading", float64(styletokens.HeadingPt)},
		{"text-body", float64(styletokens.BodyPt)},
		{"text-caption", float64(styletokens.CaptionPt)},
		{"text-micro", float64(styletokens.MicroPt)},
	} {
		rem := cssScalar(t, css, c.name)
		if pt := rem * rootPx; pt != c.want {
			t.Errorf("%s: ids.css %grem → %gpt != styletokens %gpt", c.name, rem, pt, c.want)
		}
	}
}
