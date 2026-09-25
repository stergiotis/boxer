package jackstay

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/hmi/progressest"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/filepicker"
)

func (inst *App) render() {
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.renderStepBar()
		inst.renderPlanRow()
		inst.renderStatus()
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("page-" + inst.step.String())) {
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
				case stepMonitor:
					inst.renderMonitor()
				}
			}
		}
	}
	inst.renderDialogs()
}

func (inst *App) renderStepBar() {
	for range c.HorizontalTop().KeepIter() {
		for _, st := range allSteps {
			if c.Button(inst.ids.PrepareStr("step-"+st.String()), c.Atoms().Text(st.String()).Keep()).
				Selected(inst.step == st).SendResp().HasPrimaryClicked() {
				inst.step = st
			}
		}
	}
}

func (inst *App) renderPlanRow() {
	for range c.Horizontal().KeepIter() {
		path := inst.planPath
		if path == "" {
			path = "(not saved)"
		}
		c.Label("plan: " + path).Send()
		if c.Button(inst.ids.PrepareStr("open"), c.Atoms().Text("Open…").Keep()).SendResp().HasPrimaryClicked() {
			inst.openDlg.Show()
		}
		save := c.Button(inst.ids.PrepareStr("save"), c.Atoms().Text("Save").Keep()).SendResp().HasPrimaryClicked()
		if save && inst.plan != nil {
			if inst.planPath == "" {
				inst.saveDlg.Show()
			} else {
				inst.autosave()
				inst.note = "plan saved"
			}
		}
		if c.Button(inst.ids.PrepareStr("saveas"), c.Atoms().Text("Save as…").Keep()).SendResp().HasPrimaryClicked() && inst.plan != nil {
			inst.saveDlg.Show()
		}
	}
}

func (inst *App) renderStatus() {
	switch {
	case inst.lastError != "":
		badge.New(inst.ids.PrepareStr("err"), inst.lastError).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	case inst.note != "":
		badge.New(inst.ids.PrepareStr("note"), inst.note).Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Send()
	default:
		weak("Passwords come from the environment only and are never written to the plan.")
	}
	for i, s := range inst.stale {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			c.Label("  moved: " + s).Send()
		}
	}
}

func (inst *App) renderDialogs() {
	if action, paths := inst.openDlg.Render(inst.ids); action == filepicker.ActionOpen && len(paths) == 1 {
		p, err := jk.LoadPlan(paths[0])
		if err != nil {
			inst.lastError = err.Error()
		} else {
			inst.plan, inst.planPath = &p, paths[0]
			inst.srcURL, inst.srcUser = p.Source.URL, p.Source.User
			inst.dstURL, inst.dstUser = p.Target.URL, p.Target.User
			inst.note, inst.lastError = "plan opened", ""
			inst.step = stepStructure
		}
	}
	if action, paths := inst.saveDlg.Render(inst.ids); action == filepicker.ActionSave && len(paths) == 1 {
		inst.planPath = paths[0]
		inst.autosave()
		if inst.lastError == "" {
			inst.note = "plan saved"
		}
	}
}

// --- helpers -----------------------------------------------------------------

func weak(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Weak().End().Keep()).Send()
}

func heading(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Strong().End().Keep()).Send()
}

