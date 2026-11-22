package agent

import (
	"encoding/json/jsontext"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/decision"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// The host chrome of the contract (ADR-0269 §SD5): a badge in every window a
// task works in, and the dialog in which the person decides a request. Both
// are drawn by the window host on the render goroutine, outside every app's
// window body, so neither an app nor an agent can draw or click them.

// Chrome draws the service's host chrome; install it on the window host
// with SetAgentChrome.
type Chrome struct {
	svc *Service
}

// Chrome returns the host chrome of the service.
func (inst *Service) Chrome() (ch *Chrome) { return &Chrome{svc: inst} }

func (inst *Service) display(id app.AppIdT) (s string) {
	if m, ok := inst.cfg.Registry.LookupManifest(id); ok {
		return m.Display
	}
	return string(id)
}

// windowTask is one task working in a window, as the badge shows it.
type windowTask struct {
	t    *task
	mode ModeE
	// effective is mode under the task's ceiling: what the task may do here
	// now, which the badge shows (ADR-0280). mode is what the grant holds,
	// which the menu sets.
	effective ModeE
}

// RenderWindowChrome draws the badge of window key: what works in it, and
// the person's controls over it.
func (inst *Chrome) RenderWindowChrome(key uint64, ids *c.WidgetIdStack) {
	svc := inst.svc
	svc.mu.Lock()
	var here []windowTask
	for _, t := range svc.tasks {
		if t.revoked != "" {
			continue
		}
		if e := t.entries[key]; e != nil {
			here = append(here, windowTask{t: t, mode: e.mode, effective: t.modeOf(e)})
		}
	}
	proposed := len(svc.proposals(key, false))
	confirming := len(svc.proposals(key, true))
	paused := false
	for _, wt := range here {
		if svc.isPaused(wt.t, key) {
			paused = true
		}
	}
	svc.mu.Unlock()
	if len(here) == 0 {
		if left := svc.leftByTask(key); left != "" {
			// A window a task opened, left to the person when it ended.
			c.Label(icons.PhRobot + " left by " + left).Send()
		}
		return
	}
	slices.SortFunc(here, func(a, b windowTask) int { return a.t.created.Compare(b.t.created) })
	label := icons.PhRobot + " agent · " + here[0].effective.String()
	if len(here) > 1 {
		label = icons.PhRobot + " " + strconv.Itoa(len(here)) + " agents"
	}
	// Badge flags (ADR-0269 §SD5): what waits on the person here.
	if paused {
		label += " · paused"
	}
	if proposed > 0 {
		label += " · proposals " + strconv.Itoa(proposed)
	}
	if confirming > 0 {
		label += " · confirmation due"
	}
	if svc.Unattended() {
		// The host approves in the person's place (ADR-0298).
		label += " · unattended"
	}
	for range c.MenuButton(c.Atoms().Text(label).Keep()).KeepIter() {
		for _, wt := range here {
			inst.renderTaskMenu(key, wt, ids)
		}
	}
}

func (inst *Chrome) renderTaskMenu(key uint64, wt windowTask, ids *c.WidgetIdStack) {
	svc := inst.svc
	t := wt.t
	c.Label("task " + t.id + " · " + svc.display(t.actor) + " (window " + strconv.FormatUint(t.actorInstance, 10) + ")").Send()
	if t.plan != "" {
		for rt := range c.RichTextLabel("plan, as the model wrote it: " + t.plan) {
			rt.Weak()
		}
	}
	busy := svc.busyFor(key, t)
	if wt.effective < wt.mode {
		for rt := range c.RichTextLabel("The grant holds " + wt.mode.String() + "; the coordinator's settings allow at most " + wt.effective.String() + " now.") {
			rt.Weak()
		}
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("Mode").Send()
		for _, m := range AllModes {
			id := ids.PrepareStr("agent-mode-" + t.id + "-" + strconv.FormatUint(key, 10) + "-" + m.String())
			if c.SelectableLabel(id, wt.mode == m, m.String()).SendResp().HasPrimaryClicked() && m != wt.mode {
				if m >= ModeSuggest && busy {
					continue
				}
				svc.setMode(t, key, m)
			}
		}
	}
	if busy {
		c.Label("Another task suggests or acts here; this one may only observe").Send()
	}
	inst.renderProposals(key, t, ids)
	inst.renderUndo(key, t, ids)
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("agent-detach-"+t.id+"-"+strconv.FormatUint(key, 10)),
			c.Atoms().Text("Detach this window").Keep()).SendResp().HasPrimaryClicked() {
			svc.detachEntry(t, key, "the person detached the window", "person")
		}
		if c.Button(ids.PrepareStr("agent-stop-"+t.id+"-"+strconv.FormatUint(key, 10)),
			c.Atoms().Text("Stop the task").Keep()).SendResp().HasPrimaryClicked() {
			svc.endTask(t, "the person stopped it", "person")
		}
		// The task's marks on the overlay, cleared by the person (ADR-0297
		// §SD8); the Window menu clears every task's.
		if svc.cfg.Host != nil {
			if n := svc.cfg.Host.Marks().Count(t.id); n > 0 {
				if c.Button(ids.PrepareStr("agent-clear-marks-"+t.id+"-"+strconv.FormatUint(key, 10)),
					c.Atoms().Text("Clear its marks ("+strconv.Itoa(n)+")").Keep()).SendResp().HasPrimaryClicked() {
					svc.cfg.Host.Marks().Clear(t.id, "")
				}
			}
		}
	}
	c.Separator().Send()
}

