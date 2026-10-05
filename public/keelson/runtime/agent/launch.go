package agent

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// Launches (ADR-0269 §SD3, §SD6): a grant may name apps the task may open
// windows of, with a mode and a count. A window the task opens belongs to
// the task; when the task ends it passes to the person, badged as left by
// the task.

// resolveApp finds a launchable app by its id or subject alias, or else by
// a name that matches one launchable app's id, alias or display name,
// ignoring case — what a model writes when it names the app it read in
// describe_app ("Play", "SQL Playground"). A name two apps answer to finds
// neither. An app the launch limit refuses (ADR-0272) is not found, so no
// grant can name it.
func (inst *Service) resolveApp(name string) (id app.AppIdT, ok bool) {
	name = strings.TrimSpace(name)
	ms := inst.cfg.Registry.LaunchableManifests()
	for _, m := range ms {
		if matchesApp(m, name) {
			return m.Id, true
		}
	}
	n := 0
	for _, m := range ms {
		if strings.EqualFold(string(m.Id), name) || strings.EqualFold(m.Id.SubjectAlias(), name) || strings.EqualFold(m.Display, name) {
			id, n = m.Id, n+1
		}
	}
	if n == 1 {
		return id, true
	}
	return "", false
}

// unknownAppReason says why name opens no window.
func (inst *Service) unknownAppReason(name string) (reason string) {
	if inst.refusedByLimit(name) {
		return "this host does not open windows of " + name + " (launch limit)"
	}
	return "no app named " + strconv.Quote(name) + "; describe_app lists the apps by id"
}

// resolveLaunches checks a request's launches as it arrives: every name
// must find an app, so a grant never comes back without a launch the
// caller asked for. Each name becomes the app's id, which the person's
// dialog shows, and a count of zero becomes one.
func (inst *Service) resolveLaunches(ls []wireLaunchEntry) (out []wireLaunchEntry, reason string) {
	var bad []string
	for _, l := range ls {
		id, ok := inst.resolveApp(l.App)
		if !ok {
			bad = append(bad, inst.unknownAppReason(l.App))
			continue
		}
		l.App = string(id)
		if l.Count == 0 {
			l.Count = 1
		}
		out = append(out, l)
	}
	if len(bad) > 0 {
		return nil, "cannot open: " + strings.Join(bad, "; ")
	}
	return
}

// refusedByLimit reports whether name is a registered app the launch limit
// refuses, so a refusal can say so rather than that no such app exists.
func (inst *Service) refusedByLimit(name string) (refused bool) {
	for _, r := range inst.cfg.Registry.Registrations() {
		if matchesApp(r.Manifest, name) {
			return !inst.cfg.Registry.Launchable(r.Manifest.Id)
		}
	}
	return
}

// addLaunches adds launch entries to a task. The caller holds mu.
func (inst *Service) addLaunches(t *task, ls []wireLaunchEntry) {
	for _, l := range ls {
		id, ok := inst.resolveApp(l.App)
		if !ok || l.Count == 0 {
			continue
		}
		m := ParseMode(l.Mode)
		if m == ModeUnspecified {
			m = ModeAct
		}
		if t.test && m == ModeSuggest {
			m = ModeObserve
		}
		if e := t.launches[id]; e != nil {
			e.count += int(l.Count)
			continue
		}
		t.launches[id] = &launchEntry{mode: m, count: int(l.Count)}
	}
}

// launch opens a window of an app the grant names; the new window joins
// the task in the mode the grant gave the app.
func (inst *Service) launch(msg *app.Msg) (rep wireLaunchReply) {
	rep.V = wireVersion
	req, err := decode[wireLaunch](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		inst.mu.Unlock()
		rep.Reason = out.Reason
		return
	}
	id, known := inst.resolveApp(req.App)
	le := t.launches[id]
	// The launch leaves an action row naming the window it opened, so which
	// task opened a window is a join on the window's key (ADR-0277 §SD7).
	inst.nextCall++
	rec := &callRec{key: req.Key, callId: t.id + "-" + strconv.FormatUint(inst.nextCall, 10), app: id,
		spec: app.OperationSpec{Name: launchOperation}, turn: req.Turn,
		cause: causeOf(wireCall{ModelCall: req.ModelCall, ToolCall: req.ToolCall, ToolIndex: req.ToolIndex}), created: time.Now()}
	defer func() {
		phase := opwire.PhaseCompleted
		switch {
		case rep.Ok:
			rec.instance = rep.Instance
		case rec.outcome.Phase == opwire.PhaseFailed:
			phase = opwire.PhaseFailed
		default:
			phase = opwire.PhaseRefused
		}
		inst.record(t, rec, "final", phaseOutcome(phase, rep.Reason))
	}()
	switch {
	case t.ceiling.refuseLaunch() != "":
		rep.Reason = t.ceiling.refuseLaunch()
	case !known:
		rep.Reason = inst.unknownAppReason(req.App)
	case le == nil:
		rep.Reason = "the grant does not let the task open windows of " + string(id)
	case le.used >= le.count:
		rep.Reason = "the task opened every window of " + string(id) + " its grant allows"
	}
	if rep.Reason != "" {
		inst.mu.Unlock()
		return
	}
	le.used++
	mode := le.mode
	inst.mu.Unlock()
	// A window opening is a change the person sees, paced like the rest.
	inst.pace(t)
	key, err := inst.cfg.Host.OpsOpen(id, req.Kind, req.Config)
	if err != nil {
		inst.mu.Lock()
		le.used--
		inst.mu.Unlock()
		rep.Reason = "the host did not open it: " + err.Error()
		rec.outcome = phaseOutcome(opwire.PhaseFailed, rep.Reason)
		return
	}
	load, loadReason := inst.waitLoaded(key)
	inst.mu.Lock()
	t.entries[key] = &entry{instance: key, app: id, alias: id.SubjectAlias(), mode: mode}
	t.launched[key] = true
	inst.startTurnAt(t, key)
	inst.mu.Unlock()
	inst.attach(key)
	if loadReason != "" {
		// A mount error is the app's text (ADR-0269 §SD7).
		inst.taint(t)
	}
	rep.Ok, rep.Instance = true, key
	rep.Load, rep.LoadReason = load.String(), loadReason
	return
}

