package attention

import (
	"slices"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Notices are the highlights of ADR-0323: an event nothing waits on that the
// person asked to hear of, such as a watched conversation's turn ending
// (§SD4). An app reports the event; the host classifies it with its own
// table and keeps it only at highlight. A notice is not an item of the
// queue: it opens no card and is counted apart from what waits. One is kept
// per source, the latest — a conversation, or, for a job, the owning app
// and the job id (§SD7); it goes when the person looks at the
// conversation's window or dismisses it. A job's notice is reported by the
// watchbill worker in process (NotifyJob), never over the bus, since no
// window owns it.

const (
	// SubjectNotify reports an event the app's copy of the table classed as
	// a highlight.
	SubjectNotify = SubjectPrefix + "notify"
	// SubjectSeen says the person looked at a conversation.
	SubjectSeen = SubjectPrefix + "seen"
	// SubjectNotices lists the notices kept.
	SubjectNotices = SubjectPrefix + "notices"
)

// MaxNotices bounds the notices kept; the oldest goes first.
const MaxNotices = 32

// Notice is a highlight kept for the person.
type Notice struct {
	Conversation string `json:"conversation"`
	// Job is the job id when the source is a job rather than a
	// conversation; App is then the app that owns the job.
	Job string `json:"job,omitempty"`
	// Title is the conversation's title, as the app reports it.
	Title string `json:"title,omitempty"`
	// Kind and Effect are the event's, as ADR-0323 §SD3 keys it.
	Kind   string               `json:"kind"`
	Effect app.OperationEffectE `json:"effect,omitempty"`
	// Text is the app's line for it, such as "the turn ended".
	Text     string     `json:"text,omitempty"`
	App      app.AppIdT `json:"app,omitempty"`
	Instance uint64     `json:"instance,omitempty"`
	Level    LevelE     `json:"level,omitempty"`
	Created  time.Time  `json:"created"`
}

// Notices holds the notices. Create it with NewNotices.
type Notices struct {
	mu     sync.Mutex
	levels *Levels
	items  []Notice
	// version counts changes, for a reader that redraws on one.
	version uint64
	onKept  func(Notice)
}

// SetOnKept installs the hook told of each notice kept, outside the lock.
func (inst *Notices) SetOnKept(f func(Notice)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onKept = f
}

func NewNotices(levels *Levels) (n *Notices) {
	return &Notices{levels: levels}
}

// Notify keeps n when the table classes it as a highlight, replacing the
// conversation's earlier notice; it reports the level the host gave it.
func (inst *Notices) Notify(sender app.AppIdT, instance uint64, n Notice, now time.Time) (level LevelE) {
	if n.Conversation == "" {
		return LevelUnspecified
	}
	if s, known := ScaleOf(n.Kind); !known || s != ScaleAttention {
		return LevelUnspecified
	}
	level = inst.levels.Classify(Event{Kind: n.Kind, Effect: n.Effect, App: sender, Conversation: n.Conversation})
	if level != LevelHighlight {
		return
	}
	n.Title, n.Text = clip(n.Title, MaxLabelRunes), clip(n.Text, MaxLabelRunes)
	n.App, n.Instance, n.Level, n.Created = sender, instance, level, now
	inst.mu.Lock()
	inst.items = slices.DeleteFunc(inst.items, func(x Notice) bool { return x.Conversation == n.Conversation })
	inst.items = append(inst.items, n)
	if len(inst.items) > MaxNotices {
		inst.items = slices.Delete(inst.items, 0, len(inst.items)-MaxNotices)
	}
	inst.version++
	hook := inst.onKept
	inst.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return
}

// NotifyJob keeps a job's terminal state as owner's notice when the table
// classes it as a highlight under owner's key (ADR-0323 §SD6, §SD7),
// replacing the job's earlier notice; text is the worker's line for it.
// It reports the level the table gave it.
func (inst *Notices) NotifyJob(owner app.AppIdT, job string, kind string, text string, now time.Time) (level LevelE) {
	if job == "" {
		return LevelUnspecified
	}
	if s, known := ScaleOf(kind); !known || s != ScaleAttention {
		return LevelUnspecified
	}
	level = inst.levels.Classify(Event{Kind: kind, App: owner})
	if level != LevelHighlight {
		return
	}
	n := Notice{Job: job, App: owner, Kind: kind, Title: clip("job "+job, MaxLabelRunes), Text: clip(text, MaxLabelRunes), Level: level, Created: now}
	inst.mu.Lock()
	inst.items = slices.DeleteFunc(inst.items, func(x Notice) bool { return x.Job == job && x.App == owner })
	inst.items = append(inst.items, n)
	if len(inst.items) > MaxNotices {
		inst.items = slices.Delete(inst.items, 0, len(inst.items)-MaxNotices)
	}
	inst.version++
	hook := inst.onKept
	inst.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return
}

// SeenJob drops a job's notice: the person opened the watchbill window
// from it, or dismissed it.
func (inst *Notices) SeenJob(owner app.AppIdT, job string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n := len(inst.items)
	inst.items = slices.DeleteFunc(inst.items, func(x Notice) bool { return x.Job == job && x.App == owner && x.Job != "" })
	if len(inst.items) != n {
		inst.version++
	}
}

// Seen drops the conversation's notice.
func (inst *Notices) Seen(conversation string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n := len(inst.items)
	inst.items = slices.DeleteFunc(inst.items, func(x Notice) bool { return x.Conversation == conversation && x.Job == "" })
	if len(inst.items) != n {
		inst.version++
	}
}

// List is the notices kept, newest first.
func (inst *Notices) List() (out []Notice) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = slices.Clone(inst.items)
	slices.Reverse(out)
	return
}