// renderProposals lists the task's suggestions in window key with their
// Accept and Reject.
func (inst *Chrome) renderProposals(key uint64, t *task, ids *c.WidgetIdStack) {
	svc := inst.svc
	svc.mu.Lock()
	var mine []proposalRef
	for _, p := range svc.proposals(key, false) {
		if p.t == t {
			mine = append(mine, p)
		}
	}
	svc.mu.Unlock()
	for _, p := range mine {
		for range c.HorizontalTop().KeepIter() {
			c.Label("proposed: " + p.rec.spec.Summary + " (" + p.rec.spec.Name + ")").Send()
			if c.Button(ids.PrepareStr("agent-accept-"+t.id+"-"+p.rec.key), c.Atoms().Text("Accept").Keep()).SendResp().HasPrimaryClicked() {
				svc.accept(p)
			}
			if c.Button(ids.PrepareStr("agent-reject-"+t.id+"-"+p.rec.key), c.Atoms().Text("Reject").Keep()).SendResp().HasPrimaryClicked() {
				svc.rejectProposal(p)
			}
		}
	}
}

// undoLimit bounds how many of a task's changes the badge offers to undo.
const undoLimit = 5

// renderUndo lists the task's latest changes in window key with Undo
// (ADR-0269 §SD8). A write outside the app is not undone, so consequential
// commands are not offered.
func (inst *Chrome) renderUndo(key uint64, t *task, ids *c.WidgetIdStack) {
	svc := inst.svc
	svc.mu.Lock()
	var done []*callRec
	for _, rec := range t.keys {
		if rec.instance != key || !rec.routed || rec.spec.Class != app.OperationClassCommand ||
			rec.spec.Effect == app.OperationEffectConsequential {
			continue
		}
		if rec.outcome.Phase == opwire.PhaseApplied || rec.outcome.Phase == opwire.PhaseRendered {
			done = append(done, rec)
		}
	}
	svc.mu.Unlock()
	slices.SortFunc(done, func(a, b *callRec) int { return b.created.Compare(a.created) })
	if len(done) > undoLimit {
		done = done[:undoLimit]
	}
	for _, rec := range done {
		status := ""
		if svc.cfg.Host != nil {
			status, _ = svc.cfg.Host.OpsUndoStatus(key, rec.callId)
		}
		for range c.HorizontalTop().KeepIter() {
			c.Label("changed: " + rec.spec.Summary + " (" + rec.key + ")").Send()
			if status != "" {
				c.Label(status).Send()
				continue
			}
			if c.Button(ids.PrepareStr("agent-undo-"+t.id+"-"+rec.key), c.Atoms().Text("Undo").Keep()).SendResp().HasPrimaryClicked() &&
				svc.cfg.Host != nil {
				svc.cfg.Host.OpsUndo(key, rec.callId)
			}
		}
	}
}

// busyFor reports whether a task other than t holds key in suggest or act.
func (inst *Service) busyFor(key uint64, t *task) (busy bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.holder(key, t) != nil
}

