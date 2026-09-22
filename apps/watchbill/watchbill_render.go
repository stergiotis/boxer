package watchbill

import (
	"fmt"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

const seqChip = 0x100

func (inst *App) render() {
	snap := inst.snapshot()
	jobs := inst.visibleJobs(snap.jobs)
	// The body probe goes first, before any panel takes its share: it is
	// the width the split is kept against.
	bodyW, _, bodyOK := c.CapturePaneSize(c.ProbeSeq(string(manifest.Id), "body"))
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).Resizable(false).KeepIter() {
		inst.renderFilters(snap, len(jobs))
	}
	for range c.PanelBottomInside(inst.ids.PrepareStr("bottom")).Resizable(false).KeepIter() {
		inst.renderWorkers(snap)
	}
	// The list panel's own probe reports what the panel gave it last frame;
	// the split state tells a drag from a window resize by the body probe.
	obs, obsOK := c.CurrentApplicationState.StateManager.GetUiRect(c.ProbeSeq(string(manifest.Id), "list"))
	inst.mu.Lock()
	exact, width := inst.split.frame(bodyW, bodyOK, obs.MaxX-obs.MinX, obsOK)
	inst.mu.Unlock()
	panel := c.PanelLeftInside(inst.ids.PrepareStr("list")).Resizable(true)
	if exact {
		panel = panel.ExactSize(width)
	} else {
		panel = panel.DefaultSize(width)
	}
	for range panel.KeepIter() {
		c.CaptureUiAvailableRect(c.ProbeSeq(string(manifest.Id), "list"))
		inst.renderList(jobs)
	}
	for range c.PanelCentralInside().KeepIter() {
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.renderDetail(snap, jobs)
		}
	}
}

func (inst *App) renderFilters(snap snapshot, shown int) {
	for range c.HorizontalTop().KeepIter() {
		c.Label("States").Send()
		for i, s := range watchbillstore.AllStates {
			on := inst.filters.states[s]
			variant := badge.VariantOutline
			if on {
				variant = badge.VariantSolid
			}
			if badge.New(inst.ids.PrepareSeq(seqChip+uint64(i)), s).Tone(toneOf(s)).Variant(variant).Size(badge.SizeSm).Selected(on).SendResp().HasPrimaryClicked() {
				inst.toggleState(s)
			}
		}
		// Clear drops every pill and the kind text at once; it is drawn
		// whether or not a filter is set, so the row never shifts, and it
		// does nothing when there is nothing to clear.
		if c.Button(inst.ids.PrepareStr("clear"), c.Atoms().Text("Clear").Keep()).Small().SendResp().HasPrimaryClicked() {
			inst.clearFilters()
		}
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Label("Kind").Send()
		if c.TextEdit(inst.ids.PrepareStr("kind"), inst.kindDraft, false).SendRespVal(&inst.kindDraft).HasChanged() {
			inst.filters.kind = inst.kindDraft
		}
		if c.Button(inst.ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.markDirty()
		}
		var auto bool
		inst.mu.Lock()
		auto = inst.autoRefresh
		inst.mu.Unlock()
		if c.Checkbox(inst.ids.PrepareStr("auto"), auto, "Auto-refresh").SendRespVal(&auto).HasChanged() {
			inst.mu.Lock()
			inst.autoRefresh = auto
			inst.mu.Unlock()
		}
	}
	for range c.HorizontalTop().KeepIter() {
		switch {
		case snap.lastError != "":
			badge.New(inst.ids.PrepareStr("err"), snap.lastError).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
			if strings.Contains(snap.lastError, "timeout") {
				c.Label("No worker answered: the process's watchbill service needs a live ClickHouse.").Send()
			}
		case snap.inflight > 0:
			badge.New(inst.ids.PrepareStr("busy"), fmt.Sprintf("%d request(s) in flight", snap.inflight)).Tone(badge.ToneInfo).Variant(badge.VariantSoft).Send()
		case snap.lastNote != "":
			badge.New(inst.ids.PrepareStr("note"), snap.lastNote).Tone(badge.ToneSuccess).Variant(badge.VariantSoft).Send()
		}
		if snap.refreshed.IsZero() {
			c.Label("Waiting for the first list…").Send()
		} else {
			c.Label(fmt.Sprintf("%d of %d job(s) shown, listed %s ago", shown, len(snap.jobs), time.Since(snap.refreshed).Round(time.Second))).Send()
		}
	}
}

