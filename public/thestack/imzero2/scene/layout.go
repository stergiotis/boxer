package scene

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/layoutcheck"
)

// LayoutFinding is a layout finding and the capture it was found in.
type LayoutFinding struct {
	Capture string
	layoutcheck.Finding
}

func (inst LayoutFinding) String() string {
	return inst.Capture + ": " + inst.Finding.String()
}

// LayoutReport is what CheckLayout found in one scene's captures.
type LayoutReport struct {
	// Checked are the captures whose tree sidecar was read.
	Checked []string
	// Missing are tree sidecars the trace asks for that are not in the
	// output directory.
	Missing  []string
	Findings []LayoutFinding
	// Excused are findings one of the scene's layout exceptions covers.
	Excused []LayoutFinding
	// Advisory are findings of an invariant that does not fail a scene
	// (layoutcheck.InvariantE.Blocking), reported beside the result.
	Advisory []LayoutFinding
}

// Err is nil when nothing was found, and otherwise lists every finding.
func (inst LayoutReport) Err() error {
	if len(inst.Findings) == 0 {
		return nil
	}
	return eb.Build().Int("findings", len(inst.Findings)).Int("excused", len(inst.Excused)).
		Errorf("layout invariants do not hold: %w", findingsError(inst.Findings))
}

// findingsError carries the findings one per line, so the scene command's
// error rendering prints them as a list.
type findingsError []LayoutFinding

func (inst findingsError) Error() string {
	var b strings.Builder
	for _, f := range inst {
		b.WriteString("\n  " + f.String())
	}
	return b.String()
}

// CheckLayout applies the layout invariants (ADR-0363 M1) to every capture of
// the scene whose step asks for a tree sidecar, reading the files from
// outDir, and sorts the findings by the scene's layout exceptions. The
// viewport each capture is checked against is the scene's size as changed by
// any `resize` step before it. A capture that also wrote an svg sidecar is
// checked for elided text too.
func CheckLayout(doc *Doc, outDir string) (rep LayoutReport, err error) {
	w, h, err := doc.Spec.Dimensions()
	if err != nil {
		return rep, err
	}
	vw, vh := float32(w), float32(h)
	for _, st := range doc.Steps {
		if st.Do == "resize" && st.W > 0 && st.H > 0 {
			vw, vh = st.W, st.H
			continue
		}
		if st.Do != "capture" || st.Text == "" || !slices.Contains(st.Sidecars, carrierclient.SidecarTree) {
			continue
		}
		capture := CaptureStem(st.Text)
		treePath := filepath.Join(outDir, carrierclient.SidecarFile(st.Text, carrierclient.SidecarTree))
		f, e := os.Open(treePath)
		if os.IsNotExist(e) {
			rep.Missing = append(rep.Missing, filepath.Base(treePath))
			continue
		}
		if e != nil {
			return rep, eb.Build().Str("path", treePath).Errorf("unable to open the tree sidecar: %w", e)
		}
		nodes, e := layoutcheck.ReadTree(f)
		_ = f.Close()
		if e != nil {
			return rep, eb.Build().Str("path", treePath).Errorf("unable to read the tree sidecar: %w", e)
		}
		findings := layoutcheck.Check(nodes, layoutcheck.Options{ViewportW: vw, ViewportH: vh})
		if slices.Contains(st.Sidecars, carrierclient.SidecarSVG) || slices.Contains(st.Sidecars, carrierclient.SidecarSVGFonts) {
			svgPath := filepath.Join(outDir, carrierclient.SidecarFile(st.Text, carrierclient.SidecarSVG))
			var runs []layoutcheck.TextRun
			if runs, err = readSVGRuns(svgPath); err != nil {
				return rep, err
			}
			findings = append(findings, layoutcheck.CheckElided(nodes, runs)...)
		}
		rep.Checked = append(rep.Checked, capture)
		kept, excused := layoutcheck.Apply(capture, findings, doc.Spec.LayoutExceptions)
		for _, k := range kept {
			if k.Invariant.Blocking() {
				rep.Findings = append(rep.Findings, LayoutFinding{Capture: capture, Finding: k})
			} else {
				rep.Advisory = append(rep.Advisory, LayoutFinding{Capture: capture, Finding: k})
			}
		}
		for _, x := range excused {
			rep.Excused = append(rep.Excused, LayoutFinding{Capture: capture, Finding: x})
		}
	}
	return rep, nil
}

func readSVGRuns(path string) (runs []layoutcheck.TextRun, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to open the svg sidecar: %w", err)
	}
	defer func() { _ = f.Close() }()
	if runs, err = layoutcheck.ReadSVGText(f); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to read the svg sidecar: %w", err)
	}
	return runs, nil
}

// Summary is one line for the scene command's status column: how many
// captures were checked, and how many findings exceptions covered.
func (inst LayoutReport) Summary() string {
	if len(inst.Checked) == 0 {
		return ""
	}
	s := "layout: " + strconv.Itoa(len(inst.Checked)) + " checked"
	if len(inst.Excused) > 0 {
		s += ", " + strconv.Itoa(len(inst.Excused)) + " excused"
	}
	if len(inst.Advisory) > 0 {
		s += ", " + strconv.Itoa(len(inst.Advisory)) + " advisory"
	}
	return s
}