func mono(s string) {
	c.LabelAtoms(c.Atoms().BeginRichText(s).Monospace().End().Keep()).Send()
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

// jobRow draws a job's progress row while it runs. The Cancel id is prepared
// only then: bgjobrow consumes it only for a running job, and a prepared id
// left unconsumed breaks the id stack.
func (inst *App) jobRow(job bgjobrow.JobI, title string, cancelKey string, rateUnit string) (running bool) {
	if job.Snapshot().State != bgjob.StateRunning {
		return false
	}
	in := bgjobrow.Input{Title: title, RateUnit: rateUnit}
	if cancelKey != "" {
		in.CancelId = inst.ids.PrepareStr(cancelKey)
	}
	return bgjobrow.Render(job, in)
}

func failedNote(job interface{ Snapshot() bgjob.Snapshot }) {
	if snap := job.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		c.Label("failed: " + snap.Err.Error()).Wrap().Send()
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

// --- 1 Connect -----------------------------------------------------------------

func setOrNot(v string) (s string) {
	if v == "" {
		return "not set"
	}
	return "set"
}

func (inst *App) renderConnect() {
	heading("Which two servers?")
	weak("A server is an HTTP URL or host:port. The source is read; only the steps you confirm write to the target.")
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
		weak("CLICKHOUSE_PASSWORD, " + setOrNot(clickhouseenv.Password.Get()))
		weak("BOXER_JACKSTAY_TARGET_PASSWORD, " + setOrNot(jk.TargetPasswordEnv.Get()))
		c.EndRow()
	}
	if inst.actionButton("discover", "Discover", inst.discoverJob.Running()) {
		inst.startDiscover()
	}
	inst.jobRow(&inst.discoverJob, "discovering", "cancel-discover", "")
	failedNote(&inst.discoverJob)
	if inst.disc == nil {
		return
	}
	c.Separator().Send()
	for _, side := range []struct {
		role string
		ep   jk.Endpoint
		inv  *jk.Inventory
	}{{"source", inst.disc.srcEp, &inst.disc.src}, {"target", inst.disc.dstEp, &inst.disc.dst}} {
		c.Label(fmt.Sprintf("%s %s: ClickHouse %s, %s on the %s, up %s", side.role, side.ep.URL, side.inv.Server.Version,
			plural(len(side.inv.UserDatabases()), "database"), side.role,
			progressest.FormatDuration(durationSeconds(side.inv.Server.UptimeSeconds)))).Send()
	}
	if inst.disc.src.Server.Version != inst.disc.dst.Server.Version {
		weak("The servers run different releases; jackstay assumes close versions.")
	}
	if c.Button(inst.ids.PrepareStr("to-databases"), c.Atoms().Text("Next: choose databases").Keep()).SendResp().HasPrimaryClicked() {
		inst.step = stepDatabases
	}
}

// --- 2 Databases -----------------------------------------------------------------

type dbStat struct {
	tables, leeway int
	rows, bytes    uint64
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
		weak("Discover the servers first.")
		return
	}
	heading("Which databases, and where do they land?")
	weak("A target name other than the source's renames the database on the target.")
	stats := inst.dbStats()
	for range c.Grid(inst.ids.PrepareStr("databases")).NumColumns(4).Striped(true).KeepIter() {
		heading("source database")
		heading("tables")
		heading("size")
		heading("target database")
		c.EndRow()
		for _, db := range inst.disc.src.UserDatabases() {
			for range c.IdScope(inst.ids.PrepareStr("db-" + db)) {
				pick := inst.dbPick[db]
				c.Checkbox(inst.ids.PrepareStr("pick"), *pick, db).SendRespVal(pick)
				s := stats[db]
				c.Label(strconv.Itoa(s.tables)).Send()
				c.Label(fmt.Sprintf("%d rows, %s", s.rows, progressest.FormatBytes(int64(s.bytes)))).Send()
				c.TextEdit(inst.ids.PrepareStr("target"), *inst.dbTarget[db], false).DesiredWidth(240).SendRespVal(inst.dbTarget[db])
				c.EndRow()
			}
		}
	}
	c.Checkbox(inst.ids.PrepareStr("leeway-only"), inst.leewayOnly, "only tables that classify as leeway").SendRespVal(&inst.leewayOnly)
	if inst.actionButton("plan-structure", "Plan the structure", inst.structureJob.Running()) {
		inst.startStructure()
	}
}

// --- 3 Structure -----------------------------------------------------------------

func (inst *App) renderStructure() {
	inst.jobRow(&inst.structureJob, "structure", "cancel-structure", "")
	failedNote(&inst.structureJob)
	if inst.plan == nil {
		weak("Choose databases and plan the structure, or open a plan.")
		return
	}
	p := inst.plan
	heading(fmt.Sprintf("%s → %s", p.Source.URL, p.Target.URL))
	counts := p.CountVerdicts()
	parts := make([]string, 0, len(jk.AllVerdicts))
	for _, v := range jk.AllVerdicts {
		if counts[v] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[v], v))
		}
	}
	c.Label(fmt.Sprintf("%s: %s", plural(len(p.Tables), "table"), strings.Join(parts, ", "))).Send()
	for _, n := range p.Notes {
		weak("note: " + n)
	}
	inst.renderApply()
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
				badge.New(inst.ids.PrepareStr("v"), t.Verdict.String()).Tone(verdictTone(t.Verdict)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
				if c.Button(inst.ids.PrepareStr("src"), c.Atoms().Text(t.Source.String()).Keep()).Frame(false).
					Selected(inst.selected == t.Source).SendResp().HasPrimaryClicked() {
					inst.selected = t.Source
				}
				c.Label(t.Target.String()).Send()
				c.Label(strconv.FormatUint(t.Rows, 10)).Send()
				c.Label(progressest.FormatBytes(int64(t.Bytes))).Send()
				lw := ""
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
}

