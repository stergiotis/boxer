// Package riskviews is an exploratory imzero2 app: nine risk-management
// views over one synthetic register, ported from a self-contained HTML
// prototype to imzero2's own widgets and the IDS token layer. It is a
// curiosity exercise, not a product — the data is synthetic and the views
// are throwaway prototypes.
//
// Which view uses what: the loss-exceedance, calibration and
// impact-tolerance views are ImPlot-port plots (widgets/implot) with Custom
// items where the plot has no native item; the matrix, dotplot, icon arrays,
// KRI bullets and sparklines, bow-tie, tornado and incident timeline are
// painter-lane canvases (c.Paint* + c.PaintCanvas). Status chips are
// widgets/badge.
package riskviews

import (
	"fmt"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

type App struct {
	ids     *c.WidgetIdStack
	density styletokens.DensityE

	view int

	// view 1 controls
	mxCont bool
	mxInh  bool

	paneW float32
}

var _ app.AppI = (*App)(nil)

func newApp() *App {
	return &App{ids: c.NewWidgetIdStack(), density: styletokens.ActiveDensity()}
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

type viewDef struct {
	tab, num, title, job string
	needs, evidence      string
	render               func(inst *App, w float32, p palette)
}

var views = []viewDef{
	{tab: "1 Matrix", num: "View 1 · entry point", title: "The matrix, done right",
		job:      "The familiar 5×5, drawn as one binning of the stored ranges. Axis labels are numeric and geometric, cells grow toward the high-risk corner, and the key sits on the axes. Switch to the continuous view to see the ranges the cells hide.",
		needs:    "Likelihood and impact as numeric intervals per risk, inherent and residual. Bands are a rendering choice, stored separately.",
		evidence: "Geometric labels and growing cells improved comprehension in two RCTs (Sutherland et al.); ordinal arithmetic is avoided because nothing is multiplied.",
		render:   (*App).renderMatrix},
	{tab: "2 Loss exceedance", num: "View 2 · executive", title: "Loss exceedance against tolerance",
		job:      "Chance that total annual loss reaches at least a given amount, before and after controls, against the board's tolerance curve.",
		needs:    "Simulation samples, one row per iteration and risk, so totals aggregate per iteration. Tolerance as versioned points.",
		evidence: "Guidance (FAIR, Hubbard); no readability study for executives found.",
		render:   (*App).renderLEC},
	{tab: "3 Dotplot", num: "View 3 · one risk, plain language", title: "Quantile dotplot and icon arrays",
		job:      "The same distribution as 50 equally likely years, and one risk's annual chance as counts out of 100.",
		needs:    "The same samples as view 2; a reserve or budget figure to compare against.",
		evidence: "Strongest of any view here: quantile dotplots and frequency framing beat intervals in incentivised experiments (Kay et al.; Fernandes et al.); icon arrays reduce base-rate neglect.",
		render:   (*App).renderDots},
	{tab: "4 Indicators", num: "View 4 · monitoring", title: "Indicators against versioned appetite",
		job:      "Each indicator as a bullet graph against its appetite and tolerance bands, with twelve months of history drawn against the threshold in force at the time.",
		needs:    "Long-format time series plus a thresholds table with valid-from dates. Drill-down from a value to the records behind it.",
		evidence: "Guidance only; no study of indicator dashboards for decision quality found. Versioning prevents past values being judged against today's limit.",
		render:   (*App).renderKRI},
	{tab: "5 Bow-tie", num: "View 5 · how the loss arises", title: "Bow-tie with live barrier status",
		job:      "Threats on the left, consequences on the right, the barriers between them coloured by their last test, not by an opinion.",
		needs:    "A property graph: threats, barriers, top event and consequences as typed nodes, each barrier linked to its control-test records.",
		evidence: "Guidance (CCPS / Energy Institute). Useful when linked to test data; static wallpaper when not.",
		render:   (*App).renderBowtie},
	{tab: "6 Tornado", num: "View 6 · what to measure next", title: "Tornado of expected annual loss",
		job:      "How far expected annual loss moves when one input runs from the low to the high end of its range, the others held at their midpoints.",
		needs:    "The model's inputs with their ranges. The widest bar is where better information is worth most.",
		evidence: "Guidance. One-at-a-time swings ignore interactions between inputs.",
		render:   (*App).renderTornado},
	{tab: "7 Calibration", num: "View 7 · closing the loop", title: "Calibration of past estimates",
		job:      "Of the risks we put at about 30% a year, how many happened? Points on the diagonal are well calibrated; points below it are overconfident.",
		needs:    "Every past likelihood estimate with its date, and whether the event occurred in the period.",
		evidence: "Standard in forecasting research; not a convention of any risk framework. An addition proposed here.",
		render:   (*App).renderCalib},
	{tab: "8 Incidents", num: "View 8 · learning", title: "Incident timeline",
		job:      "Losses and near misses over two years, by area, with the control changes that should have moved them.",
		needs:    "An incident and near-miss log with dates, area and loss; control changes as dated events.",
		evidence: "Guidance. Feeds the base rates used for new estimates.",
		render:   (*App).renderTimeline},
	{tab: "9 Impact tolerance", num: "View 9 · resilience", title: "Impact tolerance under scenarios",
		job:      "For each important service, the estimated time to recover under three named scenarios, against the maximum disruption the board accepts.",
		needs:    "Services with tolerances, scenarios, and recovery estimates per pair, ideally from exercises.",
		evidence: "Regulatory direction (DORA, UK operational resilience); works without credible probabilities.",
		render:   (*App).renderTolerance},
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.density = styletokens.ActiveDensity()
	p := pal()
	ids := inst.ids
	for range c.PanelTopInside(ids.PrepareStr("top")).Resizable(false).KeepIter() {
		c.AddSpace(styletokens.PaddingDefault(inst.density))
		c.LabelAtoms(c.Atoms().BeginRichTextColored(p.muted, transparent, "THROWAWAY PROTOTYPES · SYNTHETIC DATA").Monospace().Size(styletokens.CaptionPt).End().Keep()).Send()
		c.LabelAtoms(c.Atoms().BeginRichText("Risk view prototypes").Strong().Size(styletokens.DisplayPt).End().Keep()).Send()
		inst.renderStrip(p)
		c.AddSpace(styletokens.GapItems(inst.density))
		for range c.HorizontalWrapped().KeepIter() {
			for i, v := range views {
				sel := i == inst.view
				if c.Button(ids.PrepareSeq(uint64(i)+1), c.Atoms().Text(v.tab).Keep()).Selected(sel).SendResp().HasPrimaryClicked() {
					inst.view = i
				}
			}
		}
		c.AddSpace(styletokens.PaddingDefault(inst.density))
	}
	for range c.PanelCentralInside().KeepIter() {
		if w, _, ok := c.CapturePaneSize(ids.ProbeSeq("pane")); ok && w > 0 {
			inst.paneW = w
		}
		w := inst.paneW
		if w <= 0 {
			w = 1100
		}
		w -= 24 // scrollbar and frame margin
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			v := views[inst.view]
			for range c.IdScope(ids.PrepareStr(v.tab)) {
				inst.renderCard(v, w, p)
			}
		}
	}
	return
}

func (inst *App) renderStrip(p palette) {
	lo := func(s string) {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(p.ink2, transparent, s).Monospace().Size(styletokens.CaptionPt).End().Keep()).Send()
	}
	hi := func(s string) {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(p.ink, transparent, s).Monospace().Size(styletokens.CaptionPt).End().Keep()).Send()
	}
	for range c.HorizontalWrapped().KeepIter() {
		lo("Risks")
		hi(strconv.Itoa(len(risks)))
		lo("  Simulated years")
		hi(fmtThousands(simYears))
		lo("  Expected annual loss, residual")
		hi(eur(ealResidual()))
		lo("  inherent")
		hi(eur(ealInherent()))
		lo("  P(loss ≥ €5m in a year), residual")
		hi(pct(exceed(simRes, 5e6), 1))
	}
}

