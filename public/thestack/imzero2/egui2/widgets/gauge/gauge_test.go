package gauge

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestNewDefaults(t *testing.T) {
	r := New("g")
	if r.idPrefix != "g" {
		t.Errorf("idPrefix = %q, want g", r.idPrefix)
	}
	if r.min != 0 || r.max != 100 {
		t.Errorf("range = [%v,%v], want [0,100]", r.min, r.max)
	}
	if !approx(r.startDeg, 225) || !approx(r.endDeg, -45) {
		t.Errorf("sweep = [%v,%v], want [225,-45]", r.startDeg, r.endDeg)
	}
	if r.size != SizeMd {
		t.Errorf("size = %v, want SizeMd", r.size)
	}
	if !r.showTicks || !r.showValue {
		t.Errorf("showTicks=%v showValue=%v, want both true", r.showTicks, r.showValue)
	}
	if r.formatFunc == nil {
		t.Fatal("formatFunc is nil; must be usable by default")
	}
	if got := r.formatFunc(0); got != "0" {
		t.Errorf("default formatFunc(0) = %q, want 0", got)
	}
}

func TestFluentSettersReturnCopies(t *testing.T) {
	base := New("g")
	mod := base.Range(10, 20).Size(SizeLg).Suffix("%").ShowTicks(false).NeedleFollowsZone(true)

	// Originals untouched (value-receiver contract).
	if base.min != 0 || base.max != 100 {
		t.Errorf("base range mutated: [%v,%v]", base.min, base.max)
	}
	if base.size != SizeMd || base.suffix != "" || !base.showTicks || base.needleFollowsZone {
		t.Error("base mutated by setters on a copy")
	}
	// Copy carries the changes.
	if mod.min != 10 || mod.max != 20 || mod.size != SizeLg || mod.suffix != "%" ||
		mod.showTicks || !mod.needleFollowsZone {
		t.Errorf("copy missing changes: %+v", mod)
	}
}

func TestFormatNilIsNoop(t *testing.T) {
	r := New("g").Format(nil)
	if r.formatFunc == nil {
		t.Fatal("Format(nil) cleared the formatter; should be a no-op")
	}
}

func TestValueToAngle(t *testing.T) {
	const start, end float32 = 225, -45
	cases := []struct {
		name string
		v    float64
		want float32
	}{
		{"min", 0, 225},
		{"max", 100, -45},
		{"mid", 50, 90},
		{"below clamps to start", -25, 225},
		{"above clamps to end", 250, -45},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := valueToAngle(c.v, 0, 100, start, end); !approx(got, c.want) {
				t.Errorf("valueToAngle(%v) = %v, want %v", c.v, got, c.want)
			}
		})
	}
	if got := valueToAngle(5, 10, 10, start, end); !approx(got, start) {
		t.Errorf("degenerate range = %v, want start %v", got, start)
	}
}

func TestResolveZones(t *testing.T) {
	if resolveZones(nil, ZoneAbsolute, 0, 100) != nil {
		t.Error("empty zones should resolve to nil")
	}
	abs := resolveZones([]Zone{{From: 60, To: 85}}, ZoneAbsolute, 0, 100)
	if len(abs) != 1 || abs[0].From != 60 || abs[0].To != 85 {
		t.Errorf("absolute passthrough wrong: %+v", abs)
	}
	pct := resolveZones([]Zone{{From: 0.6, To: 0.85}}, ZonePercentage, 0, 200)
	if len(pct) != 1 || !approx(float32(pct[0].From), 120) || !approx(float32(pct[0].To), 170) {
		t.Errorf("percentage expansion wrong: %+v", pct)
	}
}

func TestZoneAt(t *testing.T) {
	zones := []Zone{
		{From: 0, To: 60, Tone: styletokens.ToneSuccess},
		{From: 85, To: 100, Tone: styletokens.ToneError},  // gap 60..85
		{From: 84, To: 70, Tone: styletokens.ToneWarning}, // reversed bounds
	}
	if z, ok := zoneAt(30, zones); !ok || z.Tone != styletokens.ToneSuccess {
		t.Errorf("zoneAt(30) = %v,%v want Success", z.Tone, ok)
	}
	if z, ok := zoneAt(75, zones); !ok || z.Tone != styletokens.ToneWarning {
		t.Errorf("zoneAt(75) = %v,%v want Warning (reversed bounds)", z.Tone, ok)
	}
	if _, ok := zoneAt(62, zones); ok {
		t.Error("zoneAt(62) should miss (in the 60..70 gap)")
	}
}

func TestTickValues(t *testing.T) {
	majors, minors := tickValues(0, 100, 5, 0)
	want := []float64{0, 25, 50, 75, 100}
	if len(majors) != len(want) {
		t.Fatalf("majors len = %d, want %d", len(majors), len(want))
	}
	for i, w := range want {
		if !approx(float32(majors[i]), float32(w)) {
			t.Errorf("major[%d] = %v, want %v", i, majors[i], w)
		}
	}
	if len(minors) != 0 {
		t.Errorf("minors = %v, want none", minors)
	}
	// major < 2 falls back to the default; minor subdivides each gap.
	majors2, minors2 := tickValues(0, 100, 0, 1)
	if len(majors2) != defaultMajorTicks {
		t.Errorf("default majors = %d, want %d", len(majors2), defaultMajorTicks)
	}
	if len(minors2) != (defaultMajorTicks-1)*1 {
		t.Errorf("minors = %d, want %d", len(minors2), defaultMajorTicks-1)
	}
}

func TestDiameterFor(t *testing.T) {
	cases := []struct {
		size SizeE
		d    styletokens.DensityE
		want float32
	}{
		{SizeSm, styletokens.DensityTight, 88},
		{SizeMd, styletokens.DensityStandard, 144},
		{SizeLg, styletokens.DensityRoomy, 240},
		{SizeE(99), styletokens.DensityStandard, 144}, // out-of-range size -> Md
	}
	for _, c := range cases {
		if got := diameterFor(c.size, c.d); !approx(got, c.want) {
			t.Errorf("diameterFor(%v,%v) = %v, want %v", c.size, c.d, got, c.want)
		}
	}
}

func TestDefaultFormat(t *testing.T) {
	cases := map[float64]string{
		72:   "72",
		0:    "0",
		100:  "100",
		72.5: "72.5",
		-3:   "-3",
	}
	for v, want := range cases {
		if got := defaultFormat(v); got != want {
			t.Errorf("defaultFormat(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestResolveDiameterOverride(t *testing.T) {
	if got := New("g").Diameter(200).resolveDiameter(); !approx(got, 200) {
		t.Errorf("explicit diameter = %v, want 200", got)
	}
	// Diameter(0) clears the override -> falls back to the Size preset.
	got := New("g").Size(SizeLg).Diameter(0).resolveDiameter()
	if !approx(got, diameterFor(SizeLg, styletokens.DensityFromEnv())) {
		t.Errorf("cleared override = %v, want Size preset", got)
	}
}
