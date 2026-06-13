// Package tour implements the Tier 2 SSIM pre-filter for the screenshot
// tour (ADR-0029 §SD9). It walks two directories of PNGs, pairs files
// by basename, and computes the pairwise SSIM via the sibling
// [ssim] package — the deterministic gate ahead of LLM grading.
//
// Per the SSIM docstring: high SSIM (≥ ImperceptibleThreshold) means
// "skip downstream review", low SSIM means "this pair carries a
// material visual change worth semantic evaluation". SSIM itself is
// never a build gate; the catastrophic-regression `--gate-below`
// threshold on the CLI is for the narrow case of "an obvious
// regression slipped past CR" and defaults to 0 (off).
//
// Design notes:
//
//   - Pairing is by basename + extension (only *.png today). Files
//     present in only one side surface as MissingCandidate /
//     MissingBaseline outcomes — distinct so the caller can treat
//     "capture incomplete" differently from "new scene to baseline".
//   - Outcomes are returned sorted ascending by SSIM so the most-
//     interesting (lowest-similarity) pairs land at the top. Missing
//     / error outcomes precede the numeric ones.
//   - No I/O beyond os.Open / png.Decode and the deterministic
//     ssim.Compute pass — safe to run in unit tests against a
//     fixture directory of small PNGs.
package tour

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"

	"github.com/stergiotis/boxer/public/keelson/designsystem/review/ssim"
)

// StatusE classifies the per-scene outcome of a tour comparison.
type StatusE uint8

const (
	// StatusOK — both PNGs decoded, SSIM computed.
	StatusOK StatusE = iota
	// StatusMissingCandidate — baseline exists but candidate is absent.
	// Usually means the tour capture is incomplete; treat as op error.
	StatusMissingCandidate
	// StatusMissingBaseline — candidate exists but baseline is absent.
	// Usually means a new scene was added; treat as warning (caller may
	// promote to baseline).
	StatusMissingBaseline
	// StatusDimMismatch — both PNGs decoded but dimensions differ.
	// Usually means a window-size or layout change; treat as op error.
	StatusDimMismatch
	// StatusDecodeError — file read or PNG decode failed.
	StatusDecodeError
)

// String renders the status as a short verdict tag for CLI output.
func (inst StatusE) String() (s string) {
	switch inst {
	case StatusOK:
		s = "ok"
	case StatusMissingCandidate:
		s = "missing-candidate"
	case StatusMissingBaseline:
		s = "missing-baseline"
	case StatusDimMismatch:
		s = "dim-mismatch"
	case StatusDecodeError:
		s = "decode-error"
	default:
		s = "unknown"
	}
	return
}

// Outcome is the per-scene result of a tour comparison.
type Outcome struct {
	Scene  string  // basename without extension (e.g. "idsshowcase-geometry")
	SSIM   float64 // 0..1; 1.0 = byte-identical. Undefined when Status != StatusOK
	DSSIM  float64 // (1 - SSIM) / 2
	Status StatusE
	Err    error // populated for StatusDecodeError / StatusDimMismatch
}

// Config tunes the per-pair SSIM and the SkipLLM / LLMGradeWarranted
// split. Threshold = 0 falls back to ssim.ImperceptibleThreshold.
type Config struct {
	Window    int
	Threshold float64
}

// Summary aggregates Outcome counts for the human / CI surface.
type Summary struct {
	Total             int
	SkipLLM           int // SSIM >= Threshold
	LLMGradeWarranted int // SSIM < Threshold, Status == StatusOK
	MissingCandidate  int
	MissingBaseline   int
	OpErrors          int // dim-mismatch, decode-error
	LowestSSIM        float64
	LowestScene       string
}

// Result holds the per-scene outcomes and the aggregate summary. The
// Outcomes slice is sorted: errors / missing first, then OK rows
// ascending by SSIM (lowest similarity / most interesting at the top).
type Result struct {
	Outcomes []Outcome
	Summary  Summary
}