func (inst *App) renderDetail(snap snapshot, jobs []watchbillstore.Job) {
	if inst.selectedID == "" {
		c.Label("Select a job in the list.").Send()
		return
	}
	job, found := findJob(snap.jobs, inst.selectedID)
	if !found {
		c.Label("Job " + short(inst.selectedID) + " is not in the listed rows; widen the filter or wait for the list.").Send()
		return
	}
	inst.machine.Mirror(job.State)
	for range c.Horizontal().KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichText(job.ID).Monospace().Heading().End().Keep()).Send()
		inst.chip.Render()
		cancellable := job.State == watchbillstore.StateQueued || job.State == watchbillstore.StateRunning
		if c.Button(inst.ids.PrepareStr("cancel"), c.Atoms().Text("Cancel").Keep()).SendResp().HasPrimaryClicked() && cancellable {
			inst.cancel(job.ID)
		}
		if c.Button(inst.ids.PrepareStr("retry"), c.Atoms().Text("Retry").Keep()).SendResp().HasPrimaryClicked() && watchbillstore.IsFinal(job.State) {
			inst.retry(job.ID)
		}
	}
	c.AddSpace(styletokens.PaddingInner(inst.density))
	for range c.Grid(inst.ids.PrepareStr("fields")).NumColumns(2).Striped(true).KeepIter() {
		field := func(k, v string) {
			c.LabelAtoms(c.Atoms().BeginRichText(k).Weak().End().Keep()).Send()
			c.Label(v).Send()
			c.EndRow()
		}
		field("kind", job.Kind)
		field("subject", job.Subject)
		field("queue", job.Queue)
		field("priority", fmt.Sprint(job.Priority))
		field("attempt", fmt.Sprintf("%d of %d", job.Attempt, job.MaxAttempts))
		field("backoff", policyOf(job))
		field("timeout", durationOf(job.TimeoutMs))
		field("run after", when(job.RunAfter))
		field("finished at", when(job.FinishedAt))
		field("worker run", job.WorkerRun)
		field("owner app", job.OwnerAppId)
		field("requester run", job.RequesterRun)
		field("args kind", job.ArgsKind)
	}
	if job.LastError != "" {
		for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-err"), c.WidgetText().Text("Last error").Keep()).DefaultOpen(true).KeepIter() {
			c.LabelAtoms(c.Atoms().BeginRichText(job.LastError).Monospace().End().Keep()).Send()
		}
	}
	inst.renderTrail(snap, job)
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-live"), c.WidgetText().Text("Live runs").Keep()).DefaultOpen(true).KeepIter() {
		if inst.monitor != nil {
			inst.monitor.Render()
		}
	}
}

