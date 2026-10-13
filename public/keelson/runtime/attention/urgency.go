package attention

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"iter"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Urgency (ADR-0323 §SD4, §SD5, update of 2026-10-12): a conversation
// marked urgent has its waiting items raised to critical, so they open at
// once, also while the person is Busy (ADR-0328 §SD2). The waiting scale is
// set only in host chrome, so an app does not set it: it asks, the host
// shows the request as an item of the queue, and the person's Make urgent
// in the host's card sets the levels, recorded on the trail as theirs. An
// app cannot make itself urgent, whoever drives it.

// KindUrgency is a window's request to make a conversation urgent, or to
// stop.
const KindUrgency = "urgency"

// SubjectUrgent carries an app's request.
const SubjectUrgent = SubjectPrefix + "urgent"

// UrgentKinds are the waiting items of a conversation an urgency raises to
// critical: what a conversation asks of the person.
var UrgentKinds = []string{KindGrantRequest, KindWidening, KindConfirmation, KindQuestion, KindExitApproval}

// IsUrgent is whether s, a conversation's snapshot of the table, marks it
// urgent: its questions raised to critical by the person.
func IsUrgent(conversation map[Key]LevelE) bool {
	return conversation[Key{Kind: KindQuestion}] == LevelCritical
}

const urgencyKeyPrefix = "urgency/"

type urgencyRequest struct {
	conversation string
	title        string
	on           bool
	app          app.AppIdT
	instance     uint64
	created      time.Time
}

// Urgencies holds the windows' requests, one per conversation, the latest:
// a source of the queue. Create it with NewUrgencies.
type Urgencies struct {
	mu     sync.Mutex
	levels *Levels
	flood  *Flood
	items  map[string]*urgencyRequest
	order  []string
}

func NewUrgencies(levels *Levels, flood *Flood) (u *Urgencies) {
	return &Urgencies{levels: levels, flood: flood, items: make(map[string]*urgencyRequest, 4)}
}

var _ SourceI = (*Urgencies)(nil)

// WaitingItems lists the requests not yet decided.
func (inst *Urgencies) WaitingItems() iter.Seq[Item] {
	return func(yield func(Item) bool) {
		inst.mu.Lock()
		items := make([]Item, 0, len(inst.order))
		for _, conv := range inst.order {
			r := inst.items[conv]
			items = append(items, Item{Key: urgencyKeyPrefix + conv, Kind: KindUrgency, App: r.app, Instance: r.instance,
				Conversation: conv, Summary: urgencySummary(r), Created: r.created})
		}
		inst.mu.Unlock()
		for _, it := range items {
			if !yield(it) {
				return
			}
		}
	}
}

func urgencySummary(r *urgencyRequest) string {
	name := r.title
	if name == "" {
		name = "conversation " + r.conversation
	}
	if r.on {
		return "make " + name + " urgent?"
	}
	return "stop treating " + name + " as urgent?"
}

// Request admits a window's request through the flood limit; it replaces the
// conversation's earlier one. reason is the refusal.
func (inst *Urgencies) Request(sender app.AppIdT, instance uint64, conversation string, title string, on bool, now time.Time) (reason string) {
	if conversation == "" {
		return "an urgency names its conversation"
	}
	if ok, why := inst.flood.Admit(conversation, now); !ok {
		return why
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, had := inst.items[conversation]; !had {
		inst.order = append(inst.order, conversation)
	}
	inst.items[conversation] = &urgencyRequest{conversation: conversation, title: clip(title, MaxLabelRunes), on: on,
		app: sender, instance: instance, created: now}
	return ""
}

// Lookup is the request behind an item key, for its card.
func (inst *Urgencies) Lookup(itemKey string) (conversation string, title string, on bool, from app.AppIdT, instance uint64, ok bool) {
	conv, isU := cutPrefix(itemKey, urgencyKeyPrefix)
	if !isU {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	r := inst.items[conv]
	if r == nil {
		return
	}
	return r.conversation, r.title, r.on, r.app, r.instance, true
}

// Decide settles a request from its card. Accepted, the person's levels for
// the conversation's waiting kinds are set to critical, or cleared; either
// way the request goes. ok is false when it is no longer waiting.
func (inst *Urgencies) Decide(itemKey string, accept bool) (ok bool, err error) {
	conv, isU := cutPrefix(itemKey, urgencyKeyPrefix)
	if !isU {
		return
	}
	inst.mu.Lock()
	r := inst.items[conv]
	if r != nil {
		delete(inst.items, conv)
		for i, k := range inst.order {
			if k == conv {
				inst.order = append(inst.order[:i], inst.order[i+1:]...)
				break
			}
		}
	}
	inst.mu.Unlock()
	if r == nil {
		return
	}
	if accept {
		for _, kind := range UrgentKinds {
			k := Key{Kind: kind}
			var kerr error
			if r.on {
				kerr = inst.levels.Set(AuthorPerson, "", conv, k, LevelCritical)
			} else {
				kerr = inst.levels.Clear(AuthorPerson, "", conv, k)
			}
			if kerr != nil && err == nil {
				err = eb.Build().Str("kind", kind).Errorf("urgency: %w", kerr)
			}
		}
	}
	return true, err
}

// OwnsUrgency reports whether an item key is an urgency request's.
func OwnsUrgency(itemKey string) bool {
	_, ok := cutPrefix(itemKey, urgencyKeyPrefix)
	return ok
}

type wireUrgent struct {
	V            uint8  `json:"v"`
	Conversation string `json:"conversation"`
	Title        string `json:"title,omitempty"`
	On           bool   `json:"on"`
}

// RequestUrgent asks the host to make conversation urgent, or, with on
// false, to stop. The person decides in the host's card; refused is the
// host's refusal.
func RequestUrgent(bus app.BusI, conversation string, title string, on bool) (refused string, err error) {
	payload, err := buscodec.Encode(wireUrgent{V: wireVersion, Conversation: conversation, Title: title, On: on})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectUrgent, payload)
	if err != nil {
		err = eh.Errorf("urgent: %w", err)
		return
	}
	rep, err := buscodec.Decode[wireAck](raw)
	if err != nil {
		return
	}
	return rep.Reason, nil
}

func (inst *BusService) urgentHandle(msg *app.Msg) (rep wireAck) {
	rep.V = wireVersion
	if inst.urgencies == nil {
		rep.Reason = "this host takes no urgency requests"
		return
	}
	req, err := buscodec.Decode[wireUrgent](msg.Payload)
	if err != nil {
		rep.Reason = "the request does not decode"
		return
	}
	if rep.Reason = inst.urgencies.Request(msg.Sender, msg.SenderInstance, req.Conversation, req.Title, req.On, time.Now()); rep.Reason == "" {
		rep.Ok = true
	}
	return
}
