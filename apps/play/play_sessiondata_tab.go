package play

import (
	"context"
	"strconv"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// play_sessiondata_tab.go — the Data sets tab: the session data sets of
// the data session the window's runs read, beside the ad-hoc datasets the
// window binds, with Keep for the editor's statement and Drop per data set
// (ADR-0406). What it lists is read off the frame and drawn from the last
// reading, so a frame costs the rows on screen.

// The tab's texts, named so a scene reads them.
const (
	datasetsNoBus       = "This window is not wired to the session data service: the host runs no bus for it."
	datasetsNoneYet     = "No session data sets yet. Name the editor's statement and Keep it; later statements read it as session('<name>')."
	datasetsKeepTooltip = "Keeps the editor's statement as a session data set on the ClickHouse server: a SELECT without a SET prelude. Later statements read it as session('<name>') and join it with server tables and keelson('<alias>') datasets. The same name again redefines it."
	datasetsTaskNote    = "The task's data session: every window and app working for the task reads these data sets."
	datasetsOwnNote     = "This window's data session: Open in play hands it to the window it opens."
)

// renderDatasetsTab draws the Data sets tab.
func (inst *PlayApp) renderDatasetsTab() {
	ids := inst.ids
	for range c.IdScope(ids.PrepareStr("datasetsTab")) {
		if inst.client == nil || inst.client.sessionBus() == nil {
			c.Label(datasetsNoBus).Send()
			return
		}
		inst.takeKeepResult()
		infos, task, errText, at := inst.sessions.read()
		if time.Since(at) > sessionViewEvery {
			inst.refreshSessions()
		}
		inst.renderKeepRow()
		switch {
		case task:
			c.Label(datasetsTaskNote).Send()
		case inst.client.windowTicket() != "":
			c.Label(datasetsOwnNote).Send()
		}
		if errText != "" {
			for rt := range c.RichTextLabel("The data sets could not be listed: " + errText) {
				rt.Small().Weak()
			}
		}
		if len(infos) == 0 && errText == "" {
			c.Label(datasetsNoneYet).Send()
		}
		for _, d := range infos {
			for range c.IdScope(ids.PrepareStr("sd-" + d.Name)) {
				inst.renderSessionDataset(d)
			}
		}
		if aliases := inst.client.DatasetAliases(); len(aliases) > 0 {
			c.Separator().Horizontal().Send()
			c.Label("Ad-hoc datasets bound in this window, held by the host:").Send()
			for _, a := range aliases {
				for rt := range c.RichTextLabel("keelson('" + a + "')") {
					rt.Monospace()
				}
			}
		}
	}
}

// renderKeepRow draws the name field and Keep, and the last outcome.
func (inst *PlayApp) renderKeepRow() {
	ids := inst.ids
	for range c.HorizontalTop().KeepIter() {
		c.TextEdit(ids.PrepareStr("keepName"), inst.keepName, false).
			DesiredWidth(160).HintText("name").AccessibleName("Data set name").
			SendRespVal(&inst.keepName)
		label := "Keep the editor's statement"
		if inst.keepBusy != nil {
			label = "Keeping…"
		}
		for range c.HoverText(datasetsKeepTooltip).KeepIter() {
			if c.Button(ids.PrepareStr("keep"), c.Atoms().Text(label).Keep()).
				Kind(c.ButtonKindPrimary).
				SendResp().HasPrimaryClicked() && inst.keepBusy == nil {
				inst.personKeep()
			}
		}
		if c.Button(ids.PrepareStr("refresh"), c.Atoms().Text("Refresh").Keep()).Kind(c.ButtonKindGhost).
			SendResp().HasPrimaryClicked() {
			inst.refreshSessions()
		}
	}
	if inst.keepNote != "" {
		for rt := range c.RichTextLabel(inst.keepNote) {
			rt.Small().Weak()
		}
	}
}

// renderSessionDataset draws one data set: a header with its size, state
// and server, its definition on demand, and Drop.
func (inst *PlayApp) renderSessionDataset(d sessiondata.DatasetInfo) {
	ids := inst.ids
	for range c.HorizontalTop().KeepIter() {
		for rt := range c.RichTextLabel(d.Name) {
			rt.Monospace().Strong()
		}
		c.Label(humanize.Comma(int64(d.Rows)) + " rows · " + humanize.IBytes(d.Bytes) + " · r" + strconv.FormatUint(d.Revision, 10) + " · " + d.Endpoint).Send()
		tone := badge.ToneSuccess
		switch d.State {
		case sessiondata.DatasetStateExpired, sessiondata.DatasetStateStale:
			tone = badge.ToneWarning
		case sessiondata.DatasetStateFailed:
			tone = badge.ToneError
		}
		tip := "its table is on the server"
		if d.Reason != "" {
			tip = d.Reason
		} else if d.State != sessiondata.DatasetStateLive {
			tip = "made again from its definition by the next statement that reads it"
		}
		badge.New(ids.PrepareStr("state"), d.State.String()).Tone(tone).Size(badge.SizeSm).Tooltip(tip).Send()
		if d.Confined {
			badge.New(ids.PrepareStr("confined"), "confined").Tone(badge.ToneWarning).Size(badge.SizeSm).
				Tooltip("derived from an ad-hoc dataset, which is sealed data: a statement reading it is confined").Send()
		}
		if c.Button(ids.PrepareStr("drop"), c.Atoms().Text("Drop").Keep()).Kind(c.ButtonKindDanger).
			SendResp().HasPrimaryClicked() {
			inst.personDropDataset(d.Name)
		}
	}
	for range c.CollapsingHeader(ids.PrepareStr("definition"), c.WidgetText().Text("Definition").Keep()).KeepIter() {
		for rt := range c.RichTextLabel(d.SQL) {
			rt.Monospace()
		}
		if reads := append(append([]string(nil), d.Dependencies...), d.Attachments...); len(reads) > 0 {
			for rt := range c.RichTextLabel("reads " + strings.Join(reads, ", ")) {
				rt.Small().Weak()
			}
		}
	}
}

// personKeep keeps the editor's statement under the name typed, off the
// frame: in the task's data session while an agent's mark is on the
// window, else in the window's own, opened at its first definition.
func (inst *PlayApp) personKeep() {
	name := strings.TrimSpace(inst.keepName)
	if !sessiondata.ValidName(name) {
		inst.keepNote = "a name is an identifier: letters, digits and _, not starting with a digit"
		return
	}
	sql, _, _ := inst.runBuffer()
	if strings.TrimSpace(sql) == "" {
		inst.keepNote = "the editor holds no statement to keep"
		return
	}
	inst.keepNote = ""
	inst.keepBusy = inst.startKeep(inst.client.agentMark.Load(), name, sql, nil)
}

// takeKeepResult reports a Keep that finished.
func (inst *PlayApp) takeKeepResult() {
	if inst.keepBusy == nil {
		return
	}
	res, err, done := inst.keepBusy.result()
	if !done {
		return
	}
	inst.keepBusy = nil
	inst.refreshSessions()
	if err != nil {
		inst.keepNote = "not kept: " + err.Error()
		return
	}
	d := res.res.Dataset
	inst.keepNote = "kept " + d.Name + " r" + strconv.FormatUint(d.Revision, 10) + " — " + humanize.Comma(int64(d.Rows)) + " rows; read it as session('" + d.Name + "')"
	if len(res.stale) > 0 {
		inst.keepNote += "; " + strings.Join(res.stale, ", ") + " rebuilt when next read"
	}
}

// personDropDataset is the person's Drop, through the catalog where a host
// serves it and directly otherwise.
func (inst *PlayApp) personDropDataset(name string) {
	in := DropDatasetArgs{Name: name}
	direct := func() {
		c := inst.client
		obo := c.agentMark.Load()
		c.Go(func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, sessiondata.RequestTimeout)
			defer cancel()
			if _, err := c.dropSession(ctx, obo, "", name, false); err != nil {
				inst.setActionNote("drop " + name + ": " + err.Error())
			}
			inst.refreshSessions()
		})
	}
	if inst.gestureCtx == nil {
		direct()
		return
	}
	if _, err := appops.Gesture[DropDatasetArgs, DropDatasetResult](inst.gestureCtx, opDropDataset, in); err != nil {
		direct()
	}
}
