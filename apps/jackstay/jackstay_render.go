package jackstay

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/hmi/progressest"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// footer is what a page puts in the bottom bar: the page's own action in the
// middle, Back and Next at the ends. Next is enabled once the step it leads
// to is unlocked; nextNote says why it is not.
type footer struct {
	primary func()
	next    stepE
	hasNext bool
}

func (inst *App) render() {
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.renderBreadcrumb()
		inst.renderPlanRow()
		inst.renderStatus()
	}
	for range c.PanelBottomInside(inst.ids.PrepareStr("bottom")).Resizable(false).KeepIter() {
		inst.renderFooter(inst.footerFor(inst.step))
	}
	for range c.PanelLeftInside(inst.ids.PrepareStr("brief")).Resizable(false).ExactSize(250).KeepIter() {
		inst.renderBrief()
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("page-" + inst.step.String())) {
				for range c.Frame(inst.ids.PrepareStr("pad")).InnerMarginSides(12, 12, 8, 8).KeepIter() {
					switch inst.step {
					case stepConnect:
						inst.renderConnect()
					case stepDatabases:
						inst.renderDatabases()
					case stepStructure:
						inst.renderStructure()
					case stepDifferences:
						inst.renderDifferences()
					case stepSync:
						inst.renderSync()
					case stepRun:
						inst.renderRun()
					}
				}
			}
		}
	}
}

// goTo moves to a step, unless it is locked.
func (inst *App) goTo(st stepE) {
	if inst.stepLocked(st) != "" {
		return
	}
	inst.step = st
}

// --- the breadcrumb: the steps in order, where we are -----------------------------

// renderBreadcrumb draws the six steps as a trail. A done step is ticked, the
// current one is emphasised, a locked one is dimmed with its reason on hover.
// Each is a frameless button, so a done step can be revisited by clicking it.
func (inst *App) renderBreadcrumb() {
	for range c.HorizontalTop().KeepIter() {
		for i, st := range allSteps {
			if i > 0 {
				weak(icons.PhCaretRight)
			}
			for range c.IdScope(inst.ids.PrepareStr("crumb-" + st.String())) {
				locked := inst.stepLocked(st)
				if inst.step == st {
					locked = ""
				}
				done, _ := inst.stepDone(st)
				// The button's text is the step's name alone, so a driver
				// finds a step by the same name whether it is done or not.
				text := c.Atoms().Text(st.String()).Keep()
				switch {
				case inst.step == st:
					text = c.Atoms().BeginRichText(st.String()).Strong().End().Keep()
				case locked != "":
					text = c.Atoms().BeginRichText(st.String()).Weak().End().Keep()
				}
				for range c.HorizontalTop().KeepIter() {
					if done && inst.step != st {
						weak(icons.PhCheck)
					}
					if locked != "" {
						c.UiDisable()
					}
					clicked := false
					if locked != "" {
						for range c.HoverText(locked).KeepIter() {
							clicked = c.Button(inst.ids.PrepareStr("step"), text).Frame(false).SendResp().HasPrimaryClicked()
						}
					} else {
						clicked = c.Button(inst.ids.PrepareStr("step"), text).Frame(false).SendResp().HasPrimaryClicked()
					}
					if clicked && locked == "" {
						inst.step = st
					}
				}
			}
		}
	}
}

// renderBrief is the column beside the page: the step's icon, its name,
// what it does, and what the earlier steps produced.
func (inst *App) renderBrief() {
	st := inst.step
	for range c.Frame(inst.ids.PrepareStr("brief-pad")).InnerMarginSides(12, 12, 16, 8).KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichText(st.icon()).Size(64).End().Keep()).Send()
		c.LabelAtoms(c.Atoms().BeginRichText(st.short()).Heading().End().Keep()).Send()
		small("step " + strconv.Itoa(int(st)+1) + " of " + strconv.Itoa(len(allSteps)))
		space()
		note(st.describe())
		space()
		shown := false
		for _, s := range allSteps {
			done, summary := inst.stepDone(s)
			if !done {
				continue
			}
			if !shown {
				heading("So far")
				shown = true
			}
			for range c.IdScope(inst.ids.PrepareStr("sofar-" + s.String())) {
				note(icons.PhCheck + " " + s.short() + ": " + summary)
			}
		}
	}
}

