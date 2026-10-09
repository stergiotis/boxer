package agent

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// wireCeiling is a Ceiling on the wire.
type wireCeiling struct {
	Mode    string `json:"mode,omitempty"`
	Effect  uint8  `json:"effect,omitempty"`
	Launch  bool   `json:"launch,omitempty"`
	Desktop bool   `json:"desktop,omitempty"`
	Reach   uint8  `json:"reach,omitempty"`
	Unpaced bool   `json:"unpaced,omitempty"`
}

func wireOfCeiling(c Ceiling) (w wireCeiling) {
	w = wireCeiling{Effect: uint8(c.Effect), Launch: c.Launch, Desktop: c.Desktop, Reach: uint8(c.Reach), Unpaced: c.Unpaced}
	if c.Mode != ModeUnspecified {
		w.Mode = c.Mode.String()
	}
	return
}

func ceilingOfWire(w wireCeiling) (c Ceiling) {
	c = Ceiling{Mode: ParseMode(w.Mode), Effect: app.OperationEffectE(w.Effect), Launch: w.Launch, Desktop: w.Desktop,
		Reach: min(ReachE(w.Reach), ReachNetwork), Unpaced: w.Unpaced}
	c.Effect = min(c.Effect, app.OperationEffectConsequential)
	return
}

// wireAuthority reads a task's two bounds and, when Ceiling is set, moves
// its ceiling first.
type wireAuthority struct {
	V       uint8        `json:"v"`
	Handle  string       `json:"handle"`
	Ceiling *wireCeiling `json:"ceiling,omitempty"`
}

type wireAuthorityReply struct {
	V       uint8       `json:"v"`
	Ok      bool        `json:"ok"`
	Reason  string      `json:"reason,omitempty"`
	Limited bool        `json:"limited,omitempty"`
	Ceiling wireCeiling `json:"ceiling"`
	Granted wireCeiling `json:"granted"`
}