// setMode is the person's change of a task's mode in one window. Lowering
// it ends the commands queued there under the higher mode (ADR-0269 §SD5).
func (inst *Service) setMode(t *task, key uint64, m ModeE) {
	inst.mu.Lock()
	e := t.entries[key]
	if e == nil {
		inst.mu.Unlock()
		return
	}
	lowered := m < e.mode
	e.mode = m
	inst.grantEvent(trail.GrantEventMode, "person", "window "+strconv.FormatUint(key, 10)+": "+m.String(), t, nil)
	var ids []string
	if lowered && m == ModeObserve {
		ids = t.queuedOn(key)
	}
	inst.mu.Unlock()
	switch {
	case lowered && m == ModeSuggest:
		// Bumpless: what was queued under act waits as a proposal.
		inst.requeueAsProposals(t, key)
	case len(ids) > 0 && inst.cfg.Host != nil:
		inst.cfg.Host.OpsExpire(key, ids, "the person lowered the mode to "+m.String())
	}
}

const (
	requestWidth float32 = 520
	confirmWidth float32 = 480
	// A modal is sized by its body, so every part of a decision whose length
	// the model or the desktop decides scrolls under a ceiling of its own;
	// otherwise a long list would push the decision off screen.
	// shareListHeight bounds the windows to share, launchListHeight the apps
	// the task may open, and modelTextHeight what the model wrote — its
	// plan or reason — and the destinations it named.
	shareListHeight  float32 = 240
	launchListHeight float32 = 160
	modelTextHeight  float32 = 120
)

// maxConfirmationArgs bounds the arguments a confirmation shows; what is
// cut is counted in their place.
const maxConfirmationArgs = 8 << 10

// confirmationArgs is a call's arguments as a confirmation shows them: the
// model's JSON, indented when it parses, cut at maxConfirmationArgs bytes.
// The person confirms what the call carries, so nothing is summarised.
func confirmationArgs(args string) string {
	if strings.TrimSpace(args) == "" || strings.TrimSpace(args) == "{}" {
		return "(none)"
	}
	v := jsontext.Value(args)
	if v.Indent(jsontext.WithIndent("  ")) == nil {
		args = string(v)
	}
	if len(args) > maxConfirmationArgs {
		cut := maxConfirmationArgs
		for cut > 0 && !utf8.RuneStart(args[cut]) {
			cut--
		}
		args = args[:cut] + "\n… " + strconv.Itoa(len(args)-cut) + " more bytes not shown"
	}
	return args
}

// bounded draws body in a vertical scroll area no taller than maxH. The
// area is pushed under an id of its own: two scroll areas in one modal
// would otherwise share egui's default scroll id, and with it the offset.
func bounded(id c.WidgetIdCreatorI, maxH float32, body func()) {
	for range c.PushId(id).KeepIter() {
		for range c.ScrollArea().Vscroll(true).MaxHeight(maxH).KeepIter() {
			body()
		}
	}
}

// dialogHeading starts a decision's modal: a fixed width, since a modal is
// sized by its body, and the title a window would carry as a heading.
func dialogHeading(title string, width float32) {
	c.UiSetMinWidth(width)
	c.UiSetMaxWidth(width)
	for rt := range c.RichTextLabel(icons.PhRobot + " " + title) {
		rt.Heading()
	}
	c.Separator().Send()
}