// renderPlanRow is one small line about the plan file: its name in the plan
// store and when it was saved, with Import and Export at hand.
func (inst *App) renderPlanRow() {
	for range c.Horizontal().KeepIter() {
		switch {
		case inst.planName == "" && inst.plan == nil:
			small("No plan yet. One is written when the structure is planned, and kept up to date after every step.")
		case inst.planName == "":
			small("plan not saved")
		default:
			s := "plan " + inst.planName
			if !inst.savedAt.IsZero() {
				s += " · saved " + inst.savedAt.Local().Format("15:04:05")
			}
			if inst.planLocation == "" {
				small(s)
				break
			}
			for range c.HoverText(inst.planLocation).KeepIter() {
				small(s)
			}
		}
		// A click while a gesture runs is dropped rather than hiding the
		// buttons mid-gesture.
		busy := inst.fileJob.Running()
		if c.Button(inst.ids.PrepareStr("import"), c.Atoms().Text("Import…").Keep()).Small().SendResp().HasPrimaryClicked() && !busy {
			inst.startImport()
		}
		if inst.plan != nil {
			if c.Button(inst.ids.PrepareStr("export"), c.Atoms().Text("Export…").Keep()).Small().SendResp().HasPrimaryClicked() && !busy {
				inst.startExport()
			}
		}
		inst.failedNote("file", &inst.fileJob)
	}
}

func (inst *App) renderStatus() {
	switch {
	case inst.lastError != "":
		badge.New(inst.ids.PrepareStr("err"), inst.lastError).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	case inst.note != "":
		badge.New(inst.ids.PrepareStr("note"), inst.note).Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Send()
	}
	for i, s := range inst.stale {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			c.Label("  moved: " + s).Send()
		}
	}
	for i, s := range inst.skipped {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(1000 + i))) {
			small("skipped " + s)
		}
	}
}

// --- the bottom bar -------------------------------------------------------------

// footerFor is each page's bottom bar.
func (inst *App) footerFor(st stepE) (ft footer) {
	switch st {
	case stepConnect:
		ft.primary = func() {
			if inst.actionButton("discover", "Discover the servers", inst.discoverJob.Running()) {
				inst.startDiscover()
			}
		}
		ft.next, ft.hasNext = stepDatabases, true
	case stepDatabases:
		ft.primary = func() {
			busy := inst.structureJob.Running() || inst.disc == nil
			if inst.actionButton("plan-structure", "Plan the structure", busy) {
				inst.startStructure()
			}
		}
		ft.next, ft.hasNext = stepStructure, true
	case stepStructure:
		ft.primary = inst.renderApplyControls
		ft.next, ft.hasNext = stepDifferences, true
	case stepDifferences:
		ft.primary = func() {
			if inst.actionButton("diff", "Compare content", inst.plan == nil || inst.diffJob.Running() || inst.syncJob.Running()) {
				inst.startDiff()
			}
		}
		ft.next, ft.hasNext = stepSync, true
	case stepSync:
		ft.primary = inst.renderStartControls
		ft.next, ft.hasNext = stepRun, true
	case stepRun:
		ft.primary = func() {
			if inst.actionButton("compare-again", "Compare content again", inst.plan == nil || inst.syncJob.Running() || inst.diffJob.Running()) {
				inst.step = stepDifferences
				inst.startDiff()
			}
		}
	}
	return
}

