package play

// play_diagnostics_analyze.go is the Diagnostics pane's Execution profile:
// the active result's query run again under `EXPLAIN ANALYZE`, which
// executes it and annotates the plan with what each step measured — rows and
// bytes in and out, wall time and its share of the execution, parallelism
// (ClickHouse 26.7+).
//
// It runs only when asked. Unlike the EXPLAIN AST probe, which is parse-only
// and armed by every grammar-rejected buffer, this executes the whole query
// on the server; the result is discarded there and only the annotated plan
// comes back.
//
// The statement is compiled the way the Flow tab's Run-mode lenses compile
// theirs (compileNodeFor over the last Run's split, the observed node), and
// the EXPLAIN wrap is applied to the residual by the transport
// (ExecOptions.WrapStatement), so the profile routes, rewrites and binds
// exactly as the run it describes.

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// analyzeHotSteps is how many of the costliest steps the profile names above
// the tree.
const analyzeHotSteps = 3

// explainAnalyzeWrap is the profile lane's wire-body wrap. The closing paren
// sits on its own line for the reason explainWrap gives: a residual ending in
// a `--` comment would otherwise swallow it.
func explainAnalyzeWrap(residual string) string {
	return "SELECT * FROM (EXPLAIN ANALYZE " + strings.TrimRight(strings.TrimSpace(residual), ";") + "\n)"
}

// analyzeStep is one timed line of the plan: a step, or one stage of a step.
type analyzeStep struct {
	Line  int     // index into analyzeProfile.Tree
	Step  string  // the plan step the time belongs to
	Stage string  // the stage within it, when the step reports several
	Time  string  // as the server wrote it, `5.56 ms`
	Share float64 // percent of the query's execution time
}

// analyzeProfile is EXPLAIN ANALYZE's text output taken apart: the summary
// block, the plan tree as written, and the timed lines.
type analyzeProfile struct {
	Summary []string
	Tree    []string
	Timed   []analyzeStep // in tree order
}

// parseAnalyze reads the server's rendering. The output has no
// machine-readable form (`json = 1` is refused for ANALYZE), so this reads
// the text: a `Query summary:` block, then the tree. A step's header is the
// root line or one drawn with `├──` / `└──`; the lines under it carry its
// details, and those with a `time … (…%)` annotation are what the profile
// ranks. Lines that do not match are kept in the tree and simply not ranked,
// so a change in the server's wording loses the ranking, never the output.
func parseAnalyze(lines []string) (p analyzeProfile) {
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.TrimSpace(lines[i]) == "Query summary:" {
		i++
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" {
			p.Summary = append(p.Summary, strings.TrimSpace(lines[i]))
			i++
		}
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	p.Tree = lines[i:]
	step := ""
	for n, ln := range p.Tree {
		if name, ok := analyzeHeader(ln); ok {
			step = name
			continue
		}
		t, share, ok := analyzeTiming(ln)
		if !ok {
			continue
		}
		s := analyzeStep{Line: n, Step: step, Time: t, Share: share}
		detail := strings.TrimLeft(ln, " │")
		if rest, isStage := strings.CutPrefix(detail, "Stage ("); isStage {
			if j := strings.Index(rest, "):"); j >= 0 {
				s.Stage = rest[:j]
			}
		}
		p.Timed = append(p.Timed, s)
	}
	return
}

// analyzeHeader recognises a step's header line and returns the step's name.
func analyzeHeader(ln string) (name string, ok bool) {
	if ln == "" || strings.HasPrefix(ln, "Output:") {
		return
	}
	rest := strings.TrimLeft(ln, " │")
	for _, glyph := range []string{"└──", "├──"} {
		if after, found := strings.CutPrefix(rest, glyph); found {
			return after, true
		}
	}
	// The root step is the one header written at the margin.
	if rest == ln {
		return ln, true
	}
	return
}

// analyzeTiming reads `time <value> <unit> (<share>%)` off a detail line.
func analyzeTiming(ln string) (t string, share float64, ok bool) {
	i := strings.Index(ln, "time ")
	if i < 0 {
		return
	}
	rest := ln[i+len("time "):]
	open := strings.Index(rest, " (")
	if open < 0 {
		return
	}
	pct, _, found := strings.Cut(rest[open+2:], "%)")
	if !found {
		return
	}
	share, err := strconv.ParseFloat(pct, 64)
	if err != nil {
		return
	}
	return rest[:open], share, true
}

// hottest returns the n timed lines with the largest share, largest first.
func (inst analyzeProfile) hottest(n int) (out []analyzeStep) {
	out = slices.Clone(inst.Timed)
	slices.SortStableFunc(out, func(a, b analyzeStep) int {
		switch {
		case a.Share > b.Share:
			return -1
		case a.Share < b.Share:
			return 1
		}
		return 0
	})
	if len(out) > n {
		out = out[:n]
	}
	return
}

