package agent

import (
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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
	svc.mu.Unlock()
	if len(here) == 0 {
		return
	}
	slices.SortFunc(here, func(a, b windowTask) int { return a.t.created.Compare(b.t.created) })
	label := icons.PhRobot + " agent · " + here[0].mode.String()
	if len(here) > 1 {
		label = icons.PhRobot + " " + strconv.Itoa(len(here)) + " agents"
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
		c.Label("mode").Send()
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
		c.Label("another task suggests or acts here; this one may only observe").Send()
	}
	for range c.HorizontalTop().KeepIter() {
		if c.Button(ids.PrepareStr("agent-detach-"+t.id+"-"+strconv.FormatUint(key, 10)),
			c.Atoms().Text("Detach this window").Keep()).SendResp().HasPrimaryClicked() {
			svc.detachEntry(t, key, "the person detached the window")
		}
		if c.Button(ids.PrepareStr("agent-stop-"+t.id+"-"+strconv.FormatUint(key, 10)),
			c.Atoms().Text("Stop the task").Keep()).SendResp().HasPrimaryClicked() {
			svc.endTask(t, "the person stopped it")
		}
	}
	c.Separator().Send()
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
	var ids []string
	if lowered {
		ids = t.queuedOn(key)
	}
	inst.mu.Unlock()
	if len(ids) > 0 && inst.cfg.Host != nil {
		inst.cfg.Host.OpsExpire(key, ids, "the person lowered the mode to "+m.String())
	}
}

// RenderDialogs draws the oldest request the person has not decided.
func (inst *Chrome) RenderDialogs(ids *c.WidgetIdStack) {
	svc := inst.svc
	svc.mu.Lock()
	open := svc.pending()
	svc.mu.Unlock()
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
	win := c.Window(ids.PrepareStr("agent-request-"+r.key), c.WidgetText().Text(icons.PhRobot+" "+title).Keep()).
		Resizable(true).Collapsible(false).DefaultSize(520, 360).DefaultPos(200, 120)
	var approve, decline bool
	for range win.KeepIter() {
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

type windowRow struct {
	key   uint64
	title string
}

func (inst *Chrome) renderRequest(r *request, windows []windowRow, waiting int, ids *c.WidgetIdStack) (approve bool, decline bool) {
	svc := inst.svc
	who := svc.display(r.actor) + " (window " + strconv.FormatUint(r.actorInstance, 10) + ")"
	switch {
	case r.held != nil:
		h := r.held
		c.Label(who + " asks to call " + h.req.Operation + " in window " + strconv.FormatUint(h.req.Instance, 10) + ".").Wrap().Send()
		c.Label(needText(h)).Wrap().Send()
		if h.req.Reason != "" {
			for rt := range c.RichTextLabel("its reason, as the model wrote it: " + h.req.Reason) {
				rt.Weak()
			}
		}
	case r.task != nil:
		c.Label(who + " asks to widen task " + r.task.id + ".").Wrap().Send()
	default:
		c.Label(who + " asks to start a task.").Wrap().Send()
	}
	if r.plan != "" && r.held == nil {
		for rt := range c.RichTextLabel("its plan, as the model wrote it: " + r.plan) {
			rt.Weak()
		}
	}
	if r.held == nil || r.held.need == needInstance {
		c.Separator().Send()
		c.Label("Share these windows, in the mode you pick:").Send()
		for _, w := range windows {
			inst.renderShareRow(r, w, ids)
		}
		if len(windows) == 0 {
			c.Label("no other window is open").Send()
		}
	}
	if len(r.destinations) > 0 {
		c.Label("destinations: " + joinComma(r.destinations)).Wrap().Send()
	}
	if r.task == nil {
		calls := r.calls
		if calls == 0 {
			calls = DefaultCallBudget
		}
		c.Label("budget: " + strconv.FormatUint(uint64(calls), 10) + " calls").Send()
	}
	if waiting > 1 {
		c.Label(strconv.Itoa(waiting-1) + " more waiting").Send()
	}
	c.Separator().Send()
	for range c.HorizontalTop().KeepIter() {
		approve = c.Button(ids.PrepareStr("agent-approve-"+r.key), c.Atoms().Text("Approve").Keep()).SendResp().HasPrimaryClicked()
		decline = c.Button(ids.PrepareStr("agent-decline-"+r.key), c.Atoms().Text("Decline").Keep()).SendResp().HasPrimaryClicked()
	}
	return
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
	svc.mu.Unlock()
	for range c.HorizontalTop().KeepIter() {
		c.Checkbox(ids.PrepareStr("agent-share-"+r.key+"-"+k), *share, w.title+" (window "+k+")").SendRespVal(share)
		for _, m := range AllModes {
			if busy && m >= ModeSuggest {
				continue
			}
			if c.SelectableLabel(ids.PrepareStr("agent-share-mode-"+r.key+"-"+k+"-"+m.String()), mode == m, m.String()).
				SendResp().HasPrimaryClicked() {
				svc.mu.Lock()
				r.mode[w.key] = m
				svc.mu.Unlock()
			}
		}
		if busy {
			c.Label("busy: another task acts here").Send()
		}
	}
	svc.mu.Lock()
	r.share[w.key] = *share
	svc.mu.Unlock()
}