// renderFooter is the classic wizard bar: Back, the page's action and Next
// at the right edge. The row is laid out right to left, so Next is drawn
// first.
func (inst *App) renderFooter(ft footer) {
	for range c.UiWithLayout().MainDirRightToLeft().KeepIter() {
		if ft.hasNext {
			locked := inst.stepLocked(ft.next)
			for range c.HorizontalTop().KeepIter() {
				if locked != "" {
					c.UiDisable()
				}
				label := "Next: " + ft.next.short() + " " + icons.PhArrowRight
				clicked := false
				if locked != "" {
					for range c.HoverText(locked).KeepIter() {
						clicked = c.Button(inst.ids.PrepareStr("next"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked()
					}
				} else {
					clicked = c.Button(inst.ids.PrepareStr("next"), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked()
				}
				if clicked && locked == "" {
					inst.step = ft.next
				}
			}
		}
		if ft.primary != nil {
			for range c.UiWithLayout().MainDirRightToLeft().KeepIter() {
				ft.primary()
			}
		}
		if inst.step > stepConnect {
			prev := inst.step - 1
			if c.Button(inst.ids.PrepareStr("back"), c.Atoms().Text(icons.PhArrowLeft+" "+prev.short()).Keep()).SendResp().HasPrimaryClicked() {
				inst.goTo(prev)
			}
		}
	}
}

// --- helpers -----------------------------------------------------------------

// weak is a dimmed line; it does not wrap, so it is safe in a grid cell.
func weak(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Weak().End().Keep()).Send()
}

// note is a dimmed paragraph that wraps to the width it is given.
func note(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Weak().End().Keep()).Wrap().Send()
}

func small(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Weak().Small().End().Keep()).Send()
}

func heading(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Strong().End().Keep()).Send()
}

// space is a small vertical gap between blocks.
func space() {
	c.LabelAtoms(c.Atoms().BeginRichText(" ").Small().End().Keep()).Send()
}

func mono(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Monospace().End().Keep()).Send()
}

// pageHeader opens a page with the question it answers; the brief beside
// the page says how.
func pageHeader(title string) {
	c.LabelAtoms(c.Atoms().BeginRichText(title).Heading().End().Keep()).Send()
	space()
}

// card is a framed block for a summary the operator should read before a
// click; it inherits the group preset.
func (inst *App) card(key string) (it func(func() bool)) {
	fr := c.Frame(inst.ids.PrepareStr(key)).PresetGroup().InnerMargin(10)
	return func(body func() bool) {
		for range fr.KeepIter() {
			body()
		}
	}
}

func plural(n int, word string) (s string) {
	s = strconv.Itoa(n) + " " + word
	if n != 1 {
		s += "s"
	}
	return
}

func parseFraction(s string) (num uint32, den uint32, err error) {
	a, b, found := strings.Cut(s, "/")
	var n, d uint64
	if found {
		n, err = strconv.ParseUint(strings.TrimSpace(a), 10, 32)
		if err == nil {
			d, err = strconv.ParseUint(strings.TrimSpace(b), 10, 32)
		}
	}
	if !found || err != nil || n == 0 || d == 0 || n > d {
		return 0, 0, eh.Errorf("the sample must be a fraction num/den with 0 < num <= den")
	}
	return uint32(n), uint32(d), nil
}

// actionButton draws a button that is disabled while busy; disabled, it
// still takes its place in the layout.
func (inst *App) actionButton(key string, label string, busy bool) (clicked bool) {
	for range c.HorizontalTop().KeepIter() {
		if busy {
			c.UiDisable()
		}
		clicked = c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() && !busy
	}
	return
}

// jobRow draws a job's progress row while it runs; cancelKey names the
// row's id scope and, when empty, leaves the row without a Cancel.
func (inst *App) jobRow(job bgjobrow.JobI, title string, cancelKey string, rateUnit string) (running bool) {
	if job.Snapshot().State != bgjob.StateRunning {
		return false
	}
	return bgjobrow.Render(bgjobrow.Input{
		Job: job, Ids: inst.ids, ScopeKey: cancelKey, Cancel: cancelKey != "",
		Title: title, RateUnit: rateUnit,
	}).Running
}

func (inst *App) failedNote(key string, job interface{ Snapshot() bgjob.Snapshot }) {
	if snap := job.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		badge.New(inst.ids.PrepareStr(key), "failed: "+snap.Err.Error()).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	}
}