// authority answers SubjectAuthority: it sets the task's ceiling when the
// request carries one, and reports the ceiling and what the grant allows
// under it. Only the task's coordinator may ask.
func (inst *Service) authority(msg *app.Msg) (rep wireAuthorityReply) {
	rep.V = wireVersion
	req, err := decode[wireAuthority](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	t, out, ok := inst.resolve(req.Handle, msg)
	if !ok {
		rep.Reason = out.Reason
		return
	}
	if req.Ceiling != nil {
		inst.setCeiling(t, ceilingOfWire(*req.Ceiling))
	}
	a := inst.authorityOf(t)
	rep.Ok, rep.Limited, rep.Ceiling, rep.Granted = true, a.Limited, wireOfCeiling(a.Ceiling), wireOfCeiling(a.Granted)
	return
}

// setCeiling moves a task's ceiling. Lowering it binds at once: calls are
// checked against it as they arrive, and proposals waiting on the person
// that it no longer allows end. The caller holds mu.
func (inst *Service) setCeiling(t *task, c Ceiling) {
	inst.setCeilingBy(t, c, "person", "")
}

// setCeilingBy is setCeiling recording who moved the ceiling and why: the
// person through the coordinator's settings, or a moderator lowering it
// (ADR-0300 §SD8). The caller holds mu.
func (inst *Service) setCeilingBy(t *task, c Ceiling, by string, why string) {
	c = c.normal()
	if t.ceiling != nil && *t.ceiling == c {
		return
	}
	t.ceiling = &c
	for _, rec := range t.keys {
		if rec.outcome.Phase != opwire.PhaseProposed || rec.proposal == nil || rec.proposal.taken {
			continue
		}
		if why := t.ceiling.refuseEffect(rec.spec.Effect); why != "" {
			rec.outcome = phaseOutcome(opwire.PhaseRefused, why)
		}
	}
	if why == "" {
		why = "the chat's settings"
	}
	inst.grantEvent(trail.GrantEventCeiling, by, why+": "+c.Level().String(), t, nil)
}

// modeOf is the mode an entry acts in: its own, and no higher than the
// task's ceiling. The caller holds mu.
func (inst *task) modeOf(e *entry) (m ModeE) {
	m = e.mode
	if inst.ceiling != nil {
		m = min(m, inst.ceiling.normal().Mode)
	}
	return
}

// refuseRequest is why the ceiling forbids what a grant request asks for,
// "" when it forbids none of it: a request above the ceiling never reaches
// the person.
func (inst *Ceiling) refuseRequest(modes []ModeE, launches bool, desktop ModeE, destinations []string) (why string) {
	for _, m := range modes {
		if why = inst.refuseMode(m); why != "" {
			return
		}
	}
	if launches {
		if why = inst.refuseLaunch(); why != "" {
			return
		}
	}
	if desktop != ModeUnspecified {
		if why = inst.refuseDesktop(); why != "" {
			return
		}
	}
	for _, d := range destinations {
		if why = inst.refuseDestination(d); why != "" {
			return
		}
	}
	return ""
}

// authorityOf is t's two bounds. Granted is computed from the grant as it
// stands — the windows shared and their modes, the operations their apps'
// catalogs offer an agent, the launches left, the desktop mode and the
// destinations — and capped by the ceiling. The caller holds mu.
func (inst *Service) authorityOf(t *task) (a Authority) {
	a.Ceiling = Unlimited()
	if t.ceiling != nil {
		a.Limited, a.Ceiling = true, t.ceiling.normal()
	}
	g := Ceiling{}
	for _, e := range t.entries {
		m := t.modeOf(e)
		g.Mode = max(g.Mode, m)
		if m == ModeObserve {
			g.Effect = max(g.Effect, app.OperationEffectNone)
			continue
		}
		man, ok := inst.cfg.Registry.LookupManifest(e.app)
		if !ok || man.Operations == nil {
			g.Effect = max(g.Effect, app.OperationEffectNone)
			continue
		}
		for _, spec := range man.Operations.Operations {
			// An operation above the ceiling is refused, not weakened, so
			// it adds nothing to what is granted.
			if spec.Agents && e.covers(spec.Name) && spec.Effect <= a.Ceiling.Effect {
				g.Effect = max(g.Effect, spec.Effect)
			}
		}
	}
	for _, l := range t.launches {
		if l.used < l.count {
			g.Launch = a.Ceiling.Launch
			// A window the task may open is one it will work in.
			g.Mode = max(g.Mode, min(l.mode, a.Ceiling.Mode))
		}
	}
	g.Desktop = t.desktop == ModeAct && a.Ceiling.Desktop
	for _, d := range t.destinations {
		g.Reach = max(g.Reach, min(ReachOf(d), a.Ceiling.Reach))
	}
	if g.Mode != ModeUnspecified && g.Effect < app.OperationEffectNone {
		g.Effect = app.OperationEffectNone
	}
	// Speed is the ceiling's alone: no grant asks for it.
	g.Unpaced = g.Mode != ModeUnspecified && a.Ceiling.Unpaced
	a.Granted = g.normal()
	return
}

// Authority reads a task's two bounds: the ceiling in force and what the
// grant allows under it. With set, it moves the ceiling first — the
// coordinator relays the person's settings; the model has no such tool.
func (inst *Client) Authority(ctx context.Context, handle string, set *Ceiling) (a Authority, err error) {
	req := wireAuthority{V: wireVersion, Handle: handle}
	if set != nil {
		w := wireOfCeiling(*set)
		req.Ceiling = &w
	}
	rep, err := roundTrip[wireAuthority, wireAuthorityReply](ctx, inst, SubjectAuthority, req)
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	a = Authority{Limited: rep.Limited, Ceiling: ceilingOfWire(rep.Ceiling), Granted: ceilingOfWire(rep.Granted)}
	return
}

// DefaultPace is the least time between two changes of a paced task that the
// person can see: slow enough to read what changed before the next lands.
const DefaultPace = 750 * time.Millisecond

// pace holds a visible change of a paced task until its turn: at least the
// pace after the task's previous one (ADR-0280 §SD6). It is a wait, not a
// refusal — the model's call takes longer — and the coordinator shows what
// the call is about to do while it waits. A task whose coordinator set no
// ceiling, or whose ceiling is unpaced, does not wait. Call it without mu.
func (inst *Service) pace(t *task) {
	inst.mu.Lock()
	if t.ceiling == nil || t.ceiling.Unpaced {
		inst.mu.Unlock()
		return
	}
	every := inst.cfg.Pace
	if every <= 0 {
		every = DefaultPace
	}
	now := time.Now()
	at := now
	if t.nextChange.After(at) {
		at = t.nextChange
	}
	// The slot is taken before the wait, so two calls arriving together are
	// spaced from each other as well as from the last.
	t.nextChange = at.Add(every)
	inst.mu.Unlock()
	if wait := at.Sub(now); wait > 0 {
		time.Sleep(wait)
	}
}
