// Package vendor implements the IDS data-encoding palette vendor converter
// (ADR-0031 §SD3 / §SD8 Flow 2; ADR-0033 §SD4).
//
// Reads upstream artefacts from two sources:
//   - Crameri family (batlow, vik, batlowS, lapaz, oslo, lajolla, roma, broc,
//     cork) — `.txt` LUT files vendored under
//     rust/imzero2/assets/colors/scientific/upstream/, originally
//     from cmcrameri (https://github.com/callumrollo/cmcrameri), MIT.
//   - matplotlib viridis family (viridis, magma, plasma, inferno) — imported
//     from github.com/dim13/colormap (ISC), which carries the canonical
//     256-entry LUTs from BIDS/colormap (CC0).
//   - Okabe-Ito qualitative — a bare-hex `.txt` alongside the Crameri files,
//     transcribed from the Color Universal Design publication. It carries no
//     formal license; the values are published as a standard and are widely
//     redistributed. ADR-0156 §SD2 records that gap rather than papering
//     over it.
//
// Cividis is omitted from M0 — neither cmcrameri nor dim13/colormap ships
// it; it lands in a follow-on PR with a Nuñez-paper-traceable source.
//
// Emits to:
//   - rust/imzero2/imzero2_egui/src/style/data_encoding/<palette>.rs
//   - public/keelson/designsystem/styletokens/data_encoding/<palette>.out.go
//
// Each emitted file carries provenance: source name, license, upstream
// SHA-256 (Crameri) or upstream package version (viridis family).
//
// The cli wiring lives at public/app/commands/designsystem/ — this package
// only exposes Run(ctx, Config) and a Result for the caller to format.
package vendor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/dim13/colormap"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/cvd"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config controls a vendor invocation.
type Config struct {
	// RepoRoot overrides the runtime.Caller-based repo-root discovery.
	RepoRoot string
}

// Result summarises a vendor run.
type Result struct {
	Total int
	Names []string // ordered names that were emitted
}

type lut struct {
	Name           string     // file-system + identifier name
	Family         string     // "crameri" | "viridis-mpl"
	License        string     // "MIT" | "CC0"
	Provenance     string     // long-form source description
	UpstreamSHA256 string     // for crameri files; empty for matplotlib
	UpstreamRef    string     // package@version for matplotlib
	Cardinality    int        // 256 for sequential/diverging; 7 for okabe_ito
	RGB            [][3]uint8 // gamma-encoded sRGB

	// Qualitative marks a palette used as a categorical *cycle*, whose
	// entries are drawn as foreground — strokes, markers, glyphs — on the
	// IDS spine. Those are gated (see gateQualitative); sequential and
	// diverging ramps are not, because they are sampled by t and are meant
	// to span lightness. batlowS is deliberately not marked: it remains
	// vendored for fill use, which is the polarity it is legible in.
	Qualitative bool
}

// Run executes the vendor pipeline.
func Run(ctx context.Context, cfg Config) (res Result, err error) {
	repoRoot := cfg.RepoRoot
	if repoRoot == "" {
		repoRoot, err = findRepoRoot()
		if err != nil {
			return
		}
	}

	upstreamDir := filepath.Join(repoRoot,
		"rust/imzero2/assets/colors/scientific/upstream")
	rustDir := filepath.Join(repoRoot,
		"rust/imzero2/imzero2_egui/src/style/data_encoding")
	goDir := filepath.Join(repoRoot,
		"public/keelson/designsystem/styletokens/data_encoding")

	err = os.MkdirAll(rustDir, 0o755)
	if err != nil {
		return
	}
	err = os.MkdirAll(goDir, 0o755)
	if err != nil {
		return
	}

	luts, err := assemble(upstreamDir)
	if err != nil {
		return
	}

	// Deterministic order: family then name.
	sort.Slice(luts, func(i, j int) bool {
		if luts[i].Family != luts[j].Family {
			return luts[i].Family < luts[j].Family
		}
		return luts[i].Name < luts[j].Name
	})

	// Gate before emitting: a qualitative palette that fails on the IDS
	// spine must not reach the tree at all. Findings are collected across
	// every palette so one run reports the whole picture.
	var gateFindings []string
	for _, l := range luts {
		if !l.Qualitative {
			continue
		}
		gateFindings = append(gateFindings, gateQualitative(l)...)
	}
	if len(gateFindings) > 0 {
		err = eb.Build().Strs("findings", gateFindings).
			Errorf("qualitative palette gate reported findings (ADR-0156 §SD3)")
		return
	}

	for _, l := range luts {
		rustOut := emitRust(l)
		goOut := emitGo(l)
		err = os.WriteFile(filepath.Join(rustDir, l.Name+".rs"), []byte(rustOut), 0o644)
		if err != nil {
			return
		}
		err = os.WriteFile(filepath.Join(goDir, l.Name+".out.go"), []byte(goOut), 0o644)
		if err != nil {
			return
		}
		res.Names = append(res.Names, l.Name)
	}

	// Emit mod indexes (Rust mod.rs additions; Go is package-flat).
	rustMod := emitRustMod(luts)
	goMod := emitGoMod(luts)
	err = os.WriteFile(filepath.Join(rustDir, "mod.rs"), []byte(rustMod), 0o644)
	if err != nil {
		return
	}
	err = os.WriteFile(filepath.Join(goDir, "doc.out.go"), []byte(goMod), 0o644)
	if err != nil {
		return
	}
	res.Total = len(luts)
	return
}

