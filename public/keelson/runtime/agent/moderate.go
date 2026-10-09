package agent

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The moderator's levers on agent tasks (ADR-0300 §SD8) and its questions
// to the person (§SD9). A moderator is an app the deployment lists as one
// (BOXER_LLM_MODERATORS, installed with SetModerators): it may stop any
// task and lower any task's ceiling, never raise one — raising stays the
// person's (ADR-0298 §SD3).

const (
	// SubjectModeratePrefix precedes the moderator's verbs. Three tokens
	// after "runtime.", so no `runtime.agent.*` grant covers them.
	SubjectModeratePrefix = SubjectPrefix + "moderate."
	// SubjectModerateStop ends the tasks named.
	SubjectModerateStop = SubjectModeratePrefix + "stop"
	// SubjectModerateAuthority lowers the ceiling of the tasks named.
	SubjectModerateAuthority = SubjectModeratePrefix + "authority"
	// SubjectModerateAll is the service's subscription pattern for them.
	SubjectModerateAll = SubjectModeratePrefix + "*"
)

// ModeratorCaps is what a moderator declares to reach the verbs, and to
// follow the action records (ADR-0302 §SD2).
func ModeratorCaps(reason string) (caps []app.SubjectFilter) {
	caps = []app.SubjectFilter{
		{Pattern: SubjectModerateAll, Direction: app.CapDirectionPub, Reason: reason},
		{Pattern: SubjectActionRecorded, Direction: app.CapDirectionSub, Reason: reason},
	}
	return
}

// ModerateTarget names the tasks a moderator's verb acts on: the task with
// Task's id, or every live task whose coordinator is the window Instance —
// the account a moderator sees a loop's model calls charged to.
type ModerateTarget struct {
	Task     string
	Instance uint64
}

type wireModerate struct {
	V        uint8        `json:"v"`
	Task     string       `json:"task,omitempty"`
	Instance uint64       `json:"instance,omitempty"`
	Reason   string       `json:"reason,omitempty"`
	Ceiling  *wireCeiling `json:"ceiling,omitempty"`
}

type wireModerateReply struct {
	V      uint8    `json:"v"`
	Ok     bool     `json:"ok"`
	Reason string   `json:"reason,omitempty"`
	Tasks  []string `json:"tasks,omitempty"`
}

// moderation is the service's moderator state.
type moderation struct {
	moderators atomic.Pointer[[]string]
	unsub      func()

	mu        sync.Mutex
	questions []*question
	nextKey   uint64
}

// question is one a moderator put to the person, waiting for the answer.
type question struct {
	key       string
	moderator app.AppIdT
	text      string
	rule      string
	answer    chan bool
}

// SetModerators installs the apps that may use the moderator's verbs.
func (inst *Service) SetModerators(ids []string) {
	ids = slices.Clone(ids)
	inst.moderation.moderators.Store(&ids)
}

func (inst *Service) isModerator(id app.AppIdT) (yes bool) {
	if ids := inst.moderation.moderators.Load(); ids != nil {
		return slices.Contains(*ids, string(id))
	}
	return false
}

// subscribeModerate is called by NewService.
func (inst *Service) subscribeModerate() (err error) {
	inst.moderation.unsub, err = inst.busClient.Subscribe(SubjectModerateAll, inst.handleModerate)
	if err != nil {
		err = eh.Errorf("agent: subscribe to the moderator's verbs: %w", err)
	}
	return
}