// Compare walks BaselineDir / CandidateDir, pairs *.png files by
// basename, and computes SSIM for each pair. Missing pairs surface as
// MissingCandidate / MissingBaseline outcomes — distinct so callers
// can treat "capture incomplete" differently from "new scene".
//
// The function returns err != nil only on operational failures that
// invalidate the entire run (unreadable directory). Per-file errors
// surface in Outcome.Err with Status == StatusDecodeError /
// StatusDimMismatch — the run continues so the caller sees the full
// picture across the tour.
func Compare(ctx context.Context, baselineDir, candidateDir string, cfg Config) (r Result, err error) {
	if cfg.Threshold <= 0 {
		cfg.Threshold = ssim.ImperceptibleThreshold
	}

	baselines, err := listPngs(baselineDir)
	if err != nil {
		err = fmt.Errorf("scan baseline dir %s: %w", baselineDir, err)
		return
	}
	candidates, err := listPngs(candidateDir)
	if err != nil {
		err = fmt.Errorf("scan candidate dir %s: %w", candidateDir, err)
		return
	}

	scenes := unionScenes(baselines, candidates)
	r.Outcomes = make([]Outcome, 0, len(scenes))

	for _, scene := range scenes {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		baselinePath, hasBaseline := baselines[scene]
		candidatePath, hasCandidate := candidates[scene]
		switch {
		case !hasCandidate:
			r.Outcomes = append(r.Outcomes, Outcome{Scene: scene, Status: StatusMissingCandidate})
			continue
		case !hasBaseline:
			r.Outcomes = append(r.Outcomes, Outcome{Scene: scene, Status: StatusMissingBaseline})
			continue
		}
		o := compareOne(scene, baselinePath, candidatePath, cfg.Window)
		r.Outcomes = append(r.Outcomes, o)
	}

	sortOutcomes(r.Outcomes)
	r.Summary = summarise(r.Outcomes, cfg.Threshold)
	return
}

func compareOne(scene, baselinePath, candidatePath string, window int) (o Outcome) {
	o.Scene = scene
	a, decodeErr := decodePng(baselinePath)
	if decodeErr != nil {
		o.Status = StatusDecodeError
		o.Err = fmt.Errorf("baseline: %w", decodeErr)
		return
	}
	b, decodeErr := decodePng(candidatePath)
	if decodeErr != nil {
		o.Status = StatusDecodeError
		o.Err = fmt.Errorf("candidate: %w", decodeErr)
		return
	}
	s, computeErr := ssim.Compute(a, b, window)
	if computeErr != nil {
		switch {
		case errors.Is(computeErr, ssim.ErrSizeMismatch):
			o.Status = StatusDimMismatch
			o.Err = fmt.Errorf("baseline %dx%d vs candidate %dx%d",
				a.Bounds().Dx(), a.Bounds().Dy(),
				b.Bounds().Dx(), b.Bounds().Dy())
		default:
			o.Status = StatusDecodeError
			o.Err = computeErr
		}
		return
	}
	o.Status = StatusOK
	o.SSIM = s
	o.DSSIM = (1.0 - s) / 2.0
	return
}

func listPngs(dir string) (m map[string]string, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	m = make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".png" {
			continue
		}
		scene := name[:len(name)-len(".png")]
		m[scene] = filepath.Join(dir, name)
	}
	return
}

func unionScenes(a, b map[string]string) (out []string) {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out = make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return
}

func decodePng(path string) (img image.Image, err error) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	img, _, err = image.Decode(f)
	return
}

// sortOutcomes orders the slice so the most interesting rows come
// first: errors and missing pairs at the top (in their own order),
// followed by StatusOK rows ascending by SSIM (lowest similarity =
// most likely to need review).
func sortOutcomes(out []Outcome) {
	sort.SliceStable(out, func(i, j int) bool {
		// errors / missing come before StatusOK
		switch {
		case out[i].Status != StatusOK && out[j].Status == StatusOK:
			return true
		case out[i].Status == StatusOK && out[j].Status != StatusOK:
			return false
		case out[i].Status != StatusOK && out[j].Status != StatusOK:
			// Stable across error-status groups: sort by status discriminant
			// then by scene name so output is deterministic.
			if out[i].Status != out[j].Status {
				return out[i].Status < out[j].Status
			}
			return out[i].Scene < out[j].Scene
		}
		return out[i].SSIM < out[j].SSIM
	})
}

func summarise(outcomes []Outcome, threshold float64) (s Summary) {
	s.Total = len(outcomes)
	s.LowestSSIM = 1.0
	for _, o := range outcomes {
		switch o.Status {
		case StatusOK:
			if o.SSIM < s.LowestSSIM {
				s.LowestSSIM = o.SSIM
				s.LowestScene = o.Scene
			}
			if o.SSIM >= threshold {
				s.SkipLLM++
			} else {
				s.LLMGradeWarranted++
			}
		case StatusMissingCandidate:
			s.MissingCandidate++
		case StatusMissingBaseline:
			s.MissingBaseline++
		default:
			s.OpErrors++
		}
	}
	return
}
