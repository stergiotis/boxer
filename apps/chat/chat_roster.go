package chat

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgui"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// The roster: the Conversations panel as the streams the person juggles
// (ADR-0324 background §3.1), not only an archive. Conversations live in a
// chat window come first, each with the windows its task works in; then the
// open runs no window holds, which Resume continues; then the stranded ones,
// written by another build and readable only; then the closed ones,
// folded. A run is live when the chat window that writes it is open: the
// run's coordinator names that window, and the host's keelson('windows')
// lists the open ones. The windows a task works in come from the same
// table's agent_tasks and keelson('agent_grants'), which names each task's
// coordinator window.

// rosterGroupE is where a conversation sits in the roster.
type rosterGroupE uint8

const (
	rosterLive rosterGroupE = iota
	rosterResumable
	// rosterStranded is a run another build wrote: readable, not
	// continuing (ADR-0308 §SD5).
	rosterStranded
	rosterClosed
)

// rosterWindow is a row of keelson('windows').
type rosterWindow struct {
	Key        string   `json:"key"`
	AppId      string   `json:"app_id"`
	Display    string   `json:"display"`
	AgentTasks []string `json:"agent_tasks"`
}

// rosterGrant is a row of keelson('agent_grants').
type rosterGrant struct {
	Task          string `json:"task"`
	ActorInstance string `json:"actor_instance"`
	Revoked       string `json:"revoked"`
}

// rosterExtra is what the roster reads beside the runs.
type rosterExtra struct {
	windows []rosterWindow
	grants  []rosterGrant
	titles  map[string]string
	// notices are the host's highlights (ADR-0323 §SD4).
	notices []attention.Notice
	// runs are the runs as this read listed them, for the pace.
	runs []agentrun.Run
	at   time.Time
}

// rosterRow is one conversation in the roster.
type rosterRow struct {
	run   agentrun.Run
	title string
	group rosterGroupE
	// window is the chat window that holds it, when live; here says that
	// window is this one.
	window string
	here   bool
	// working are the windows its task works in.
	working []rosterWindow
	// notice is the host's highlight of it, nil for none.
	notice *attention.Notice
}

// buildRoster sorts runs into the roster's groups: per live chat window its
// newest open run is in a window, other open runs are resumable, stranded
// runs apart, the rest closed; deleted runs are left out. here is this
// window's key.
func buildRoster(runs []agentrun.Run, extra rosterExtra, chatApp string, here string) (live, resumable, stranded, closed []rosterRow) {
	chatWindows := make(map[string]bool, 4)
	for _, w := range extra.windows {
		if w.AppId == chatApp {
			chatWindows[w.Key] = true
		}
	}
	// Each live window's current conversation: its newest open run.
	current := make(map[string]int, len(chatWindows))
	for i, r := range runs {
		if r.State != agentrun.StateOpen || !chatWindows[r.Coordinator] {
			continue
		}
		if j, ok := current[r.Coordinator]; !ok || r.Created.After(runs[j].Created) {
			current[r.Coordinator] = i
		}
	}
	noticeOf := make(map[string]*attention.Notice, len(extra.notices))
	for i := range extra.notices {
		noticeOf[extra.notices[i].Conversation] = &extra.notices[i]
	}
	tasksOf := make(map[string][]string, 4)
	for _, g := range extra.grants {
		if g.Revoked == "" {
			tasksOf[g.ActorInstance] = append(tasksOf[g.ActorInstance], g.Task)
		}
	}
	for i, r := range runs {
		if r.State == agentrun.StateDeleted {
			continue
		}
		row := rosterRow{run: r, title: extra.titles[r.Conversation], notice: noticeOf[r.Conversation]}
		switch {
		case r.State == agentrun.StateOpen && chatWindows[r.Coordinator] && current[r.Coordinator] == i:
			row.group, row.window, row.here = rosterLive, r.Coordinator, r.Coordinator == here
			tasks := tasksOf[r.Coordinator]
			for _, w := range extra.windows {
				if w.Key == r.Coordinator {
					continue
				}
				for _, t := range w.AgentTasks {
					if slices.Contains(tasks, t) {
						row.working = append(row.working, w)
						break
					}
				}
			}
			live = append(live, row)
		case r.State == agentrun.StateOpen:
			row.group = rosterResumable
			resumable = append(resumable, row)
		case r.State == agentrun.StateStranded:
			row.group = rosterStranded
			stranded = append(stranded, row)
		default:
			row.group = rosterClosed
			closed = append(closed, row)
		}
	}
	// This window first, then by when the conversation started.
	slices.SortStableFunc(live, func(a, b rosterRow) int {
		switch {
		case a.here && !b.here:
			return -1
		case b.here && !a.here:
			return 1
		}
		return b.run.Created.Compare(a.run.Created)
	})
	return
}