// assemble walks upstream Crameri .txt files and pairs them with the
// in-process viridis-family arrays from dim13/colormap.
func assemble(upstreamDir string) (out []lut, err error) {
	// ---- Crameri sequential / diverging (256 entries) ----
	crameri := []string{"batlow", "vik", "lapaz", "oslo", "lajolla", "roma", "broc", "cork"}
	for _, n := range crameri {
		var l lut
		l, err = readCrameriTxt(filepath.Join(upstreamDir, n+".txt"), n, 256)
		if err != nil {
			return
		}
		l.Family = "crameri"
		l.License = "MIT"
		l.Provenance = "Fabio Crameri, Scientific colour maps " +
			"(Zenodo DOI 10.5281/zenodo.1243862); mirrored via cmcrameri " +
			"(github.com/callumrollo/cmcrameri)."
		out = append(out, l)
	}

	// Crameri CVD/monochrome variants — upstream uses mixed-case names;
	// remap to snake_case internal names for file/identifier consistency
	// (matches batlowS → batlow_s convention below).
	for _, alias := range []struct {
		upstream string
		internal string
	}{
		{"batlowK", "batlow_k"}, // darker batlow; tritanopia-tuned sequential
		{"grayC", "gray_c"},     // pure grayscale sequential (white→black)
	} {
		var l lut
		l, err = readCrameriTxt(filepath.Join(upstreamDir, alias.upstream+".txt"), alias.internal, 256)
		if err != nil {
			return
		}
		l.Family = "crameri"
		l.License = "MIT"
		l.Provenance = "Fabio Crameri, Scientific colour maps " +
			"(Zenodo DOI 10.5281/zenodo.1243862); mirrored via cmcrameri " +
			"(github.com/callumrollo/cmcrameri)."
		out = append(out, l)
	}

	// ---- Crameri batlowS qualitative — 100 lines upstream; keep first 10 ----
	// batlowS is prefix-ordered by categorical distinctness: the first N rows
	// are the intended N-color qualitative palette, later rows progressively
	// subdivide batlow and nearly duplicate earlier ones. Subsetting must
	// therefore truncate, never resample (even sampling yields near-duplicate
	// pairs, e.g. rows 0/88 and 11/99 are ~4 RGB units apart).
	var bS lut
	bS, err = readCrameriTxt(filepath.Join(upstreamDir, "batlowS.txt"), "batlow_s", 100)
	if err != nil {
		return
	}
	bS.Cardinality = 10
	bS.RGB = bS.RGB[:10]
	bS.Family = "crameri"
	bS.License = "MIT"
	bS.Provenance = "Fabio Crameri, batlowS categorical sampling " +
		"(Zenodo DOI 10.5281/zenodo.1243862); first-10 subset per ADR-0033 §SD4."
	out = append(out, bS)

	// ---- Okabe-Ito qualitative — 8 entries upstream; drop the black anchor ----
	// The published set opens with black, which the source assumes will sit on
	// a white figure background. On the IDS dark spine it reaches 1.28:1
	// against NeutralBgSurface, so the converter subsets it away and keeps the
	// remaining seven verbatim (ADR-0156 §SD2). Subsetting, not perturbation:
	// the values that ship are unmodified, as with batlowS's first-10 rule.
	var oi lut
	oi, err = readHexPalette(filepath.Join(upstreamDir, "okabe_ito.txt"), "okabe_ito", 8)
	if err != nil {
		return
	}
	oi.Cardinality = 7
	oi.RGB = oi.RGB[1:]
	oi.Qualitative = true
	oi.Family = "okabe-ito"
	oi.License = "public (no formal license; values published as a standard)"
	oi.Provenance = "Okabe & Ito, Color Universal Design (2002, rev. 2008), " +
		"https://jfly.uni-koeln.de/color/; black anchor dropped per ADR-0156 §SD2."
	out = append(out, oi)

	// ---- matplotlib viridis family (256 entries) via dim13/colormap ----
	for _, vf := range []struct {
		name string
		src  color.Palette
	}{
		{"viridis", colormap.Viridis},
		{"magma", colormap.Magma},
		{"plasma", colormap.Plasma},
		{"inferno", colormap.Inferno},
	} {
		l := lut{
			Name:    vf.name,
			Family:  "viridis-mpl",
			License: "CC0",
			Provenance: "van der Walt & Smith, Default colors for matplotlib " +
				"(https://bids.github.io/colormap/); BIDS/colormap CC0. " +
				"Mirrored via github.com/dim13/colormap (ISC).",
			UpstreamRef: "github.com/dim13/colormap@v1.1.0",
			Cardinality: len(vf.src),
		}
		l.RGB = make([][3]uint8, len(vf.src))
		for i, c := range vf.src {
			r, g, b, _ := c.RGBA()
			// color.Color RGBA returns 16-bit channels; downshift.
			l.RGB[i] = [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
		}
		out = append(out, l)
	}
	return
}

// readCrameriTxt reads a `r g b\n` LUT (floats in [0, 1]). expected is
// the line count we expect; mismatch is an error.
func readCrameriTxt(path, name string, expected int) (l lut, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("read: %w", err)
		return
	}
	sum := sha256.Sum256(b)
	l.Name = name
	l.UpstreamSHA256 = hex.EncodeToString(sum[:])

	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var rgbs [][3]uint8
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			err = eb.Build().Str("path", path).Int("line", lineNo).Str("text", line).Errorf("expected at least 3 floats")
			return
		}
		var r, g, bl float64
		r, err = strconv.ParseFloat(fields[0], 64)
		if err != nil {
			err = eb.Build().Str("path", path).Int("line", lineNo).Errorf("parse r: %w", err)
			return
		}
		g, err = strconv.ParseFloat(fields[1], 64)
		if err != nil {
			err = eb.Build().Str("path", path).Int("line", lineNo).Errorf("parse g: %w", err)
			return
		}
		bl, err = strconv.ParseFloat(fields[2], 64)
		if err != nil {
			err = eb.Build().Str("path", path).Int("line", lineNo).Errorf("parse b: %w", err)
			return
		}
		rgbs = append(rgbs, [3]uint8{toU8(r), toU8(g), toU8(bl)})
	}
	err = scanner.Err()
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("scan failed: %w", err)
		return
	}
	if len(rgbs) != expected {
		err = eb.Build().Str("path", path).Int("got", len(rgbs)).Int("want", expected).Errorf("palette has the wrong number of entries")
		return
	}
	l.Cardinality = expected
	l.RGB = rgbs
	return
}

