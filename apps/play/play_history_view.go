package play

// The History tab's inline run inspection. Selecting a row opens its detail
// beneath it: badges for what the run was (outcome, ADR-0132 §SD5 security
// class, confinement, agent), its accounting as a key/value grid, and the
// full SQL highlighted, with the hand-offs — restore, copy, open as query.
// Both halves of the tab use it: the session ring (HistoryEntry, this
// window's runs) and the recorded runs (queryrunfacts, the server's record).
//
// A click on a row selects rather than restores. Restoring replaces the
// editor buffer, which made reading an old run cost the current one; it is
// now the Restore button inside the detail.

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// renderHistoryTab is the History dock tab body. The tab title already
// labels the pane; the outer ScrollArea wrap lives in Render().
func (inst *PlayApp) renderHistoryTab() {
	ids := inst.ids
	hist := inst.graph.MainHistory()
	present := false
	// Newest first.
	for i := len(hist) - 1; i >= 0; i-- {
		entry := hist[i]
		for range c.IdScope(ids.PrepareSeq(uint64(i))) {
			open := !inst.histSelected.IsZero() && entry.Executed.Equal(inst.histSelected)
			if c.Button(ids.PrepareStr("entry"),
				c.Atoms().Text(historyLabel(entry)).Keep()).
				Frame(false).
				Selected(open).
				Truncate().
				SendResp().HasPrimaryClicked() {
				if open {
					inst.histSelected = time.Time{}
				} else {
					inst.histSelected = entry.Executed
				}
				open = !open
			}
			if open {
				present = true
				for range c.Indent(ids.PrepareStr("detail")).KeepIter() {
					inst.renderHistoryEntryDetail(entry)
				}
			}
		}
	}
	if !present {
		// Trimmed off the ring; nothing left to show.
		inst.histSelected = time.Time{}
	}
	// The durable half: captured runs from boxer.facts (ADR-0115 S2).
	inst.renderRecordedRuns()
}

// renderHistoryEntryDetail is a session run opened in place.
func (inst *PlayApp) renderHistoryEntryDetail(e HistoryEntry) {
	ids := inst.ids
	for range c.HorizontalWrapped().KeepIter() {
		historyOutcomeBadge(ids.PrepareStr("outcome"), e)
		securityClassBadge(ids.PrepareStr("sec"), e.Security)
		if e.Confined {
			badge.New(ids.PrepareStr("confined"), "confined").
				Tone(badge.ToneWarning).Size(badge.SizeSm).
				Tooltip("The dispatch decision labelled the run confined: it touched data the placement keeps apart (ADR-0270 §SD4).").
				Send()
		}
		if e.Agent != nil {
			badge.New(ids.PrepareStr("agent"), "agent").
				Tone(badge.ToneInfo).Size(badge.SizeSm).
				Tooltip("An agent's work caused this run (ADR-0270 §SD2); the grid names the task.").
				Send()
		}
		if e.Buffer != "" {
			badge.New(ids.PrepareStr("stmt"), "one statement of the buffer").
				Tone(badge.ToneNeutral).Size(badge.SizeSm).
				Tooltip("The buffer held several statements and the one under the caret ran (ADR-0130 L3); Restore brings back the whole buffer.").
				Send()
		}
	}
	renderSecurityWitnesses(e.Security.witnesses)
	if e.ErrorText != "" {
		c.Label(e.ErrorText).Wrap().Selectable(true).Send()
	}
	for range c.Grid(ids.PrepareStr("fields")).NumColumns(2).Striped(true).KeepIter() {
		historyField("executed", historyWhen(e.Executed))
		historyField("elapsed", e.Elapsed.Round(time.Millisecond).String())
		historyField("rows returned", humanize.Comma(e.NumRows))
		if e.Terminal == runstream.TerminalTruncated {
			historyField("truncated", e.TerminalReason)
		}
		s := e.Summary
		if s.ReadRows > 0 || s.ReadBytes > 0 {
			historyField("read", fmt.Sprintf("%s rows · %s", humanize.Comma(int64(s.ReadRows)), humanize.IBytes(s.ReadBytes)))
		}
		if s.ResultBytes > 0 {
			historyField("result", fmt.Sprintf("%s rows · %s", humanize.Comma(int64(s.ResultRows)), humanize.IBytes(s.ResultBytes)))
		}
		if s.WrittenRows > 0 || s.WrittenBytes > 0 {
			historyField("written", fmt.Sprintf("%s rows · %s", humanize.Comma(int64(s.WrittenRows)), humanize.IBytes(s.WrittenBytes)))
		}
		if s.MemoryUsage > 0 {
			historyField("memory", humanize.IBytes(s.MemoryUsage))
		}
		if s.ElapsedNs > 0 {
			historyField("server time", time.Duration(s.ElapsedNs).Round(time.Millisecond).String())
		}
		if e.Dispatch != "" {
			historyField("dispatch", e.Dispatch)
		}
		if e.Agent != nil {
			historyField("agent task", agentLine(e.Agent))
		}
	}
	if len(e.SigParams) > 0 {
		for rt := range c.RichTextLabel("Signals sent") {
			rt.Small().Strong()
		}
		keys := slices.Sorted(maps.Keys(e.SigParams))
		for range c.Grid(ids.PrepareStr("signals")).NumColumns(2).Striped(true).KeepIter() {
			for _, k := range keys {
				historyField(strings.TrimPrefix(k, "param_"), e.SigParams[k])
			}
		}
	}
	inst.historySqlBlock("sql", "SQL", e.SQL)
	if e.Buffer != "" {
		for range c.CollapsingHeader(ids.PrepareStr("buffer-hdr"),
			c.WidgetText().Text("Source buffer").Keep()).DefaultOpen(false).KeepIter() {
			inst.historySqlBlock("buffer", "", e.Buffer)
		}
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("restore"), c.Atoms().Text("Restore").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.restoreHistoryEntry(e)
		}
		diagWeak("puts the buffer and the signal values it sent back")
	}
	c.Separator().Send()
}