// RenderDialogs draws the oldest request the person has not decided, then
// the oldest consequential command awaiting confirmation. Both are modals
// (a grant is decided before anything else is clicked, and no window can
// cover the dialog asking for it).
//
// With the attention service installed (ADR-0324) nothing here is
// modal: the same decisions are items of the person's queue, drawn as host
// cards by RenderCard.
func (inst *Chrome) RenderDialogs(ids *c.WidgetIdStack) (modal bool) {
	svc := inst.svc
	if svc.cfg.Attention != nil {
		return false
	}
	// One modal at a time: a moderator's question waits behind nothing
	// and holds the others back until it is answered.
	if inst.renderQuestion(ids) {
		modal = true
		return
	}
	svc.mu.Lock()
	open := svc.pending()
	confirms := svc.proposals(0, true)
	svc.mu.Unlock()
	if len(confirms) > 0 {
		inst.renderConfirmation(confirms[0], len(confirms), ids)
		modal = true
	}
	if len(open) == 0 {
		return
	}
	modal = true
	r := open[0]
	var windows []windowRow
	if svc.cfg.Host != nil {
		for _, info := range svc.cfg.Host.OpsInstances() {
			if info.Key == r.actorInstance {
				continue
			}
			windows = append(windows, windowRow{key: info.Key, title: info.Title})
		}
	}
	title := "Agent task request"
	if r.task != nil {
		title = "Widen an agent task"
	}
	var approve, decline bool
	for range c.Modal(ids.PrepareStr("agent-request-" + r.key)).KeepIter() {
		dialogHeading(title, requestWidth)
		approve, decline = inst.renderRequest(r, windows, len(open), "", ids)
	}
	if !approve && !decline {
		return
	}
	svc.mu.Lock()
	var route *held
	if approve {
		route = svc.approve(r)
	} else {
		svc.reject(r)
	}
	svc.mu.Unlock()
	if route != nil {
		svc.routeHeld(route)
	}
	return
}

// renderConfirmation asks the person to confirm one consequential command
// (ADR-0269 §SD5): asked every time, in suggest and act.
func (inst *Chrome) renderConfirmation(p proposalRef, waiting int, ids *c.WidgetIdStack) {
	svc := inst.svc
	rec := p.rec
	var confirm, decline bool
	for range c.Modal(ids.PrepareStr("agent-confirm-" + p.t.id + "-" + rec.key)).KeepIter() {
		dialogHeading("Confirm a change outside the app", confirmWidth)
		confirm, decline = inst.renderConfirmationBody(p, waiting, ids)
	}
	switch {
	case confirm:
		svc.accept(p)
	case decline:
		svc.rejectProposal(p)
	}
}

// renderConfirmationBody is a confirmation's text and buttons, in a modal
// or a card. The model asks for the call, so it is a request; Confirm is
// Danger because the write cannot be undone from here (ADR-0404 SD4).
func (inst *Chrome) renderConfirmationBody(p proposalRef, waiting int, ids *c.WidgetIdStack) (confirm bool, decline bool) {
	svc := inst.svc
	rec := p.rec
	who := svc.display(p.t.actor) + " (window " + strconv.FormatUint(p.t.actorInstance, 10) + ")"
	in := decision.Input{
		Ids:      ids,
		ScopeKey: "agent-confirm-decision-" + rec.key,
		Origin:   decision.OriginRequest,
		Unframed: true,
		Title: who + " asks to " + rec.spec.Summary + " (" + rec.spec.Name + ") in window " +
			strconv.FormatUint(rec.instance, 10) + ".",
		Follows: []string{"This writes outside the app and cannot be undone from here."},
		// The person confirms what the call carries, so the arguments come
		// before the options.
		Form: func() {
			c.Label("Its arguments, as the model wrote them:").Wrap().Send()
			bounded(ids.PrepareStr("agent-confirm-args-"+rec.key), modelTextHeight, func() {
				for rt := range c.RichTextLabel(confirmationArgs(rec.req.Args)) {
					rt.Monospace()
				}
			})
		},
		Options: []decision.Option{
			{Key: "confirm", Label: "Confirm", Role: decision.RoleAffirm, Destroys: true},
			{Key: "decline", Label: "Decline", Role: decision.RoleDecline},
		},
	}
	if svc.Unattended() {
		in.Follows = append(in.Follows, "The host runs unattended, and still asks a person to confirm every change outside the app (ADR-0298).")
	}
	if waiting > 1 {
		in.Notes = append(in.Notes, decision.Note{Text: strconv.Itoa(waiting-1) + " more waiting", Tone: styletokens.ToneNeutral})
	}
	if rec.req.Reason != "" {
		in.Detail = func() {
			bounded(ids.PrepareStr("agent-confirm-reason-"+rec.key), modelTextHeight, func() {
				for rt := range c.RichTextLabel("its reason, as the model wrote it: " + rec.req.Reason) {
					rt.Weak()
				}
			})
		}
	}
	switch decision.Render(in).Chosen {
	case "confirm":
		confirm = true
	case "decline":
		decline = true
	}
	return
}