// The titles the roster names conversations by: each window keeps its
// conversation's title in the app's storage, which every chat window of the
// host reads.
const titleStorePrefix = "title/"

// storeTitle keeps the conversation's title once it changed, and hands it
// to the coordinator for the questions it posts. The render goroutine calls
// it once per frame; a write happens only on a change.
func (inst *App) storeTitle() {
	conv := inst.conv
	if conv.title == "" {
		return
	}
	if inst.coord != nil && (inst.titled != inst.coord || inst.titledAs != conv.title) {
		inst.coord.setTitle(conv.title)
		inst.titled, inst.titledAs = inst.coord, conv.title
	}
	if inst.storage == nil || !conv.started || conv.title == inst.storedTitle[conv.id] {
		return
	}
	if inst.storedTitle == nil {
		inst.storedTitle = make(map[string]string, 4)
	}
	inst.storedTitle[conv.id] = conv.title
	if err := inst.storage.Set(titleStorePrefix+conv.id, []byte(conv.title)); err != nil {
		inst.log.Debug().Err(err).Msg("chat: keep the conversation's title for the roster")
	}
}

// rosterRefresh is how often an open roster reads the windows again.
const rosterRefresh = 3 * time.Second

// listRoster reads the windows, the grants and the titles, off the frame.
func (inst *App) listRoster() {
	kq, storage, store, bus := inst.kq, inst.storage, inst.journalStore(), inst.bus
	inst.rosterAt = time.Now()
	inst.rosterJob.Start(nil, bgjob.Spec{Kind: "chat-roster", Title: "the roster"}, func(ctx context.Context) (out *rosterExtra, err error) {
		var runs []agentrun.Run
		if store != nil {
			if runs, err = store.ListRuns(ctx, string(ManifestId), runsListLimit); err != nil {
				return
			}
		}
		x := rosterExtra{titles: make(map[string]string, len(runs)), runs: runs, at: time.Now()}
		if kq != nil {
			if x.windows, err = queryRows[rosterWindow](ctx, kq, providersgui.TableWindows,
				"SELECT toString(key) AS key, app_id, display, agent_tasks FROM keelson('windows')"); err != nil {
				return
			}
			if x.grants, err = queryRows[rosterGrant](ctx, kq, agent.TableGrants,
				"SELECT task, toString(actor_instance) AS actor_instance, revoked FROM keelson('agent_grants')"); err != nil {
				return
			}
		}
		if bus != nil {
			// A host without notices leaves the roster without highlights.
			x.notices, _ = attention.FetchNotices(bus)
		}
		if storage != nil {
			for _, r := range runs {
				if b, found, gerr := storage.Get(titleStorePrefix + r.Conversation); gerr == nil && found {
					x.titles[r.Conversation] = string(b)
				}
			}
		}
		return &x, nil
	})
}