func verdictTone(v jk.VerdictE) (t badge.ToneE) {
	switch v {
	case jk.VerdictIdentical, jk.VerdictNarrower:
		return badge.ToneSuccess
	case jk.VerdictCreate, jk.VerdictExtend:
		return badge.ToneInfo
	case jk.VerdictIncompatible:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// verdictGloss is the one line a hover shows for a verdict.
func verdictGloss(v jk.VerdictE) (s string) {
	switch v {
	case jk.VerdictIdentical:
		return "the target holds this table with the same columns and key"
	case jk.VerdictNarrower:
		return "the target holds this table with fewer columns; the shared ones sync"
	case jk.VerdictCreate:
		return "the target does not hold this table; the DDL creates it"
	case jk.VerdictExtend:
		return "the target holds this table with fewer columns; the DDL adds the missing ones"
	case jk.VerdictIncompatible:
		return "the target holds this table with a different key or column types; it cannot sync"
	case jk.VerdictUnsupported:
		return "views, dictionaries and non-table engines are not synced"
	}
	return v.String()
}

// tableGloss is the hover text of a table's verdict badge: the gloss, then
// the reasons and notes the engine recorded.
func tableGloss(t *jk.PlanTable) (s string) {
	parts := []string{verdictGloss(t.Verdict)}
	parts = append(parts, t.Reasons...)
	parts = append(parts, t.Notes...)
	return strings.Join(parts, "\n")
}

// cliHint shows the CLI that does what the page does, for the run someone
// automates afterwards. It is a selectable line, so it can be copied.
func (inst *App) cliHint(cmd string) {
	space()
	for range c.CollapsingHeader(inst.ids.PrepareStr("cli"), c.WidgetText().Text("The same step from a shell").Keep()).KeepIter() {
		plan := inst.planLocation
		if plan == "" {
			plan = "<plan.json>"
		}
		mono("boxer jackstay " + strings.ReplaceAll(cmd, "{plan}", plan))
	}
}

func onTarget(inv *jk.Inventory, db string) (s string) {
	if inv == nil {
		return ""
	}
	if !inv.HasDatabase(db) {
		return "new"
	}
	n := 0
	for i := range inv.Tables {
		if inv.Tables[i].Ref.Database == db {
			n++
		}
	}
	return "exists, " + plural(n, "table")
}

// --- 1 Connect -----------------------------------------------------------------

func passwordLine(name string, set bool) (s string) {
	if set {
		return "from " + name + " (set)"
	}
	return "from " + name + " (not set)"
}

func (inst *App) renderConnect() {
	pageHeader("Which two servers?")
	if len(inst.recent) > 0 {
		inst.renderRecent()
	}
	heading("New sync")
	for range c.Grid(inst.ids.PrepareStr("endpoints")).NumColumns(3).KeepIter() {
		c.Label("").Send()
		heading("source")
		heading("target")
		c.EndRow()
		c.Label("server").Send()
		c.TextEdit(inst.ids.PrepareStr("src-url"), inst.srcURL, false).HintText("localhost:8123").DesiredWidth(320).SendRespVal(&inst.srcURL)
		c.TextEdit(inst.ids.PrepareStr("dst-url"), inst.dstURL, false).HintText("replica:8123").DesiredWidth(320).SendRespVal(&inst.dstURL)
		c.EndRow()
		c.Label("user").Send()
		c.TextEdit(inst.ids.PrepareStr("src-user"), inst.srcUser, false).DesiredWidth(320).SendRespVal(&inst.srcUser)
		c.TextEdit(inst.ids.PrepareStr("dst-user"), inst.dstUser, false).DesiredWidth(320).SendRespVal(&inst.dstUser)
		c.EndRow()
		c.Label("password").Send()
		weak(passwordLine("CLICKHOUSE_PASSWORD", clickhouseenv.Password.Get() != ""))
		weak(passwordLine("BOXER_JACKSTAY_TARGET_PASSWORD", jk.TargetPasswordEnv.Get() != ""))
		c.EndRow()
	}
	small("A server is an HTTP URL or host:port. Passwords come from the environment only and are never written to the plan.")
	inst.jobRow(&inst.discoverJob, "discovering", "cancel-discover", "")
	inst.failedNote("discover-failed", &inst.discoverJob)
	if inst.disc == nil {
		if inst.plan != nil {
			space()
			note("A plan is open; its servers are filled in above. Discover them again to change the databases, or go on to Structure.")
		}
		return
	}
	space()
	inst.card("discovered")(func() bool {
		for _, side := range []struct {
			role string
			ep   jk.Endpoint
			inv  *jk.Inventory
		}{{"source", inst.disc.srcEp, &inst.disc.src}, {"target", inst.disc.dstEp, &inst.disc.dst}} {
			c.Label(fmt.Sprintf("%s  %s — ClickHouse %s, %s, up %s", side.role, hostLabel(side.ep.URL), side.inv.Server.Version,
				plural(len(side.inv.UserDatabases()), "database"),
				progressest.FormatDuration(durationSeconds(side.inv.Server.UptimeSeconds)))).Send()
		}
		switch {
		case inst.disc.same():
			note("Both are one server, so the target databases will be renamed copies; the next step proposes the names.")
		case inst.disc.src.Server.Version != inst.disc.dst.Server.Version:
			note("The servers run different releases. jackstay assumes close versions: the row digests must agree on both.")
		}
		return true
	})
	inst.cliHint("discover --plan {plan}")
}

// renderRecent lists the plans this window used before, newest first.
func (inst *App) renderRecent() {
	heading("Resume a plan")
	small("A plan file records what each step found and did. Opening one lands on the step it was left at.")
	for range c.Grid(inst.ids.PrepareStr("recent")).NumColumns(4).Striped(true).KeepIter() {
		for i, r := range inst.recent {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				if c.Button(inst.ids.PrepareStr("open"), c.Atoms().Text(r.Name).Keep()).Frame(false).SendResp().HasPrimaryClicked() && !inst.fileJob.Running() {
					inst.startOpen(r.Name, false)
				}
				c.Label(r.Source + " → " + r.Target).Send()
				c.Label("at " + r.Step).Send()
				weak(r.At.Local().Format("Jan 2 15:04"))
				c.EndRow()
			}
		}
	}
	space()
}

// --- 2 Databases -----------------------------------------------------------------

type dbStat struct {
	tables      int
	rows, bytes uint64
}

func (inst *App) dbStats() (stats map[string]dbStat) {
	stats = make(map[string]dbStat, len(inst.disc.src.Databases))
	for i := range inst.disc.src.Tables {
		t := &inst.disc.src.Tables[i]
		s := stats[t.Ref.Database]
		s.tables++
		s.rows += t.TotalRows
		s.bytes += t.TotalBytes
		stats[t.Ref.Database] = s
	}
	return
}

func (inst *App) renderDatabases() {
	if inst.disc == nil {
		pageHeader("Which databases?")
		note("Discover the servers first.")
		return
	}
	pageHeader("Which databases, and where do they land?")
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("all"), c.Atoms().Text("Select all").Keep()).Small().SendResp().HasPrimaryClicked() {
			for _, p := range inst.dbPick {
				*p = true
				c.CurrentApplicationState.StateManager.OverrideDatabindingBPtr(p)
			}
		}
		if c.Button(inst.ids.PrepareStr("none"), c.Atoms().Text("Select none").Keep()).Small().SendResp().HasPrimaryClicked() {
			for _, p := range inst.dbPick {
				*p = false
				c.CurrentApplicationState.StateManager.OverrideDatabindingBPtr(p)
			}
		}
		small(plural(inst.pickedCount(), "database") + " chosen")
	}
	stats := inst.dbStats()
	for range c.Grid(inst.ids.PrepareStr("databases")).NumColumns(5).Striped(true).KeepIter() {
		heading("source database")
		heading("tables")
		heading("size")
		heading("target database")
		heading("on the target")
		c.EndRow()
		for _, db := range inst.disc.src.UserDatabases() {
			for range c.IdScope(inst.ids.PrepareStr("db-" + db)) {
				pick := inst.dbPick[db]
				c.Checkbox(inst.ids.PrepareStr("pick"), *pick, db).SendRespVal(pick)
				s := stats[db]
				c.Label(strconv.Itoa(s.tables)).Send()
				c.Label(fmt.Sprintf("%d rows, %s", s.rows, progressest.FormatBytes(int64(s.bytes)))).Send()
				for range c.HorizontalTop().KeepIter() {
					c.UiSetMinWidth(240)
					c.TextEdit(inst.ids.PrepareStr("target"), *inst.dbTarget[db], false).DesiredWidth(240).SendRespVal(inst.dbTarget[db])
				}
				target := strings.TrimSpace(*inst.dbTarget[db])
				if target == "" {
					target = db
				}
				c.Label(onTarget(&inst.disc.dst, target)).Send()
				c.EndRow()
			}
		}
	}
	space()
	for range c.HoverText("Leeway tables are classified through the data catalog (ADR-0170); other tables are synced as plain rows.").KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("leeway-only"), inst.leewayOnly, "only tables that classify as leeway").SendRespVal(&inst.leewayOnly)
	}
	note("Planning the structure judges every table of the chosen databases against the target and writes the plan file. It changes nothing on either server.")
	inst.cliHint(inst.structureCommand())
}