type windowRow struct {
	key   uint64
	title string
}

// renderRequest draws a request through the decision widget. A request is
// asked by an agent, so Approve is not the primary (ADR-0404 SD4). stale,
// when not empty, says why Approve cannot be given.
func (inst *Chrome) renderRequest(r *request, windows []windowRow, waiting int, stale string, ids *c.WidgetIdStack) (approve bool, decline bool) {
	svc := inst.svc
	who := svc.display(r.actor) + " (window " + strconv.FormatUint(r.actorInstance, 10) + ")"
	in := decision.Input{
		Ids:      ids,
		ScopeKey: "agent-request-decision-" + r.key,
		Origin:   decision.OriginRequest,
		Unframed: true,
	}
	// Host-derived facts first; the model's words come last, as its claim
	// (ADR-0269 §SD5).
	switch {
	case r.held != nil:
		in.Title = who + " asks to call " + r.held.req.Operation + " in window " + strconv.FormatUint(r.held.req.Instance, 10) + "."
		in.Follows = append(in.Follows, needText(r.held))
	case r.task != nil:
		in.Title = who + " asks for more in the task it holds."
		in.Subject = "task " + r.task.id
		in.Follows = append(in.Follows, "What it already holds is folded below; what is new is listed.")
	default:
		in.Title = who + " asks to start a task."
	}
	if r.rounds > 0 {
		in.Follows = append(in.Follows, "The running turn may take "+strconv.FormatUint(uint64(r.rounds), 10)+" more rounds of model calls.")
	}
	svc.mu.Lock()
	taintedConv := r.task != nil && svc.tainted(r.task)
	left := svc.leftToPerson(r)
	svc.mu.Unlock()
	if left != "" {
		in.Notes = append(in.Notes, decision.Note{Text: "The host runs unattended and left this to you: " + left + " (ADR-0298).", Tone: styletokens.ToneInfo})
	}
	if taintedConv {
		in.Notes = append(in.Notes, decision.Note{Text: "This conversation has read untrusted content: what the model writes may have been steered by it.", Tone: styletokens.ToneWarning})
	}
	if waiting > 1 {
		in.Notes = append(in.Notes, decision.Note{Text: strconv.Itoa(waiting-1) + " more waiting", Tone: styletokens.ToneNeutral})
	}
	in.Form = func() { inst.renderRequestTerms(r, windows, ids) }
	if (r.held != nil && r.held.req.Reason != "") || (r.held == nil && (r.purpose != "" || r.plan != "")) {
		in.Detail = func() { inst.renderRequestWords(r, ids) }
	}
	in.Options = []decision.Option{
		{Key: "approve", Label: "Approve", Role: decision.RoleAffirm, Disabled: stale != "", Why: stale},
		{Key: "decline", Label: "Decline", Role: decision.RoleDecline},
	}
	switch decision.Render(in).Chosen {
	case "approve":
		approve = true
	case "decline":
		decline = true
	}
	return
}

