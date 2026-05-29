//go:build llm_generated_opus47

// Package scctree converts `go tool scc --by-file --format json` output
// into a *layout.Node hierarchy keyed on directory path, suitable for
// visualization with the treemap widget.
package scctree

import (
	"bytes"
	"encoding/json"
	"math"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// ComplexityPalette is a 9-stop diverging gradient (ColorBrewer RdYlGn,
// reversed) from green (low) through yellow to red (high), suitable for
// passing to treemap.WithCellColor when encoding complexity-like metrics.
var ComplexityPalette = []uint32{
	0x1a9850ee, 0x66bd63ee, 0xa6d96aee, 0xd9ef8bee,
	0xffffbfee, 0xfee08bee, 0xfdae61ee, 0xf46d43ee,
	0xd73027ee,
}

// SccFile mirrors the per-file entries emitted by `scc --by-file --format json`.
type SccFile struct {
	Language           string   `json:"Language"`
	PossibleLanguages  []string `json:"PossibleLanguages"`
	Filename           string   `json:"Filename"`
	Extension          string   `json:"Extension"`
	Location           string   `json:"Location"`
	Symlocation        string   `json:"Symlocation"`
	Bytes              int64    `json:"Bytes"`
	Lines              int64    `json:"Lines"`
	Code               int64    `json:"Code"`
	Comment            int64    `json:"Comment"`
	Blank              int64    `json:"Blank"`
	Complexity         int64    `json:"Complexity"`
	WeightedComplexity float64  `json:"WeightedComplexity"`
	Binary             bool     `json:"Binary"`
	Minified           bool     `json:"Minified"`
	Generated          bool     `json:"Generated"`
}

// SccGroup mirrors the top-level per-language groups emitted by scc.
type SccGroup struct {
	Name  string    `json:"Name"`
	Files []SccFile `json:"Files"`
}

// Weight extracts a numeric weight from an SccFile.
type Weight func(f *SccFile) float64

// WeightComplexity weights files by their scc Complexity score.
var WeightComplexity Weight = func(f *SccFile) float64 { return float64(f.Complexity) }

// WeightCode weights files by their scc Code (non-comment, non-blank) line count.
var WeightCode Weight = func(f *SccFile) float64 { return float64(f.Code) }

// WeightLines weights files by their total line count.
var WeightLines Weight = func(f *SccFile) float64 { return float64(f.Lines) }

// WeightBytes weights files by their byte size.
var WeightBytes Weight = func(f *SccFile) float64 { return float64(f.Bytes) }

// IsGenerated reports whether f looks like generated code. The check is the
// union of two heuristics:
//
//   - scc's own --gen heuristic: SccFile.Generated == true. scc inspects
//     leading bytes for canonical "do not edit" markers and similar.
//   - The project's filename / path conventions: `.gen.go` / `.out.go`
//     suffixes (boxer/pebble2impl convention for generated Go sources),
//     and any path under a `golay24` subtree (the vendored generator
//     output the not-match regex used to mask).
//
// The Filename and Location fields are matched lowercased so case
// differences on case-insensitive filesystems don't slip through.
func IsGenerated(f *SccFile) (yes bool) {
	if f == nil {
		return
	}
	if f.Generated {
		yes = true
		return
	}
	name := strings.ToLower(f.Filename)
	if strings.HasSuffix(name, ".gen.go") || strings.HasSuffix(name, ".out.go") {
		yes = true
		return
	}
	loc := strings.ToLower(filepath.ToSlash(f.Location))
	if loc == "golay24" || strings.HasPrefix(loc, "golay24/") || strings.Contains(loc, "/golay24/") {
		yes = true
		return
	}
	return
}

// RepoRoot returns the git top-level directory for the current working directory.
func RepoRoot() (root string, err error) {
	var out []byte
	out, err = exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		err = eh.Errorf("git rev-parse --show-toplevel: %w", err)
		return
	}
	root = strings.TrimSpace(string(out))
	return
}

// RunScc shells out to `go tool scc` in root with the project's agreed flags
// and returns the parsed per-language groups. Passing an empty root runs in
// the current working directory.
//
// All files scc finds are returned — including those that look generated.
// Use IsGenerated and a caller-side filter (e.g., the keep parameter of
// BuildColormappedTree) to drop generated files when needed; the previous
// `--not-match` regex baked into this function made the toggle uniformly
// impossible at the consumer.
func RunScc(root string) (groups []SccGroup, err error) {
	cmd := exec.Command("go", "tool", "scc",
		"--sort", "code",
		"--by-file",
		"--format", "json",
		"--gen",
		"--no-cocomo",
		".",
	)
	if root != "" {
		cmd.Dir = root
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		err = eh.Errorf("go tool scc: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
		return
	}
	err = json.Unmarshal(stdout.Bytes(), &groups)
	if err != nil {
		err = eh.Errorf("decode scc json: %w", err)
		return
	}
	return
}

// BuildTree builds a *layout.Node whose leaves are the files in groups,
// nested under their directory path parsed from SccFile.Location. Files
// whose weight is zero are skipped so they don't inflate the layout.
func BuildTree(groups []SccGroup, rootName string, weight Weight) *layout.Node {
	root := &layout.Node{Name: rootName}
	dirs := map[string]*layout.Node{"": root}
	for _, g := range groups {
		for i := range g.Files {
			f := &g.Files[i]
			w := weight(f)
			if w <= 0 {
				continue
			}
			loc := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(f.Location)), "./")
			if loc == "" || loc == "." {
				continue
			}
			parts := strings.Split(loc, "/")
			parent := ensureDir(dirs, parts[:len(parts)-1])
			parent.Children = append(parent.Children, &layout.Node{
				Name: parts[len(parts)-1],
				Size: w,
			})
		}
	}
	return root
}

