package chat

// The Settings panel (ADR-0280): every option of a chat window in one
// place, beside the transcript, and the scale of what the model may do.
//
// The scale is one ladder with two markers. "may" is the ceiling: the most
// the settings here let the model ask for, which the host enforces. "now" is
// what the running task was granted, by the person in the host's dialog,
// under that ceiling. The chat computes neither bound's effect: it relays the
// settings and draws what the host reports.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bandscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// allowE is the highest action the settings let the model take in a window.
type allowE uint8

const (
	allowRead allowE = iota
	allowView
	allowEdit
	allowRun
	allowOutside
)

// changesE is whether a change waits for the person or applies at once.
type changesE uint8

const (
	changesAsk changesE = iota
	changesApply
)

// permissions are what the person lets the model do in app windows: the
// settings behind the ceiling.
type permissions struct {
	allow   allowE
	changes changesE
	launch  bool
	desktop bool
	reach   agent.ReachE
	// unpaced lets the model's changes land as fast as it makes them; off,
	// the host spaces them so each can be followed and stopped.
	unpaced bool
}

// defaultPermissions let the model do what a conversation with Apps on could
// do before there was a ceiling, short of acting outside the app: read, edit
// and run, open windows and arrange them, and reach the data endpoints — at
// a pace the person can follow.
func defaultPermissions() (p permissions) {
	return permissions{allow: allowRun, changes: changesApply, launch: true, desktop: true, reach: agent.ReachData}
}

// ceiling is the settings as the host takes them; without Apps the model
// only talks.
func (inst permissions) ceiling(apps bool) (ceil agent.Ceiling) {
	if !apps {
		return
	}
	ceil = agent.Ceiling{Mode: agent.ModeObserve, Effect: app.OperationEffectNone, Launch: inst.launch, Desktop: inst.desktop, Reach: inst.reach,
		Unpaced: inst.unpaced}
	if inst.allow == allowRead {
		return
	}
	ceil.Mode = agent.ModeSuggest
	if inst.changes == changesApply {
		ceil.Mode = agent.ModeAct
	}
	switch inst.allow {
	case allowView:
		ceil.Effect = app.OperationEffectView
	case allowEdit:
		ceil.Effect = app.OperationEffectDocument
	case allowRun:
		ceil.Effect = app.OperationEffectRun
	case allowOutside:
		ceil.Effect = app.OperationEffectConsequential
	}
	return
}

// artPolicy is what the settings let the model do to the artefact
// (ADR-0282 §SD3): Edit and above write, Ask first makes each write a
// proposal. The chat enforces it; the host never sees an artefact call.
func (inst permissions) artPolicy() (p artPolicy) {
	return artPolicy{write: inst.allow >= allowEdit, ask: inst.changes == changesAsk}
}

// artCeiling is the artefact's place on the ladder: edit when it may be
// written, read when it may only be read.
func (inst permissions) artCeiling() (ceil agent.Ceiling) {
	p := inst.artPolicy()
	if !p.write {
		return agent.Ceiling{Mode: agent.ModeObserve, Effect: app.OperationEffectNone}
	}
	ceil = agent.Ceiling{Mode: agent.ModeAct, Effect: app.OperationEffectDocument}
	if p.ask {
		ceil.Mode = agent.ModeSuggest
	}
	return
}

// withArtefact raises a ceiling to what the artefact allows, for the scale;
// what goes to the host stays the windows' ceiling alone.
func withArtefact(ceil agent.Ceiling, art agent.Ceiling) (out agent.Ceiling) {
	out = ceil
	if art.Level() <= ceil.Level() {
		return
	}
	out.Mode, out.Effect = max(out.Mode, art.Mode), max(out.Effect, art.Effect)
	return
}

// artefactOn says whether the conversation at hand has an artefact: its own
// choice once it started, the setting before.
func (inst *App) artefactOn() (on bool) {
	if inst.coord == nil {
		return false
	}
	if inst.conv.started {
		return inst.conv.artefact
	}
	return inst.artefact
}

// authorityAsk is what the last question to the host was about; a change in
// any part asks again.
type authorityAsk struct {
	handle  string
	ceiling agent.Ceiling
	tick    int64
}

