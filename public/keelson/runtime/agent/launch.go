package agent

import (
	"context"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// Launches (ADR-0269 §SD3, §SD6): a grant may name apps the task may open
// windows of, with a mode and a count. A window the task opens belongs to
// the task; when the task ends it passes to the person, badged as left by
// the task.

// resolveApp finds a launchable app by id or subject alias. An app the
// launch limit refuses (ADR-0272) is not found, so no grant can name it.
func (inst *Service) resolveApp(name string) (id app.AppIdT, ok bool) {
	for _, m := range inst.cfg.Registry.LaunchableManifests() {
		if matchesApp(m, name) {
			return m.Id, true
		}
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
	switch {
	case !known && inst.refusedByLimit(req.App):
		rep.Reason = "this host does not open windows of " + req.App + " (launch limit)"
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
	rep, err := roundTrip[wireLaunch, wireLaunchReply](ctx, inst, SubjectLaunch,
		wireLaunch{V: wireVersion, Handle: handle, App: appName, Kind: kind, Config: config})
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

func launchText(l wireLaunchEntry) (s string) {
	return "open up to " + strconv.FormatUint(uint64(l.Count), 10) + " window(s) of " + l.App + " in " + l.Mode + " mode"
}