func (inst *App) renderTrail(snap snapshot, job watchbillstore.Job) {
	for range c.CollapsingHeader(inst.ids.PrepareStr("hdr-trail"), c.WidgetText().Text("Trail").Keep()).DefaultOpen(true).KeepIter() {
		switch {
		case !snap.reads:
			c.Label("The trail needs a bus to read keelson('watchbill_event') through, and this window has none.").Send()
			return
		case snap.eventsFor != job.ID:
			c.Label("Reading the trail…").Send()
			return
		case len(snap.events) == 0:
			c.Label("No transitions recorded yet.").Send()
			return
		}
		for range c.Grid(inst.ids.PrepareStr("events")).NumColumns(6).Striped(true).KeepIter() {
			for _, h := range []string{"at", "state", "attempt", "worker run", "note", ""} {
				c.LabelAtoms(c.Atoms().BeginRichText(h).Strong().End().Keep()).Send()
			}
			c.EndRow()
			for i, e := range snap.events {
				for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
					c.Label(e.At).Send()
					badge.New(inst.ids.PrepareStr("st"), e.State).Tone(toneOf(e.State)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
					c.Label(fmt.Sprint(e.Attempt)).Send()
					c.Label(short(e.WorkerRun)).Send()
					c.Label(e.Note).Send()
					if e.Error == "" {
						c.Label("").Send()
					} else {
						label := "show error"
						if inst.shownEvent == i {
							label = "hide error"
						}
						if c.Button(inst.ids.PrepareStr("err"), c.Atoms().Text(label).Keep()).Small().SendResp().HasPrimaryClicked() {
							if inst.shownEvent == i {
								inst.shownEvent = -1
							} else {
								inst.shownEvent = i
							}
						}
					}
				}
				c.EndRow()
			}
		}
		if inst.shownEvent >= 0 && inst.shownEvent < len(snap.events) {
			c.LabelAtoms(c.Atoms().BeginRichText(snap.events[inst.shownEvent].Error).Monospace().Small().End().Keep()).Send()
		}
	}
}

// renderWorkers is the cell's live workers (ADR-0237 §SD4), one line
// each, this process's own first with its live fields.
func (inst *App) renderWorkers(snap snapshot) {
	switch {
	case !snap.reads:
		c.Label("Workers: unknown, this window has no bus to read keelson('watchbill_worker') through").Send()
		return
	case len(snap.workers) == 0:
		c.Label("Workers: none alive on the cell (a worker needs a live ClickHouse)").Send()
		return
	}
	c.LabelAtoms(c.Atoms().BeginRichText(fmt.Sprintf("Workers alive on the cell: %d", len(snap.workers))).Strong().End().Keep()).Send()
	for i, w := range snap.workers {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(i))) {
			for range c.HorizontalTop().KeepIter() {
				queues := "every queue"
				if len(w.Queues) > 0 {
					queues = strings.Join(w.Queues, ", ")
				}
				line := fmt.Sprintf("run %s on %s · kinds %s · %s · %d per kind", short(w.RunId), w.Host, strings.Join(w.Kinds, ", "), queues, w.MaxWorkers)
				if w.Local {
					line += fmt.Sprintf(" · %d running · poll %dms", len(w.Running), w.PollMs)
				}
				c.Label(line).Send()
				if w.Local {
					badge.New(inst.ids.PrepareStr("local"), "this process").Tone(badge.ToneInfo).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
					if !w.Serving {
						badge.New(inst.ids.PrepareStr("noserve"), "not serving").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
					}
					if !w.Sweeping {
						badge.New(inst.ids.PrepareStr("nosweep"), "not sweeping").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Tooltip("this run reads no heartbeats, so it rescues nothing a dead run left").Send()
					}
				}
			}
		}
	}
}

func toneOf(state string) (tone badge.ToneE) {
	switch state {
	case watchbillstore.StateQueued:
		return badge.ToneInfo
	case watchbillstore.StateRunning:
		return badge.TonePrimary
	case watchbillstore.StateSucceeded:
		return badge.ToneSuccess
	case watchbillstore.StateFailed, watchbillstore.StateCancel:
		return badge.ToneWarning
	case watchbillstore.StateDiscarded, watchbillstore.StateAbandoned:
		return badge.ToneError
	default:
		return badge.ToneNeutral
	}
}

func policyOf(j watchbillstore.Job) (s string) {
	if j.Backoff == "" || j.Backoff == watchbillstore.BackoffNone {
		return "none"
	}
	return j.Backoff + " from " + durationOf(j.BackoffBaseMs)
}

func durationOf(ms uint64) (s string) {
	if ms == 0 {
		return "none"
	}
	return (time.Duration(ms) * time.Millisecond).String()
}

func when(t time.Time) (s string) {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
