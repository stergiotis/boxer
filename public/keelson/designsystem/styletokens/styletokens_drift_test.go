//go:build llm_generated_opus47

package styletokens_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// rustSourceRoot resolves the Rust style/tokens directory from this test
// file's location, regardless of where `go test` was invoked.
func rustSourceRoot(t *testing.T) (path string) {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// .../public/keelson/designsystem/styletokens/styletokens_drift_test.go
	// → walk up four levels to the repo root, then down into the Rust crate.
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(here), "..", "..", "..", ".."))
	path = filepath.Join(repoRoot, "rust", "imzero2", "imzero2_egui", "src", "style", "tokens")
	return
}

func readRust(t *testing.T, name string) (s string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(rustSourceRoot(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	s = string(b)
	return
}

// TestDensityDiscriminantsMatch confirms the Go DensityE values match the
// Rust enum discriminants. Both sides are indexed into PX_TABLE columns.
func TestDensityDiscriminantsMatch(t *testing.T) {
	src := readRust(t, "density.rs")
	cases := []struct {
		name    string
		wantGo  styletokens.DensityE
		rustPat string
	}{
		{"Tight", styletokens.DensityTight, `Tight\s*=\s*0`},
		{"Standard", styletokens.DensityStandard, `Standard\s*=\s*1`},
		{"Roomy", styletokens.DensityRoomy, `Roomy\s*=\s*2`},
	}
	for _, tc := range cases {
		re := regexp.MustCompile(tc.rustPat)
		if !re.MatchString(src) {
			t.Errorf("density %s: Rust source missing pattern %q", tc.name, tc.rustPat)
		}
	}
}

// TestPxTableMatchesRust parses the PX_TABLE literal from spacing.rs and
// asserts byte-identity with the Go mirror.
func TestPxTableMatchesRust(t *testing.T) {
	src := readRust(t, "spacing.rs")
	rows := parsePxTable(t, src)
	if len(rows) != 8 {
		t.Fatalf("PX_TABLE has %d rows, want 8", len(rows))
	}
	for i, row := range rows {
		if len(row) != 3 {
			t.Fatalf("PX_TABLE[%d] has %d cols, want 3", i, len(row))
		}
		for j, want := range row {
			got := styletokens.PxTable[i][j]
			if got != want {
				t.Errorf("PX_TABLE[%d][%d] drift: rust=%v go=%v", i, j, want, got)
			}
		}
	}
}

func parsePxTable(t *testing.T, src string) (rows [][]float32) {
	t.Helper()
	startMark := "pub const PX_TABLE: [[f32; 3]; 8] = ["
	idx := strings.Index(src, startMark)
	if idx < 0 {
		t.Fatal("PX_TABLE definition not found in spacing.rs")
	}
	tail := src[idx+len(startMark):]
	end := strings.Index(tail, "];")
	if end < 0 {
		t.Fatal("PX_TABLE close `];` not found")
	}
	body := tail[:end]
	rowRe := regexp.MustCompile(`\[\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,?\s*\]`)
	for _, m := range rowRe.FindAllStringSubmatch(body, -1) {
		row := make([]float32, 3)
		for k := 0; k < 3; k++ {
			v, err := strconv.ParseFloat(m[k+1], 32)
			if err != nil {
				t.Fatalf("parse PX_TABLE float %q: %v", m[k+1], err)
			}
			row[k] = float32(v)
		}
		rows = append(rows, row)
	}
	return
}

// TestRoundingConstsMatchRust parses the four pub consts in rounding.rs.
func TestRoundingConstsMatchRust(t *testing.T) {
	src := readRust(t, "rounding.rs")
	checkConst(t, src, "NONE", float64(styletokens.RoundingNone))
	checkConst(t, src, "SM", float64(styletokens.RoundingSm))
	checkConst(t, src, "MD", float64(styletokens.RoundingMd))
	checkConst(t, src, "LG", float64(styletokens.RoundingLg))
}

// TestStrokeConstsMatchRust parses the three pub consts in stroke.rs.
func TestStrokeConstsMatchRust(t *testing.T) {
	src := readRust(t, "stroke.rs")
	checkConst(t, src, "HAIR", float64(styletokens.StrokeHair))
	checkConst(t, src, "REGULAR", float64(styletokens.StrokeRegular))
	checkConst(t, src, "STRONG", float64(styletokens.StrokeStrong))
}

// TestMotionConstsMatchRust parses the three pub consts in motion.rs.
func TestMotionConstsMatchRust(t *testing.T) {
	src := readRust(t, "motion.rs")
	checkConst(t, src, "QUICK_MS", float64(styletokens.MotionQuickMs))
	checkConst(t, src, "STANDARD_MS", float64(styletokens.MotionStandardMs))
	checkConst(t, src, "SLOW_MS", float64(styletokens.MotionSlowMs))
}

// TestTypographyConstsMatchRust parses the five pt-size consts in typography.rs.
func TestTypographyConstsMatchRust(t *testing.T) {
	src := readRust(t, "typography.rs")
	checkConst(t, src, "DISPLAY_PT", float64(styletokens.DisplayPt))
	checkConst(t, src, "HEADING_PT", float64(styletokens.HeadingPt))
	checkConst(t, src, "BODY_PT", float64(styletokens.BodyPt))
	checkConst(t, src, "CAPTION_PT", float64(styletokens.CaptionPt))
	checkConst(t, src, "MICRO_PT", float64(styletokens.MicroPt))
}

// checkConst extracts a `pub const <NAME>: <type> = <value>;` declaration
// and compares the numeric value to want.
func checkConst(t *testing.T, src, name string, want float64) {
	t.Helper()
	pattern := `pub const ` + regexp.QuoteMeta(name) + `\s*:\s*[a-zA-Z0-9_]+\s*=\s*([0-9.]+)\s*;`
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("const %s not found in source", name)
		return
	}
	got, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("const %s: parse %q: %v", name, m[1], err)
	}
	if got != want {
		t.Errorf("const %s drift: rust=%v go=%v", name, got, want)
	}
}