// structureCommand is the CLI spelling of the Databases page's choices.
func (inst *App) structureCommand() (cmd string) {
	cmd = "structure --plan {plan}"
	sel := inst.selection()
	for _, db := range sel.Databases {
		cmd += " --database " + db
		if t, has := sel.DatabaseMap[db]; has {
			cmd += " --map " + db + "=" + t
		}
	}
	if sel.LeewayOnly {
		cmd += " --leeway-only"
	}
	return
}

// --- 3 Structure -----------------------------------------------------------------

// ddlSummary describes the pending DDL in the operator's terms.
func (inst *App) ddlSummary() (statements int, s string) {
	p := inst.plan
	dbs := len(p.DatabaseDDL)
	creates, extends := 0, 0
	for _, t := range p.Tables {
		if len(t.DDL) == 0 {
			continue
		}
		statements += len(t.DDL)
		if t.Verdict == jk.VerdictCreate {
			creates++
		} else {
			extends++
		}
	}
	statements += dbs
	parts := make([]string, 0, 3)
	if dbs > 0 {
		parts = append(parts, "create "+plural(dbs, "database"))
	}
	if creates > 0 {
		parts = append(parts, "create "+plural(creates, "table"))
	}
	if extends > 0 {
		parts = append(parts, "add columns to "+plural(extends, "table"))
	}
	s = strings.Join(parts, ", ")
	return
}

