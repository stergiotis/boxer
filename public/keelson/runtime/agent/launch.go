package agent

import (
	"context"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Launches (ADR-0269 §SD3, §SD6): a grant may name apps the task may open
// windows of, with a mode and a count. A window the task opens belongs to
// the task; when the task ends it passes to the person, badged as left by
// the task.

// resolveApp finds a registered app by id or subject alias.
func (inst *Service) resolveApp(name string) (id app.AppIdT, ok bool) {
	for _, r := range inst.cfg.Registry.Registrations() {
		if matchesApp(r.Manifest, name) {
			return r.Manifest.Id, true
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
	switch {
	case !known:
		rep.Reason = "no app by that name"
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
	key, err := inst.cfg.Host.OpsOpen(id, req.Kind, req.Config)
	if err != nil {
		inst.mu.Lock()
		le.used--
		inst.mu.Unlock()
		rep.Reason = "the host did not open it: " + err.Error()
		return
	}
	inst.mu.Lock()
	t.entries[key] = &entry{instance: key, app: id, alias: id.SubjectAlias(), mode: mode}
	t.launched[key] = true
	inst.startTurnAt(t, key)
	inst.mu.Unlock()
	inst.attach(key)
	rep.Ok, rep.Instance = true, key
	return
}

// leftByTask names the task that left window key to the person, if one did.
func (inst *Service) leftByTask(key uint64) (task string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.leftBy[key]
}

// Launch opens a window of an app the grant names (an id or a subject
// alias), with an optional launch config in the app's LaunchKind; the
// window joins the task.
func (inst *Client) Launch(ctx context.Context, handle string, appName string, kind string, config []byte) (instance uint64, err error) {
	rep, err := roundTrip[wireLaunch, wireLaunchReply](ctx, inst, SubjectLaunch,
		wireLaunch{V: wireVersion, Handle: handle, App: appName, Kind: kind, Config: config})
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	instance = rep.Instance
	return
}

func launchText(l wireLaunchEntry) (s string) {
	return "open up to " + strconv.FormatUint(uint64(l.Count), 10) + " window(s) of " + l.App + " in " + l.Mode + " mode"
}