// renderRequestTerms draws what a request asks for and the person sets
// before answering: what the task holds, the windows to share and their
// modes, the apps it may open, the desktop, the destinations and the budget.
func (inst *Chrome) renderRequestTerms(r *request, windows []windowRow, ids *c.WidgetIdStack) {
	svc := inst.svc
	if r.task != nil {
		inst.renderHeldTerms(r, windows, ids)
	}
	if r.held == nil || r.held.need == needInstance {
		c.Separator().Send()
		asked, others := svc.splitWindows(r, windows)
		switch {
		case len(windows) == 0:
			c.Label("No other window is open").Send()
		case len(asked) > 0:
			c.Label("Share these windows, in the mode you pick:").Send()
		default:
			c.Label("The model asked for none of the open windows.").Wrap().Send()
		}
		// One row per window: whether it is shared, the mode, and what the
		// mode lets the task do — the columns aligned across windows. The
		// windows the model asked for come first; the rest are folded, to be
		// shared from here when the person wants to.
		inst.renderShareGrid(r, asked, "asked", ids)
		if len(others) > 0 {
			for range c.CollapsingHeader(ids.PrepareStr("agent-share-others-"+r.key), c.WidgetText().Text("Other open windows · "+strconv.Itoa(len(others))).Keep()).KeepIter() {
				inst.renderShareGrid(r, others, "others", ids)
			}
		}
	}
	if len(r.launches) > 0 {
		bounded(ids.PrepareStr("agent-launch-list-"+r.key), launchListHeight, func() {
			for _, l := range r.launches {
				c.Label("the task may " + launchText(l, svc.display(app.AppIdT(l.App)))).Wrap().Send()
			}
		})
	}
	if r.desktop == ModeAct {
		// The desktop as a whole (ADR-0276 §SD4): arranging moves every
		// window, the person's own included.
		svc.mu.Lock()
		if r.desktopFlag == nil {
			r.desktopFlag = new(bool)
			*r.desktopFlag = r.desktopShare
		}
		flag := r.desktopFlag
		svc.mu.Unlock()
		c.Checkbox(ids.PrepareStr("agent-desktop-"+r.key), *flag, "let the task arrange every window on the desktop").SendRespVal(flag)
		svc.mu.Lock()
		r.desktopShare = *flag
		svc.mu.Unlock()
	}
	if dests := svc.newDestinations(r); len(dests) > 0 {
		c.Label("Its work may also:").Send()
		bounded(ids.PrepareStr("agent-destinations-"+r.key), modelTextHeight, func() {
			for _, d := range dests {
				renderDestination(d)
			}
		})
	}
	if r.task == nil {
		// The budget is tuning: its default is what the coordinator asked
		// for, and the header says it without unfolding.
		svc.mu.Lock()
		calls := svc.clampCalls(r.calls)
		if r.callsFlag != nil {
			calls = int(math.Round(*r.callsFlag))
		}
		svc.mu.Unlock()
		for range c.CollapsingHeader(ids.PrepareStr("agent-details-"+r.key), c.WidgetText().Text("Details · up to "+strconv.Itoa(calls)+" calls").Keep()).KeepIter() {
			inst.renderBudget(r, ids)
		}
	}
}

// renderRequestWords draws what the model and the app wrote in support of a
// request — its reason, the app's purpose, the model's plan — after the
// options, as the decision's detail.
func (inst *Chrome) renderRequestWords(r *request, ids *c.WidgetIdStack) {
	svc := inst.svc
	if r.held != nil && r.held.req.Reason != "" {
		bounded(ids.PrepareStr("agent-reason-"+r.key), modelTextHeight, func() {
			for rt := range c.RichTextLabel("its reason, as the model wrote it: " + r.held.req.Reason) {
				rt.Weak()
			}
		})
	}
	if r.purpose != "" && r.held == nil {
		// The app's words, not the host's and not the model's.
		bounded(ids.PrepareStr("agent-purpose-"+r.key), modelTextHeight, func() {
			c.Label("what it is for, as " + svc.display(r.actor) + " describes itself: " + r.purpose).Wrap().Send()
		})
	}
	if r.plan != "" && r.held == nil {
		bounded(ids.PrepareStr("agent-plan-"+r.key), modelTextHeight, func() {
			for rt := range c.RichTextLabel("its plan, as the model wrote it: " + r.plan) {
				rt.Weak()
			}
		})
	}
}

// renderShareGrid is one grid of window rows; part keeps the ids of a
// request's two grids apart.
func (inst *Chrome) renderShareGrid(r *request, windows []windowRow, part string, ids *c.WidgetIdStack) {
	if len(windows) == 0 {
		return
	}
	bounded(ids.PrepareStr("agent-share-list-"+part+"-"+r.key), shareListHeight, func() {
		for range c.Grid(ids.PrepareStr("agent-share-grid-" + part + "-" + r.key)).NumColumns(3).KeepIter() {
			for _, w := range windows {
				inst.renderShareRow(r, w, ids)
				c.EndRow()
			}
		}
	})
}

// splitWindows is the windows a request asked for, and the others it may
// share, leaving out what its task already holds: that is listed under
// what the task holds.
func (inst *Service) splitWindows(r *request, windows []windowRow) (asked []windowRow, others []windowRow) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, w := range windows {
		_, wanted := r.wanted[w.key]
		switch {
		case wanted || r.share[w.key]:
			asked = append(asked, w)
		case r.task != nil && r.task.entries[w.key] != nil:
		default:
			others = append(others, w)
		}
	}
	return
}

