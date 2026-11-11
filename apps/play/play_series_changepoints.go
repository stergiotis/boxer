package play

import (
	"fmt"
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_series_changepoints.go is the Series pane's way into the change point
// vocabulary (ADR-0163 Update 2026-11-11): a menu whose every entry writes a
// `ts*` scaffold into the buffer, and nothing else.
//
// The menu does not run an analysis and holds no parameters. That is the
// ADR's rejected alternative — panel-side controls leave an invocation
// unrecorded, unreplayable and invisible to an agent — so what a person picks
// here becomes SQL they can read, edit and rerun, and get_series hands an
// agent the same text.

// seriesChangeMethod is one entry: which question it answers, the caveat that
// belongs beside the answer, and the CTEs that ask it.
type seriesChangeMethod struct {
	// ID keys the widget and names the entry to an agent.
	ID string
	// Label is the question in the reader's words, not the method's name.
	Label string
	// About names the method and states what it assumes or cannot see. It is
	// the menu's line under the button and the scaffold's leading comment.
	About string
	// ctes renders the analysis CTEs over `base` for a series of n samples.
	ctes func(t string, v string, n int) []string
}

// seriesChangeMethods is the roster, in the order the procedure runs: the
// offline default first, its variants, then the sequential detectors.
var seriesChangeMethods = []seriesChangeMethod{
	{
		ID: "level", Label: "shifts in level",
		About: "PELT with a mean cost: the exact penalised optimum. Assumes independent noise within a segment; " +
			"autocorrelation and trends come back as changes. Check the count against the `penalties` table.",
		ctes: func(t string, v string, _ int) []string {
			return []string{
				"spans AS (SELECT tsPeltMean(" + t + ", " + v + ", 5, 1) FROM base)",
				"-- bind a Table pane to `penalties`: a change count that holds over many octaves is the data's\n" +
					"  penalties AS (SELECT tsPeltMeanPath(" + t + ", " + v + ", 5) FROM base)",
			}
		},
	},
	{
		ID: "level-long", Label: "shifts in level, on a long series",
		About: "Binary segmentation: greedy where PELT is exact, so a short excursion can hide between two splits. " +
			"For series past PELT's row ceiling. Same assumption: independent noise within a segment.",
		ctes: func(t string, v string, _ int) []string {
			return []string{"spans AS (SELECT tsBinSegMean(" + t + ", " + v + ", 5, 1) FROM base)"}
		},
	},
	{
		ID: "level-or-spread", Label: "shifts in level or spread",
		About: "PELT with a mean-and-variance cost. Needs room per segment (30 samples here): " +
			"a variance fitted to a few samples that agree looks like a regime.",
		ctes: func(t string, v string, _ int) []string {
			return []string{"spans AS (SELECT tsPeltMeanVar(" + t + ", " + v + ", 30, 1) FROM base)"}
		},
	},
	{
		ID: "robust", Label: "shifts despite outliers",
		About: "PELT on the ranks of the values: outliers and skew do not move it. " +
			"A change in spread around an unchanged median is invisible to it.",
		ctes: func(t string, v string, _ int) []string {
			return []string{"spans AS (SELECT tsPeltRank(" + t + ", " + v + ", 5, 1) FROM base)"}
		},
	},
	{
		ID: "windowed", Label: "shifts at a known time scale",
		About: "MOSUM: the mean before against the mean after, over a bandwidth, with a 5% critical value that " +
			"holds for independent noise and no change. Changes closer than a bandwidth blur together.",
		ctes: func(t string, v string, n int) []string {
			g := fmt.Sprint(min(max(n/20, 10), 200))
			return []string{
				"scores AS (SELECT tsMosumScores(" + t + ", " + v + ", " + g + ") FROM base)",
				"spans AS (SELECT tsMosumSegments(" + t + ", " + v + ", " + g + ") FROM base)",
			}
		},
	},
	{
		ID: "monitor", Label: "what a monitor would have alarmed on",
		About: "Two-sided CUSUM, causal. Its false-alarm rate is tabulated for a known mean and scale; " +
			"here both are estimated from the warm-up, so alarms on noise are more frequent than the table says.",
		ctes: func(t string, v string, n int) []string {
			w := fmt.Sprint(min(max(n/10, 30), 200))
			return []string{
				"scores AS (SELECT tsCusumScores(" + t + ", " + v + ", " + w + ") FROM base)",
				"spans AS (SELECT tsCusumAlarms(" + t + ", " + v + ", " + w + ") FROM base)",
			}
		},
	},
	{
		ID: "probability", Label: "the probability a change just happened",
		About: "Bayesian online detection, causal: the probability that the regime began within the last 10 samples, " +
			"under a Gaussian model with a change every 250 samples on average. A model's probability, not a frequency.",
		ctes: func(t string, v string, _ int) []string {
			return []string{"scores AS (SELECT tsBocpdScores(" + t + ", " + v + ", 250, 10) FROM base)"}
		},
	},
}

// changeScaffold is the SQL one entry writes: the reader's query as `base`,
// the analysis beside it, and the series itself as the result, so the Series
// pane draws the data with the analysis over it.
func (inst *SeriesDriver) changeScaffold(m seriesChangeMethod) (sql string) {
	t, v := inst.tLabel, "value"
	if len(inst.lanes) > 0 {
		v = inst.lanes[0].label
	}
	var b strings.Builder
	// A leading newline: the scaffold is spliced at the caret, which may sit
	// mid-line.
	b.WriteString("\n-- ADR-0163, change points — " + m.Label + ". " + m.About + "\n")
	b.WriteString("WITH\n  base AS (\n    -- your query here: " + t + ", " + v + ", ordered by " + t + "\n  )")
	for _, cte := range m.ctes(t, v, len(inst.t)) {
		b.WriteString(",\n  " + cte)
	}
	b.WriteString("\nSELECT * FROM base\n")
	return b.String()
}

// changeScaffolds lists every entry with its SQL, for get_series.
func (inst *SeriesDriver) changeScaffolds() (out []SeriesChangeScaffold) {
	out = make([]SeriesChangeScaffold, 0, len(seriesChangeMethods))
	for _, m := range seriesChangeMethods {
		out = append(out, SeriesChangeScaffold{Question: m.Label, About: m.About, Scaffold: inst.changeScaffold(m)})
	}
	return
}

// renderChangePointMenu draws the menu in the controls row. The label is
// fixed: a MenuButton's id derives from it, and a label that changed would
// drop the open state.
func (inst *SeriesDriver) renderChangePointMenu() {
	if inst.deliver == nil {
		return
	}
	for range c.MenuButton(c.Atoms().Text("Change points").Keep()).KeepIter() {
		menuWeak("Each entry writes SQL at the caret; nothing runs until you do. " +
			"Every method here reads samples as independent — remove trend and season first.")
		for _, m := range seriesChangeMethods {
			if c.Button(inst.ids.PrepareStr("series-change-"+m.ID), c.Atoms().Text(m.Label).Keep()).
				SendResp().HasPrimaryClicked() {
				inst.deliver(inst.changeScaffold(m))
			}
			menuWeak(m.About)
		}
	}
}
