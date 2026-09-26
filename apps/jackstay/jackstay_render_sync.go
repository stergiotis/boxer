package jackstay

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/hmi/progressest"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// --- 4 Differences ---------------------------------------------------------------

func (inst *App) renderDifferences() {
	if inst.plan == nil {
		pageHeader("How do the tables' contents differ?")
		note("Plan the structure first.")
		return
	}
	pageHeader("How do the tables' contents differ?")
	for range c.HoverText("ReplacingMergeTree and its relatives hold rows a merge will drop. FINAL reads each side as if merged, at a cost; without it a difference can be spurious.").KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("final"), inst.final, "read merge engines (Replacing, Collapsing, …) with FINAL").SendRespVal(&inst.final)
	}
	inst.jobRow(&inst.diffJob, "comparing", "cancel-diff", "tables")
	inst.failedNote("diff-failed", &inst.diffJob)
	same, differ := inst.diffCounts()
	notCompared, waiting := 0, 0
	var missing, extra, changed uint64
	for i := range inst.plan.Tables {
		t := &inst.plan.Tables[i]
		if !t.Verdict.IsSyncable() {
			continue
		}
		switch {
		case !t.IsDiffable():
			waiting++
		case t.Diff == nil:
			notCompared++
		case !t.Diff.IsIdentical():
			missing += t.Diff.Missing
			extra += t.Diff.Extra
			changed += t.Diff.Changed
		}
	}
	if same+differ == 0 && waiting > 0 {
		note(plural(waiting, "table") + " cannot be compared before the DDL is applied on the Structure step.")
	}
	if same+differ > 0 {
		inst.card("diff-summary")(func() bool {
			parts := []string{plural(same, "table") + " identical"}
			if differ > 0 {
				parts = append(parts, fmt.Sprintf("%s differ: %d rows missing on the target, %d extra, %d changed", plural(differ, "table"), missing, extra, changed))
			}
			heading(strings.Join(parts, "; "))
			switch {
			case differ > 0:
				note("The Sync step proposes repair, which makes only the differing leaves equal.")
			case notCompared == 0 && waiting == 0:
				note("The target already matches the source for every table compared.")
			}
			if waiting > 0 {
				note(plural(waiting, "table") + " cannot be compared before its DDL is applied.")
			}
			return true
		})
	}
	space()
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
					tip := "rows missing on the target, extra on the target, and present on both with different content"
					if d.UnresolvedLeaves > 0 {
						s += fmt.Sprintf(" (+%d leaves not compared row by row)", d.UnresolvedLeaves)
						tip += "\nsome differing leaves were too large for the row-by-row budget; their rows are not counted"
					}
					if d.MaybeSpurious {
						s += " — merge engine read without FINAL"
						tip += "\na merge engine read without FINAL: rows a merge would drop still count"
					}
					badge.New(inst.ids.PrepareStr("differs"), s).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip(tip).Send()
					c.Label(fmt.Sprintf("%d of %d differ", len(d.Differing), d.Chunks)).Send()
				}
				c.EndRow()
			}
		}
	}
	inst.renderSelectedDiff()
	inst.cliHint("diff --plan {plan} [--final]")
}

func (inst *App) renderSelectedDiff() {
	pt := inst.selectedTable()
	if pt == nil || pt.Diff == nil || pt.Diff.IsIdentical() {
		return
	}
	space()
	inst.card("selected-diff")(func() bool {
		heading(pt.Source.String() + ": the differing chunks")
		for i, cd := range pt.Diff.Differing {
			if i == 20 {
				note(fmt.Sprintf("… %d more differing chunks", len(pt.Diff.Differing)-20))
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
		if len(pt.Diff.Examples) > 0 {
			small("example keys")
			for i, ex := range pt.Diff.Examples {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(1000 + i))) {
					mono(ex.Kind + " " + ex.Key)
				}
			}
		}
		return true
	})
}

// --- 5 Sync ------------------------------------------------------------------------

// radio is one option of a choice, drawn as a selectable button: its state is
// the App field the caller compares, so no per-frame bool is bound.
func (inst *App) radio(key string, label string, on bool, tip string) (clicked bool) {
	for range c.HoverText(tip).KeepIter() {
		clicked = c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).Selected(on).SendResp().HasPrimaryClicked()
	}
	return
}

