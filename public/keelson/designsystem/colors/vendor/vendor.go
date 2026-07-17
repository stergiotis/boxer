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
//
// Cividis is omitted from M0 — neither cmcrameri nor dim13/colormap ships
// it; it lands in a follow-on PR with a Nuñez-paper-traceable source.
//
// Emits to:
//   - rust/imzero2/imzero2_egui/src/style/data_encoding/<palette>.rs
//   - public/keelson/designsystem/styletokens/data_encoding/<palette>.go
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
	Cardinality    int        // 256 for sequential/diverging; 10 for batlowS
	RGB            [][3]uint8 // gamma-encoded sRGB
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

	for _, l := range luts {
		rustOut := emitRust(l)
		goOut := emitGo(l)
		err = os.WriteFile(filepath.Join(rustDir, l.Name+".rs"), []byte(rustOut), 0o644)
		if err != nil {
			return
		}
		err = os.WriteFile(filepath.Join(goDir, l.Name+".go"), []byte(goOut), 0o644)
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
	err = os.WriteFile(filepath.Join(goDir, "doc.go"), []byte(goMod), 0o644)
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
		err = fmt.Errorf("read %s: %w", path, err)
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
			err = fmt.Errorf("%s line %d: expected ≥3 floats, got %q", path, lineNo, line)
			return
		}
		var r, g, bl float64
		r, err = strconv.ParseFloat(fields[0], 64)
		if err != nil {
			err = fmt.Errorf("%s line %d: parse r: %w", path, lineNo, err)
			return
		}
		g, err = strconv.ParseFloat(fields[1], 64)
		if err != nil {
			err = fmt.Errorf("%s line %d: parse g: %w", path, lineNo, err)
			return
		}
		bl, err = strconv.ParseFloat(fields[2], 64)
		if err != nil {
			err = fmt.Errorf("%s line %d: parse b: %w", path, lineNo, err)
			return
		}
		rgbs = append(rgbs, [3]uint8{toU8(r), toU8(g), toU8(bl)})
	}
	err = scanner.Err()
	if err != nil {
		err = fmt.Errorf("%s scan: %w", path, err)
		return
	}
	if len(rgbs) != expected {
		err = fmt.Errorf("%s: got %d entries, want %d", path, len(rgbs), expected)
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
		fmt.Fprintf(&sb, "\t{%3d, %3d, %3d},\n", c[0], c[1], c[2])
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
		err = fmt.Errorf("runtime.Caller failed")
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
	err = fmt.Errorf("could not locate repo root above %s", here)
	return
}
