package agent

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

// Intervention (ADR-0269 §SD8): a task depends on what it read since its
// last turn and on what its queued commands expect. A change by the person
// or by another task to one of those pauses the task in that window until
// the coordinator's next turn, which returns every change by other writers
// since the previous one. Changes by the task itself, and the app's own,
// never pause it.

// EventSubject is where the host announces a task's events: kind, window,
// resources, sequence — never content.
func EventSubject(task string) (subject string) { return SubjectEventPrefix + task }

// eventQueueLen bounds the events waiting to be published; past it the
// oldest are dropped, and a coordinator that misses events reads again.
const eventQueueLen = 1024

// wireEvent is one event on runtime.agent.event.{task}.
type wireEvent struct {
	V        uint8    `json:"v"`
	Task     string   `json:"task"`
	Instance uint64   `json:"instance"`
	Seq      uint64   `json:"seq"`
	Writer   string   `json:"writer"`
	Op       string   `json:"op,omitempty"`
	Phase    string   `json:"phase,omitempty"`
	Resource []string `json:"resources,omitempty"`
	// Pauses says the change paused the task in this window.
	Pauses bool `json:"pauses,omitempty"`
}

type wireChange struct {
	Instance  uint64            `json:"instance"`
	Seq       uint64            `json:"seq"`
	Writer    string            `json:"writer"`
	Op        string            `json:"op,omitempty"`
	Phase     string            `json:"phase,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Resources []string          `json:"resources,omitempty"`
	Revisions map[string]uint64 `json:"revisions,omitempty"`
}

type wireTurnReply struct {
	V       uint8        `json:"v"`
	Ok      bool         `json:"ok"`
	Reason  string       `json:"reason,omitempty"`
	Changes []wireChange `json:"changes,omitempty"`
}

// writerKind is how an event names a writer: the person, the app, this
// task, or another task.
func writerKind(writer string, task string) (kind string) {
	switch {
	case writer == opwire.WriterPerson || writer == opwire.WriterApp:
		kind = writer
	case writer == opwire.WriterTask(task):
		kind = "this task"
	case strings.HasPrefix(writer, "task:"):
		kind = "another task"
	default:
		kind = writer
	}
	return
}

// pausesTask reports whether a logged change pauses t in window key: by the
// person or another task, to a resource t depends on. The caller holds mu.
func (inst *Service) pausesTask(t *task, key uint64, e opengine.LogEntry) (pauses bool) {
	if e.Writer != opwire.WriterPerson && (!strings.HasPrefix(e.Writer, "task:") || e.Writer == opwire.WriterTask(t.id)) {
		return
	}
	if e.Phase != opwire.PhaseApplied {
		return
	}
	deps := t.dependsOn(key)
	for _, r := range e.Resources {
		if since, ok := deps[r]; ok && e.Seq > since {
			return true
		}
	}
	return
}

// dependsOn is what t depends on in window key, each with the log sequence
// after which a change matters: what it read since its last turn, as of
// the read, and what its queued commands expect. The caller holds mu.
func (inst *task) dependsOn(key uint64) (deps map[string]uint64) {
	deps = make(map[string]uint64)
	for r, seq := range inst.readSinceTurn[key] {
		deps[r] = seq
	}
	for _, rec := range inst.keys {
		if rec.instance == key && rec.routed && rec.outcome.Phase == opwire.PhaseAccepted {
			for r := range rec.req.Expects {
				if _, read := deps[r]; !read {
					deps[r] = inst.turnSeq[key]
				}
			}
		}
	}
	return
}

// pausedBy returns the change that paused t in window key since its last
// turn, if one did. The caller holds mu; it asks the host.
func (inst *Service) pausedBy(t *task, key uint64) (e opengine.LogEntry, paused bool) {
	if inst.cfg.Host == nil {
		return
	}
	entries, _, ok := inst.cfg.Host.OpsLogSince(key, t.turnSeq[key])
	if !ok {
		return
	}
	for _, cand := range entries {
		if inst.pausesTask(t, key, cand) {
			return cand, true
		}
	}
	return
}

// isPaused reports whether t is paused in window key. The caller holds mu.
func (inst *Service) isPaused(t *task, key uint64) (paused bool) {
	_, paused = inst.pausedBy(t, key)
	return
}

// startTurnAt sets where a task's first turn in window key starts: changes
// before the task came are not news to it. The caller holds mu.
func (inst *Service) startTurnAt(t *task, key uint64) {
	if inst.cfg.Host == nil {
		return
	}
	if _, latest, ok := inst.cfg.Host.OpsLogSince(key, ^uint64(0)); ok {
		t.turnSeq[key] = latest
	}
}

// turn starts a model turn: it returns every change by other writers since
// the task's previous turn in each of its windows, and lifts its pauses
// (ADR-0269 §SD3, §SD8).
func (inst *Service) turn(msg *app.Msg) (rep wireTurnReply) {
	rep.V = wireVersion
	req, err := decode[wireHandle](msg.Payload)
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
	keys := make([]uint64, 0, len(t.entries))
	for k := range t.entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		entries, latest, found := inst.cfg.Host.OpsLogSince(k, t.turnSeq[k])
		if !found {
			continue
		}
		for _, e := range entries {
			if e.Writer == opwire.WriterTask(t.id) {
				continue
			}
			rep.Changes = append(rep.Changes, wireChange{Instance: k, Seq: e.Seq, Writer: writerKind(e.Writer, t.id),
				Op: e.Op, Phase: e.Phase.String(), Reason: e.Reason, Resources: e.Resources, Revisions: e.Revisions})
		}
		if _, paused := t.pausedAt[k]; paused {
			delete(t.pausedAt, k)
			inst.grantEventAsked(trail.GrantEventResumed, "coordinator",
				"window "+strconv.FormatUint(k, 10)+": the turn listed the changes", t, nil, req.wireCause)
		}
		t.turnSeq[k] = latest
		t.readSinceTurn[k] = nil
	}
	rep.Ok = true
	return
}

// hear takes in a change a window's engine logged, on the render
// goroutine: it queues an event for each task working in that window.
func (inst *Service) hear(key uint64, e opengine.LogEntry) {
	inst.mu.Lock()
	var evs []wireEvent
	for _, t := range inst.tasks {
		if t.revoked != "" || t.entries[key] == nil {
			continue
		}
		pauses := inst.pausesTask(t, key, e)
		evs = append(evs, wireEvent{V: wireVersion, Task: t.id, Instance: key, Seq: e.Seq,
			Writer: writerKind(e.Writer, t.id), Op: e.Op, Phase: e.Phase.String(), Resource: e.Resources,
			Pauses: pauses})
		if _, already := t.pausedAt[key]; pauses && !already {
			if t.pausedAt == nil {
				t.pausedAt = make(map[uint64]uint64)
			}
			t.pausedAt[key] = e.Seq
			inst.grantEvent(trail.GrantEventPaused, writerKind(e.Writer, t.id), "window "+strconv.FormatUint(key, 10)+": "+
				strings.Join(e.Resources, ", ")+" changed (change "+strconv.FormatUint(e.Seq, 10)+")", t, nil)
		}
	}
	inst.mu.Unlock()
	for _, ev := range evs {
		select {
		case inst.events <- ev:
		default:
			inst.log.Debug().Str("task", ev.Task).Msg("agent: event queue full; dropped an event")
		}
	}
}

// publishEvents sends queued events off the render goroutine: a
// coordinator's handler runs on the publisher's goroutine.
func (inst *Service) publishEvents() {
	defer close(inst.eventsDone)
	for ev := range inst.events {
		payload, err := buscodec.Encode(ev)
		if err != nil {
			continue
		}
		if err = inst.busClient.Publish(EventSubject(ev.Task), payload); err != nil {
			inst.log.Debug().Err(err).Str("task", ev.Task).Msg("agent: publish an event")
		}
	}
}

// pausedReason is what a call refused for a pause says.
func pausedReason(e opengine.LogEntry, t *task) (s string) {
	return "paused: " + writerKind(e.Writer, t.id) + " changed " + strings.Join(e.Resources, ", ") +
		" (change " + strconv.FormatUint(e.Seq, 10) + "); call turn to see the changes"
}