// Gate floors for qualitative palettes (ADR-0156 §SD3). All three are
// enforcing — a palette that misses any of them is not emitted.
const (
	// gateMinRatio is WCAG 1.4.11's floor for graphical objects. Measured
	// against NeutralBgSurface, the lightest of the three IDS dark
	// surfaces and therefore the binding one.
	gateMinRatio = 3.0
	// gateMinDeltaE is the normal-vision OKLab separation floor, the same
	// number ADR-0031 §SD5 uses for the semantic palette.
	gateMinDeltaE = 15.0
	// gateMinDeltaECVD is the separation floor under simulated dichromacy.
	// It is empirical rather than perceptual: dichromacy collapses a colour
	// axis, so no categorical palette of this cardinality reaches 15 once
	// simulated. Set below what the shipped palette achieves (min 7.5) and
	// above every candidate ADR-0156 §SD4 measured and rejected.
	gateMinDeltaECVD = 6.0
)

// gateQualitative measures a categorical palette against the IDS spine and
// returns one finding per violation. This is the check whose absence let a
// palette with an entry at the background's own luminance ship: ADR-0031
// §SD5 exempted data-encoding palettes from in-house verification on the
// grounds that publication pre-validates them, but publications validate
// CVD safety, not contrast against one particular dark UI surface.
func gateQualitative(l lut) (findings []string) {
	bg := styletokens.NeutralBgSurface
	for i, c := range l.RGB {
		r := contrast.Ratio(c[0], c[1], c[2], bg.R, bg.G, bg.B)
		if r < gateMinRatio {
			findings = append(findings, fmt.Sprintf(
				"%s slot %d (#%02x%02x%02x): %.2f:1 on NeutralBgSurface < %.1f:1",
				l.Name, i, c[0], c[1], c[2], r, gateMinRatio))
		}
	}
	for i := 0; i < len(l.RGB); i++ {
		for j := i + 1; j < len(l.RGB); j++ {
			a, b := l.RGB[i], l.RGB[j]
			de := cvd.DeltaEOklab(a[0], a[1], a[2], b[0], b[1], b[2])
			if de <= gateMinDeltaE {
				findings = append(findings, fmt.Sprintf(
					"%s slots %d/%d: ΔE %.2f ≤ %.1f (normal vision)",
					l.Name, i, j, de, gateMinDeltaE))
			}
			for _, t := range []cvd.Type{cvd.Deuteranopia, cvd.Protanopia, cvd.Tritanopia} {
				ar, ag, ab := cvd.Simulate(t, a[0], a[1], a[2])
				br, bg2, bb := cvd.Simulate(t, b[0], b[1], b[2])
				deC := cvd.DeltaEOklab(ar, ag, ab, br, bg2, bb)
				if deC <= gateMinDeltaECVD {
					findings = append(findings, fmt.Sprintf(
						"%s slots %d/%d: ΔE %.2f ≤ %.1f under %s",
						l.Name, i, j, deC, gateMinDeltaECVD, t))
				}
			}
		}
	}
	return
}