func (inst *App) pendingStatements() (n int) {
	n = len(inst.plan.DatabaseDDL)
	for _, t := range inst.plan.Tables {
		n += len(t.DDL)
	}
	return
}

func (inst *App) renderApply() {
	n := inst.pendingStatements()
	if n == 0 {
		return
	}
	busy := inst.structureJob.Running()
	if !inst.applyArmed {
		for range c.Horizontal().KeepIter() {
			c.Label(plural(n, "statement") + " would bring the target in line.").Send()
			if inst.actionButton("arm-apply", "Apply the DDL…", busy) {
				inst.applyArmed = true
			}
		}
		return
	}
	for range c.Horizontal().KeepIter() {
		badge.New(inst.ids.PrepareStr("armed"), "Run "+plural(n, "statement")+" on "+inst.plan.Target.URL+"?").
			Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
		if inst.actionButton("apply", "Run on the target", busy) {
			inst.startApply()
		}
		if c.Button(inst.ids.PrepareStr("disarm"), c.Atoms().Text("Cancel").Keep()).SendResp().HasPrimaryClicked() {
			inst.applyArmed = false
		}
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
		weak("Select a table to see its reasons, notes and DDL.")
		return
	}
	c.Separator().Send()
	heading(pt.Source.String() + " — " + pt.Verdict.String())
	for _, r := range pt.Reasons {
		c.Label("✗ " + r).Wrap().Send()
	}
	for _, n := range pt.Notes {
		c.Label("· " + n).Wrap().Send()
	}
	if len(pt.CopyColumns) > 0 {
		weak("copied and hashed: " + strings.Join(pt.CopyColumns, ", "))
	}
	for i, sql := range pt.DDL {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			mono(sql + ";")
		}
	}
}

// --- 4 Differences ---------------------------------------------------------------

func (inst *App) renderDifferences() {
	if inst.plan == nil {
		weak("Plan the structure first.")
		return
	}
	heading("How do the tables' contents differ?")
	weak("Each side scans every table once for leaf digests; small differing leaves are compared row by row. No row moves.")
	c.Checkbox(inst.ids.PrepareStr("final"), inst.final, "read merge engines (Replacing, Collapsing, …) with FINAL").SendRespVal(&inst.final)
	if inst.actionButton("diff", "Compare content", inst.diffJob.Running() || inst.syncJob.Running()) {
		inst.startDiff()
	}
	inst.jobRow(&inst.diffJob, "comparing", "cancel-diff", "tables")
	failedNote(&inst.diffJob)
	for range c.Grid(inst.ids.PrepareStr("diffs")).NumColumns(3).Striped(true).KeepIter() {
		heading("table")
		heading("content")
		heading("chunks")
		c.EndRow()
		for i := range inst.plan.Tables {
			t := &inst.plan.Tables[i]
			if !t.Verdict.IsSyncable() {
				continue
			}
			for range c.IdScope(inst.ids.PrepareStr("d-" + t.Source.String())) {
				if c.Button(inst.ids.PrepareStr("src"), c.Atoms().Text(t.Source.String()).Keep()).Frame(false).
					Selected(inst.selected == t.Source).SendResp().HasPrimaryClicked() {
					inst.selected = t.Source
				}
				switch {
				case !t.IsDiffable():
					weak("apply the DDL first")
					c.Label("").Send()
				case t.Diff == nil:
					weak("not compared")
					c.Label("").Send()
				case t.Diff.IsIdentical():
					badge.New(inst.ids.PrepareStr("same"), "identical").Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
					c.Label(fmt.Sprintf("%d", t.Diff.Chunks)).Send()
				default:
					d := t.Diff
					s := fmt.Sprintf("%d missing, %d extra, %d changed", d.Missing, d.Extra, d.Changed)
					if d.UnresolvedLeaves > 0 {
						s += fmt.Sprintf(" (+%d leaves not compared row by row)", d.UnresolvedLeaves)
					}
					if d.MaybeSpurious {
						s += " — merge engine read without FINAL"
					}
					badge.New(inst.ids.PrepareStr("differs"), s).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
					c.Label(fmt.Sprintf("%d of %d differ", len(d.Differing), d.Chunks)).Send()
				}
				c.EndRow()
			}
		}
	}
	inst.renderSelectedDiff()
}