// analyzeTone is the tone a timed line is drawn in: the hot steps stand out
// of a tree that is otherwise mostly sub-percent.
func analyzeTone(share float64) gloss.ToneE {
	switch {
	case share >= 25:
		return gloss.ToneError
	case share >= 10:
		return gloss.ToneWarning
	}
	return gloss.ToneNeutral
}

// renderDiagAnalyze is the Execution profile section.
func (inst *PlayApp) renderDiagAnalyze() {
	diagHeading("Execution profile")
	d := inst.diag
	if d == nil || d.analyzeLane == nil {
		diagWeak("Needs a connected endpoint.")
		return
	}
	split := inst.currentSplit
	active := inst.activeNodeID()
	node, ok := findSplitNode(split, active)
	if !ok && active == mainNodeID {
		node, ok = findSplitNode(split, split.Sink)
	}

	var cn compiledNode
	canProfile := false
	switch {
	case !ok:
		diagWeak("Run a query first: the profile runs the active result's query again under EXPLAIN ANALYZE.")
	case node.Client != nil:
		diagWeak("The active result is computed in play, not by a query, so there is nothing to EXPLAIN.")
	default:
		cn = compileNodeFor(split, node, inst.lastRunBound, inst.frameSig)
		canProfile = true
	}

	v := laneView{}
	if d.analyzeArmed {
		v = d.analyzeLane.demand(d.analyzeReq)
	}
	p, fresh := d.analyzeLane.progressView()
	prog := d.analyzeProgress.observe(time.Now(), "diagnostics-analyze", p, fresh)
	for range c.HorizontalTop().KeepIter() {
		if canProfile {
			label := "Profile the active result"
			if d.analyzeArmed {
				label = "Profile again"
			}
			if c.Button(inst.ids.PrepareStr("diag-analyze"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() {
				d.analyzeReq = cn
				d.analyzeArmed = true
				d.analyzeLane.forget()
			}
		}
		if v.loading {
			c.Separator().Vertical().Send()
			if renderLaneProgress(inst.ids, "diag-analyze-cancel", prog) {
				d.analyzeLane.abort()
			}
		}
	}
	if !d.analyzeArmed {
		if canProfile {
			diagWeak("Runs the query on the server once more and reports, per plan step, the rows it passed and the time it took. The result itself is discarded.")
		}
		return
	}
	if canProfile && d.analyzeReq.key() != cn.key() {
		diagWeak("This profile is of an earlier statement or run; profile again for the active result.")
	}
	if v.err != nil {
		if explainUnsupportedByEndpoint(v.err) {
			diagWeak("This endpoint's SQL surface has no EXPLAIN: the query runs here, but cannot be profiled.")
			return
		}
		c.Label("EXPLAIN ANALYZE failed (it needs ClickHouse 26.7 or later):").Wrap().Send()
		c.Label(v.err.Error()).Wrap().Selectable(true).Send()
		return
	}
	if v.rec == nil {
		return
	}
	lines := make([]string, 0, v.rec.NumRows())
	for row := range v.rec.NumRows() {
		lines = append(lines, strings.Split(formatCell(v.rec, 0, row), "\n")...)
	}
	v.rec.Release()
	renderAnalyzeProfile(parseAnalyze(lines))
}

// renderAnalyzeProfile draws a parsed profile: the summary, the costliest
// steps, then the plan tree with the timed lines toned by their share.
func renderAnalyzeProfile(p analyzeProfile) {
	for _, ln := range p.Summary {
		for rt := range c.RichTextLabel(ln) {
			rt.Monospace()
		}
	}
	if hot := p.hottest(analyzeHotSteps); len(hot) > 0 {
		c.Label("Most of the time went to:").Send()
		for _, s := range hot {
			text := strconv.FormatFloat(s.Share, 'f', 1, 64) + "%  " + s.Step
			if s.Stage != "" {
				text += " — " + s.Stage
			}
			text += " · " + s.Time
			analyzeLine(text, analyzeTone(s.Share))
		}
	}
	shares := make(map[int]float64, len(p.Timed))
	for _, s := range p.Timed {
		shares[s.Line] = s.Share
	}
	c.Separator().Horizontal().Send()
	for n, ln := range p.Tree {
		analyzeLine(ln, analyzeTone(shares[n]))
	}
	diagWeak("Shares are of the query's execution time, from this one run; a step's stages are timed separately.")
}

func analyzeLine(text string, tone gloss.ToneE) {
	if col, toned := toneColor(tone); toned {
		for rt := range c.RichTextLabelColored(col, color.Transparent, text) {
			rt.Monospace()
		}
		return
	}
	for rt := range c.RichTextLabel(text) {
		rt.Monospace()
	}
}