// launchOperation is the operation name a launch's action row carries: the
// coordinator's tool, since a launch is the dispatcher's own and no app's.
const launchOperation = "open_window"

// launchSettle bounds how long launch waits for a new window to load.
const launchSettle = 3 * time.Second

// waitLoaded waits until a new window has mounted and, when its app serves
// a catalog, serves it, so the caller's first query does not meet a window
// that has not drawn. Past the bound it reports the window as it stands —
// opening, when its app's Mount has not returned — rather than as ready.
func (inst *Service) waitLoaded(key uint64) (load opwire.LoadE, reason string) {
	deadline := time.Now().Add(launchSettle)
	for {
		info, found := inst.instanceInfo(key)
		load, reason = loadOf(info)
		if !found {
			load, reason = opwire.LoadFailed, "the window closed while it was opening"
			return
		}
		switch load {
		case opwire.LoadFailed:
			return
		case opwire.LoadReady:
			if !info.Ops {
				return
			}
			if revs, ok := inst.cfg.Host.OpsRevisions(key); ok && revs != nil {
				return
			}
		}
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// instanceInfo is the host's account of one open window.
func (inst *Service) instanceInfo(key uint64) (info opwire.InstanceInfo, found bool) {
	for _, i := range inst.cfg.Host.OpsInstances() {
		if i.Key == key {
			return i, true
		}
	}
	return
}

// loadOf is a window's load state; a host that does not track it reports
// its windows as ready.
func loadOf(info opwire.InstanceInfo) (load opwire.LoadE, reason string) {
	load, reason = info.Load, info.Reason
	if load == opwire.LoadUnspecified {
		load = opwire.LoadReady
	}
	return
}

// leftByTask names the task that left window key to the person, if one did.
func (inst *Service) leftByTask(key uint64) (task string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.leftBy[key]
}

// Launched is a window a task opened, and how far it had come when Launch
// returned: Load is opening, ready or failed, and LoadReason says why it
// failed. A window still opening takes no calls until list shows it ready.
type Launched struct {
	Instance   uint64
	Load       string
	LoadReason string
}

// Launch opens a window of an app the grant names (an id or a subject
// alias), with an optional launch config in the app's LaunchKind; the
// window joins the task.
func (inst *Client) Launch(ctx context.Context, handle string, appName string, kind string, config []byte) (out Launched, err error) {
	return inst.LaunchFrom(ctx, CallRequest{Handle: handle}, appName, kind, config)
}

// LaunchFrom is Launch for a model's tool call: from names the task's
// handle and, as for a call, the key, the turn and the model call that
// asked — what the launch's action row records.
func (inst *Client) LaunchFrom(ctx context.Context, from CallRequest, appName string, kind string, config []byte) (out Launched, err error) {
	rep, err := roundTrip[wireLaunch, wireLaunchReply](ctx, inst, SubjectLaunch,
		wireLaunch{V: wireVersion, Handle: from.Handle, App: appName, Kind: kind, Config: config,
			Key: from.Key, Turn: from.Turn, ModelCall: from.ModelCall, ToolCall: from.ToolCall, ToolIndex: from.ToolIndex})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	out = Launched{Instance: rep.Instance, Load: rep.Load, LoadReason: rep.LoadReason}
	return
}

func launchText(l wireLaunchEntry, display string) (s string) {
	mode := l.Mode
	if mode == "" || ParseMode(mode) == ModeUnspecified {
		mode = ModeAct.String()
	}
	return "open up to " + strconv.FormatUint(uint64(l.Count), 10) + " window(s) of " + display + " in " + mode + " mode"
}