// newDestinations is what a request asks to reach that its task does not
// already reach.
func (inst *Service) newDestinations(r *request) (out []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, d := range r.destinations {
		if r.task == nil || !slices.Contains(r.task.destinations, d) {
			out = append(out, d)
		}
	}
	return
}

// renderHeldTerms is what a widened task already holds, folded: its
// windows and their modes, the apps it may open, where its work reaches,
// its desktop mode and its calls.
func (inst *Chrome) renderHeldTerms(r *request, windows []windowRow, ids *c.WidgetIdStack) {
	svc := inst.svc
	titles := make(map[uint64]string, len(windows))
	for _, w := range windows {
		titles[w.key] = w.title
	}
	svc.mu.Lock()
	t := r.task
	var lines []string
	keys := make([]uint64, 0, len(t.entries))
	for k := range t.entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		e := t.entries[k]
		name := titles[k]
		if name == "" {
			name = e.alias
		}
		lines = append(lines, name+" (window "+strconv.FormatUint(k, 10)+") in "+e.mode.String()+" mode")
	}
	apps := make([]string, 0, len(t.launches))
	for id, le := range t.launches {
		apps = append(apps, "open up to "+strconv.Itoa(le.count)+" window(s) of "+svc.display(id)+" in "+le.mode.String()+" mode, "+strconv.Itoa(le.used)+" opened")
	}
	slices.Sort(apps)
	lines = append(lines, apps...)
	dests := slices.Clone(t.destinations)
	if t.desktop == ModeAct {
		lines = append(lines, "arrange every window on the desktop")
	}
	calls := strconv.Itoa(t.callsUsed) + " of " + strconv.Itoa(t.callsBudget) + " calls made"
	if !t.deadline.IsZero() {
		if left := time.Until(t.deadline).Round(time.Minute); left > 0 {
			calls += "; ends in " + left.String()
		} else {
			calls += "; its time is up"
		}
	}
	lines = append(lines, calls)
	svc.mu.Unlock()
	for range c.CollapsingHeader(ids.PrepareStr("agent-held-"+r.key), c.WidgetText().Text("What the task already holds").Keep()).KeepIter() {
		bounded(ids.PrepareStr("agent-held-list-"+r.key), modelTextHeight, func() {
			for _, l := range lines {
				for rt := range c.RichTextLabel(l) {
					rt.Small()
				}
			}
			for _, d := range dests {
				renderDestination(d)
			}
		})
	}
}

// destinationText is a grant destination in words: what reaching it lets
// the task's work do. A class it does not know is shown as its name.
func destinationText(d string) string {
	class, rest, _ := strings.Cut(d, ":")
	switch class {
	case "clickhouse":
		return "query the ClickHouse server at " + rest
	case "clickhouse-session":
		return "keep named tables for this task in a session on the ClickHouse server at " + rest
	case "keelson":
		return "read this host's table keelson('" + rest + "')"
	case "keelson-bundle":
		return "read the shared dataset bundle " + rest
	case "publish":
		return "publish bundles whose names start with " + rest + ", without a confirmation each"
	case "expose":
		return "let programs on this machine read the ad-hoc datasets whose names start with " + rest + ", over this host's loopback port, by a token that lapses"
	case "git":
		return "read the git repository " + rest
	case "http":
		return "fetch over HTTP from " + rest
	case "llm":
		return "call a model service"
	}
	return d
}

// renderDestination is one destination: its words, and its name as the
// grant records it.
func renderDestination(d string) {
	for range c.HorizontalWrapped().KeepIter() {
		c.Label("· " + destinationText(d)).Send()
		for rt := range c.RichTextLabel(d) {
			rt.Small().Weak().Monospace()
		}
	}
}