func fmtThousands(n int) string {
	s := strconv.Itoa(n)
	out := ""
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(r)
	}
	return out
}

func (inst *App) renderCard(v viewDef, w float32, p palette) {
	d := inst.density
	for range c.Frame(inst.ids.PrepareStr("card")).
		Fill(p.bg).
		Stroke(styletokens.StrokeHair, p.border).
		CornerRadius(styletokens.RoundingLg).
		InnerMargin(styletokens.PaddingOuter(d)).
		KeepIter() {
		c.UiSetMinWidth(w - 2*styletokens.PaddingOuter(d))
		c.UiSetMaxWidth(w - 2*styletokens.PaddingOuter(d))
		c.LabelAtoms(c.Atoms().BeginRichTextColored(p.muted, transparent, v.num).Monospace().Size(styletokens.CaptionPt).End().Keep()).Send()
		c.LabelAtoms(c.Atoms().BeginRichText(v.title).Strong().Size(fsTitle + 4).End().Keep()).Send()
		c.LabelAtoms(c.Atoms().BeginRichTextColored(p.ink2, transparent, v.job).End().Keep()).Wrap().Send()
		c.AddSpace(styletokens.GapItems(d))
		v.render(inst, w-2*styletokens.PaddingOuter(d)-4, p)
		c.AddSpace(styletokens.GapItems(d))
		c.Separator().Send()
		for range c.Grid(inst.ids.PrepareStr("meta")).NumColumns(2).KeepIter() {
			c.LabelAtoms(c.Atoms().BeginRichTextColored(p.muted, transparent, "NEEDS").Monospace().Size(styletokens.MicroPt + 1).End().Keep()).Send()
			c.LabelAtoms(c.Atoms().BeginRichTextColored(p.ink2, transparent, v.needs).End().Keep()).Wrap().Send()
			c.EndRow()
			c.LabelAtoms(c.Atoms().BeginRichTextColored(p.muted, transparent, "EVIDENCE").Monospace().Size(styletokens.MicroPt + 1).End().Keep()).Send()
			c.LabelAtoms(c.Atoms().BeginRichTextColored(p.ink2, transparent, v.evidence).End().Keep()).Wrap().Send()
			c.EndRow()
		}
	}
}

// callout is the prototype's <p class="callout">: a bold lead and plain rest.
func callout(p palette, bold, rest string) {
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(p.ink, transparent, bold).Strong().End().
		BeginRichTextColored(p.ink, transparent, rest).End().
		Keep()).Wrap().Send()
}

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
