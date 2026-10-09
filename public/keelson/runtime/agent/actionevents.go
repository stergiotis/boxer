package agent

import (
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// SubjectActionRecorded carries every action record as it is recorded
// (ADR-0302 §SD2): what a moderator reads to tell a loop from work. Four
// tokens, so no `runtime.agent.*` grant covers it; a moderator subscribes
// with ModeratorCaps. The model's own words — the call's title and reason,
// its arguments — are not on it.
const SubjectActionRecorded = SubjectPrefix + "action.recorded"

// actionQueueLen bounds the records waiting to be published; past it the
// oldest are dropped, so a slow subscriber never holds up a dispatch.
const actionQueueLen = 1024

type wireActionEvent struct {
	V             uint8      `json:"v"`
	AtUnixMs      int64      `json:"at_unix_ms"`
	Task          string     `json:"task,omitempty"`
	Epoch         uint64     `json:"epoch,omitempty"`
	Actor         app.AppIdT `json:"actor,omitempty"`
	ActorInstance uint64     `json:"actor_instance,omitempty"`
	Conversation  string     `json:"conversation,omitempty"`
	Turn          string     `json:"turn,omitempty"`
	ModelCall     string     `json:"model_call,omitempty"`
	ToolCallId    string     `json:"tool_call_id,omitempty"`
	ToolIndex     uint32     `json:"tool_index,omitempty"`
	Key           string     `json:"key,omitempty"`
	CallId        string     `json:"call_id,omitempty"`
	Instance      uint64     `json:"instance,omitempty"`
	App           app.AppIdT `json:"app,omitempty"`
	Operation     string     `json:"operation,omitempty"`
	Effect        string     `json:"effect,omitempty"`
	ArgsDigest    string     `json:"args_digest,omitempty"`
	Decision      string     `json:"decision,omitempty"`
	Phase         string     `json:"phase,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	BudgetLeft    int32      `json:"budget_left,omitempty"`
	Test          bool       `json:"test,omitempty"`
	Tainted       bool       `json:"tainted,omitempty"`
}

func wireOfAction(r ActionRecord) (w wireActionEvent) {
	return wireActionEvent{V: wireVersion, AtUnixMs: r.At.UnixMilli(), Task: r.Task, Epoch: r.Epoch, Actor: r.Actor,
		ActorInstance: r.ActorInstance, Conversation: r.Conversation, Turn: r.Turn, ModelCall: r.ModelCall,
		ToolCallId: r.ToolCallId, ToolIndex: r.ToolIndex, Key: r.Key, CallId: r.CallId, Instance: r.Instance, App: r.App,
		Operation: r.Operation, Effect: r.Effect, ArgsDigest: r.ArgsDigest, Decision: r.Decision, Phase: r.Phase,
		Reason: r.Reason, BudgetLeft: r.BudgetLeft, Test: r.Test, Tainted: r.Tainted}
}

// DecodeActionEvent reads a runtime.agent.action.recorded payload as the
// record it was published from, less the model's words.
func DecodeActionEvent(payload []byte) (r ActionRecord, err error) {
	w, err := decode[wireActionEvent](payload)
	if err != nil {
		return
	}
	r = ActionRecord{At: time.UnixMilli(w.AtUnixMs).UTC(), Task: w.Task, Epoch: w.Epoch, Actor: w.Actor,
		ActorInstance: w.ActorInstance, Conversation: w.Conversation, Turn: w.Turn, ModelCall: w.ModelCall,
		ToolCallId: w.ToolCallId, ToolIndex: w.ToolIndex, Key: w.Key, CallId: w.CallId, Instance: w.Instance, App: w.App,
		Operation: w.Operation, Effect: w.Effect, ArgsDigest: w.ArgsDigest, Decision: w.Decision, Phase: w.Phase,
		Reason: w.Reason, BudgetLeft: w.BudgetLeft, Test: w.Test, Tainted: w.Tainted}
	return
}

// actionQueue hands records from the dispatch path to the publisher.
type actionQueue struct {
	mu      sync.Mutex
	pending []wireActionEvent
	dropped uint64
	closed  bool
	wake    chan struct{}
	done    chan struct{}
}

// startActionEvents starts the publisher; NewService calls it.
func (inst *Service) startActionEvents() {
	q := &inst.actionEvents
	q.wake, q.done = make(chan struct{}, 1), make(chan struct{})
	go inst.publishActions()
}

// stopActionEvents publishes what is queued and stops; Close calls it.
func (inst *Service) stopActionEvents() {
	q := &inst.actionEvents
	if q.done == nil {
		return
	}
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	<-q.done
}

// queueAction queues a record for publishing, dropping the oldest when
// the queue is full.
func (inst *Service) queueAction(r ActionRecord) {
	q := &inst.actionEvents
	q.mu.Lock()
	if q.closed || q.wake == nil {
		q.mu.Unlock()
		return
	}
	if len(q.pending) >= actionQueueLen {
		q.pending = q.pending[1:]
		q.dropped++
	}
	q.pending = append(q.pending, wireOfAction(r))
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (inst *Service) publishActions() {
	q := &inst.actionEvents
	defer close(q.done)
	var dropped uint64
	for range q.wake {
		q.mu.Lock()
		batch, closed := q.pending, q.closed
		q.pending = nil
		if q.dropped != dropped {
			inst.log.Warn().Uint64("dropped", q.dropped-dropped).Msg("agent: action events dropped; a subscriber is slow")
			dropped = q.dropped
		}
		q.mu.Unlock()
		for _, w := range batch {
			payload, err := encode(w)
			if err == nil {
				err = inst.busClient.Publish(SubjectActionRecorded, payload)
			}
			if err != nil {
				inst.log.Debug().Err(err).Msg("agent: publish an action event")
			}
		}
		if closed {
			return
		}
	}
}
