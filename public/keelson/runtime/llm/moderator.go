package llm

import (
	"context"
	"errors"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Moderator is the rule surface as a moderator sees it (ADR-0300 §SD7):
// set and remove rules, list their state, cancel calls, and put a proposal
// to the person. The app declares [ModeratorCaps] and must be listed in
// BOXER_LLM_MODERATORS; the service refuses every verb otherwise.
type Moderator struct {
	bus app.BusI
	// Timeout bounds one request other than Ask; zero is 5 s.
	Timeout time.Duration
}

// NewModerator wraps bus.
func NewModerator(bus app.BusI) (inst *Moderator) {
	inst = &Moderator{bus: bus}
	return
}

// ErrModeratorRefused is a verb the service declined: the sender is not a
// moderator, the rule is malformed, or the change loosens a rule on the
// moderator's own calls. A refusal is a *ModeratorRefusedError carrying the
// service's reason; errors.Is matches it against this sentinel.
var ErrModeratorRefused = errors.New("llm: the service refused the moderator's request")

// ModeratorRefusedError is a moderator's verb the service declined, with
// its reason.
type ModeratorRefusedError struct {
	Reason string
}

func (inst *ModeratorRefusedError) Error() string {
	return ErrModeratorRefused.Error() + ": " + inst.Reason
}

func (inst *ModeratorRefusedError) Unwrap() error { return ErrModeratorRefused }

func (inst *Moderator) request(ctx context.Context, subject string, v any, wait time.Duration) (rep wireRationReply, err error) {
	if inst == nil || inst.bus == nil {
		return rep, eh.Errorf("llm: moderator without a bus")
	}
	payload, err := encode(v)
	if err != nil {
		return
	}
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(deadline))
	}
	raw, err := inst.bus.RequestWithTimeout(subject, payload, wait)
	if err != nil {
		return rep, eb.Build().Str("subject", subject).Errorf("llm: moderator request: %w", err)
	}
	if rep, err = decode[wireRationReply](raw); err != nil {
		return
	}
	if !rep.Ok {
		err = &ModeratorRefusedError{Reason: rep.Reason}
	}
	return
}

func (inst *Moderator) wait() (d time.Duration) {
	if d = inst.Timeout; d <= 0 {
		d = 5 * time.Second
	}
	return
}

// Set adds a rule, or replaces the one with its id. Author and SetAt are
// the service's.
func (inst *Moderator) Set(ctx context.Context, r ration.Rule) (err error) {
	_, err = inst.request(ctx, SubjectRationSet, wireRationSet{V: wireVersion, Rule: wireOfRule(r)}, inst.wait())
	return
}

// Remove drops the rule with id.
func (inst *Moderator) Remove(ctx context.Context, id string) (err error) {
	_, err = inst.request(ctx, SubjectRationSet, wireRationSet{V: wireVersion, Rule: wireRule{Id: id}, Remove: true}, inst.wait())
	return
}

// List is each rule's state on each account it applies to.
func (inst *Moderator) List(ctx context.Context) (states []ration.RuleState, err error) {
	rep, err := inst.request(ctx, SubjectRationList, struct {
		V uint8 `json:"v"`
	}{V: wireVersion}, inst.wait())
	if err != nil {
		return
	}
	return statesOfWire(rep.States)
}

// CancelCall cancels one call by its id; it says how many it stopped.
func (inst *Moderator) CancelCall(ctx context.Context, callId string, reason string) (cancelled int, err error) {
	rep, err := inst.request(ctx, SubjectRationCancel, wireRationCancel{V: wireVersion, CallId: callId, Reason: reason}, inst.wait())
	return rep.Cancelled, err
}

// CancelAccount cancels every call in flight or waiting that is charged to
// a.
func (inst *Moderator) CancelAccount(ctx context.Context, a ration.Account, reason string) (cancelled int, err error) {
	rep, err := inst.request(ctx, SubjectRationCancel, wireRationCancel{V: wireVersion, AccountKind: a.Kind.String(), AccountKey: a.Key, Reason: reason}, inst.wait())
	return rep.Cancelled, err
}

// Ask puts a proposed rule to the person with the question shown, and
// waits for the answer or ctx (ADR-0300 §SD9). A yes sets the rule with the
// person as its author. decided false is no answer.
func (inst *Moderator) Ask(ctx context.Context, r ration.Rule, question string) (granted bool, decided bool, err error) {
	rep, err := inst.request(ctx, SubjectRationAsk, wireRationAsk{V: wireVersion, Rule: wireOfRule(r), Question: question}, AskTimeout+5*time.Second)
	return rep.Granted, rep.Decided, err
}

// CallEvent is one llm.event.call: a call admission decided.
type CallEvent struct {
	CallId string
	At     time.Time
	Chain  ration.Chain
	// Conversation, Turn and Round are what the request said the call
	// belongs to; Round is read only beside a Turn.
	Conversation string
	Turn         string
	Round        uint32
	Class        string
	Admission    string
	Rule         string
	Refusal      ration.RefusalE
	Reason       string
	Queued       time.Duration
	Usage        ration.Usage
	Failed       bool
}

// DecodeCallEvent reads an llm.event.call payload.
func DecodeCallEvent(payload []byte) (e CallEvent, err error) {
	w, err := decode[wireCallEvent](payload)
	if err != nil {
		return
	}
	e = CallEvent{CallId: w.CallId, At: timeOfNs(w.At), Chain: ration.Chain{App: w.App, Instance: w.Instance, Task: w.Task, Purpose: w.Purpose},
		Class: w.Class, Admission: w.Admission, Rule: w.Rule, Refusal: ration.ParseRefusal(w.Refusal), Reason: w.Reason,
		Queued: time.Duration(w.QueuedNs), Failed: w.Failed, Conversation: w.Conversation, Turn: w.Turn, Round: w.Round}
	if len(w.Usage) > 0 {
		e.Usage = make(ration.Usage, len(w.Usage))
		for q, v := range w.Usage {
			e.Usage[ration.QuantityE(q)] = v
		}
	}
	return
}

// DecodeThresholdEvent reads an llm.event.threshold payload.
func DecodeThresholdEvent(payload []byte) (e ration.Event, err error) {
	w, err := decode[wireThresholdEvent](payload)
	if err != nil {
		return
	}
	e = ration.Event{Kind: ration.EventKindThreshold, Rule: w.Rule, Account: ration.Account{Key: w.AccountKey},
		Quantity: ration.QuantityE(w.Quantity), Used: w.Used, Limit: w.Limit, Percent: w.Percent}
	e.Account.Kind, err = ration.ParseAccountKind(w.AccountKind)
	return
}