func (inst *App) renderSelectedDiff() {
	pt := inst.selectedTable()
	if pt == nil || pt.Diff == nil || pt.Diff.IsIdentical() {
		return
	}
	c.Separator().Send()
	heading(pt.Source.String())
	for i, cd := range pt.Diff.Differing {
		if i == 20 {
			weak(fmt.Sprintf("… %d more differing chunks", len(pt.Diff.Differing)-20))
			break
		}
		label := cd.Display
		if label == "" {
			label = "(whole table)"
		}
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			switch {
			case cd.AbsentOnTarget:
				c.Label(fmt.Sprintf("chunk %s: absent on the target (%d rows)", label, cd.SrcRows)).Send()
			case cd.AbsentOnSource:
				c.Label(fmt.Sprintf("chunk %s: absent on the source (%d target rows)", label, cd.DstRows)).Send()
			default:
				c.Label(fmt.Sprintf("chunk %s: %d / %d rows, %s", label, cd.SrcRows, cd.DstRows, plural(len(cd.Leaves), "differing leaf"))).Send()
			}
		}
	}
	for i, ex := range pt.Diff.Examples {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(1000 + i))) {
			mono(ex.Kind + " " + ex.Key)
		}
	}
}

// --- 5 Sync ------------------------------------------------------------------------

// radio is one option of a choice, drawn as a selectable button: its state is
// the App field the caller compares, so no per-frame bool is bound.
func (inst *App) radio(key string, label string, on bool) (clicked bool) {
	return c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).Selected(on).SendResp().HasPrimaryClicked()
}

func (inst *App) renderSync() {
	if inst.plan == nil {
		weak("Plan the structure first.")
		return
	}
	heading("Copy what, and how?")
	for range c.HorizontalTop().KeepIter() {
		c.Label("mode").Send()
		for _, m := range jk.AllSyncModes {
			if inst.radio("mode-"+m.String(), m.String(), inst.syncMode == m) {
				inst.syncMode = m
			}
		}
		if inst.syncMode == jk.SyncModeSample {
			c.TextEdit(inst.ids.PrepareStr("sample"), inst.sampleText, false).DesiredWidth(80).SendRespVal(&inst.sampleText)
			weak("of the keys")
		}
	}
	switch inst.syncMode {
	case jk.SyncModeRepair:
		var clear, copyRows uint64
		for _, t := range inst.plan.Tables {
			if t.Diff != nil {
				clear += jk.RepairClearRows(t.Diff)
				copyRows += jk.RepairCopyRows(t.Diff)
			}
		}
		weak(fmt.Sprintf("Repair clears %d target rows and copies %d source rows, as of the last diff, and only where the target still holds what the diff showed.", clear, copyRows))
	default:
		for range c.HorizontalTop().KeepIter() {
			c.Label("a target table with rows").Send()
			for _, p := range jk.AllExistingPolicies {
				if inst.radio("existing-"+p.String(), p.String(), inst.existing == p) {
					inst.existing = p
				}
			}
		}
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("on the wire").Send()
		for _, comp := range []string{"zstd", "gzip", "none"} {
			if inst.radio("comp-"+comp, comp, inst.compression == comp || (comp == "none" && inst.compression == "")) {
				inst.compression = comp
				if comp == "none" {
					inst.compression = ""
				}
			}
		}
	}
	c.Checkbox(inst.ids.PrepareStr("restart"), inst.restart, "begin a new run instead of resuming the plan's").SendRespVal(&inst.restart)
	busy := inst.syncJob.Running() || inst.diffJob.Running() || inst.structureJob.Running()
	for range c.HorizontalTop().KeepIter() {
		if inst.actionButton("preflight", "Pre-flight", busy || inst.previewJob.Running()) {
			inst.startPreview()
		}
		if inst.actionButton("sync", "Start the sync", busy) {
			inst.startSync()
		}
	}
	if inst.planPath == "" {
		weak("Save the plan first: the sync keeps its journal beside the plan file.")
	}
	inst.jobRow(&inst.previewJob, "pre-flight", "", "")
	failedNote(&inst.previewJob)
	for i, g := range inst.preflight {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			verdict, tone := "fits", badge.ToneSuccess
			if !g.OK {
				verdict, tone = "may not fit", badge.ToneWarning
			}
			held := ""
			if g.Held > 0 {
				held = ", plus " + progressest.FormatBytes(int64(g.Held)) + " cleared but held until merges"
			}
			c.Label(fmt.Sprintf("disks %s: need about %s%s (%s with headroom), %s free",
				strings.Join(g.Disks, ","), progressest.FormatBytes(int64(g.Need)), held,
				progressest.FormatBytes(int64(g.Headroom)), progressest.FormatBytes(int64(g.Free)))).Send()
			badge.New(inst.ids.PrepareStr("fit"), verdict).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
		}
	}
	failedNote(&inst.syncJob)
}

