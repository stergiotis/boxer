package attention

import (
	"crypto/rand"
	"encoding/hex"
	"iter"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Questions an app asks the person on a model's behalf wait in the queue
// beside the host's own decisions (ADR-0324 §SD1, §SD5): an app posts one on
// the bus, under the capability ClientCaps declares rather than a task
// grant, polls its status and withdraws it once answered elsewhere.

// ServiceAppId is the identity the attention service holds on the bus.
const ServiceAppId app.AppIdT = "runtime.attention"

const (
	SubjectPrefix   = "runtime.attention."
	SubjectAll      = SubjectPrefix + "*"
	SubjectPost     = SubjectPrefix + "post"
	SubjectStatus   = SubjectPrefix + "status"
	SubjectWithdraw = SubjectPrefix + "withdraw"
)

// Bounds on a posted question: what the host accepts at all, and what a card
// answers in place (ADR-0324 §SD5).
const (
	MaxTextRunes   = 600
	MaxLabelRunes  = 80
	MaxOptions     = 6
	MinQuickChoice = 2
)

// ClientCaps is what an app declares in Manifest.Caps to post questions.
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	return []app.SubjectFilter{{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: reason, Sticky: true}}
}

// QuestionOption is one choice of a question, as the model wrote it.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Question is what an app posts: the first question of a model's form, and
// how many more the form holds. Everything but Kind, Conversation, Title and
// Tainted is the model's text.
type Question struct {
	// Kind is KindQuestion or KindExitApproval.
	Kind         string `json:"kind"`
	Conversation string `json:"conversation,omitempty"`
	// Title is the conversation's title, as the app reports it.
	Title   string           `json:"title,omitempty"`
	Header  string           `json:"header,omitempty"`
	Text    string           `json:"text"`
	Options []QuestionOption `json:"options,omitempty"`
	Multi   bool             `json:"multi,omitempty"`
	// More is how many further questions the form holds.
	More int `json:"more,omitempty"`
	// Note is whether an answer needs the person's own words, which a card
	// does not take.
	Note bool `json:"note,omitempty"`
	// Tainted is whether the conversation has read untrusted content.
	Tainted bool `json:"tainted,omitempty"`
}

// Quick reports whether a card answers q in place: one question, a single
// choice among two to six options (ADR-0324 §SD5).
func (inst Question) Quick() bool {
	return inst.More == 0 && !inst.Multi && !inst.Note && len(inst.Options) >= MinQuickChoice && len(inst.Options) <= MaxOptions
}

// PhaseE is where a posted question stands.
type PhaseE uint8

const (
	PhaseUnspecified PhaseE = iota
	PhaseWaiting
	PhaseAnswered
	PhaseSkipped
	PhaseWithdrawn
	// PhaseGone is a key the service does not know, or not from this sender.
	PhaseGone
)

func (inst PhaseE) String() (s string) {
	switch inst {
	case PhaseWaiting:
		s = "waiting"
	case PhaseAnswered:
		s = "answered"
	case PhaseSkipped:
		s = "skipped"
	case PhaseWithdrawn:
		s = "withdrawn"
	case PhaseGone:
		s = "gone"
	default:
		s = "unspecified"
	}
	return
}

// Answer is a question's state as its app polls it. Choice is the chosen
// option's index when answered.
type Answer struct {
	Phase  PhaseE `json:"phase"`
	Choice int    `json:"choice"`
}

type posted struct {
	q        Question
	app      app.AppIdT
	instance uint64
	created  time.Time
	answer   Answer
}

// Questions holds the posted questions: a source of the queue. Create it with
// NewQuestions.
type Questions struct {
	mu    sync.Mutex
	items map[string]*posted
	order []string
	flood *Flood
}

func NewQuestions(flood *Flood) (q *Questions) {
	return &Questions{items: make(map[string]*posted, 8), flood: flood}
}

const questionKeyPrefix = "question/"

var _ SourceI = (*Questions)(nil)