// renderBudget is the new task's call budget: a slider over the host's
// range, starting at what the coordinator asked for.
func (inst *Chrome) renderBudget(r *request, ids *c.WidgetIdStack) {
	svc := inst.svc
	lo, hi := svc.callRange()
	svc.mu.Lock()
	if r.callsFlag == nil {
		r.callsFlag = new(float64)
		*r.callsFlag = float64(svc.clampCalls(r.calls))
	}
	flag := r.callsFlag
	svc.mu.Unlock()
	for range c.HorizontalTop().KeepIter() {
		for range c.HoverText("Every operation the task calls in a window counts against its budget. When it is spent, the next call waits for you to give more.").KeepIter() {
			c.SliderF64(ids.PrepareStr("agent-calls-"+r.key), *flag, float64(lo), float64(hi)).
				Integer().Logarithmic(true).Suffix(" calls").Text("call budget").SendRespVal(flag)
		}
		for rt := range c.RichTextLabel("(" + strconv.Itoa(lo) + "–" + strconv.Itoa(hi) + ")") {
			rt.Small().Weak()
		}
	}
	svc.mu.Lock()
	r.calls = uint32(min(max(int(math.Round(*flag)), lo), hi))
	svc.mu.Unlock()
}

// modeLook is how the dialog shows a mode: its glyph, what it lets the
// task do — once as the option's hover, once as the badge beside the
// choice — and the badge's tone, which rises with what the task may
// change.
func modeLook(m ModeE) (glyph string, tip string, effect string, tone badge.ToneE) {
	switch m {
	case ModeObserve:
		return icons.PhEye, "The task reads the window's state and changes nothing.", "reads only", badge.ToneNeutral
	case ModeSuggest:
		return icons.PhLightbulb, "Each change the task makes waits in the window as a proposal you accept or reject.", "you accept each change", badge.ToneInfo
	default:
		return icons.PhLightning, "The task changes the window directly; each change can be undone there, and a change outside the app still asks you.", "changes the window", badge.ToneWarning
	}
}

func needText(h *held) (s string) {
	key := strconv.FormatUint(h.req.Instance, 10)
	switch h.need {
	case needInstance:
		s = "The task does not cover window " + key + ". Share it to let the call go ahead."
	case needOperation:
		s = "The task's grant for window " + key + " does not name " + h.req.Operation + ". Approving adds it."
	case needMode:
		s = "Window " + key + " is in observe mode for this task. Approving raises it to act."
	case needBudget:
		s = "The task has spent its call budget. Approving adds " + strconv.Itoa(DefaultCallBudget/4) + " calls."
	case needDeadline:
		s = "The task's deadline has passed. Approving gives it another " + h.extra + "."
	}
	return
}

func (inst *Chrome) renderShareRow(r *request, w windowRow, ids *c.WidgetIdStack) {
	svc := inst.svc
	k := strconv.FormatUint(w.key, 10)
	svc.mu.Lock()
	share, ok := r.shareFlag[w.key]
	if !ok {
		share = new(bool)
		*share = r.share[w.key]
		r.shareFlag[w.key] = share
	}
	mode := r.mode[w.key]
	if mode == ModeUnspecified {
		mode = ModeObserve
	}
	busy := svc.holder(w.key, r.task) != nil
	if busy && mode >= ModeSuggest {
		// Observe is all a busy window offers; what is approved is what
		// the dialog shows.
		mode = ModeObserve
		r.mode[w.key] = mode
	}
	svc.mu.Unlock()
	// Three grid cells: the window, its mode, and what the mode allows. A
	// window not shared offers no mode; a window another task acts in
	// offers observe alone.
	c.Checkbox(ids.PrepareStr("agent-share-"+r.key+"-"+k), *share, w.title+" (window "+k+")").SendRespVal(share)
	if !*share {
		for rt := range c.RichTextLabel("not shared") {
			rt.Weak()
		}
		c.Label("").Send()
	} else {
		bar := selector.Segmented(ids, "agent-share-mode-"+r.key+"-"+k, &mode)
		for _, m := range AllModes {
			if busy && m >= ModeSuggest {
				continue
			}
			glyph, tip, _, _ := modeLook(m)
			bar = bar.OptionIcon(m, glyph, m.String(), tip)
		}
		if bar.SendResp() {
			svc.mu.Lock()
			r.mode[w.key] = mode
			svc.mu.Unlock()
		}
		_, _, effect, tone := modeLook(mode)
		if busy {
			effect, tone = "busy: another task acts here", badge.ToneNeutral
		}
		badge.New(ids.PrepareStr("agent-share-effect-"+r.key+"-"+k), effect).Tone(tone).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
	}
	svc.mu.Lock()
	r.share[w.key] = *share
	svc.mu.Unlock()
}