const (
	// settingsPanelW is the Settings panel's width.
	settingsPanelW float32 = 380
	// barScaleW is the width of the scale in the bar.
	barScaleW float32 = 132

	tipSettings = "This chat's options, and what its model may do in your windows"
	tipScale    = "What the model may do. The hollow pointer is the most these settings let it ask for; the filled one is what the running task was granted. Settings has the detail."
	tipAllow    = "The most the model may do in a window you share. The host refuses anything above it, whatever a task was granted.\n" +
		"Read: read what windows hold and show.\n" +
		"View: also change what a window shows — a selection, a camera.\n" +
		"Edit: also change what it holds — text, parameters — and write the artefact.\n" +
		"Run: also run against a data source.\n" +
		"Outside: also act outside the app — publish, export — confirmed by you each time."
	tipChanges = "Ask first: every change waits as a proposal you accept or reject — in the window it changes, or in the Artefact panel. Apply directly: changes land as the model makes them. Reads never wait, and a change outside the app is confirmed each time either way."
	tipLaunch  = "Let the model ask to open windows of apps. You still decide which, in the host's dialog."
	tipDesktop = "Let the model ask to arrange every window on the desktop."
	tipReach   = "How far the model's work may reach beyond the windows. The task's grant names each place, and you approve the list.\n" +
		"This host: reads of this host's keelson() tables, one table at a time — apps, windows, desktop, app_state and the like, and datasets the apps publish — and its local git repositories.\n" +
		"Data endpoints: also the ClickHouse servers the apps query; At most decides whether the work only reads them.\n" +
		"Network: also HTTP fetches to the destinations this host declares, and calls to a model service. Anything the host does not recognise counts as network."
	tipPace     = "At a pace I can follow: the host spaces the model's changes — an edit, a run, a window opened or moved — so you can see each one and stop the task. As fast as it can: they land as the model makes them, faster than you can read or intervene."
	tipArtefact = "Give this conversation one markdown document the model reads and edits through tools, shown in the Artefact panel. At most and Changes above decide whether it may write and whether each change waits for you."
	tipScore    = "may is what these settings allow, now what the running task was granted, each 0–100: the level picks the band, and what else is allowed — listed below — moves it within the band."
	tipTyped    = "Offer each operation of the task's windows to the model as a tool of its own, instead of one call_operation tool. More tools in every request; some models call them more reliably."
)

var (
	atomsSettings = c.Atoms().Text(icons.IconGear + " Settings").Keep()
	// ladder is the scale's bands, one per level of agent.AllLevels.
	ladder = []bandscale.Band{
		{Label: "talk", Tone: styletokens.ToneNeutral},
		{Label: "read", Tone: styletokens.ToneInfo},
		{Label: "view", Tone: styletokens.ToneSuccess},
		{Label: "edit", Tone: styletokens.ToneWarning, Shade: bandscale.ShadeStrong},
		{Label: "run", Tone: styletokens.ToneWarning},
		{Label: "outside", Tone: styletokens.ToneError},
	}
)

// appsOn says whether the conversation at hand works with Apps: its own
// choice once it started, the setting before.
func (inst *App) appsOn() (on bool) {
	if inst.coord == nil {
		return false
	}
	if inst.conv.started {
		return inst.conv.apps
	}
	return inst.apps
}

// remoteModel says the model is reached off this machine.
func (inst *App) remoteModel() (remote bool) {
	return inst.answered && inst.model.Configured && !inst.model.Local
}

// syncAuthority hands the settings to the coordinator and keeps the host's
// account of the task's two bounds current: it asks when the task, the
// ceiling or — while a turn runs and may be widening the grant — the second
// changes. Asking is also what moves the task's ceiling in the host.
func (inst *App) syncAuthority() {
	ceil := inst.perms.ceiling(inst.appsOn())
	inst.conv.art.setPolicy(inst.perms.artPolicy())
	inst.conv.art.setMeta(inst.artMeta())
	if inst.coord == nil {
		inst.authority = agent.Authority{Limited: true}
		return
	}
	inst.coord.setOptions(ceil, inst.opTools)
	if a, _, ok := inst.authJob.TakeResult(); ok {
		inst.authority = *a
	} else if inst.authJob.Snapshot().State == bgjob.StateFailed {
		// The task is gone or the host did not answer: nothing is granted.
		inst.authJob.Invalidate()
		inst.authority.Granted = agent.Ceiling{}
	}
	inst.authority.Limited, inst.authority.Ceiling = true, ceil
	ask := authorityAsk{handle: inst.coord.handle(), ceiling: ceil}
	if ask.handle == "" {
		inst.authority.Granted = agent.Ceiling{}
		inst.authAsked = ask
		return
	}
	if inst.pending != nil {
		ask.tick = time.Now().Unix()
	}
	if ask == inst.authAsked || inst.authJob.Running() {
		return
	}
	cli := inst.agentCli
	if inst.authJob.Start(nil, bgjob.Spec{Kind: "chat-authority", Title: "what the model may do"},
		func(ctx context.Context) (a *agent.Authority, err error) {
			got, err := cli.Authority(ctx, ask.handle, &ask.ceiling)
			if err != nil {
				return
			}
			a = &got
			return
		}) {
		inst.authAsked = ask
	}
}

