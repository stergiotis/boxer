package agent

import (
	"math"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
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
			here = append(here, windowTask{t: t, mode: e.mode})
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
	label := icons.PhRobot + " agent · " + here[0].mode.String()
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
func (inst *Chrome) RenderDialogs(ids *c.WidgetIdStack) {
	svc := inst.svc
	// One modal at a time: a moderator's question waits behind nothing
	// and holds the others back until it is answered.
	if inst.renderQuestion(ids) {
		return
	}
	svc.mu.Lock()
	open := svc.pending()
	confirms := svc.proposals(0, true)
	svc.mu.Unlock()
	if len(confirms) > 0 {
		inst.renderConfirmation(confirms[0], len(confirms), ids)
	}
	if len(open) == 0 {
		return
	}
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
		approve, decline = inst.renderRequest(r, windows, len(open), ids)
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
}

// renderConfirmation asks the person to confirm one consequential command
// (ADR-0269 §SD5): asked every time, in suggest and act.
func (inst *Chrome) renderConfirmation(p proposalRef, waiting int, ids *c.WidgetIdStack) {
	svc := inst.svc
	rec := p.rec
	who := svc.display(p.t.actor) + " (window " + strconv.FormatUint(p.t.actorInstance, 10) + ")"
	var confirm, decline bool
	for range c.Modal(ids.PrepareStr("agent-confirm-" + p.t.id + "-" + rec.key)).KeepIter() {
		dialogHeading("Confirm a change outside the app", confirmWidth)
		c.Label(who + " asks to " + rec.spec.Summary + " (" + rec.spec.Name + ") in window " +
			strconv.FormatUint(rec.instance, 10) + ".").Wrap().Send()
		c.Label("This writes outside the app and cannot be undone from here.").Wrap().Send()
		if svc.Unattended() {
			c.Label("The host runs unattended, and still asks a person to confirm every change outside the app (ADR-0298).").Wrap().Send()
		}
		if rec.req.Reason != "" {
			bounded(ids.PrepareStr("agent-confirm-reason-"+rec.key), modelTextHeight, func() {
				for rt := range c.RichTextLabel("its reason, as the model wrote it: " + rec.req.Reason) {
					rt.Weak()
				}
			})
		}
		if waiting > 1 {
			c.Label(strconv.Itoa(waiting-1) + " more waiting").Send()
		}
		c.Separator().Send()
		// Right-aligned, the decision at the edge: a right-to-left row
		// places its first child rightmost.
		for range c.UiWithLayout().MainDirRightToLeft().CrossAlignMin().KeepIter() {
			confirm = c.Button(ids.PrepareStr("agent-confirm-yes-"+rec.key), c.Atoms().Text("Confirm").Keep()).Kind(c.ButtonKindDanger).SendResp().HasPrimaryClicked()
			decline = c.Button(ids.PrepareStr("agent-confirm-no-"+rec.key), c.Atoms().Text("Decline").Keep()).SendResp().HasPrimaryClicked()
		}
	}
	switch {
	case confirm:
		svc.accept(p)
	case decline:
		svc.rejectProposal(p)
	}
}

type windowRow struct {
	key   uint64
	title string
}

func (inst *Chrome) renderRequest(r *request, windows []windowRow, waiting int, ids *c.WidgetIdStack) (approve bool, decline bool) {
	svc := inst.svc
	who := svc.display(r.actor) + " (window " + strconv.FormatUint(r.actorInstance, 10) + ")"
	// Host-derived facts first; the model's words come last, as its claim
	// (ADR-0269 §SD5).
	switch {
	case r.held != nil:
		c.Label(who + " asks to call " + r.held.req.Operation + " in window " + strconv.FormatUint(r.held.req.Instance, 10) + ".").Wrap().Send()
		c.Label(needText(r.held)).Wrap().Send()
	case r.task != nil:
		c.Label(who + " asks to widen task " + r.task.id + ".").Wrap().Send()
	default:
		c.Label(who + " asks to start a task.").Wrap().Send()
	}
	svc.mu.Lock()
	taintedConv := r.task != nil && svc.tainted(r.task)
	left := svc.leftToPerson(r)
	svc.mu.Unlock()
	if left != "" {
		c.Label("The host runs unattended and left this to you: " + left + " (ADR-0298).").Wrap().Send()
	}
	if taintedConv {
		c.Label("This conversation has read untrusted content: what the model writes may have been steered by it.").Wrap().Send()
	}
	if r.held == nil || r.held.need == needInstance {
		c.Separator().Send()
		c.Label("Share these windows, in the mode you pick:").Send()
		if len(windows) == 0 {
			c.Label("No other window is open").Send()
		}
		// One row per window: whether it is shared, the mode, and what the
		// mode lets the task do — the columns aligned across windows.
		bounded(ids.PrepareStr("agent-share-list-"+r.key), shareListHeight, func() {
			for range c.Grid(ids.PrepareStr("agent-share-grid-" + r.key)).NumColumns(3).KeepIter() {
				for _, w := range windows {
					inst.renderShareRow(r, w, ids)
					c.EndRow()
				}
			}
		})
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
	if len(r.destinations) > 0 {
		bounded(ids.PrepareStr("agent-destinations-"+r.key), modelTextHeight, func() {
			c.Label("destinations: " + joinComma(r.destinations)).Wrap().Send()
		})
	}
	if r.task == nil {
		inst.renderBudget(r, ids)
	}
	if r.held != nil && r.held.req.Reason != "" {
		bounded(ids.PrepareStr("agent-reason-"+r.key), modelTextHeight, func() {
			for rt := range c.RichTextLabel("its reason, as the model wrote it: " + r.held.req.Reason) {
				rt.Weak()
			}
		})
	}
	if r.plan != "" && r.held == nil {
		bounded(ids.PrepareStr("agent-plan-"+r.key), modelTextHeight, func() {
			for rt := range c.RichTextLabel("its plan, as the model wrote it: " + r.plan) {
				rt.Weak()
			}
		})
	}
	if waiting > 1 {
		c.Label(strconv.Itoa(waiting-1) + " more waiting").Send()
	}
	c.Separator().Send()
	// Right-aligned, Approve at the edge: a right-to-left row places its
	// first child rightmost.
	for range c.UiWithLayout().MainDirRightToLeft().CrossAlignMin().KeepIter() {
		approve = c.Button(ids.PrepareStr("agent-approve-"+r.key), c.Atoms().Text("Approve").Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked()
		decline = c.Button(ids.PrepareStr("agent-decline-"+r.key), c.Atoms().Text("Decline").Keep()).SendResp().HasPrimaryClicked()
	}
	return
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