// --- 6 Monitor ---------------------------------------------------------------------

func (inst *App) renderMonitor() {
	if inst.plan == nil {
		weak("Nothing to monitor yet.")
		return
	}
	if !inst.jobRow(&inst.syncJob, "sync", "cancel-sync", "rows") {
		weak("No sync is running. A stopped sync resumes from its journal when started again.")
	}
	failedNote(&inst.syncJob)
	c.Label(fmt.Sprintf("%d rows landed, %s on the wire", inst.rows.Load(), progressest.FormatBytes(inst.bytes.Load()))).Send()

	if err := inst.demandDisks(); err != nil {
		c.Label("unable to read the target's disks: " + err.Error()).Wrap().Send()
	}
	if d := inst.lastDisks; d != nil {
		heading("target disks")
		for i, disk := range d.Disks {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				used := float32(0)
				if disk.TotalSpace > 0 {
					used = 1 - float32(disk.FreeSpace)/float32(disk.TotalSpace)
				}
				c.ProgressBar(used).Text(fmt.Sprintf("%s: %s free of %s", disk.Name,
					progressest.FormatBytes(int64(disk.FreeSpace)), progressest.FormatBytes(int64(disk.TotalSpace)))).Send()
			}
		}
		heading("footprint (active parts; rows a sync cleared hold space until their parts merge)")
		for range c.Grid(inst.ids.PrepareStr("footprint")).NumColumns(4).Striped(true).KeepIter() {
			heading("target table")
			heading("source")
			heading("target")
			heading("parts")
			c.EndRow()
			for i := range inst.plan.Tables {
				t := &inst.plan.Tables[i]
				fp, has := d.Table(t.Target)
				if !has {
					continue
				}
				for range c.IdScope(inst.ids.PrepareStr("f-" + t.Target.String())) {
					c.Label(t.Target.String()).Send()
					c.Label(progressest.FormatBytes(int64(t.Bytes))).Send()
					c.Label(progressest.FormatBytes(int64(fp.BytesOnDisk))).Send()
					c.Label(strconv.FormatUint(fp.Parts, 10)).Send()
					c.EndRow()
				}
			}
		}
	}
	inst.renderReports()
	inst.renderChunkLog()
}

func (inst *App) renderReports() {
	heading("tables")
	for i := range inst.plan.Tables {
		t := &inst.plan.Tables[i]
		r := t.SyncReport
		if r == nil {
			continue
		}
		for range c.IdScope(inst.ids.PrepareStr("r-" + t.Source.String())) {
			c.Label(fmt.Sprintf("%s: %d copied, %d already done, %d identical, %d stale, %d failed; %d rows, %s",
				t.Source, r.Copied, r.Done, r.Identical, r.Stale, r.Failed, r.Rows, progressest.FormatBytes(int64(r.Bytes)))).Send()
			for j, p := range r.Problems {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(j))) {
					c.Label("  " + p).Wrap().Send()
				}
			}
		}
	}
}

func (inst *App) renderChunkLog() {
	log := inst.chunkLogSnapshot()
	if len(log) == 0 {
		return
	}
	heading("latest chunks")
	slices.Reverse(log)
	for range c.Grid(inst.ids.PrepareStr("chunks")).NumColumns(5).Striped(true).KeepIter() {
		for i, r := range log {
			if i == 40 {
				break
			}
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				c.Label(r.Status.String()).Send()
				c.Label(r.Table).Send()
				label := r.Display
				if label == "" {
					label = r.Chunk
				}
				c.Label(label).Send()
				c.Label(fmt.Sprintf("%d rows, %s", r.Rows, progressest.FormatBytes(int64(r.Bytes)))).Send()
				c.Label(r.Note).Send()
				c.EndRow()
			}
		}
	}
}