// readHexPalette reads a bare-hex LUT (`RRGGBB` per line, '#'-prefixed
// comments, optional trailing name). Used for palettes the upstream
// publishes as 8-bit integers rather than floats, so no float round-trip
// sits between the publication and the emitted table. expected is the line
// count we expect; mismatch is an error.
func readHexPalette(path, name string, expected int) (l lut, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("read: %w", err)
		return
	}
	sum := sha256.Sum256(b)
	l.Name = name
	l.UpstreamSHA256 = hex.EncodeToString(sum[:])

	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	var rgbs [][3]uint8
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		field := strings.Fields(line)[0]
		if len(field) != 6 {
			err = eb.Build().Str("path", path).Int("line", lineNo).Str("text", field).Errorf("expected 6 hex digits")
			return
		}
		var v uint64
		v, err = strconv.ParseUint(field, 16, 32)
		if err != nil {
			err = eb.Build().Str("path", path).Int("line", lineNo).Errorf("parse hex: %w", err)
			return
		}
		rgbs = append(rgbs, [3]uint8{uint8(v >> 16), uint8(v >> 8), uint8(v)})
	}
	err = scanner.Err()
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("scan failed: %w", err)
		return
	}
	if len(rgbs) != expected {
		err = eb.Build().Str("path", path).Int("got", len(rgbs)).Int("want", expected).Errorf("palette has the wrong number of entries")
		return
	}
	l.Cardinality = expected
	l.RGB = rgbs
	return
}

func toU8(v float64) (u uint8) {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	u = uint8(v*255.0 + 0.5)
	return
}

// rustIdent normalises "batlow_s" → "BATLOW_S".
func rustIdent(name string) (s string) {
	s = strings.ToUpper(name)
	return
}

// goIdent normalises "batlow_s" → "BatlowS".
func goIdent(name string) (s string) {
	for p := range strings.SplitSeq(name, "_") {
		if p == "" {
			continue
		}
		s += strings.ToUpper(p[:1]) + p[1:]
	}
	return
}