func (inst *App) renderStructure() {
	inst.jobRow(&inst.structureJob, "structure", "cancel-structure", "")
	inst.failedNote("structure-failed", &inst.structureJob)
	if inst.plan == nil {
		pageHeader("What is on each side?")
		note("Choose databases and plan the structure, or open a plan.")
		return
	}
	p := inst.plan
	pageHeader("What is on each side?")
	counts := p.CountVerdicts()
	for range c.HorizontalTop().KeepIter() {
		c.Label(plural(len(p.Tables), "table") + ":").Send()
		for _, v := range jk.AllVerdicts {
			if counts[v] > 0 {
				badge.New(inst.ids.PrepareStr("count-"+v.String()), fmt.Sprintf("%d %s", counts[v], v)).
					Tone(verdictTone(v)).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(verdictGloss(v)).Send()
			}
		}
	}
	for _, n := range p.Notes {
		note("note: " + n)
	}
	if n, s := inst.ddlSummary(); n > 0 {
		inst.card("ddl")(func() bool {
			heading(plural(n, "statement") + " would " + s + " on " + hostLabel(p.Target.URL))
			note("Nothing runs until you apply it below. Tables with a create or extend verdict cannot be compared or synced before that.")
			return true
		})
	}
	space()
	for range c.Grid(inst.ids.PrepareStr("tables")).NumColumns(6).Striped(true).KeepIter() {
		heading("verdict")
		heading("source")
		heading("target")
		heading("rows")
		heading("size")
		heading("leeway")
		c.EndRow()
		for i := range p.Tables {
			t := &p.Tables[i]
			for range c.IdScope(inst.ids.PrepareStr("t-" + t.Source.String())) {
				badge.New(inst.ids.PrepareStr("v"), t.Verdict.String()).Tone(verdictTone(t.Verdict)).Variant(badge.VariantSoft).Size(badge.SizeSm).
					Tooltip(tableGloss(t)).Send()
				if c.Button(inst.ids.PrepareStr("src"), c.Atoms().Text(t.Source.String()).Keep()).Frame(false).
					Selected(inst.selected == t.Source).SendResp().HasPrimaryClicked() {
					inst.selected = t.Source
				}
				c.Label(t.Target.String()).Send()
				c.Label(strconv.FormatUint(t.Rows, 10)).Send()
				c.Label(progressest.FormatBytes(int64(t.Bytes))).Send()
				lw := "—"
				if t.Leeway {
					lw = "yes"
					if t.LeewayRelation != "" {
						lw += " (" + t.LeewayRelation + ")"
					}
				}
				c.Label(lw).Send()
				c.EndRow()
			}
		}
	}
	inst.renderSelectedStructure()
	inst.cliHint("apply-ddl --plan {plan} --confirm")
}