// WaitingItems lists the questions not yet answered.
func (inst *Questions) WaitingItems() iter.Seq[Item] {
	return func(yield func(Item) bool) {
		inst.mu.Lock()
		items := make([]Item, 0, len(inst.order))
		for _, k := range inst.order {
			p := inst.items[k]
			if p.answer.Phase != PhaseWaiting {
				continue
			}
			summary := "a question"
			if p.q.Kind == KindExitApproval {
				summary = "an exit approval"
			}
			if p.q.Title != "" {
				summary += " in " + clip(p.q.Title, 60)
			}
			items = append(items, Item{Key: questionKeyPrefix + k, Kind: p.q.Kind, App: p.app, Instance: p.instance,
				Conversation: p.q.Conversation, Summary: summary, Created: p.created})
		}
		inst.mu.Unlock()
		for _, it := range items {
			if !yield(it) {
				return
			}
		}
	}
}

// Post admits q from sender through the flood limit; reason is the refusal
// the model reads.
func (inst *Questions) Post(sender app.AppIdT, instance uint64, q Question, now time.Time) (key string, reason string) {
	if q.Kind != KindQuestion && q.Kind != KindExitApproval {
		return "", "a question's kind is " + KindQuestion + " or " + KindExitApproval
	}
	if q.Text == "" {
		return "", "a question needs its text"
	}
	q.Text, q.Header, q.Title = clip(q.Text, MaxTextRunes), clip(q.Header, MaxLabelRunes), clip(q.Title, MaxLabelRunes)
	if len(q.Options) > MaxOptions {
		q.Options, q.More = q.Options[:MaxOptions], max(q.More, 1)
	}
	for i := range q.Options {
		q.Options[i].Label, q.Options[i].Description = clip(q.Options[i].Label, MaxLabelRunes), clip(q.Options[i].Description, MaxLabelRunes)
	}
	stream := q.Conversation
	if stream == "" {
		stream = string(sender) + "#" + itoa(instance)
	}
	if ok, why := inst.flood.Admit(stream, now); !ok {
		return "", "the person is not taking questions now (" + why + "); go on and say what you assumed"
	}
	key = randomKey()
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.items[key] = &posted{q: q, app: sender, instance: instance, created: now, answer: Answer{Phase: PhaseWaiting}}
	inst.order = append(inst.order, key)
	return key, ""
}

// Status is the question's state for its sender; a status read of a settled
// question forgets it.
func (inst *Questions) Status(sender app.AppIdT, instance uint64, key string) (a Answer) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p := inst.items[key]
	if p == nil || p.app != sender || p.instance != instance {
		return Answer{Phase: PhaseGone}
	}
	if p.answer.Phase != PhaseWaiting {
		inst.forget(key)
	}
	return p.answer
}

// Withdraw takes a question back, answered in its window or abandoned with
// its turn.
func (inst *Questions) Withdraw(sender app.AppIdT, instance uint64, key string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if p := inst.items[key]; p != nil && p.app == sender && p.instance == instance {
		inst.forget(key)
	}
}

func (inst *Questions) forget(key string) {
	delete(inst.items, key)
	for i, k := range inst.order {
		if k == key {
			inst.order = append(inst.order[:i], inst.order[i+1:]...)
			break
		}
	}
}

