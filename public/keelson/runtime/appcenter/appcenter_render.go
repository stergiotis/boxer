package appcenter

import (
	"fmt"
	"strings"
	"time"

	playlaunch "github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/observability/humanfmt"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// leftWidth is the app list's first width; the panel is resizable.
const leftWidth = 300

// appStateManagerId is the window the state lens hands deletes to
// (ADR-0260 §SD4). Named by id rather than imported: the manager lives
// under apps/, above the runtime, and the link shows only when the list
// says it is registered.
const appStateManagerId = "github.com/stergiotis/boxer/apps/appstate"

// eventRowsShown bounds the event list; the counts above it cover every row
// read.
const eventRowsShown = 60

func (inst *App) render() {
	snap := inst.snapshot()
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.renderStatus(snap)
	}
	for range c.PanelLeftInside(inst.ids.PrepareStr("apps")).Resizable(true).DefaultSize(leftWidth).KeepIter() {
		c.TextEdit(inst.ids.PrepareStr("filter"), inst.filter, false).
			HintText("filter apps").
			SendRespVal(&inst.filter)
		c.AddSpace(styletokens.GapItems(inst.density))
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderList(snap)
		}
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderPage(snap)
		}
	}
}

func (inst *App) renderStatus(snap snapshot) {
	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("refresh"), c.Atoms().Text(icons.PhArrowClockwise+" Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.refreshAll()
		}
		switch {
		case !snap.reads && snap.lastError != "":
			c.Label(snap.lastError).Send()
		case snap.global.read.IsZero():
			c.Label("Reading the introspection tables…").Send()
		default:
			c.Label(fmt.Sprintf("%d apps registered", len(snap.global.apps.cols.Id))).Send()
		}
		if snap.openNote != "" {
			badge.New(inst.ids.PrepareStr("open-note"), snap.openNote).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
		}
	}
}

func (inst *App) renderList(snap snapshot) {
	cols := &snap.global.apps.cols
	if !inst.lensReady("apps-state", snap.global.apps.state, snap.global.apps.note, len(cols.Id), "No app is registered.") {
		return
	}
	idx := matchingApps(cols, inst.filter)
	if len(idx) == 0 {
		c.Label("No app matches the filter.").Send()
		return
	}
	for _, i := range idx {
		id := cols.Id[i]
		for range c.IdScope(inst.ids.PrepareStr(id)) {
			clicked := false
			for range c.HoverText(id).KeepIter() {
				label := cols.Icon[i] + " " + displayOf(cols, i)
				clicked = c.SelectableLabel(inst.ids.PrepareStr("row"), inst.selected == id, label).SendResp().HasPrimaryClicked()
			}
			if clicked {
				inst.selectApp(id)
			}
		}
	}
}

// displayOf is the app's display name, or its id's leaf when it has none.
func displayOf(cols *appCols, i int) string {
	if d := cols.Display[i]; d != "" {
		return d
	}
	return shortApp(cols.Id[i])
}

func (inst *App) renderPage(snap snapshot) {
	if inst.selected == "" {
		c.Label("Select an app to see what it did, what it may do, and what it keeps.").Send()
		return
	}
	apps := &snap.global.apps.cols
	i := appIndex(apps, inst.selected)
	inst.renderHead(apps, i)
	p := &snap.page
	if p.appId != inst.selected || p.read.IsZero() {
		c.Label("Reading…").Send()
		return
	}
	adrDir := packageDir(inst.selected, snap.global.coderefs.cols.Pkg)
	play := appIndex(apps, playlaunch.AppId) >= 0
	sec := func(key string, title string, body func()) {
		sql := ""
		if play {
			sql, _ = playQuery(key, inst.selected, adrDir)
		}
		inst.section(key, title, sql, body)
	}
	sec(secRuns, "Runs", func() { inst.renderRuns(p) })
	sec(secLogs, "Logs", func() { inst.renderLogs(p) })
	sec(secAudit, "Audited requests", func() { inst.renderAudit(p) })
	sec(secRun, "This process", func() { inst.renderRun(p) })
	sec(secCaps, "Capabilities", func() { inst.renderCaps(apps, i, p) })
	sec(secState, "Kept state", func() { inst.renderState(apps, p) })
	sec(secCoverage, "Coverage", func() { inst.renderCoverage(&snap.global, p) })
	sec(secAdrs, "ADRs its code cites", func() { inst.renderAdrs(&snap.global) })
	sec(secJobs, "Watchbill jobs", func() { inst.renderJobs(p) })
	sec(secDatasets, "Published datasets", func() { inst.renderDatasets(p) })
	sec(secLlm, "Model calls", func() { inst.renderLlm(p) })
	sec(secTasks, "Tasks", func() { inst.renderTasks(p) })
}