// syncRoster takes a finished read and starts the next while the panel is
// open.
func (inst *App) syncRoster() {
	if got, _, ok := inst.rosterJob.TakeResult(); ok && got != nil {
		inst.roster = *got
		inst.pace.observe(got.runs, got.at)
		if got.runs != nil {
			// The same listing the runs job makes: the rows follow it, so a
			// conversation started while the tab is in front appears.
			inst.runs = got.runs
		}
	}
	if !inst.tabShown(tabConversations) || inst.rosterJob.Snapshot().State == bgjob.StateRunning || time.Since(inst.rosterAt) < rosterRefresh {
		return
	}
	inst.listRoster()
}

// renderRoster draws the panel's groups.
func (inst *App) renderRoster() {
	here := strconv.FormatUint(inst.window, 10)
	live, resumable, stranded, closed := buildRoster(inst.runs, inst.roster, string(ManifestId), here)
	if len(live) > 0 {
		inst.rosterHeading("In windows", len(live))
		for i := range live {
			inst.renderRosterRow("live-"+strconv.Itoa(i), &live[i])
		}
	}
	if len(resumable) > 0 {
		inst.rosterHeading("Resumable — no window", len(resumable))
		for i := range resumable {
			inst.renderRosterRow("res-"+strconv.Itoa(i), &resumable[i])
		}
	}
	if len(stranded) > 0 {
		for range c.HoverText(tipRosterStranded).KeepIter() {
			inst.rosterHeading("Stranded — another build wrote these; read only", len(stranded))
		}
		for i := range stranded {
			inst.renderRosterRow("str-"+strconv.Itoa(i), &stranded[i])
		}
	}
	if len(closed) > 0 {
		for range c.CollapsingHeader(inst.ids.PrepareStr("roster-closed"), c.WidgetText().Text("Closed · "+strconv.Itoa(len(closed))).Keep()).KeepIter() {
			for i := range closed {
				inst.renderRosterRow("closed-"+strconv.Itoa(i), &closed[i])
			}
		}
	}
	if len(live)+len(resumable)+len(stranded)+len(closed) == 0 {
		c.Label("no conversations kept yet").Send()
	}
}

// conversationSwatch is the conversation's colour (ADR-0324 background
// §3.12), as the host's queue shows it too.
func conversationSwatch(conversation string) {
	for range c.HoverText("This conversation's colour: the same wherever it is shown, here and in the host's queue.").KeepIter() {
		for range c.RichTextLabelColored(color.FromImage(attention.ConversationColor(conversation)), color.Transparent, "●") {
		}
	}
}

const tipRosterStranded = "A run is stranded when the code now running differs from the code that wrote it (ADR-0308): " +
	"its transcript opens, but the conversation does not continue."

const tipRosterPace = "The bars are the steps the conversation wrote in each of the last five minutes, newest last, read off its journal while this tab is in front. " +
	"Quiet means an open conversation wrote nothing for two minutes or more: it may be waiting on you, on a slow model, or stuck."

func (inst *App) rosterHeading(title string, n int) {
	for rt := range c.RichTextLabel(title + " · " + strconv.Itoa(n)) {
		rt.Small().Strong()
	}
}

// rosterName is how the roster names a conversation: its title, else this
// window's own, else its id.
func (inst *App) rosterName(row *rosterRow) string {
	switch {
	case row.title != "":
		return row.title
	case row.run.Conversation == inst.conv.id && inst.conv.title != "":
		return inst.conv.title
	}
	return clip(row.run.Conversation, 28)
}

// rosterStatus is a conversation's pull-only line and its level: what the
// turn of the chat window that holds it is doing now, or what landed there
// while the person was away; else the host's highlight of it; the window
// that holds it; when a run without a window was written. A level is the highest reached since the person
// last looked (ADR-0323 §SD7).
func (inst *App) rosterStatus(row *rosterRow) (line string, level attention.LevelE) {
	level = attention.LevelData
	switch {
	case row.here:
		line, level = inst.turnLine()
		return "this window · " + line, level
	case row.group == rosterLive:
		if p, ok := peerTurnOf(row.window, row.run.Conversation); ok {
			level = p.level
			if row.notice != nil {
				level = max(level, row.notice.Level)
			}
			return "chat window " + row.window + " · " + p.line, level
		}
	}
	switch {
	case row.notice != nil:
		n := row.notice
		line = n.Text + " · " + ago(time.Since(n.Created))
		if row.group == rosterLive {
			line += " · in chat window " + row.window
		}
		return line, max(level, n.Level)
	case row.group == rosterLive:
		return "in chat window " + row.window, level
	}
	when := row.run.Created
	if !row.run.Closed.IsZero() {
		when = row.run.Closed
	}
	return row.run.State.String() + " · " + when.Local().Format("2006-01-02 15:04") + " · " + strconv.Itoa(int(row.run.LastOrdinal)) + " entries", level
}