// Lookup is the question behind an item key, for its card.
func (inst *Questions) Lookup(itemKey string) (q Question, app app.AppIdT, instance uint64, ok bool) {
	key, isQ := cutPrefix(itemKey, questionKeyPrefix)
	if !isQ {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p := inst.items[key]
	if p == nil || p.answer.Phase != PhaseWaiting {
		return
	}
	return p.q, p.app, p.instance, true
}

// Decide settles a waiting question from its card: an answer with the
// chosen option, or a skip. The first answer wins; ok is false when the
// question is no longer waiting.
func (inst *Questions) Decide(itemKey string, skip bool, choice int) (ok bool) {
	key, isQ := cutPrefix(itemKey, questionKeyPrefix)
	if !isQ {
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	p := inst.items[key]
	if p == nil || p.answer.Phase != PhaseWaiting {
		return
	}
	if skip {
		p.answer = Answer{Phase: PhaseSkipped}
		return true
	}
	if choice < 0 || choice >= len(p.q.Options) || !p.q.Quick() {
		return
	}
	p.answer = Answer{Phase: PhaseAnswered, Choice: choice}
	return true
}

// OwnsItem reports whether an item key is a posted question's.
func OwnsItem(itemKey string) bool {
	_, ok := cutPrefix(itemKey, questionKeyPrefix)
	return ok
}

// The wire forms.
type wirePost struct {
	V        uint8    `json:"v"`
	Question Question `json:"question"`
}

type wirePostReply struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type wireKey struct {
	V   uint8  `json:"v"`
	Key string `json:"key"`
}

const wireVersion uint8 = 1

// BusService answers SubjectAll for a Questions store.
type BusService struct {
	q      *Questions
	client *inprocbus.Client
	unsub  func()
	log    zerolog.Logger
}

// NewBusService subscribes the store's subjects.
func NewBusService(bus *inprocbus.Inst, q *Questions, log zerolog.Logger) (s *BusService, err error) {
	if bus == nil || q == nil {
		err = eh.Errorf("nil bus or store")
		return
	}
	s = &BusService{q: q, log: log}
	s.client = bus.NewClient(ServiceAppId, []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "attention: serve the person's queue of questions (ADR-0324)"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "attention: reply to inboxes"},
	})
	if s.unsub, err = s.client.Subscribe(SubjectAll, s.handle); err != nil {
		_ = s.client.Close()
		err = eh.Errorf("subscribe: %w", err)
		return nil, err
	}
	return
}

func (inst *BusService) Close() {
	if inst.unsub != nil {
		inst.unsub()
	}
	_ = inst.client.Close()
}

func (inst *BusService) handle(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	var rep any
	switch msg.Subject {
	case SubjectPost:
		r := wirePostReply{V: wireVersion}
		req, err := buscodec.Decode[wirePost](msg.Payload)
		if err != nil {
			r.Reason = "the question does not decode"
		} else if r.Key, r.Reason = inst.q.Post(msg.Sender, msg.SenderInstance, req.Question, time.Now()); r.Reason == "" {
			r.Ok = true
		}
		rep = r
	case SubjectStatus:
		req, err := buscodec.Decode[wireKey](msg.Payload)
		if err != nil {
			rep = Answer{Phase: PhaseGone}
		} else {
			rep = inst.q.Status(msg.Sender, msg.SenderInstance, req.Key)
		}
	case SubjectWithdraw:
		if req, err := buscodec.Decode[wireKey](msg.Payload); err == nil {
			inst.q.Withdraw(msg.Sender, msg.SenderInstance, req.Key)
		}
		rep = wireKey{V: wireVersion}
	default:
		return
	}
	if err := buscodec.Reply(inst.client.Publish, msg.Reply, rep); err != nil {
		inst.log.Warn().Err(err).Str("subject", msg.Subject).Msg("attention: reply failed")
	}
}

// The client side, for an app holding ClientCaps.

// Post puts q in the person's queue. refused is the host's refusal, in words
// the model can act on.
func Post(bus app.BusI, q Question) (key string, refused string, err error) {
	payload, err := buscodec.Encode(wirePost{V: wireVersion, Question: q})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectPost, payload)
	if err != nil {
		err = eh.Errorf("post: %w", err)
		return
	}
	rep, err := buscodec.Decode[wirePostReply](raw)
	if err != nil {
		return
	}
	if !rep.Ok {
		return "", rep.Reason, nil
	}
	return rep.Key, "", nil
}

// Status reads a posted question's state.
func Status(bus app.BusI, key string) (a Answer, err error) {
	payload, err := buscodec.Encode(wireKey{V: wireVersion, Key: key})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectStatus, payload)
	if err != nil {
		err = eb.Build().Str("key", key).Errorf("status: %w", err)
		return
	}
	return buscodec.Decode[Answer](raw)
}

// Withdraw takes a posted question back.
func Withdraw(bus app.BusI, key string) (err error) {
	payload, err := buscodec.Encode(wireKey{V: wireVersion, Key: key})
	if err != nil {
		return
	}
	if _, err = bus.Request(SubjectWithdraw, payload); err != nil {
		err = eb.Build().Str("key", key).Errorf("withdraw: %w", err)
	}
	return
}

func randomKey() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