func (inst *App) pendingStatements() (n int) {
	n = len(inst.plan.DatabaseDDL)
	for _, t := range inst.plan.Tables {
		n += len(t.DDL)
	}
	return
}

// renderApplyControls is the Structure page's footer action: apply the DDL,
// behind a second click that names the target.
func (inst *App) renderApplyControls() {
	if inst.plan == nil {
		return
	}
	n := inst.pendingStatements()
	if n == 0 {
		small("the target's structure is in line")
		return
	}
	busy := inst.structureJob.Running()
	if !inst.applyArmed {
		if inst.actionButton("arm-apply", "Apply the DDL…", busy) {
			inst.applyArmed = true
		}
		return
	}
	badge.New(inst.ids.PrepareStr("armed"), "Run "+plural(n, "statement")+" on "+hostLabel(inst.plan.Target.URL)+"?").
		Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	if inst.actionButton("apply", "Run on the target", busy) {
		inst.startApply()
	}
	if c.Button(inst.ids.PrepareStr("disarm"), c.Atoms().Text("Cancel").Keep()).SendResp().HasPrimaryClicked() {
		inst.applyArmed = false
	}
}

func (inst *App) selectedTable() (pt *jk.PlanTable) {
	if inst.plan == nil {
		return nil
	}
	for i := range inst.plan.Tables {
		if inst.plan.Tables[i].Source == inst.selected {
			return &inst.plan.Tables[i]
		}
	}
	return nil
}

func (inst *App) renderSelectedStructure() {
	pt := inst.selectedTable()
	if pt == nil {
		small("Select a table to see its reasons, notes and DDL.")
		return
	}
	space()
	inst.card("selected")(func() bool {
		for range c.HorizontalTop().KeepIter() {
			heading(pt.Source.String() + " → " + pt.Target.String())
			badge.New(inst.ids.PrepareStr("v"), pt.Verdict.String()).Tone(verdictTone(pt.Verdict)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
		note(verdictGloss(pt.Verdict))
		for i, r := range pt.Reasons {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				c.Label(icons.PhXCircle + " " + r).Wrap().Send()
			}
		}
		for i, n := range pt.Notes {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(100 + i))) {
				c.Label(icons.PhInfo + " " + n).Wrap().Send()
			}
		}
		if n := len(pt.CopyColumns); n > 0 {
			for range c.HoverText(strings.Join(pt.CopyColumns, ", ")).KeepIter() {
				small(plural(n, "column") + " copied and hashed, in source order; hover for the list")
			}
		}
		if pt.Verdict.IsSyncable() {
			inst.renderFilter(pt)
		}
		if len(pt.DDL) > 0 {
			space()
			heading("DDL that would run on the target")
			for i, sql := range pt.DDL {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(200 + i))) {
					c.CodeView(inst.ids.PrepareStr("ddl"), codeview.PrepareSql(sql+";")).Wrap().Send()
				}
			}
		}
		return true
	})
}

// renderFilter edits a table's row filter. A filter changes which rows every
// later step reads on both servers, so it takes effect by planning the
// structure again, which also checks it (ADR-0271 §SD1).
func (inst *App) renderFilter(pt *jk.PlanTable) {
	space()
	heading("Row filter")
	note("Only the rows this expression selects are compared and synced, on both servers; target rows outside it are left alone. Empty means every row.")
	text := inst.filterText(pt)
	c.TextEdit(inst.ids.PrepareStr("filter"), *text, false).HintText("tenant = 'a' AND ts >= toDateTime('2026-01-01', 'UTC')").
		DesiredWidth(480).SendRespVal(text)
	changed := strings.TrimSpace(*text) != pt.Filter
	switch {
	case inst.disc == nil:
		if changed {
			small("connect to the servers to plan with this filter")
		}
	case changed:
		if inst.actionButton("apply-filter", "Plan with this filter", inst.structureJob.Running()) {
			inst.startStructure()
		}
	}
}

func fmtWhen(t time.Time) (s string) {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("Jan 2 15:04")
}