// Version counts the changes.
func (inst *Notices) Version() (v uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.version
}

// The wire forms.
type wireNotice struct {
	V      uint8  `json:"v"`
	Notice Notice `json:"notice"`
}

type wireNoticeReply struct {
	V     uint8 `json:"v"`
	Level uint8 `json:"level"`
}

type wireConversation struct {
	V            uint8  `json:"v"`
	Conversation string `json:"conversation"`
}

type wireNotices struct {
	V       uint8    `json:"v"`
	Notices []Notice `json:"notices,omitempty"`
}

func (inst *BusService) noticesHandle(msg *app.Msg) (rep any) {
	switch msg.Subject {
	case SubjectNotify:
		r := wireNoticeReply{V: wireVersion}
		if req, err := buscodec.Decode[wireNotice](msg.Payload); err == nil {
			r.Level = uint8(inst.notices.Notify(msg.Sender, msg.SenderInstance, req.Notice, time.Now()))
		}
		return r
	case SubjectSeen:
		if req, err := buscodec.Decode[wireConversation](msg.Payload); err == nil {
			inst.notices.Seen(req.Conversation)
		}
		return wireConversation{V: wireVersion}
	default:
		return wireNotices{V: wireVersion, Notices: inst.notices.List()}
	}
}

// The client side, for an app holding ClientCaps.

// Notify reports an event of a conversation; level is the one the host gave
// it, a highlight when kept.
func Notify(bus app.BusI, n Notice) (level LevelE, err error) {
	payload, err := buscodec.Encode(wireNotice{V: wireVersion, Notice: n})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectNotify, payload)
	if err != nil {
		err = eh.Errorf("notify: %w", err)
		return
	}
	rep, err := buscodec.Decode[wireNoticeReply](raw)
	if err != nil {
		return
	}
	return LevelE(rep.Level), nil
}

// Seen says the person looked at conversation.
func Seen(bus app.BusI, conversation string) (err error) {
	payload, err := buscodec.Encode(wireConversation{V: wireVersion, Conversation: conversation})
	if err != nil {
		return
	}
	if _, err = bus.Request(SubjectSeen, payload); err != nil {
		err = eh.Errorf("seen: %w", err)
	}
	return
}

// FetchNotices lists the notices kept, newest first.
func FetchNotices(bus app.BusI) (out []Notice, err error) {
	payload, err := buscodec.Encode(wireConversation{V: wireVersion})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectNotices, payload)
	if err != nil {
		err = eh.Errorf("notices: %w", err)
		return
	}
	rep, err := buscodec.Decode[wireNotices](raw)
	if err != nil {
		return
	}
	return rep.Notices, nil
}