func (inst *App) renderHead(apps *appCols, i int) {
	id := inst.selected
	for range c.HorizontalTop().KeepIter() {
		name := shortApp(id)
		if i >= 0 {
			name = apps.Icon[i] + " " + displayOf(apps, i)
		}
		c.LabelAtoms(c.Atoms().BeginRichText(name).Heading().End().Keep()).Send()
		if i >= 0 {
			badge.New(inst.ids.PrepareStr("kind"), apps.Kind[i]).Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
			badge.New(inst.ids.PrepareStr("surface"), apps.Surface[i]).Tone(badge.ToneInfo).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
	}
	mono(id)
	if i < 0 {
		c.Label("Not a registered app in this process; only its recorded data is shown.").Send()
	} else if s := apps.Summary[i]; s != "" {
		c.Label(s).Send()
	}
	for range c.HorizontalTop().KeepIter() {
		if i >= 0 && c.Button(inst.ids.PrepareStr("open"), c.Atoms().Text(icons.PhArrowSquareOut+" Open").Keep()).SendResp().HasPrimaryClicked() {
			inst.openApp(id)
		}
	}
	c.AddSpace(styletokens.PaddingInner(inst.density))
}

// section draws one lens under a header, open by default. A non-empty
// playSql puts an "Open in play" action above the lens: the same table,
// whole and live, in a playground.
func (inst *App) section(key string, title string, playSql string, body func()) {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-"+key), c.WidgetText().Text(title).Keep()).DefaultOpen(true).KeepIter() {
		if playSql != "" {
			for range c.HoverText(playSql).KeepIter() {
				if c.Button(inst.ids.PrepareStr("play-"+key), c.Atoms().Text(icons.PhDatabase+" Open in play").Keep()).Small().SendResp().HasPrimaryClicked() {
					inst.openInPlay(playSql, playTab(key))
				}
			}
		}
		body()
	}
}

// lensReady says why a lens shows nothing, and reports whether it has rows
// to draw.
func (inst *App) lensReady(key string, state lensStateE, note string, rows int, empty string) bool {
	switch state {
	case lensStateUnread:
		c.Label("Reading…").Send()
		return false
	case lensStateRefused:
		// The service answers an unregistered table and an engine failure
		// alike, as a refusal with a reason; only the first means the host
		// does not serve the lens.
		if strings.Contains(note, "unknown keelson table") {
			weak("This host does not serve it.")
		} else {
			badge.New(inst.ids.PrepareStr(key+"-refused"), "not answered: "+note).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
		}
		return false
	case lensStateFailed:
		badge.New(inst.ids.PrepareStr(key+"-err"), "read failed: "+note).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
		return false
	}
	if rows == 0 {
		weak(empty)
		return false
	}
	return true
}