// scores are the two markers' places.
func (inst *App) scores() (may agent.Score, now agent.Score) {
	remote := inst.remoteModel()
	ceil := inst.authority.Ceiling
	if inst.artefactOn() {
		ceil = withArtefact(ceil, inst.perms.artCeiling())
	}
	return ceil.Score(remote), inst.authority.Granted.Score(remote)
}

func markers(may agent.Score, now agent.Score) (ms []bandscale.Marker) {
	return []bandscale.Marker{
		{Position: may.Position, Label: "may: " + may.Level.String(), Hollow: true},
		{Position: now.Position, Label: "now: " + now.Level.String()},
	}
}

// renderSettingsToggle is the bar's Settings button and the scale beside it.
func (inst *App) renderSettingsToggle() {
	for range c.HoverText(tipSettings).KeepIter() {
		if c.Button(inst.ids.PrepareStr("settings-toggle"), atomsSettings).Selected(inst.showSettings).SendResp().HasPrimaryClicked() {
			inst.showSettings = !inst.showSettings
			if inst.showSettings {
				inst.showStats = false
			}
		}
	}
	may, now := inst.scores()
	for range c.HoverText(tipScale).KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			bandscale.Render(bandscale.Input{Ids: inst.ids, ScopeKey: "bar-scale", Bands: ladder, Markers: markers(may, now),
				Width: barScaleW, Compact: true})
			c.Label("now " + now.Level.String() + " · may " + may.Level.String()).Selectable(false).Send()
		}
	}
}

// renderSettingsPanel is the Settings panel, beside the transcript.
func (inst *App) renderSettingsPanel() {
	if !inst.showSettings {
		return
	}
	for range c.PanelRightInside(inst.ids.PrepareStr("settings")).ExactSize(settingsPanelW).KeepIter() {
		for range c.ScrollArea().Vscroll(true).Hscroll(false).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("settings-body")) {
				inst.renderSettings()
			}
		}
	}
}

func heading(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Heading()
	}
}

func section(text string) {
	c.AddSpace(10)
	for rt := range c.RichTextLabel(text) {
		rt.Strong()
	}
	c.Separator().Horizontal().Send()
}

func weak(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Small().Weak()
	}
}

func (inst *App) renderSettings() {
	heading("Settings")
	inst.renderMaySection()
	inst.renderConversationSection()
	inst.renderModelSection()
}