func modeGloss(m jk.SyncModeE) (s string) {
	switch m {
	case jk.SyncModeFull:
		return "copy every chunk of every table"
	case jk.SyncModeRepair:
		return "copy only the leaves the last comparison found differing, after clearing them on the target"
	case jk.SyncModeSample:
		return "copy a fraction of the keys, for a smaller stand-in of the data"
	}
	return m.String()
}

func existingGloss(p jk.ExistingPolicyE) (s string) {
	switch p {
	case jk.ExistingPolicyRefuse:
		return "skip a target table that already holds rows"
	case jk.ExistingPolicyAppend:
		return "insert into a target table that already holds rows, keeping what is there"
	case jk.ExistingPolicyReplace:
		return "delete a target table's rows before copying; the space is held until its parts merge"
	}
	return p.String()
}

func (inst *App) renderSync() {
	if inst.plan == nil {
		pageHeader("Copy what, and how?")
		note("Plan the structure first.")
		return
	}
	p := inst.plan
	pageHeader("Copy what, and how?")
	recMode, why := inst.recommendMode()
	for range c.HorizontalTop().KeepIter() {
		c.Label("mode").Send()
		for _, m := range jk.AllSyncModes {
			label := m.String()
			if m == recMode {
				label += " (recommended)"
			}
			if inst.radio("mode-"+m.String(), label, inst.syncMode == m, modeGloss(m)) {
				inst.syncMode = m
				inst.syncModeChosen = true
			}
		}
		if inst.syncMode == jk.SyncModeSample {
			c.TextEdit(inst.ids.PrepareStr("sample"), inst.sampleText, false).DesiredWidth(80).SendRespVal(&inst.sampleText)
			weak("of the keys")
		}
	}
	note("Recommended: " + recMode.String() + ", because " + why + ".")
	switch inst.syncMode {
	case jk.SyncModeRepair:
		var clear, copyRows uint64
		for _, t := range p.Tables {
			if t.Diff != nil {
				clear += jk.RepairClearRows(t.Diff)
				copyRows += jk.RepairCopyRows(t.Diff)
			}
		}
		note(fmt.Sprintf("Repair clears %d target rows and copies %d source rows, as of the last comparison, and only where the target still holds what it showed.", clear, copyRows))
	default:
		for range c.HorizontalTop().KeepIter() {
			c.Label("if a target table already holds rows").Send()
			for _, pol := range jk.AllExistingPolicies {
				if inst.radio("existing-"+pol.String(), pol.String(), inst.existing == pol, existingGloss(pol)) {
					inst.existing = pol
				}
			}
		}
		if n := inst.existingTargets(); n > 0 {
			note(plural(n, "target table") + " already exist" + map[bool]string{true: "s", false: ""}[n == 1] + "; the choice applies to those that hold rows.")
		}
	}
	for range c.CollapsingHeader(inst.ids.PrepareStr("advanced"), c.WidgetText().Text("Advanced").Keep()).KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			c.Label("on the wire").Send()
			for _, comp := range []string{"zstd", "gzip", "none"} {
				if inst.radio("comp-"+comp, comp, inst.compression == comp || (comp == "none" && inst.compression == ""),
					"HTTP compression of the relayed stream, both legs") {
					inst.compression = comp
					if comp == "none" {
						inst.compression = ""
					}
				}
			}
		}
		if p.SyncRun != nil {
			for range c.HoverText("The plan's last run began " + fmtWhen(p.SyncRun.StartedAt) + ". Resuming skips the chunks its journal verified; a new run copies every chunk again.").KeepIter() {
				c.Checkbox(inst.ids.PrepareStr("restart"), inst.restart, "begin a new run instead of resuming the plan's").SendRespVal(&inst.restart)
			}
		}
	}
	inst.renderPreflight()
	inst.failedNote("sync-failed", &inst.syncJob)
	cmd := "sync --plan {plan} --mode " + inst.syncMode.String()
	if inst.syncMode == jk.SyncModeSample {
		cmd += " --sample " + inst.sampleText
	} else if inst.syncMode == jk.SyncModeFull {
		cmd += " --existing " + inst.existing.String()
	}
	if inst.restart {
		cmd += " --restart"
	}
	inst.cliHint(cmd)
}