func emitRust(l lut) (s string) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "// Code generated by ./boxer.sh designsystem colors vendor — DO NOT EDIT.\n")
	fmt.Fprintf(&sb, "// Palette: %s (%s, %s)\n", l.Name, l.Family, l.License)
	fmt.Fprintf(&sb, "// Source: %s\n", l.Provenance)
	if l.UpstreamSHA256 != "" {
		fmt.Fprintf(&sb, "// Upstream SHA-256: %s\n", l.UpstreamSHA256)
	}
	if l.UpstreamRef != "" {
		fmt.Fprintf(&sb, "// Upstream ref: %s\n", l.UpstreamRef)
	}
	fmt.Fprintf(&sb, "// Cardinality: %d entries\n\n", l.Cardinality)
	fmt.Fprintf(&sb, "pub const %s: [(u8, u8, u8); %d] = [\n", rustIdent(l.Name), l.Cardinality)
	for _, c := range l.RGB {
		// Unpadded on purpose: this is the rustfmt-stable form, so a
		// post-generation `cargo fmt` leaves the artefacts byte-identical.
		fmt.Fprintf(&sb, "    (%d, %d, %d),\n", c[0], c[1], c[2])
	}
	sb.WriteString("];\n")
	s = sb.String()
	return
}

func emitGo(l lut) (s string) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "// Code generated by ./boxer.sh designsystem colors vendor — DO NOT EDIT.\n")
	fmt.Fprintf(&sb, "// Palette: %s (%s, %s)\n", l.Name, l.Family, l.License)
	fmt.Fprintf(&sb, "// Source: %s\n", l.Provenance)
	if l.UpstreamSHA256 != "" {
		fmt.Fprintf(&sb, "// Upstream SHA-256: %s\n", l.UpstreamSHA256)
	}
	if l.UpstreamRef != "" {
		fmt.Fprintf(&sb, "// Upstream ref: %s\n", l.UpstreamRef)
	}
	fmt.Fprintf(&sb, "// Cardinality: %d entries\n\n", l.Cardinality)
	fmt.Fprintf(&sb, "package data_encoding\n\n")
	fmt.Fprintf(&sb, "var %s = [%d][3]uint8{\n", goIdent(l.Name), l.Cardinality)
	for _, c := range l.RGB {
		// Unpadded on purpose, matching emitRust: gofmt strips this column
		// padding on sight (confirmed — it is not a no-op), so padding it
		// only drifts the artefact from itself the moment anything reformats
		// it, same as an un-rustfmt-stable tuple would on the Rust side.
		fmt.Fprintf(&sb, "\t{%d, %d, %d},\n", c[0], c[1], c[2])
	}
	sb.WriteString("}\n")
	s = sb.String()
	return
}

func emitRustMod(luts []lut) (s string) {
	// Name-sorted (not family-then-name like the file emission order):
	// rustfmt's default reorder_modules / reorder_imports sorts these
	// declarations alphabetically, so any other order drifts on `cargo fmt`.
	names := make([]string, 0, len(luts))
	for _, l := range luts {
		names = append(names, l.Name)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("//! Vendored scientific colormaps (ADR-0031 §SD3, ADR-0033 §SD4).\n")
	sb.WriteString("//!\n")
	sb.WriteString("//! Code generated by ./boxer.sh designsystem colors vendor — DO NOT EDIT.\n\n")
	for _, n := range names {
		fmt.Fprintf(&sb, "pub mod %s;\n", n)
	}
	sb.WriteString("\n")
	for _, n := range names {
		fmt.Fprintf(&sb, "pub use %s::%s;\n", n, rustIdent(n))
	}
	s = sb.String()
	return
}

func emitGoMod(luts []lut) (s string) {
	var sb strings.Builder
	sb.WriteString("// Code generated by ./boxer.sh designsystem colors vendor — DO NOT EDIT.\n")
	sb.WriteString("// Package data_encoding holds vendored scientific colormaps\n")
	sb.WriteString("// (ADR-0031 §SD3, ADR-0033 §SD4). Each <palette>.go carries\n")
	sb.WriteString("// a 256-entry [3]uint8 LUT (batlow_s is 10-entry).\n\n")
	sb.WriteString("// Bundle:\n")
	for _, l := range luts {
		fmt.Fprintf(&sb, "//   - %s (%s, %s, %d entries)\n", l.Name, l.Family, l.License, l.Cardinality)
	}
	sb.WriteString("\npackage data_encoding\n")
	s = sb.String()
	return
}

func findRepoRoot() (root string, err error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		err = eh.Errorf("runtime.Caller failed")
		return
	}
	d := filepath.Dir(here)
	for range 12 {
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
	err = eb.Build().Str("from", here).Errorf("could not locate the repo root")
	return
}