// BuildColoredTree builds a tree weighted by sizeWeight and returns a bucket
// function mapping every node — leaves and directories — to [0, buckets).
// Directory weights are the recursive sum of their descendants' colorWeight,
// so a drilled-out view conveys aggregate subtree complexity; drilling in
// then shows the per-file distribution. Values are log-normalized across
// the whole tree so heavy-tailed distributions (a few very high-complexity
// files) don't flatten the rest of the palette.
//
// Pair with treemap.WithCellColor and a palette of length == buckets to use
// size as area and colorWeight as hue. Leaves whose sizeWeight <= 0 are
// skipped (they contribute no area to the layout).
func BuildColoredTree(
	groups []SccGroup, rootName string,
	sizeWeight, colorWeight Weight, buckets int,
) (*layout.Node, func(*layout.Node) int) {
	if buckets < 1 {
		buckets = 1
	}
	root := &layout.Node{Name: rootName}
	dirs := map[string]*layout.Node{"": root}
	cw := map[*layout.Node]float64{}
	for _, g := range groups {
		for i := range g.Files {
			f := &g.Files[i]
			sz := sizeWeight(f)
			if sz <= 0 {
				continue
			}
			loc := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(f.Location)), "./")
			if loc == "" || loc == "." {
				continue
			}
			parts := strings.Split(loc, "/")
			parent := ensureDir(dirs, parts[:len(parts)-1])
			leaf := &layout.Node{Name: parts[len(parts)-1], Size: sz}
			parent.Children = append(parent.Children, leaf)
			w := colorWeight(f)
			if w < 0 {
				w = 0
			}
			cw[leaf] = w
		}
	}
	var aggregate func(n *layout.Node) float64
	aggregate = func(n *layout.Node) float64 {
		if len(n.Children) == 0 {
			return cw[n]
		}
		sum := 0.0
		for _, ch := range n.Children {
			sum += aggregate(ch)
		}
		cw[n] = sum
		return sum
	}
	maxCW := aggregate(root)
	logMax := math.Log1p(maxCW)
	colorFn := func(n *layout.Node) int {
		w, ok := cw[n]
		if !ok || logMax == 0 {
			return 0
		}
		idx := int(math.Log1p(w) / logMax * float64(buckets-1))
		if idx < 0 {
			return 0
		}
		if idx >= buckets {
			return buckets - 1
		}
		return idx
	}
	return root, colorFn
}

// BuildColormappedTree builds a tree weighted by sizeWeight and returns a
// raw value extractor mapping every node — leaves and directories — to its
// aggregated colorWeight. Directory weights are the recursive sum of their
// descendants' colorWeight, so a drilled-out view conveys aggregate subtree
// magnitude; drilling in then shows the per-file distribution.
//
// keep filters files at the leaf level: a file is included only when
// keep(f) returns true. Pass nil to keep every file. Use IsGenerated (or
// its negation) to toggle inclusion of generated code without forcing a
// re-shell of scc.
//
// Unlike BuildColoredTree, no log-normalization is applied here — the
// returned valueFn returns raw aggregates. Pair with a logarithmic colormap
// (treemap.NewLogColormap) to spread heavy-tailed distributions across the
// palette. Pass the same *Colormap to colorscale.New to get a legend that
// stays in sync with the treemap automatically.
//
// maxValue is the root's aggregated total, suitable as the upper bound when
// constructing the Colormap.
func BuildColormappedTree(
	groups []SccGroup, rootName string,
	sizeWeight, colorWeight Weight,
	keep func(*SccFile) bool,
) (root *layout.Node, valueFn func(*layout.Node) float64, maxValue float64) {
	root = &layout.Node{Name: rootName}
	dirs := map[string]*layout.Node{"": root}
	cw := map[*layout.Node]float64{}
	for _, g := range groups {
		for i := range g.Files {
			f := &g.Files[i]
			if keep != nil && !keep(f) {
				continue
			}
			sz := sizeWeight(f)
			if sz <= 0 {
				continue
			}
			loc := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(f.Location)), "./")
			if loc == "" || loc == "." {
				continue
			}
			parts := strings.Split(loc, "/")
			parent := ensureDir(dirs, parts[:len(parts)-1])
			leaf := &layout.Node{Name: parts[len(parts)-1], Size: sz}
			parent.Children = append(parent.Children, leaf)
			w := colorWeight(f)
			if w < 0 {
				w = 0
			}
			cw[leaf] = w
		}
	}
	var aggregate func(n *layout.Node) float64
	aggregate = func(n *layout.Node) float64 {
		if len(n.Children) == 0 {
			return cw[n]
		}
		sum := 0.0
		for _, ch := range n.Children {
			sum += aggregate(ch)
		}
		cw[n] = sum
		return sum
	}
	maxValue = aggregate(root)
	valueFn = func(n *layout.Node) float64 { return cw[n] }
	return
}

func ensureDir(dirs map[string]*layout.Node, parts []string) *layout.Node {
	key := strings.Join(parts, "/")
	if n, ok := dirs[key]; ok {
		return n
	}
	parent := ensureDir(dirs, parts[:len(parts)-1])
	n := &layout.Node{Name: parts[len(parts)-1]}
	parent.Children = append(parent.Children, n)
	dirs[key] = n
	return n
}