// renderRunDetail is a recorded run opened in place: its accounting, the
// statement the server ran, and the editor hand-offs.
func (inst *PlayApp) renderRunDetail(row queryrunfacts.HistoryRow) {
	ids := inst.ids
	d := inst.runsHist
	if d.secFor != row.Id {
		// The text the server ran, after the passes: a keelson() macro is
		// judged as what it expanded into, which is what reached the wire.
		d.sec, _ = classifySecurity(row.QueryText, nil)
		d.secFor = row.Id
	}
	failed := row.ExceptionCode != 0 || (row.Event != "" && row.Event != "QueryFinish")
	for range c.HorizontalWrapped().KeepIter() {
		if failed {
			event := row.Event
			if event == "" {
				event = "failed"
			}
			badge.New(ids.PrepareStr("outcome"), event).
				Tone(badge.ToneError).Size(badge.SizeSm).
				Tooltip("The server recorded the run as ending in an exception.").Send()
		} else {
			badge.New(ids.PrepareStr("outcome"), "finished").
				Tone(badge.ToneSuccess).Size(badge.SizeSm).Send()
		}
		securityClassBadge(ids.PrepareStr("sec"), d.sec)
		if row.Kind != "" {
			badge.New(ids.PrepareStr("kind"), row.Kind).
				Tone(badge.ToneNeutral).Size(badge.SizeSm).
				Tooltip("The query kind ClickHouse logged.").Send()
		}
		if row.Delegated() {
			badge.New(ids.PrepareStr("agent"), "agent").
				Tone(badge.ToneInfo).Size(badge.SizeSm).
				Tooltip("An agent task caused this run (ADR-0277 §SD7); the grid names it.").Send()
		}
	}
	renderSecurityWitnesses(d.sec.witnesses)
	if row.ExceptionCode != 0 || row.Exception != "" {
		c.Label(fmt.Sprintf("exception %d: %s", row.ExceptionCode, row.Exception)).
			Wrap().Selectable(true).Send()
	}
	for range c.Grid(ids.PrepareStr("fields")).NumColumns(2).Striped(true).KeepIter() {
		historyField("executed", historyWhen(row.Ts))
		historyField("duration", humanize.Comma(int64(row.DurationMs))+" ms")
		historyField("read", fmt.Sprintf("%s rows · %s", humanize.Comma(int64(row.ReadRows)), humanize.IBytes(row.ReadBytes)))
		historyField("result", fmt.Sprintf("%s rows · %s", humanize.Comma(int64(row.ResultRows)), humanize.IBytes(row.ResultBytes)))
		if row.WrittenRows > 0 || row.WrittenBytes > 0 {
			historyField("written", fmt.Sprintf("%s rows · %s", humanize.Comma(int64(row.WrittenRows)), humanize.IBytes(row.WrittenBytes)))
		}
		historyField("peak memory", humanize.IBytes(row.MemoryPeak))
		historyField("query_id", row.QueryId)
		historyField("normalized hash", fmt.Sprintf("%016x", row.NormalizedHash))
		if row.App != "" {
			historyField("app", row.App)
		}
		if row.Lane != "" {
			historyField("lane", row.Lane)
		}
		if row.RunId != "" {
			historyField("run", row.RunId)
		}
		if row.Instance != 0 {
			historyField("window", fmt.Sprintf("%d", row.Instance))
		}
		if row.Delegated() {
			line := row.Task
			if row.TaskEpoch != 0 {
				line += fmt.Sprintf(" (epoch %d)", row.TaskEpoch)
			}
			if row.TaskCall != "" {
				line += " · call " + row.TaskCall
			}
			historyField("agent task", line)
		}
		// The ADR-0115 SD7 fingerprints, where the run was stamped.
		for _, fp := range [...]struct{ k, v string }{
			{"authored fp", row.AuthoredFp}, {"sent fp", row.SentFp},
			{"chain fp", row.ChainFp}, {"env fp", row.EnvFp},
		} {
			if fp.v != "" {
				historyField(fp.k, fp.v)
			}
		}
		historyField("fact id", fmt.Sprintf("%d", row.Id))
	}
	var authored string
	var haveAuthored bool
	if inst.client != nil {
		authored, haveAuthored = inst.client.AuthoredText(row.AuthoredFp)
	}
	switch {
	case haveAuthored:
		inst.historySqlBlock("authored", "Query as authored", authored)
	case row.AuthoredFp != "":
		diagWeak("Query as authored: not known here — the capture keeps only its fingerprint, and this process did not issue the run (or has let the text go).")
	default:
		diagWeak("Query as authored: the run carried no stamp, so there is nothing to match it by.")
	}
	inst.historySqlBlock("sql", "Query as the server ran it", row.QueryText)
	for range c.Horizontal().KeepIter() {
		if haveAuthored && c.Button(ids.PrepareStr("open-authored"), c.Atoms().Text("Open authored as query").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.ReplaceSql(authored)
		}
		if c.Button(ids.PrepareStr("open-as-query"), c.Atoms().Text("Open as query").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.ReplaceSql(row.QueryText)
		}
		if c.Button(ids.PrepareStr("profile-as-query"), c.Atoms().Text("Profile events as query").Keep()).
			SendResp().HasPrimaryClicked() {
			if sql, err := queryrunfacts.ComposeProfileEventsSql(runsHistoryFactsTable, row.Id); err == nil {
				inst.ReplaceSql(sql)
			}
		}
	}
	c.Separator().Send()
}

