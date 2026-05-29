//go:build llm_generated_opus47

package tour_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/review/tour"
)

// writePng writes a small RGBA PNG to dir/<name>.png. Useful for
// fixture construction inside table tests.
func writePng(t *testing.T, dir, name string, w, h int, fill color.RGBA) (path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	path = filepath.Join(dir, name+".png")
	err = os.WriteFile(path, buf.Bytes(), 0o644)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return
}

func TestCompareIdentical(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "baseline")
	b := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", a, err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", b, err)
	}
	// Two identical scenes — SSIM should be 1.0.
	for _, name := range []string{"scene1", "scene2"} {
		writePng(t, a, name, 64, 64, color.RGBA{R: 128, G: 200, B: 64, A: 255})
		writePng(t, b, name, 64, 64, color.RGBA{R: 128, G: 200, B: 64, A: 255})
	}

	res, err := tour.Compare(context.Background(), a, b, tour.Config{})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if res.Summary.Total != 2 {
		t.Fatalf("expected 2 scenes, got %d", res.Summary.Total)
	}
	if res.Summary.SkipLLM != 2 {
		t.Errorf("expected SkipLLM=2, got %d", res.Summary.SkipLLM)
	}
	if res.Summary.LLMGradeWarranted != 0 {
		t.Errorf("expected LLMGradeWarranted=0, got %d", res.Summary.LLMGradeWarranted)
	}
	for _, o := range res.Outcomes {
		if o.Status != tour.StatusOK {
			t.Errorf("scene %s: status=%v want OK", o.Scene, o.Status)
			continue
		}
		if o.SSIM != 1.0 {
			t.Errorf("scene %s: ssim=%v want 1.0", o.Scene, o.SSIM)
		}
	}
}

func TestCompareDivergent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "baseline")
	b := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", a, err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", b, err)
	}
	// Baseline is solid green; candidate is solid red — drastically
	// different. SSIM should be well below 0.99.
	writePng(t, a, "drift", 64, 64, color.RGBA{G: 200, A: 255})
	writePng(t, b, "drift", 64, 64, color.RGBA{R: 200, A: 255})

	res, err := tour.Compare(context.Background(), a, b, tour.Config{})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if res.Summary.LLMGradeWarranted != 1 {
		t.Errorf("expected LLMGradeWarranted=1, got %d", res.Summary.LLMGradeWarranted)
	}
	if res.Summary.SkipLLM != 0 {
		t.Errorf("expected SkipLLM=0, got %d", res.Summary.SkipLLM)
	}
}

func TestCompareMissingPairs(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "baseline")
	b := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", a, err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", b, err)
	}
	// "shared" appears in both, "only-baseline" only in A, "only-candidate" only in B.
	writePng(t, a, "shared", 32, 32, color.RGBA{B: 100, A: 255})
	writePng(t, b, "shared", 32, 32, color.RGBA{B: 100, A: 255})
	writePng(t, a, "only-baseline", 32, 32, color.RGBA{R: 50, A: 255})
	writePng(t, b, "only-candidate", 32, 32, color.RGBA{G: 50, A: 255})

	res, err := tour.Compare(context.Background(), a, b, tour.Config{})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if res.Summary.Total != 3 {
		t.Fatalf("expected 3 outcomes, got %d", res.Summary.Total)
	}
	if res.Summary.MissingCandidate != 1 {
		t.Errorf("expected MissingCandidate=1, got %d", res.Summary.MissingCandidate)
	}
	if res.Summary.MissingBaseline != 1 {
		t.Errorf("expected MissingBaseline=1, got %d", res.Summary.MissingBaseline)
	}
	if res.Summary.SkipLLM != 1 {
		t.Errorf("expected SkipLLM=1, got %d", res.Summary.SkipLLM)
	}
}

func TestCompareDimMismatch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "baseline")
	b := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", a, err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", b, err)
	}
	writePng(t, a, "resized", 64, 64, color.RGBA{R: 128, A: 255})
	writePng(t, b, "resized", 32, 32, color.RGBA{R: 128, A: 255})

	res, err := tour.Compare(context.Background(), a, b, tour.Config{})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if res.Summary.OpErrors != 1 {
		t.Errorf("expected OpErrors=1, got %d (outcomes: %+v)", res.Summary.OpErrors, res.Outcomes)
	}
	if len(res.Outcomes) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(res.Outcomes))
	}
	if res.Outcomes[0].Status != tour.StatusDimMismatch {
		t.Errorf("expected StatusDimMismatch, got %v", res.Outcomes[0].Status)
	}
}

func TestCompareOutcomeOrdering(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "baseline")
	b := filepath.Join(dir, "candidate")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", a, err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", b, err)
	}
	// Scene "drift" yields a lower SSIM than "identical", so ordering
	// should put "drift" before "identical".
	writePng(t, a, "identical", 64, 64, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	writePng(t, b, "identical", 64, 64, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	writePng(t, a, "drift", 64, 64, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	writePng(t, b, "drift", 64, 64, color.RGBA{R: 50, G: 200, B: 200, A: 255})
	// Plus a missing pair — should come first (errors/missing before OK).
	writePng(t, a, "missing", 64, 64, color.RGBA{B: 100, A: 255})

	res, err := tour.Compare(context.Background(), a, b, tour.Config{})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(res.Outcomes) != 3 {
		t.Fatalf("expected 3 outcomes, got %d", len(res.Outcomes))
	}
	if res.Outcomes[0].Scene != "missing" || res.Outcomes[0].Status != tour.StatusMissingCandidate {
		t.Errorf("expected first outcome 'missing'/StatusMissingCandidate, got %s/%v",
			res.Outcomes[0].Scene, res.Outcomes[0].Status)
	}
	if res.Outcomes[1].Scene != "drift" {
		t.Errorf("expected second outcome 'drift', got %s", res.Outcomes[1].Scene)
	}
	if res.Outcomes[2].Scene != "identical" {
		t.Errorf("expected third outcome 'identical', got %s", res.Outcomes[2].Scene)
	}
}
