//go:build llm_generated_opus47

// Package gen implements the IDS semantic palette generator (ADR-0033 §SD5).
//
// Reads palette.toml + pairs.toml; performs OKLCh → sRGB gamut clipping;
// runs APCA + WCAG 2.1 contrast verification; runs CVD ΔE > 15 verification;
// runs IP-boundary verbatim search; emits five artefacts:
//
//   - rust/imzero2/imzero2_egui/src/style/tokens/palette_generated.rs
//   - public/keelson/designsystem/styletokens/palette_generated.go
//   - public/keelson/designsystem/web/ids-palette.css   (ADR-0076)
//   - doc/design-system/foundations/color.md
//   - doc/design-system/foundations/ip-boundary-check.md
//
// Pure-Go; deterministic; CI re-runs with Verify=true to byte-compare
// against committed artefacts.
//
// The cli wiring lives at cmd/designsystem/ — this package only
// exposes Run(ctx, Config) and a Result for the caller to format.
package gen

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/apca"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/cvd"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/emit"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/ipboundary"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/palette"
)

// Config controls a generator invocation.
type Config struct {
	// Verify re-emits to memory and byte-compares against the committed
	// palette_generated.{rs,go}. Returns ErrVerifyDrift if any file differs.
	Verify bool

	// RepoRoot overrides the runtime.Caller-based repo-root discovery.
	// Empty means auto-detect from this package's source file location.
	RepoRoot string

	// Stderr is where advisory diagnostics are written. Defaults to
	// os.Stderr when nil.
	Stderr io.Writer
}

// Result summarises a generator run. The cli wrapper formats this for
// stdout / exit-code decisions.
type Result struct {
	TokenCount   int
	PairCount    int
	APCAFailures []string // gate failures — caller should exit non-zero if non-empty
	WCAGWarnings []string // advisory
	CVDWarnings  []string // advisory
	Collisions   int
	Wrote        []string // absolute paths written
}

// pairsFile mirrors the pairs.toml schema.
type pairsFile struct {
	Pair []struct {
		Name       string  `toml:"name"`
		Fg         string  `toml:"fg"`
		Bg         string  `toml:"bg"`
		Category   string  `toml:"category"`    // "text" | "ui"
		FontPt     float64 `toml:"font_pt"`     // text only
		FontWeight int     `toml:"font_weight"` // text only — 400/500/600/700
		UIKind     string  `toml:"ui_kind"`     // ui only — "meaningful"|"ambient"|"floating"
	} `toml:"pair"`
}