// historySqlShowBytes caps the statement a detail draws. A generated
// statement — a long IN list, an inlined dataset — can run to megabytes, and
// highlighting and laying that out every frame the detail is open costs more
// than anyone reads. Copy and "Open as query" still take the whole text.
const historySqlShowBytes = 16 << 10

// historySqlCut is the part of sql a detail draws: all of it up to the cap,
// otherwise a prefix ending at a line break where one falls in the last
// quarter of the cap (a cut mid-line splits a token more often), and never
// inside a UTF-8 sequence.
func historySqlCut(sql string) (shown string, cut bool) {
	if len(sql) <= historySqlShowBytes {
		return sql, false
	}
	shown = truncateBytes(sql, historySqlShowBytes)
	if i := strings.LastIndexByte(shown, '\n'); i >= historySqlShowBytes*3/4 {
		shown = shown[:i]
	}
	return shown, true
}

// historySqlBlock is a caption with a Copy button, then the statement
// highlighted — up to historySqlShowBytes, with a note when it is cut. The
// button is withheld without a clipboard rather than rendered dead
// (CanCopy), and copies the whole statement either way. key scopes the
// widget ids; an empty caption draws the button alone.
func (inst *PlayApp) historySqlBlock(key string, caption string, sql string) {
	ids := inst.ids
	shown, cut := historySqlCut(sql)
	for range c.IdScope(ids.PrepareStr(key)) {
		for range c.Horizontal().KeepIter() {
			if caption != "" {
				for rt := range c.RichTextLabel(caption) {
					rt.Small().Strong()
				}
			}
			if inst.CanCopy() && c.Button(ids.PrepareStr("copy"), c.Atoms().Text("Copy").Keep()).
				Small().SendResp().HasPrimaryClicked() {
				inst.copyToClipboard(sql)
			}
		}
		// PrepareSql: the detail redraws every frame it is open and the text
		// never changes under it (ADR-0125).
		c.CodeView(ids.PrepareStr("code"), codeview.PrepareSql(shown)).
			Wrap().
			Send()
		if cut {
			diagWeak(fmt.Sprintf("… cut: showing %s of %s — Copy, Restore and Open as query take the whole statement.",
				humanize.IBytes(uint64(len(shown))), humanize.IBytes(uint64(len(sql)))))
		}
	}
}

// historyOutcomeBadge says how a session run ended.
func historyOutcomeBadge(id c.WidgetIdCreatorI, e HistoryEntry) {
	switch {
	case e.ErrorText != "":
		badge.New(id, "failed").Tone(badge.ToneError).Size(badge.SizeSm).
			Tooltip("The run did not produce a usable result; the error is below.").Send()
	case e.Terminal == runstream.TerminalTruncated:
		badge.New(id, "truncated").Tone(badge.ToneWarning).Size(badge.SizeSm).
			Tooltip("The run ended early against a limit: the rows are a prefix of the answer.").Send()
	default:
		badge.New(id, "complete").Tone(badge.ToneSuccess).Size(badge.SizeSm).Send()
	}
}

// historyField is one key/value row of a detail grid.
func historyField(key string, value string) {
	c.LabelAtoms(c.Atoms().BeginRichText(key).Weak().End().Keep()).Send()
	c.Label(value).Selectable(true).Send()
	c.EndRow()
}

// historyWhen is a run's instant in local time and in UTC, with its age.
func historyWhen(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05.000") + " · " +
		t.UTC().Format("15:04:05") + " UTC · " + humanizeAgo(t)
}

// agentLine names the task an agent-caused run belongs to.
func agentLine(obo *app.OnBehalfOf) (line string) {
	line = obo.Task
	if obo.Epoch != 0 {
		line += fmt.Sprintf(" (epoch %d)", obo.Epoch)
	}
	if obo.Call != "" {
		line += " · call " + obo.Call
	}
	return
}