// renderPreflight runs the pre-flight whenever the choices change, and shows
// what the sync would move and whether the target's disks have room.
func (inst *App) renderPreflight() {
	key := inst.settingsKey()
	if key != inst.preflightKey && !inst.previewJob.Running() && !inst.syncJob.Running() {
		inst.preflightKey = key
		inst.preflight = nil
		inst.startPreview()
	}
	space()
	if inst.jobRow(&inst.previewJob, "measuring what would move", "", "") {
		return
	}
	inst.failedNote("preflight-failed", &inst.previewJob)
	pf := inst.preflight
	if pf == nil {
		return
	}
	inst.card("preflight")(func() bool {
		if pf.tables == 0 {
			heading("Nothing to copy with these choices")
			for _, s := range pf.skipped {
				note(s)
			}
			return true
		}
		heading(fmt.Sprintf("Copy about %d rows in %s, %s to %s: about %s on the target",
			pf.rows, plural(pf.tables, "table"), hostLabel(inst.plan.Source.URL), hostLabel(inst.plan.Target.URL),
			progressest.FormatBytes(int64(pf.bytes))))
		for i, g := range pf.disks {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
				for range c.HorizontalTop().KeepIter() {
					verdict, tone := "fits", badge.ToneSuccess
					if !g.OK {
						verdict, tone = "may not fit", badge.ToneWarning
					}
					badge.New(inst.ids.PrepareStr("fit"), verdict).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
					held := ""
					if g.Held > 0 {
						held = ", plus " + progressest.FormatBytes(int64(g.Held)) + " cleared but held until merges"
					}
					c.Label(fmt.Sprintf("disks %s: %s free; needs about %s%s, %s with headroom",
						strings.Join(g.Disks, ","), progressest.FormatBytes(int64(g.Free)),
						progressest.FormatBytes(int64(g.Need)), held, progressest.FormatBytes(int64(g.Headroom)))).Send()
				}
			}
		}
		for _, s := range pf.skipped {
			note("skipped: " + s)
		}
		return true
	})
}

// renderStartControls is the Sync page's footer action: start, behind a
// second click that names what moves where.
func (inst *App) renderStartControls() {
	if inst.plan == nil {
		return
	}
	busy := inst.syncJob.Running() || inst.diffJob.Running() || inst.structureJob.Running() || inst.previewJob.Running() ||
		(inst.preflight != nil && inst.preflight.tables == 0)
	if !inst.syncArmed {
		if inst.actionButton("arm-sync", "Start the sync…", busy) {
			inst.syncArmed = true
		}
		return
	}
	what := inst.syncMode.String() + " sync"
	if pf := inst.preflight; pf != nil && pf.tables > 0 {
		what = fmt.Sprintf("%s of %d rows", what, pf.rows)
	}
	badge.New(inst.ids.PrepareStr("armed"), "Start the "+what+" to "+hostLabel(inst.plan.Target.URL)+"?").
		Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	if inst.actionButton("sync", "Start", busy) {
		inst.startSync()
	}
	if c.Button(inst.ids.PrepareStr("disarm-sync"), c.Atoms().Text("Cancel").Keep()).SendResp().HasPrimaryClicked() {
		inst.syncArmed = false
	}
}

// --- 6 Run ---------------------------------------------------------------------