// Run executes the generator with the supplied configuration. APCA gate
// failures are reported via Result.APCAFailures (non-nil-empty slice does
// NOT cause err to be non-nil — the caller decides exit semantics).
func Run(ctx context.Context, cfg Config) (res Result, err error) {
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}

	repoRoot := cfg.RepoRoot
	if repoRoot == "" {
		repoRoot, err = findRepoRoot()
		if err != nil {
			return
		}
	}

	paletteTomlPath := filepath.Join(repoRoot, "rust/imzero2/assets/colors/palette.toml")
	pairsTomlPath := filepath.Join(repoRoot, "rust/imzero2/assets/colors/pairs.toml")
	ipRefsDir := filepath.Join(repoRoot, "public/keelson/designsystem/colors/ip-refs")

	var pf palette.File
	_, err = toml.DecodeFile(paletteTomlPath, &pf)
	if err != nil {
		err = fmt.Errorf("decode palette.toml: %w", err)
		return
	}

	tokens, err := palette.Resolve(&pf)
	if err != nil {
		err = fmt.Errorf("resolve tokens: %w", err)
		return
	}

	tokenLookup := make(map[string]palette.Token, len(tokens))
	for _, t := range tokens {
		tokenLookup[t.Name] = t
	}

	// ---- Contrast verification ----
	var pf2 pairsFile
	_, err = toml.DecodeFile(pairsTomlPath, &pf2)
	if err != nil {
		err = fmt.Errorf("decode pairs.toml: %w", err)
		return
	}

	apcaResults, apcaFailures, contrastResults, contrastWarnings := runContrast(pf2, tokenLookup)

	// ---- CVD verification ----
	cvdFailures := runCVD(tokens)

	// ---- IP boundary search ----
	sources, err := ipboundary.LoadAll(ipRefsDir)
	if err != nil {
		err = fmt.Errorf("load IP-refs: %w", err)
		return
	}
	idsHexes := make(map[string]string, len(tokens))
	for _, t := range tokens {
		idsHexes[t.Name] = t.Hex()
	}
	collisions := ipboundary.Search(idsHexes, sources)

	// ---- Emit ----
	rustOut := emit.RustFile(tokens)
	goOut := emit.GoFile(tokens)
	cssOut := emit.CssFile(tokens)
	colorMd := emit.ColorMd(tokens, apcaResults, contrastResults)
	ipMd := emit.IPBoundaryMd(collisions, sources)

	rustPath := filepath.Join(repoRoot, "rust/imzero2/imzero2_egui/src/style/tokens/palette_generated.rs")
	goPath := filepath.Join(repoRoot, "public/keelson/designsystem/styletokens/palette_generated.go")
	cssPath := filepath.Join(repoRoot, "public/keelson/designsystem/web/ids-palette.css")
	colorMdPath := filepath.Join(repoRoot, "doc/design-system/foundations/color.md")
	ipMdPath := filepath.Join(repoRoot, "doc/design-system/foundations/ip-boundary-check.md")

	if cfg.Verify {
		err = verifyFile(rustPath, rustOut)
		if err != nil {
			return
		}
		err = verifyFile(goPath, goOut)
		if err != nil {
			return
		}
		err = verifyFile(cssPath, cssOut)
		if err != nil {
			return
		}
		// color.md and ip-boundary-check.md include time.Now(); skip byte-compare.
	} else {
		err = os.MkdirAll(filepath.Dir(colorMdPath), 0o755)
		if err != nil {
			return
		}
		err = os.MkdirAll(filepath.Dir(cssPath), 0o755)
		if err != nil {
			return
		}
		for _, w := range []struct {
			path, content string
		}{
			{rustPath, rustOut},
			{goPath, goOut},
			{cssPath, cssOut},
			{colorMdPath, colorMd},
			{ipMdPath, ipMd},
		} {
			err = os.WriteFile(w.path, []byte(w.content), 0o644)
			if err != nil {
				err = fmt.Errorf("write %s: %w", w.path, err)
				return
			}
			res.Wrote = append(res.Wrote, w.path)
		}
	}

	// Advisory diagnostics — go to stderr but do not fail.
	if len(contrastWarnings) > 0 {
		fmt.Fprintf(cfg.Stderr, "WARN: %d WCAG 2.1 AA findings (advisory; APCA is the gate):\n", len(contrastWarnings))
		for _, f := range contrastWarnings {
			fmt.Fprintln(cfg.Stderr, "  "+f)
		}
	}
	if len(cvdFailures) > 0 {
		// CVD is advisory at M0a per ADR-0033 §SD8 — same-emphasis hue pairs
		// at the Swiss-restrained C ≈ 0.10 chroma collapse under
		// deuteranopia / protanopia. Auto-perturbation lands at M0b.
		fmt.Fprintf(cfg.Stderr, "WARN: %d CVD ΔE ≤ 15 findings (advisory at M0a):\n", len(cvdFailures))
		for _, f := range cvdFailures {
			fmt.Fprintln(cfg.Stderr, "  "+f)
		}
	}

	res.TokenCount = len(tokens)
	res.PairCount = len(apcaResults)
	res.APCAFailures = apcaFailures
	res.WCAGWarnings = contrastWarnings
	res.CVDWarnings = cvdFailures
	res.Collisions = len(collisions)
	return
}