func (inst *Service) handleModerate(msg *app.Msg) {
	if msg.Reply == "" {
		return
	}
	rep := wireModerateReply{V: wireVersion}
	defer func() { inst.reply(msg.Reply, rep) }()
	if !inst.isModerator(msg.Sender) {
		rep.Reason = string(msg.Sender) + " is not listed in BOXER_LLM_MODERATORS"
		return
	}
	req, err := decode[wireModerate](msg.Payload)
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	if req.Task == "" && req.Instance == 0 {
		rep.Reason = "name a task or a coordinator's window"
		return
	}
	by := string(msg.Sender)
	why := "a moderator (" + by + ")"
	if req.Reason != "" {
		why += ": " + req.Reason
	}
	inst.mu.Lock()
	var tasks []*task
	for _, t := range inst.tasks {
		if t.revoked != "" {
			continue
		}
		if req.Task != "" && t.id == req.Task || req.Task == "" && t.actorInstance == req.Instance {
			tasks = append(tasks, t)
		}
	}
	slices.SortFunc(tasks, func(a, b *task) int {
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	switch msg.Subject {
	case SubjectModerateStop:
		inst.mu.Unlock()
		for _, t := range tasks {
			inst.endTask(t, "stopped by "+why, by)
			rep.Tasks = append(rep.Tasks, t.id)
		}
		rep.Ok = true
	case SubjectModerateAuthority:
		defer inst.mu.Unlock()
		if req.Ceiling == nil {
			rep.Reason = "name the ceiling to lower to"
			return
		}
		lower := ceilingOfWire(*req.Ceiling)
		for _, t := range tasks {
			cur := Unlimited()
			if t.ceiling != nil {
				cur = *t.ceiling
			}
			inst.setCeilingBy(t, lowerOf(cur, lower), by, "lowered by "+why)
			rep.Tasks = append(rep.Tasks, t.id)
		}
		rep.Ok = true
	default:
		inst.mu.Unlock()
		rep.Reason = "unknown verb " + msg.Subject
	}
}

// lowerOf is the ceiling that allows only what both a and b allow: a
// moderator's lowering never raises any part of a task's ceiling.
func lowerOf(a Ceiling, b Ceiling) (l Ceiling) {
	l = Ceiling{Mode: min(a.Mode, b.Mode), Effect: min(a.Effect, b.Effect), Launch: a.Launch && b.Launch,
		Desktop: a.Desktop && b.Desktop, Reach: min(a.Reach, b.Reach), Unpaced: a.Unpaced && b.Unpaced}
	return l.normal()
}

// ModerateStop ends the tasks target names; it returns their ids.
func (inst *Client) ModerateStop(ctx context.Context, target ModerateTarget, reason string) (tasks []string, err error) {
	return inst.moderate(ctx, SubjectModerateStop, wireModerate{V: wireVersion, Task: target.Task, Instance: target.Instance, Reason: reason})
}

// ModerateCeiling lowers the ceiling of the tasks target names to at most
// ceiling; a part ceiling would raise is left as it is.
func (inst *Client) ModerateCeiling(ctx context.Context, target ModerateTarget, ceiling Ceiling, reason string) (tasks []string, err error) {
	w := wireOfCeiling(ceiling)
	return inst.moderate(ctx, SubjectModerateAuthority, wireModerate{V: wireVersion, Task: target.Task, Instance: target.Instance, Reason: reason, Ceiling: &w})
}

func (inst *Client) moderate(ctx context.Context, subject string, req wireModerate) (tasks []string, err error) {
	rep, err := roundTrip[wireModerate, wireModerateReply](ctx, inst, subject, req)
	if err != nil {
		return
	}
	if !rep.Ok {
		err = &RefusedError{Reason: rep.Reason}
		return
	}
	return rep.Tasks, nil
}

// AskPerson puts a moderator's question to the person in the host's
// dialog and waits for the answer until ctx ends (ADR-0300 §SD9). decided
// false is no answer: the question expired unanswered.
func (inst *Service) AskPerson(ctx context.Context, moderator app.AppIdT, text string, rule string) (granted bool, decided bool, err error) {
	m := &inst.moderation
	m.mu.Lock()
	m.nextKey++
	q := &question{key: strconv.FormatUint(m.nextKey, 10), moderator: moderator, text: text, rule: rule, answer: make(chan bool, 1)}
	m.questions = append(m.questions, q)
	m.mu.Unlock()
	select {
	case granted = <-q.answer:
		return granted, true, nil
	case <-ctx.Done():
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.Index(m.questions, q); i >= 0 {
		m.questions = slices.Delete(m.questions, i, i+1)
		return false, false, nil
	}
	// Answered as ctx ended.
	return <-q.answer, true, nil
}

// answer settles the question and takes it off the queue.
func (inst *Service) answer(q *question, granted bool) {
	m := &inst.moderation
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := slices.Index(m.questions, q); i >= 0 {
		m.questions = slices.Delete(m.questions, i, i+1)
		q.answer <- granted
	}
}

// renderQuestion draws the oldest open moderator question, one at a time,
// as the request dialog does; it says whether there was one. The caller is
// the render goroutine.
func (inst *Chrome) renderQuestion(ids *c.WidgetIdStack) (shown bool) {
	svc := inst.svc
	m := &svc.moderation
	m.mu.Lock()
	if len(m.questions) == 0 {
		m.mu.Unlock()
		return false
	}
	q, waiting := m.questions[0], len(m.questions)
	m.mu.Unlock()
	var allow, decline bool
	for range c.Modal(ids.PrepareStr("agent-moderator-question-" + q.key)).KeepIter() {
		dialogHeading("A model-use moderator asks", confirmWidth)
		c.Label(svc.display(q.moderator) + " asks:").Wrap().Send()
		bounded(ids.PrepareStr("agent-moderator-question-text-"+q.key), modelTextHeight, func() {
			c.Label(q.text).Wrap().Send()
		})
		c.Label("Allowing it sets this rule, with you as its author: " + q.rule).Wrap().Send()
		if waiting > 1 {
			c.Label(strconv.Itoa(waiting-1) + " more waiting").Send()
		}
		c.Separator().Send()
		for range c.UiWithLayout().MainDirRightToLeft().CrossAlignMin().KeepIter() {
			allow = c.Button(ids.PrepareStr("agent-moderator-allow-"+q.key), c.Atoms().Text("Allow").Keep()).SendResp().HasPrimaryClicked()
			decline = c.Button(ids.PrepareStr("agent-moderator-decline-"+q.key), c.Atoms().Text("Decline").Keep()).SendResp().HasPrimaryClicked()
		}
	}
	switch {
	case allow:
		svc.answer(q, true)
	case decline:
		svc.answer(q, false)
	}
	return true
}