func (inst *App) renderRun(p *page) {
	ev := &p.events.cols
	if !inst.lensReady("events", p.events.state, p.events.note, len(ev.Kind), "Nothing recorded for it in this process.") {
		return
	}
	weak("Everything the runtime recorded for it in this process, state writes included.")
	for range c.HorizontalWrapped().KeepIter() {
		for _, kc := range countKinds(ev) {
			badge.New(inst.ids.PrepareStr("kc-"+kc.kind), fmt.Sprintf("%s ×%d", kc.kind, kc.count)).Tone(badge.ToneNeutral).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
	}
	n := min(len(ev.Kind), eventRowsShown)
	inst.grid("events-grid", []string{"time", "kind", "window", "detail"}, n, func(j int) {
		mono(time.UnixMilli(ev.TsMs[j]).Local().Format("15:04:05.000"))
		c.Label(ev.Kind[j]).Send()
		mono(windowOf(ev.InstanceKey[j]))
		c.Label(ev.Detail[j]).Send()
	})
	if len(ev.Kind) > n {
		weak(fmt.Sprintf("… and %d earlier", len(ev.Kind)-n))
	}
}

// lookBackNote is the bound every cross-run lens reads under (ADR-0260
// §SD5), said once per section so a short list is not read as the whole
// history.
const lookBackNote = "Across processes, the last 30 days."

// timeOfMs is epoch milliseconds as local time, "—" for 0 (no such row).
func timeOfMs(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
}

// shortRun is a run id's head; the whole id is in the logs' own table.
func shortRun(runId string) string {
	if len(runId) > 8 {
		return runId[:8]
	}
	return runId
}

func (inst *App) renderRuns(p *page) {
	rc := &p.runs.cols
	if !inst.lensReady("runs", p.runs.state, p.runs.note, len(rc.RunId), "No window of it opened in the last 30 days.") {
		return
	}
	s := summarizeRuns(rc)
	head := fmt.Sprintf("%d session(s)", s.sessions)
	if s.sessions >= runsRead {
		head = fmt.Sprintf("Its newest %d sessions", s.sessions)
	}
	c.Label(fmt.Sprintf("%s in %d process(es), %d without a close; last active %s. %s",
		head, s.runs, s.open, timeOfMs(s.lastMs), lookBackNote)).Send()
	n := min(len(rc.RunId), runRowsShown)
	inst.grid("runs-grid", []string{"started", "stopped", "lasted", "run", "window", "reason"}, n, func(j int) {
		mono(timeOfMs(rc.StartedMs[j]))
		if rc.StoppedMs[j] == 0 && rc.RunSeenMs[j] > rc.StartedMs[j] {
			for range c.HoverText("No close was recorded; this is the last heartbeat of its process.").KeepIter() {
				weak("seen " + timeOfMs(rc.RunSeenMs[j]))
			}
		} else {
			mono(timeOfMs(rc.StoppedMs[j]))
		}
		mono(sessionLength(rc.StartedMs[j], rc.StoppedMs[j], rc.RunSeenMs[j]))
		mono(shortRun(rc.RunId[j]))
		mono(windowOf(rc.InstanceKey[j]))
		c.Label(rc.StopReason[j]).Send()
	})
	if len(rc.RunId) > n {
		weak(fmt.Sprintf("… and %d earlier", len(rc.RunId)-n))
	}
}

// runRowsShown bounds the session list; the summary covers every row read.
const runRowsShown = 15

// logRowsShown bounds the log list; the level counts cover every row read.
const logRowsShown = 50

func (inst *App) renderLogs(p *page) {
	lc := &p.logs.cols
	if !inst.lensReady("logs", p.logs.state, p.logs.note, len(lc.TsMs), "No log row attributed to it in the last 30 days.") {
		return
	}
	weak(fmt.Sprintf("Its newest %d rows. %s Fields and stacks are not shown.", len(lc.TsMs), lookBackNote))
	for range c.HorizontalWrapped().KeepIter() {
		for _, kc := range countValues(lc.Level) {
			badge.New(inst.ids.PrepareStr("lv-"+kc.kind), fmt.Sprintf("%s ×%d", kc.kind, kc.count)).Tone(levelTone(kc.kind)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
	}
	n := min(len(lc.TsMs), logRowsShown)
	inst.grid("logs-grid", []string{"time", "level", "message", "error", "run", "window"}, n, func(j int) {
		mono(timeOfMs(lc.TsMs[j]))
		badge.New(inst.ids.PrepareStr("lv"), lc.Level[j]).Tone(levelTone(lc.Level[j])).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		for range c.HoverText(lc.Caller[j]).KeepIter() {
			c.Label(lc.Message[j]).Send()
		}
		c.Label(lc.Error[j]).Send()
		mono(shortRun(lc.RunId[j]))
		mono(windowOf(lc.InstanceKey[j]))
	})
	if len(lc.TsMs) > n {
		weak(fmt.Sprintf("… and %d older", len(lc.TsMs)-n))
	}
}

// levelTone colours a log level; an unknown one is neutral.
func levelTone(level string) badge.ToneE {
	switch level {
	case "error", "fatal", "panic":
		return badge.ToneError
	case "warn":
		return badge.ToneWarning
	case "info":
		return badge.ToneInfo
	}
	return badge.ToneNeutral
}

func (inst *App) renderAudit(p *page) {
	ac := &p.audit.cols
	if !inst.lensReady("audit", p.audit.state, p.audit.note, len(ac.Subject), "No audited request in the last 30 days.") {
		return
	}
	weak("What it asked the bus for, per subject and outcome. " + lookBackNote)
	inst.grid("audit-grid", []string{"subject", "result", "requests", "mean ms", "max ms", "last"}, len(ac.Subject), func(j int) {
		mono(ac.Subject[j])
		if r := ac.Result[j]; r == "ok" {
			c.Label(r).Send() // designlint:ignore=L1 (the audit's own result value)
		} else {
			badge.New(inst.ids.PrepareStr("res"), r).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
		mono(fmt.Sprint(ac.Requests[j]))
		mono(fmt.Sprintf("%.1f", ac.MeanLatencyMs[j]))
		mono(fmt.Sprint(ac.MaxLatencyMs[j]))
		mono(timeOfMs(ac.LastMs[j]))
	})
}

// windowOf is an instance key as a column value; zero is unattributed
// (ADR-0191), not window zero.
func windowOf(key uint64) string {
	if key == 0 {
		return "—"
	}
	return fmt.Sprint(key)
}

func (inst *App) renderCaps(apps *appCols, i int, p *page) {
	strong("Declared by the manifest")
	if i < 0 || len(apps.Caps[i]) == 0 {
		weak("None.")
	} else {
		for _, cp := range apps.Caps[i] {
			mono(cp)
		}
	}
	c.AddSpace(styletokens.GapItems(inst.density))
	strong("Held by its open windows")
	cl := &p.caps.cols
	if !inst.lensReady("caps", p.caps.state, p.caps.note, len(cl.Pattern), "No window of it is open.") {
		return
	}
	inst.grid("caps-grid", []string{"window", "pattern", "direction", "origin", "reason"}, len(cl.Pattern), func(j int) {
		mono(windowOf(cl.InstanceKey[j]))
		mono(cl.Pattern[j])
		c.Label(cl.Direction[j]).Send()
		if cl.Declared[j] {
			c.Label("Manifest").Send()
		} else {
			badge.New(inst.ids.PrepareStr(fmt.Sprintf("granted-%d", j)), "Granted").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
		c.Label(cl.Reason[j]).Send()
	})
}

func (inst *App) renderState(apps *appCols, p *page) {
	st := &p.state.cols
	if inst.lensReady("state", p.state.state, p.state.note, len(st.Key), "It keeps nothing across restarts.") {
		inst.grid("state-grid", []string{"kind", "key", "size", "written"}, len(st.Key), func(j int) {
			c.Label(st.Kind[j]).Send()
			mono(st.Key[j])
			mono(humanfmt.Bytes(uint64(max(st.PayloadBytes[j], 0))))
			mono(st.WrittenAt[j])
		})
	}
	if appIndex(apps, appStateManagerId) >= 0 {
		if c.Button(inst.ids.PrepareStr("open-appstate"), c.Atoms().Text(icons.PhArrowSquareOut+" Manage in App state").Keep()).Small().SendResp().HasPrimaryClicked() {
			inst.openApp(appStateManagerId)
		}
	}
}

func (inst *App) renderCoverage(g *global, p *page) {
	if g.coverage.state == lensStateOk && !coverageActive(&g.coverage.cols) {
		weak("Coverage sampling is off in this process.")
		return
	}
	cv := &p.coverage.cols
	if !inst.lensReady("coverage", p.coverage.state, p.coverage.note, len(cv.PkgPath), "None of its packages is in the coverage profile.") {
		return
	}
	t := sumCoverage(cv)
	c.Label(fmt.Sprintf("%.1f%% of statements (%d/%d), %.1f%% of functions (%d/%d) across %d package(s) — its own package subtree, this process.",
		percent(t.coveredStmts, t.totalStmts), t.coveredStmts, t.totalStmts,
		percent(t.coveredFuncs, t.totalFuncs), t.coveredFuncs, t.totalFuncs, t.pkgs)).Send()
	inst.grid("coverage-grid", []string{"package", "statements", "functions"}, len(cv.PkgPath), func(j int) {
		mono(strings.TrimPrefix(cv.PkgPath[j], p.appId))
		mono(fmt.Sprintf("%.1f%% (%d/%d)", percent(cv.CoveredStmts[j], cv.TotalStmts[j]), cv.CoveredStmts[j], cv.TotalStmts[j]))
		mono(fmt.Sprintf("%.1f%% (%d/%d)", percent(cv.CoveredFuncs[j], cv.TotalFuncs[j]), cv.CoveredFuncs[j], cv.TotalFuncs[j]))
	})
}

func (inst *App) renderAdrs(g *global) {
	if !inst.lensReady("coderef", g.coderefs.state, g.coderefs.note, len(g.coderefs.cols.Pkg), "The citation index is empty — this process was not started in a checkout.") {
		return
	}
	refs, dir := adrsFor(inst.selected, &g.coderefs.cols, &g.adrs.cols)
	if dir == "" {
		weak("Its code is not in the citation index (an applet has no package).")
		return
	}
	if len(refs) == 0 {
		weak("Nothing under " + dir + " cites an ADR.")
		return
	}
	weak("Cited under " + dir + "; a lower bound — code that follows a decision without naming it is not counted.")
	inst.grid("adr-grid", []string{"ADR", "title", "status", "citations"}, len(refs), func(j int) {
		r := refs[j]
		mono(fmt.Sprintf("%04d", r.num))
		c.Label(r.title).Send()
		c.Label(r.status).Send()
		mono(fmt.Sprint(r.refs))
	})
}

func (inst *App) renderJobs(p *page) {
	jb := &p.jobs.cols
	if !inst.lensReady("jobs", p.jobs.state, p.jobs.note, len(jb.Id), "It owns no job.") {
		return
	}
	inst.grid("jobs-grid", []string{"job", "kind", "state", "attempt", "finished", "last error"}, len(jb.Id), func(j int) {
		mono(jb.Id[j])
		c.Label(jb.Kind[j]).Send()
		c.Label(jb.State[j]).Send()
		mono(fmt.Sprint(jb.Attempt[j]))
		mono(jb.FinishedAt[j])
		c.Label(jb.LastError[j]).Send()
	})
}

func (inst *App) renderDatasets(p *page) {
	ds := &p.datasets.cols
	if !inst.lensReady("datasets", p.datasets.state, p.datasets.note, len(ds.Alias), "It publishes no dataset.") {
		return
	}
	inst.grid("datasets-grid", []string{"alias", "handle", "rows", "size", "revision"}, len(ds.Alias), func(j int) {
		mono(ds.Alias[j])
		mono(ds.Handle[j])
		mono(fmt.Sprint(ds.Rows[j]))
		mono(humanfmt.Bytes(ds.Bytes[j]))
		mono(fmt.Sprint(ds.Revision[j]))
	})
}

func (inst *App) renderLlm(p *page) {
	lc := &p.llm.cols
	if !inst.lensReady("llm", p.llm.state, p.llm.note, len(lc.At), "It made no model call in this process.") {
		return
	}
	t := sumLlm(lc)
	c.Label(fmt.Sprintf("%d call(s), %d failed or refused, %d tokens in, %d out.", t.calls, t.failed, t.inputTokens, t.outputTokens)).Send()
	inst.grid("llm-grid", []string{"at", "purpose", "model", "tokens in/out", "elapsed", "outcome"}, len(lc.At), func(j int) {
		mono(lc.At[j])
		c.Label(lc.Purpose[j]).Send()
		mono(lc.Model[j])
		mono(fmt.Sprintf("%d/%d", lc.InputTokens[j], lc.OutputTokens[j]))
		mono((time.Duration(lc.ElapsedMs[j]) * time.Millisecond).String())
		switch {
		case lc.Refused[j]:
			c.Label("Refused").Send()
		case lc.Error[j] != "":
			c.Label(lc.Error[j]).Send()
		default:
			c.Label("Ok").Send()
		}
	})
}

func (inst *App) renderTasks(p *page) {
	tk := &p.tasks.cols
	if !inst.lensReady("tasks", p.tasks.state, p.tasks.note, len(tk.Title), "It runs no task.") {
		return
	}
	inst.grid("tasks-grid", []string{"title", "kind", "state", "created"}, len(tk.Title), func(j int) {
		c.Label(tk.Title[j]).Send()
		c.Label(tk.Kind[j]).Send()
		c.Label(tk.State[j]).Send()
		mono(tk.CreatedAt[j])
	})
}

// grid draws a striped table with a header row; row draws row j's cells.
func (inst *App) grid(key string, headers []string, n int, row func(j int)) {
	for range c.Grid(inst.ids.PrepareStr(key)).NumColumns(uint32(len(headers))).Striped(true).KeepIter() {
		for _, h := range headers {
			strong(h)
		}
		c.EndRow()
		for j := range n {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(j))) {
				row(j)
			}
			c.EndRow()
		}
	}
}

func strong(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Strong().End().Keep()).Send()
}

func mono(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Monospace().End().Keep()).Send()
}

func weak(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Weak().End().Keep()).Send()
}