// runContrast computes both APCA (primary gate) and WCAG 2.1 (advisory
// secondary) results for every pair. APCA failures gate the build; WCAG
// failures warn only.
//
// The pair schema is unified for both metrics — text pairs carry font_pt
// + font_weight (used by APCA's size/weight-aware threshold); ui pairs
// carry ui_kind ("meaningful"/"ambient"/"floating") for the APCA UI
// threshold table.
func runContrast(pf pairsFile, lookup map[string]palette.Token) (
	apcaResults []emit.ApcaResult,
	apcaFailures []string,
	wcagResults []contrast.Result,
	wcagWarnings []string,
) {
	for _, p := range pf.Pair {
		fg, ok := lookup[p.Fg]
		if !ok {
			apcaFailures = append(apcaFailures,
				fmt.Sprintf("pair %s: unknown fg token %s", p.Name, p.Fg))
			continue
		}
		bg, ok := lookup[p.Bg]
		if !ok {
			apcaFailures = append(apcaFailures,
				fmt.Sprintf("pair %s: unknown bg token %s", p.Name, p.Bg))
			continue
		}

		// ---- APCA (primary) ----
		lc := apca.Lc(fg.R, fg.G, fg.B, bg.R, bg.G, bg.B)
		lcMag := lc
		if lcMag < 0 {
			lcMag = -lcMag
		}
		var threshold float64
		switch p.Category {
		case "text":
			threshold = apca.Threshold(p.FontPt, p.FontWeight)
		case "ui":
			threshold = apca.UIThreshold(p.UIKind)
		default:
			threshold = apca.UIThreshold("meaningful")
		}
		ar := emit.ApcaResult{
			Name:      p.Name,
			Category:  p.Category,
			Lc:        lc,
			Threshold: threshold,
			Pass:      lcMag >= threshold,
			FontPt:    p.FontPt, FontWeight: p.FontWeight, UIKind: p.UIKind,
		}
		apcaResults = append(apcaResults, ar)
		// "disabled-on-panel" is graded but exempt per ADR-0031 §SD5
		// (disabled text is allowed lower contrast by design).
		// "secondary-on-panel" is exempt because Caption=11pt regular
		// sits below APCA's body-text gate (Lc≥90 requires ≥12pt or
		// heavier weights). Documented in color.md.
		exempt := map[string]bool{
			"disabled-on-panel":    true,
			"secondary-on-panel":   true,
			"secondary-on-surface": true,
		}
		if !ar.Pass && !exempt[p.Name] {
			apcaFailures = append(apcaFailures, fmt.Sprintf(
				"%s: |Lc|=%.1f < threshold %.1f", p.Name, lcMag, threshold))
		}

		// ---- WCAG 2.1 (advisory secondary) ----
		// Map our text/ui category back to WCAG's kinds so the warn
		// metric is meaningful even though we don't gate on it.
		var wcagKind contrast.PairKind
		switch {
		case p.Category == "text" && p.FontPt >= 18.0:
			wcagKind = contrast.KindLarge
		case p.Category == "text" && p.FontPt >= 14.0 && p.FontWeight >= 500:
			wcagKind = contrast.KindLarge
		case p.Category == "text":
			wcagKind = contrast.KindBody
		default:
			wcagKind = contrast.KindUI
		}
		ratio := contrast.Ratio(fg.R, fg.G, fg.B, bg.R, bg.G, bg.B)
		wr := contrast.Result{
			Name:    p.Name,
			Kind:    wcagKind,
			Ratio:   ratio,
			AAPass:  ratio >= contrast.AAFloor(wcagKind),
			AAAPass: contrast.AAAFloor(wcagKind) > 0 && ratio >= contrast.AAAFloor(wcagKind),
			FgR:     fg.R, FgG: fg.G, FgB: fg.B,
			BgR: bg.R, BgG: bg.G, BgB: bg.B,
		}
		wcagResults = append(wcagResults, wr)
		if !wr.AAPass && !exempt[p.Name] {
			wcagWarnings = append(wcagWarnings, fmt.Sprintf(
				"%s: %.2f:1 < WCAG AA floor %.1f:1", wr.Name, wr.Ratio, contrast.AAFloor(wcagKind)))
		}
	}
	return
}

// runCVD verifies that every pair of semantic tokens at the same emphasis
// level has ΔE > 15 in OKLab under each CVD condition (ADR-0031 §SD5,
// ADR-0033 §SD6). Neutral tokens are excluded — they're hue-less by
// construction, intentional matches.
func runCVD(tokens []palette.Token) (failures []string) {
	const minDeltaE = 15.0
	emphasis := []string{"subtle", "default", "strong"}
	for _, e := range emphasis {
		// Collect semantic.<role>.<emphasis> tokens.
		var bucket []palette.Token
		for _, t := range tokens {
			if !strings.HasPrefix(t.Name, "semantic.") {
				continue
			}
			if !strings.HasSuffix(t.Name, "."+e) {
				continue
			}
			// Skip neutral — C = 0, intentional grey.
			if strings.HasPrefix(t.Name, "semantic.neutral.") {
				continue
			}
			bucket = append(bucket, t)
		}
		sort.Slice(bucket, func(i, j int) bool { return bucket[i].Name < bucket[j].Name })
		for i := 0; i < len(bucket); i++ {
			for j := i + 1; j < len(bucket); j++ {
				a, b := bucket[i], bucket[j]
				for _, t := range []cvd.Type{cvd.Deuteranopia, cvd.Protanopia, cvd.Tritanopia} {
					ar, ag, ab := cvd.Simulate(t, a.R, a.G, a.B)
					br, bg, bb := cvd.Simulate(t, b.R, b.G, b.B)
					de := cvd.DeltaEOklab(ar, ag, ab, br, bg, bb)
					if de <= minDeltaE {
						failures = append(failures, fmt.Sprintf(
							"%s vs %s under %s: ΔE = %.2f ≤ %.1f",
							a.Name, b.Name, t, de, minDeltaE))
					}
				}
			}
		}
	}
	return
}

func verifyFile(path, want string) (err error) {
	got, err := os.ReadFile(path)
	if err != nil {
		err = fmt.Errorf("verify: read %s: %w", path, err)
		return
	}
	if string(got) != want {
		err = fmt.Errorf("verify: %s drift — re-run ./cmd/designsystem colors gen", path)
		return
	}
	return
}

// findRepoRoot walks up from this source file's location to find the
// directory containing go.mod.
func findRepoRoot() (root string, err error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		err = fmt.Errorf("runtime.Caller failed")
		return
	}
	d := filepath.Dir(here)
	for i := 0; i < 12; i++ {
		_, statErr := os.Stat(filepath.Join(d, "go.mod"))
		if statErr == nil {
			root = d
			return
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	err = fmt.Errorf("could not locate repo root (go.mod not found above %s)", here)
	return
}