// renderMaySection is the scale and the options that move it.
func (inst *App) renderMaySection() {
	section("What the model may do")
	may, now := inst.scores()
	bandscale.Render(bandscale.Input{Ids: inst.ids, ScopeKey: "scale", Bands: ladder, Markers: markers(may, now),
		Width: settingsPanelW - 36})
	for range c.HoverText(tipScore).KeepIter() {
		c.Label("may " + strconv.Itoa(may.Value()) + " · " + may.Level.String() + "   now " + strconv.Itoa(now.Value()) + " · " + now.Level.String()).
			Selectable(false).Send()
	}
	if len(may.Factors) > 0 {
		weak("may, within its level: " + strings.Join(may.Factors, " · "))
	}
	switch task, _, _ := inst.coord.stateOrNone(); {
	case inst.coord == nil:
		weak("This host offers the chat no app windows: the model only talks.")
	case !inst.appsOn() && inst.artefactOn():
		weak("Apps is off: the model works only in the artefact.")
	case !inst.appsOn():
		weak("Apps is off: the model only talks.")
	case task == "":
		weak("No task yet: nothing is shared until the model asks and you decide.")
	default:
		weak("now is what task " + strings.TrimPrefix(task, "task-") + " was granted, under may.")
	}
	if inst.coord == nil {
		return
	}
	c.AddSpace(6)
	if inst.conv.started {
		state := "off"
		if inst.conv.apps {
			state = "on"
		}
		for range c.HoverText(tipApps).KeepIter() {
			c.Label("Apps: " + state + " for this conversation").Selectable(false).Send()
		}
	} else {
		for range c.HoverText(tipApps).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("apps"), inst.apps, "Apps — let the model work in app windows").SendRespVal(&inst.apps)
		}
	}
	c.AddSpace(4)
	for range c.HoverText(tipAllow).KeepIter() {
		c.Label("At most").Selectable(false).Send()
	}
	selector.Segmented(inst.ids, "allow", &inst.perms.allow).Style(selector.StyleRadio).
		Option(allowRead, "Read").Option(allowView, "View").Option(allowEdit, "Edit").
		Option(allowRun, "Run").Option(allowOutside, "Outside").Send()
	for range c.HoverText(tipChanges).KeepIter() {
		c.Label("Changes").Selectable(false).Send()
	}
	selector.Segmented(inst.ids, "changes", &inst.perms.changes).Style(selector.StyleRadio).
		Option(changesAsk, "Ask first").Option(changesApply, "Apply directly").Send()
	for range c.HoverText(tipReach).KeepIter() {
		c.Label("Reach").Selectable(false).Send()
	}
	selector.Segmented(inst.ids, "reach", &inst.perms.reach).Style(selector.StyleRadio).
		Option(agent.ReachHost, "This host").Option(agent.ReachData, "Data endpoints").Option(agent.ReachNetwork, "Network").Send()
	for range c.HoverText(tipPace).KeepIter() {
		c.Label("Pace").Selectable(false).Send()
	}
	selector.Segmented(inst.ids, "pace", &inst.perms.unpaced).Style(selector.StyleRadio).Vertical().
		Option(false, "At a pace I can follow").Option(true, "As fast as it can").Send()
	c.AddSpace(4)
	for range c.HoverText(tipLaunch).KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("launch"), inst.perms.launch, "Open windows").SendRespVal(&inst.perms.launch)
	}
	for range c.HoverText(tipDesktop).KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("desktop"), inst.perms.desktop, "Arrange the desktop").SendRespVal(&inst.perms.desktop)
	}
	for range c.HoverText(tipTyped).KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("typed"), inst.opTools, "A tool per operation").SendRespVal(&inst.opTools)
	}
}

func (inst *App) renderConversationSection() {
	section("Conversation")
	conv := inst.conv
	for range c.HorizontalTop().KeepIter() {
		c.Label("Id").Selectable(false).Send()
		inst.renderConvId("conv-id-settings")
	}
	if conv.started {
		label, _ := keepBadge(conv)
		for range c.HoverText(tipKeep).KeepIter() {
			c.Label("Keep: " + label).Selectable(false).Send()
		}
		if conv.questions {
			for range c.HoverText(tipQuestions).KeepIter() {
				c.Label("Questions: on for this conversation").Selectable(false).Send()
			}
		}
		if conv.artefact {
			for range c.HoverText(tipArtefact).KeepIter() {
				c.Label("Artefact: on for this conversation").Selectable(false).Send()
			}
		}
		weak("Keep, Apps, Questions and Artefact are fixed at a conversation's first message; New conversation takes the settings above.")
		return
	}
	for range c.HoverText(tipKeep).KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("keep"), inst.keep, "Keep this conversation").SendRespVal(&inst.keep)
	}
	if inst.coord != nil {
		for range c.HoverText(tipQuestions).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("questions"), inst.questions, "Questions — let the model ask you with a form").SendRespVal(&inst.questions)
		}
		for range c.HoverText(tipArtefact).KeepIter() {
			c.Checkbox(inst.ids.PrepareStr("artefact"), inst.artefact, "Artefact — a markdown document the model edits").SendRespVal(&inst.artefact)
		}
	}
}

func (inst *App) renderModelSection() {
	section("Model")
	switch {
	case !inst.answered:
		weak("asking the host for a model…")
	case !inst.model.Configured:
		weak("The host offers no model: " + inst.model.Reason)
	default:
		m := inst.model
		where := "off this machine"
		switch {
		case m.Trusted:
			where = "off this machine, trusted with sealed data"
		case m.Local:
			where = "on this machine"
		}
		c.Label(m.Model + " · " + m.EndpointHost).Selectable(false).Send()
		weak(where)
		if m.ContextTokens > 0 {
			weak("context " + tokens(int64(m.ContextTokens)) + " tokens (" + m.ContextSource + ") · answers up to " + tokens(int64(m.MaxTokens)))
		}
	}
}