// ago is a short age.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	default:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	}
}

func (inst *App) renderRosterRow(key string, row *rosterRow) {
	r := row.run
	line, level := inst.rosterStatus(row)
	for range c.IdScope(inst.ids.PrepareStr("roster-" + key)) {
		for range c.HorizontalTop().KeepIter() {
			for range c.HoverText(levelTip(level)).KeepIter() {
				c.Label(levelMark(level)).Selectable(false).Send()
			}
			conversationSwatch(r.Conversation)
			name := inst.rosterName(row)
			for range c.HoverText(r.Conversation + "\nrun " + strconv.FormatUint(r.Id, 10) + "\nprofile " + r.Profile + " · " + r.Label.String()).KeepIter() {
				if row.here {
					for rt := range c.RichTextLabel(name) {
						rt.Strong()
					}
				} else {
					c.Label(name).Selectable(false).Send()
				}
			}
			if inst.rosterTainted(row) {
				for range c.HoverText(tipRosterUntrusted).KeepIter() {
					badge.New(inst.ids.PrepareStr("untrusted"), "untrusted content").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
				}
			}
		}
		if r.State == agentrun.StateOpen {
			if g := inst.pace.paceGauge(r.Conversation, time.Now()); g != "" {
				line += " · " + g
			}
		}
		for range c.HoverText(tipRosterPace).KeepIter() {
			for rt := range c.RichTextLabel(line) {
				rt.Small().Weak()
			}
		}
		for _, w := range row.working {
			for rt := range c.RichTextLabel("    └ " + w.Display + " · window " + w.Key) {
				rt.Small()
			}
		}
		if row.group != rosterLive || !row.here {
			for range c.HorizontalTop().KeepIter() {
				if row.group == rosterLive {
					for range c.HoverText(tipRosterRaise).KeepIter() {
						if c.Button(inst.ids.PrepareStr("raise"), c.Atoms().Text("Raise").Keep()).Small().SendResp().HasPrimaryClicked() {
							inst.raiseWindow(row.window)
						}
					}
				}
				if row.group != rosterLive && r.Conversation != inst.conv.id {
					if c.Button(inst.ids.PrepareStr("resume"), c.Atoms().Text("Resume").Keep()).Small().SendResp().HasPrimaryClicked() {
						inst.resumeRun(r.Conversation)
					}
				}
				if row.group != rosterLive {
					if c.Button(inst.ids.PrepareStr("delete"), c.Atoms().Text("Delete").Keep()).Small().SendResp().HasPrimaryClicked() {
						inst.deleteRun(r.Conversation)
					}
				}
			}
		}
		c.Separator().Send()
	}
}

const tipRosterUntrusted = "This conversation has read content an attacker could influence — a window's text, a file, a capture (ADR-0269 §SD7). " +
	"What its model writes, and asks you, may have been steered by it. Known for the conversations a chat window holds."

// rosterTainted is whether a live conversation has read untrusted content,
// as the window that holds it knows; false for a run no window holds, whose
// label cannot tell (every reply already ranks above external content).
func (inst *App) rosterTainted(row *rosterRow) bool {
	switch {
	case row.here:
		_, tainted, _ := inst.coord.stateOrNone()
		return tainted
	case row.group == rosterLive:
		p, ok := peerTurnOf(row.window, row.run.Conversation)
		return ok && p.tainted
	}
	return false
}