func (inst *App) renderRun() {
	if inst.plan == nil {
		pageHeader("How is the sync going?")
		note("Nothing to monitor yet.")
		return
	}
	pageHeader("How is the sync going?")
	inst.renderRunHeadline()
	inst.renderReports()
	space()
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
		for range c.CollapsingHeader(inst.ids.PrepareStr("footprint-hdr"), c.WidgetText().Text("Footprint on the target").Keep()).KeepIter() {
			small("Active parts only. Rows a sync cleared hold their space until the parts merge.")
			for range c.Grid(inst.ids.PrepareStr("footprint")).NumColumns(4).Striped(true).KeepIter() {
				heading("target table")
				heading("source size")
				heading("on the target")
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
	}
	inst.renderChunkLog()
	inst.cliHint("status --plan {plan} --disk")
}

// renderRunHeadline is the one block to read: the sync in flight, or the
// outcome of the last one.
func (inst *App) renderRunHeadline() {
	snap := inst.syncJob.Snapshot()
	if inst.jobRow(&inst.syncJob, "syncing", "cancel-sync", "rows") {
		c.Label(fmt.Sprintf("%d rows landed, %s on the wire, running for %s", inst.rows.Load(),
			progressest.FormatBytes(inst.bytes.Load()), progressest.FormatDuration(time.Since(inst.syncStarted).Truncate(time.Second)))).Send()
		return
	}
	var rows, bytes uint64
	copied, failed, stale, reported := 0, 0, 0, 0
	var finished time.Time
	for _, t := range inst.plan.Tables {
		if r := t.SyncReport; r != nil {
			reported++
			rows += r.Rows
			bytes += r.Bytes
			copied += r.Copied
			failed += r.Failed
			stale += r.Stale
			if r.FinishedAt.After(finished) {
				finished = r.FinishedAt
			}
		}
	}
	inst.card("outcome")(func() bool {
		switch {
		case snap.State == bgjob.StateFailed && snap.Err != nil:
			badge.New(inst.ids.PrepareStr("o"), "the sync stopped").Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
			c.Label(snap.Err.Error()).Wrap().Send()
			note("Start it again from the Sync step: the journal resumes it where it stopped.")
		case reported == 0:
			note("No sync has run for this plan. Choose what to copy on the Sync step.")
			return true
		case failed+stale > 0:
			badge.New(inst.ids.PrepareStr("o"), plural(failed+stale, "chunk")+" not synced").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
			note("The tables below say which. Start the sync again to retry them; the verified chunks are skipped.")
		default:
			badge.New(inst.ids.PrepareStr("o"), "sync done").Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Send()
		}
		line := fmt.Sprintf("%s in %d chunks, %s on the wire", plural(int(rows), "row"), copied, progressest.FormatBytes(int64(bytes)))
		if !inst.syncStarted.IsZero() && !inst.syncFinished.IsZero() {
			line += ", in " + progressest.FormatDuration(inst.syncFinished.Sub(inst.syncStarted).Truncate(time.Second))
		} else if !finished.IsZero() {
			line += ", finished " + fmtWhen(finished)
		}
		c.Label(line).Send()
		if reported > 0 && failed+stale == 0 {
			note("Every copied chunk's digest matched the source's after landing. Compare the content again to confirm the whole table.")
		}
		return true
	})
}

func (inst *App) renderReports() {
	n := 0
	for i := range inst.plan.Tables {
		if inst.plan.Tables[i].SyncReport != nil {
			n++
		}
	}
	if n == 0 {
		return
	}
	space()
	heading("tables")
	for range c.Grid(inst.ids.PrepareStr("reports")).NumColumns(8).Striped(true).KeepIter() {
		heading("table")
		heading("copied")
		heading("already done")
		heading("identical")
		heading("stale")
		heading("failed")
		heading("rows")
		heading("on the wire")
		c.EndRow()
		for i := range inst.plan.Tables {
			t := &inst.plan.Tables[i]
			r := t.SyncReport
			if r == nil {
				continue
			}
			for range c.IdScope(inst.ids.PrepareStr("r-" + t.Source.String())) {
				c.Label(t.Source.String()).Send()
				c.Label(strconv.Itoa(r.Copied)).Send()
				c.Label(strconv.Itoa(r.Done)).Send()
				c.Label(strconv.Itoa(r.Identical)).Send()
				c.Label(strconv.Itoa(r.Stale)).Send()
				c.Label(strconv.Itoa(r.Failed)).Send()
				c.Label(strconv.FormatUint(r.Rows, 10)).Send()
				c.Label(progressest.FormatBytes(int64(r.Bytes))).Send()
				c.EndRow()
			}
		}
	}
	for i := range inst.plan.Tables {
		t := &inst.plan.Tables[i]
		if t.SyncReport == nil || len(t.SyncReport.Problems) == 0 {
			continue
		}
		for range c.IdScope(inst.ids.PrepareStr("p-" + t.Source.String())) {
			heading(t.Source.String() + ": problems")
			for j, p := range t.SyncReport.Problems {
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
	space()
	for range c.CollapsingHeader(inst.ids.PrepareStr("chunks-hdr"), c.WidgetText().Text("Latest chunks").Keep()).KeepIter() {
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
}
