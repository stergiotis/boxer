package chat

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/anchorstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The coordinator (ADR-0269 M5): with Apps on, a turn is a tool loop over
// the host's runtime.agent services; with Questions on, the loop offers
// ask_user as well, or alone (chat_ask.go). The model works only in windows the
// person shares in the host's dialog, every call is checked by the host's
// dispatcher, and what comes back from an app reaches the model delimited
// and attributed, as data. Nothing here touches the UI.

// The rounds of a turn bound its model calls: a guard against a model that
// never stops calling tools. The host's call budget per task is what bounds
// the calls themselves. The last round offers no tools, so a turn that
// reaches it still ends with an answer. The person picks the limit per chat
// in Settings, within RoundsMinEnv and RoundsMaxEnv; RoundsEnv, by default
// defaultRounds, is where it starts.
const (
	defaultRounds   = 24
	defaultRoundsLo = 4
	defaultRoundsHi = 96
)

var RoundsEnv = env.NewInt(env.Spec{
	Name:        "BOXER_CHAT_ROUNDS",
	Default:     strconv.Itoa(defaultRounds),
	Description: "the rounds of model calls a chat turn with tools starts with in a new chat window, within BOXER_CHAT_ROUNDS_MIN and BOXER_CHAT_ROUNDS_MAX; the person can move it in the chat's Settings",
	Category:    env.CategoryE("boxer-chat"),
})

var RoundsMinEnv = env.NewInt(env.Spec{
	Name:        "BOXER_CHAT_ROUNDS_MIN",
	Default:     "4",
	Description: "the fewest rounds of model calls a chat turn with tools may be given in the chat's Settings; the last round of a turn offers no tools, so it ends with an answer",
	Category:    env.CategoryE("boxer-chat"),
})

var RoundsMaxEnv = env.NewInt(env.Spec{
	Name:        "BOXER_CHAT_ROUNDS_MAX",
	Default:     "96",
	Description: "the most rounds of model calls a chat turn with tools may be given in the chat's Settings",
	Category:    env.CategoryE("boxer-chat"),
})

// roundRange is the range the Settings slider offers.
func roundRange() (lo int, hi int) {
	if lo = int(RoundsMinEnv.Get()); lo < 2 {
		// One round would be the last, with no tools at all.
		lo = 2
	}
	return lo, max(int(RoundsMaxEnv.Get()), lo)
}

// clampRounds is n within roundRange; zero is RoundsEnv.
func clampRounds(n int) int {
	if n == 0 {
		n = int(RoundsEnv.Get())
	}
	lo, hi := roundRange()
	return min(max(n, lo), hi)
}

// callWait bounds how long a tool call waits for its call to settle while
// the call does not wait on the person.
const callWait = 10 * time.Second

const (
	untrustedOpen  = "<<untrusted source=\""
	untrustedClose = "<<end untrusted>>"
)

// coordinator is one conversation's side of the contract.
type coordinator struct {
	cli *agent.Client
	// captureHook stands in for the host's capture service in tests.
	captureHook func(ctx context.Context, windows []uint64, crop *[4]float32) (data []byte, reason string)
	// discloseHook stands in for the host's disclosure record in tests.
	discloseHook func(d agent.Disclosure) error
	// kq reads keelson('windows') and keelson('desktop') (ADR-0276 §SD2);
	// nil leaves query_windows unanswered.
	kq           *keelsonquery.Client
	conversation string

	// opTools offers each operation of the task's windows as a typed tool;
	// ceiling is the most the person's settings let the model do (ADR-0280),
	// sent with every request for access, and told is the one the model was
	// last told of. All three are the settings', guarded by mu.
	opTools  bool
	ceiling  agent.Ceiling
	told     agent.Ceiling
	toldOnce bool
	// ask carries ask_user's questions to the form and the answers back.
	ask *asker
	// art is the conversation's artefact when it has one (ADR-0282), nil
	// otherwise; artTold is the revision the model was last told of.
	art      *artefact
	artTold  int
	artToldN bool
	// curTitle is the running call's title, for the revision it makes.
	curTitle string
	// stage is what the running tool loop is doing, and waiting how many of
	// its calls wait on the person; the window reads both for the turn's
	// state (chat_turnstate.go).
	stage   stageE
	waiting int
	// steps are the running turn's steps (chat_steps.go), for the window —
	// the waiting bubble's account, not the host's audit trail.
	steps *turnSteps
	// rounds is the person's limit on a turn's rounds, read when a turn
	// starts; zero is RoundsEnv. Guarded by mu.
	rounds int
	// answered are the running turn's answered model calls, kept as they
	// answer so a turn that fails or is stopped later still counts them.
	// Guarded by mu.
	answered []callStat
	// pix is the Pixels setting and what it holds: consents, a view waiting
	// on the person, the turn's shown screenshots (ADR-0287). Guarded by mu.
	pix pixelState

	mu sync.Mutex
	// apps and questions are what the conversation offers the model, fixed
	// at its first send: the window tools, and ask_user.
	apps      bool
	questions bool
	grant     agent.Grant
	// tasks are the tasks the conversation held, in order: a stopped task
	// stays, so the Analytics panel counts its calls (ADR-0283 §SD2).
	tasks    []string
	tainted  bool
	confined bool
	// refused holds the calls refused in this turn since the last call that
	// was not, by tool and arguments; refusal is the current call's, set
	// while it runs.
	refused map[string]string
	refusal string
	// typed names the operation tools of the latest model call; opCache
	// holds each app's operations with their schemas, and windowApps which
	// app each window listed so far runs, for the effect class of a call
	// into it.
	typed      map[string]typedOp
	opCache    map[string][]agent.Operation
	windowApps map[uint64]string
	// opEffects holds each app's operations' declared effect classes, from
	// every describe_app answer and from opCache, for the effect of a call
	// before it is made (chat_journal.go effectOf).
	opEffects map[string]map[string]string
	// trees are the window trees read in this conversation, by the
	// reference read_window_tree gave the model (ADR-0301), oldest first;
	// at most maxTrees.
	trees   []namedTree
	treeSeq int
	// store is where the run's journal is kept and journal the run's
	// single writer, opened at the first act (chat_journal.go); route is
	// the model route for the marks and window the coordinator's instance
	// for the run row.
	store   agentrun.StoreI
	journal *agentrun.Writer
	route   journalRoute
	window  string
	// profile is the run's, pinned at its opening (ADR-0310 §SD9), and
	// fingerprint the profile's; anchors is where the chain's heads go,
	// nil when the host has none.
	profile     ProfileT
	fingerprint string
	anchors     anchorstore.StoreI
	// lastUntrusted says the running call's result was untrusted text.
	lastUntrusted bool
	// compacted says the run's history was compacted (chat_compact.go).
	compacted bool
	// lastWriter is who wrote the running call's result when a model did,
	// and lastDecided who decided the call when the person was asked
	// (chat_endorse.go).
	lastWriter  writerRef
	lastDecided string
	// turns counts the turns running over this coordinator, and retired
	// says the conversation moved on: the run closes once the last turn
	// has written its tail (chat_journal.go retire). Guarded by mu.
	turns   int
	retired bool
	// closed is done once the run is closed after a retire; closeOnce
	// guards it. journalFailed says the writer refused a write and the next
	// turn reopens the run before its first act.
	closed        chan struct{}
	closeOnce     sync.Once
	journalFailed bool
}

// newCoordinator is a coordinator over an in-process journal store; the
// app replaces the store with the host's service before the first act.
func newCoordinator(cli *agent.Client, kq *keelsonquery.Client, conversation string) (inst *coordinator) {
	return &coordinator{cli: cli, kq: kq, conversation: conversation, opTools: OperationToolsSeed.Get(), ask: &asker{}, steps: &turnSteps{},
		ceiling: defaultPermissions().ceiling(true), store: agentrun.NewVaulted(agentrun.NewMemStore(), agentrun.NewMemBodyStore()),
		refused: make(map[string]string), typed: make(map[string]typedOp), opCache: make(map[string][]agent.Operation), windowApps: make(map[uint64]string),
		opEffects: make(map[string]map[string]string), closed: make(chan struct{})}
}

// setOptions takes the window's settings: the ceiling and whether each
// operation is offered as a tool of its own.
func (inst *coordinator) setOptions(ceiling agent.Ceiling, opTools bool) {
	inst.mu.Lock()
	inst.ceiling, inst.opTools = ceiling, opTools
	inst.mu.Unlock()
}

// setRounds takes the person's limit on a turn's rounds; the next turn
// runs under it.
func (inst *coordinator) setRounds(n int) {
	inst.mu.Lock()
	inst.rounds = n
	inst.mu.Unlock()
}

// noteAnswered keeps an answered model call of the running turn.
func (inst *coordinator) noteAnswered(cs callStat) {
	inst.mu.Lock()
	inst.answered = append(inst.answered, cs)
	inst.mu.Unlock()
}

// answeredCalls are the running, or last, turn's answered model calls.
func (inst *coordinator) answeredCalls() (calls []callStat) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return slices.Clone(inst.answered)
}

// roundLimit is the rounds a turn starting now may take.
func (inst *coordinator) roundLimit() (n int) {
	inst.mu.Lock()
	n = inst.rounds
	inst.mu.Unlock()
	return clampRounds(n)
}

func (inst *coordinator) ceilingNow() (c agent.Ceiling) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.ceiling
}

// ceilingNote tells the model what the person's settings let it do, on the
// first turn and whenever they moved since it was last told: a model that
// knows the limit does not spend calls finding it. The host enforces the
// limit either way (ADR-0280).
func (inst *coordinator) ceilingNote() (note string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	// Without Apps the model has no window tools to limit: a conversation
	// that only asks questions is told nothing about them.
	if !inst.apps || inst.toldOnce && inst.told == inst.ceiling {
		return ""
	}
	inst.told, inst.toldOnce = inst.ceiling, true
	s := inst.ceiling.Score(false)
	note = "The person's settings let you at most: " + s.Level.String() + "."
	if len(s.Factors) > 0 {
		note += " Within that: " + strings.Join(s.Factors, "; ") + "."
	}
	note += " The host refuses anything above it; do not ask for more than this in request_access."
	if !inst.ceiling.Unpaced && s.Level >= agent.LevelView {
		note += " The host spaces your changes so the person can follow them: a call that changes a window may take a moment to return."
	}
	return
}

// stateOrNone is state for a window that may have no coordinator.
func (inst *coordinator) stateOrNone() (task string, tainted bool, confined bool) {
	if inst == nil {
		return
	}
	return inst.state()
}

// setStage notes what the tool loop is doing.
func (inst *coordinator) setStage(s stageE) {
	inst.mu.Lock()
	inst.stage = s
	inst.mu.Unlock()
}

// awaitPerson marks a call as waiting on the person until done is called.
// The running call's step says so while it waits.
func (inst *coordinator) awaitPerson() (done func()) {
	inst.mu.Lock()
	inst.waiting++
	if inst.waiting == 1 {
		inst.steps.personWait(true)
	}
	inst.mu.Unlock()
	return func() {
		inst.mu.Lock()
		inst.waiting--
		if inst.waiting == 0 {
			inst.steps.personWait(false)
		}
		inst.mu.Unlock()
	}
}

// stageNow is what the tool loop waits on: the person while a question form
// is open or a call waits on their decision, else the stage it reported.
func (inst *coordinator) stageNow() (s stageE) {
	if inst.ask != nil && inst.ask.current() != nil {
		return stagePerson
	}
	if art := inst.artefactOf(); art != nil && art.pending() != nil {
		return stagePerson
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.waiting > 0 {
		return stagePerson
	}
	return inst.stage
}

// state is what the bar shows.
func (inst *coordinator) state() (task string, tainted bool, confined bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.grant.Task, inst.tainted, inst.confined
}

// heldTasks are the tasks the conversation held.
func (inst *coordinator) heldTasks() (tasks []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return slices.Clone(inst.tasks)
}

func (inst *coordinator) handle() (h string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.grant.Handle
}

// sensitivity is the highest label the conversation's context holds; every
// model call declares it (ADR-0254 §SD3, ADR-0269 §SD7).
func (inst *coordinator) sensitivity() (s queryengine.SensitivityE) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.confined {
		return queryengine.SensitivityConfined
	}
	return queryengine.SensitivityOrdinary
}

func schema(s string) (v jsontext.Value) { return jsontext.Value(s) }

// titleProperty is the argument every tool takes beside its own: a few words
// the person reads while the call runs and in the transcript after it.
var titleProperty = map[string]any{"type": "string",
	"description": "a few words saying what this call does, for the person to read while it runs (e.g. \"Reading play's query\")"}

// toolSchema is a fixed tool's parameter schema with the title added.
func toolSchema(s string) (v jsontext.Value) {
	v, _ = withProperties(s, map[string]any{"title": titleProperty})
	return
}

// maxTitleRunes bounds a call's title as shown.
const maxTitleRunes = 60

// cleanTitle is a title as shown: one line, at most maxTitleRunes.
func cleanTitle(s string) (t string) {
	t = strings.Join(strings.Fields(s), " ")
	if r := []rune(t); len(r) > maxTitleRunes {
		t = string(r[:maxTitleRunes-1]) + "…"
	}
	return
}

// callTitle takes the title out of a call's arguments; a typed tool whose
// operation has an argument of that name keeps it.
func (inst *coordinator) callTitle(name string, args map[string]any) (title string) {
	inst.mu.Lock()
	t, typed := inst.typed[name]
	inst.mu.Unlock()
	if typed && !t.titled {
		return
	}
	title, _ = args["title"].(string)
	delete(args, "title")
	return cleanTitle(title)
}

// peekTitle is the title a call carries, for the waiting line.
func (inst *coordinator) peekTitle(call openaichat.ToolCall) (title string) {
	var args map[string]any
	if json.Unmarshal([]byte(call.Arguments), &args) != nil {
		return
	}
	return inst.callTitle(call.Name, args)
}

// offer sets what the conversation offers the model.
func (inst *coordinator) offer(apps bool, questions bool) {
	inst.mu.Lock()
	inst.apps, inst.questions = apps, questions
	inst.mu.Unlock()
}

func (inst *coordinator) offers() (apps bool, questions bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.apps, inst.questions
}

// tools are what the model is given, a projection of the registry under
// what the conversation offers now: ask_user with Questions on; the
// artefact's tools with an artefact, the writes under a write policy; with
// Apps on the fixed tools, and with operation tools on a typed tool per
// operation of the task's windows; otherwise operation schemas load on
// demand through describe_app (ADR-0269 §SD3).
func (inst *coordinator) tools(ctx context.Context) (out []openaichat.Tool) {
	offer := inst.offerNow()
	for i := range coordinatorTools {
		if spec := &coordinatorTools[i]; spec.group.offered(offer) {
			out = append(out, spec.tool())
		}
	}
	if !offer.apps {
		return
	}
	inst.mu.Lock()
	typed := inst.opTools
	inst.mu.Unlock()
	if typed {
		out = append(out, inst.operationTools(ctx)...)
	}
	return
}

// offerNow is what the conversation offers at this moment.
func (inst *coordinator) offerNow() (s toolOffer) {
	s.apps, s.questions = inst.offers()
	inst.mu.Lock()
	s.compacted = inst.compacted
	inst.mu.Unlock()
	if art := inst.artefactOf(); art != nil {
		s.artefact, s.write = true, art.policyNow().write
		s.pixels = inst.pixelsNow().reaches()
	}
	return
}

// fixedTools are the window tools, as the registry declares them.
func (inst *coordinator) fixedTools() (out []openaichat.Tool) {
	return toolsOfGroups(toolGroupFixed)
}

// changesNote is the host's account of what others changed since the
// previous turn, for the model; empty when nothing did or there is no task.
func (inst *coordinator) changesNote(ctx context.Context, turn string) (note string) {
	h := inst.handle()
	if h == "" {
		return
	}
	changes, err := inst.cli.TurnAsked(ctx, h, agent.Asked{Conversation: inst.conversation, Turn: turn})
	if err != nil || len(changes) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("Since your last turn, others changed things in your windows:")
	for _, ch := range changes {
		b.WriteString("\n- " + ch.Writer + " changed " + strings.Join(ch.Resources, ", ") + " in window " + strconv.FormatUint(ch.Instance, 10))
		if ch.Op != "" {
			b.WriteString(" (" + ch.Op + ")")
		}
	}
	b.WriteString("\nRead again before you write there.")
	return b.String()
}

// toolOrigin says where a tool call came from: the turn, the model call
// whose reply asked for it, and its index in that reply. The host records
// it with the call (ADR-0277 §SD1) and the call is keyed by it.
type toolOrigin struct {
	turn      string
	modelCall string
	index     int
}

// key is the call's key at the dispatcher: the model call and the index,
// never the provider's own id for the tool call, which nothing obliges to
// differ across replies (ADR-0277 §SD6).
func (inst toolOrigin) key() (k string) {
	if inst.modelCall == "" {
		return "call-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return trail.ToolKey(inst.modelCall, inst.index)
}

// asked is the tool call as the dispatcher records it: its key, the
// conversation and turn, and the model call that asked (ADR-0277 §SD1).
func (inst *coordinator) asked(o toolOrigin, toolCall string) (a agent.Asked) {
	return agent.Asked{Key: o.key(), Conversation: inst.conversation, Turn: o.turn, ModelCall: o.modelCall,
		ToolCall: toolCall, ToolIndex: uint32(max(o.index, 0))}
}

// exec runs one tool call; it returns what the model reads and a line for
// the transcript. A call identical to one refused since the last call that
// was not is answered without being made again.
func (inst *coordinator) exec(ctx context.Context, o toolOrigin, call openaichat.ToolCall) (content string, activity string) {
	var args map[string]any
	if call.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "error: the arguments are not a JSON object", call.Name + ": arguments not understood"
		}
	}
	// The title is the person's, not the call's: it is not dispatched, and
	// two calls differing only in it are the same call.
	title := inst.callTitle(call.Name, args)
	canon, _ := json.Marshal(args, json.Deterministic(true))
	sig := call.Name + " " + string(canon)
	inst.mu.Lock()
	prev, repeated := inst.refused[sig]
	inst.refusal = ""
	inst.curTitle = title
	inst.lastUntrusted, inst.lastWriter, inst.lastDecided = false, writerRef{}, ""
	inst.mu.Unlock()
	if repeated {
		return "error: this is the same call that was just refused, and nothing has changed since; change it as the refusal says. The refusal was: " + prev,
			call.Name + ": repeated a refused call"
	}
	content, activity = inst.dispatch(ctx, o, call, args)
	inst.mu.Lock()
	refusal := inst.refusal
	inst.mu.Unlock()
	// Only the host's own words can say the task is gone: a result's text is
	// the app's, and an app could write "the task ended" into it.
	gone := agent.TaskGone(refusal) || (strings.HasPrefix(content, "error:") && agent.TaskGone(content))
	if call.Name != "request_access" && gone && inst.dropGrant() {
		// The task is gone; the conversation is not. The next request_access
		// starts a new one instead of presenting the dead handle again.
		content += "\nnext: the task ended; request_access starts a new one"
	}
	if title != "" {
		activity = title + " · " + activity
	}
	inst.mu.Lock()
	if inst.refusal != "" || strings.HasPrefix(content, "error:") {
		inst.refused[sig] = content
	} else {
		clear(inst.refused)
	}
	inst.mu.Unlock()
	return
}

// waitsOnPerson reports whether a call waits on the person: a widening to
// decide, or a proposal to accept.
func waitsOnPerson(out agent.Outcome) bool {
	return out.Held || out.Phase == "proposed"
}

// settle polls the call key until it is final. While the call waits on the
// person it waits as long as the turn runs, as ask_user does: the host
// expires an undecided widening, and the person can stop the turn.
// Otherwise it waits at most callWait, counted from when the person last
// decided. A call still waiting on the person when the wait ends — the turn
// stopped, the host did not answer — is withdrawn, so nobody can approve
// later a call whose outcome no turn reads.
func (inst *coordinator) settle(ctx context.Context, h string, key string, out agent.Outcome) (last agent.Outcome, err error) {
	last = out
	deadline := time.Now().Add(callWait)
	personDone := func() {}
	waiting := false
	defer func() {
		personDone()
		if waitsOnPerson(last) {
			wctx, cancel := context.WithTimeout(context.Background(), agent.DefaultTimeout)
			defer cancel()
			_, _ = inst.cli.Cancel(wctx, h, key)
			if err == nil {
				err = ctx.Err()
			}
		}
	}()
	for !last.Final() && ctx.Err() == nil {
		switch on := waitsOnPerson(last); {
		case on && !waiting:
			personDone, waiting = inst.awaitPerson(), true
			inst.noteDecided(endorsedByPerson)
		case !on && waiting:
			personDone()
			personDone, waiting = func() {}, false
			deadline = time.Now().Add(callWait)
		}
		if !waiting && !time.Now().Before(deadline) {
			break
		}
		next, serr := inst.cli.Status(ctx, h, key, agent.MaxStatusWait)
		if serr != nil {
			// last stays what the host said, for the withdrawal.
			err = serr
			return
		}
		last = next
	}
	return
}

// dropGrant forgets a task that ended; it reports whether there was one.
func (inst *coordinator) dropGrant() (dropped bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	dropped = inst.grant.Handle != ""
	inst.grant = agent.Grant{}
	clear(inst.typed)
	return
}

// forgetRefusals clears the refused calls at a turn's start: the person's
// message is a change, and a request that expired while they were away
// must reach their dialog again when they ask for it.
func (inst *coordinator) forgetRefusals() {
	inst.mu.Lock()
	clear(inst.refused)
	inst.mu.Unlock()
}

// refuse marks the running call as refused.
func (inst *coordinator) refuse(reason string) {
	inst.mu.Lock()
	inst.refusal = reason
	inst.mu.Unlock()
}

func (inst *coordinator) dispatch(ctx context.Context, o toolOrigin, call openaichat.ToolCall, args map[string]any) (content string, activity string) {
	offer := inst.offerNow()
	if spec := lookupTool(call.Name); spec != nil && spec.group.accepted(offer) {
		return spec.exec(ctx, inst, spec, o, call, args)
	}
	if !offer.apps {
		return "error: no tool " + call.Name, "unknown tool " + call.Name
	}
	inst.mu.Lock()
	t, isTyped := inst.typed[call.Name]
	inst.mu.Unlock()
	if isTyped {
		reason, _ := args["reason"].(string)
		opArgs := make(map[string]any, len(args))
		for k, v := range args {
			if k != "reason" {
				opArgs[k] = v
			}
		}
		return inst.call(ctx, o, call.Id, map[string]any{"window": float64(t.window), "operation": t.op, "args": opArgs, "reason": reason})
	}
	return "error: no tool " + call.Name, "unknown tool " + call.Name
}

// describeApp runs describe_app: what the model reads of the apps and
// their operations.
func (inst *coordinator) describeApp(ctx context.Context, asked agent.Asked, args map[string]any) (content string, activity string) {
	apps, err := inst.cli.Describe(ctx, agent.DescribeRequest{App: argString(args, "app"), Search: argString(args, "search"), Operation: argString(args, "operation"),
		Asked: asked})
	if err != nil {
		return "error: " + err.Error(), "describe: " + err.Error()
	}
	inst.noteEffects(apps)
	b, _ := json.Marshal(describeView(apps))
	return string(b), "described " + strconv.Itoa(len(apps)) + " app(s)"
}

// openWindow runs open_window: a window of an app the task may open.
func (inst *coordinator) openWindow(ctx context.Context, o toolOrigin, toolCall string, appName string) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access first", "open_window: no task"
	}
	got, err := inst.cli.LaunchFrom(ctx, agent.CallRequest{Handle: h, Key: o.key(), Turn: o.turn, ModelCall: o.modelCall,
		ToolCall: toolCall, ToolIndex: uint32(max(o.index, 0))}, appName, "", nil)
	if err != nil {
		inst.refuse(err.Error())
		return "error: " + err.Error() + openNext(appName, err.Error()), "open " + appName + ": " + err.Error()
	}
	// The window's app, by id rather than the name the model opened it
	// under, for the effect class of the calls into it.
	if windows, lerr := inst.cli.List(ctx, h); lerr == nil {
		for _, w := range windows {
			inst.noteWindowApp(w.Instance, w.App)
		}
	}
	return inst.launched(appName, got)
}

// stopTask runs stop_task: the task ends, its windows pass to the person.
func (inst *coordinator) stopTask(ctx context.Context, asked agent.Asked) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "no task to stop", "stop: no task"
	}
	err := inst.cli.StopWith(ctx, h, agent.StopRequest{Reason: "the model stopped it", Asked: asked})
	inst.mu.Lock()
	inst.grant = agent.Grant{}
	inst.mu.Unlock()
	if err != nil {
		return "error: " + err.Error(), "stop: " + err.Error()
	}
	return "stopped", "task stopped"
}

// openNext is what a refused open_window can do: ask for the app when the
// grant does not name it, look the name up when no app answers to it.
func openNext(appName string, reason string) (next string) {
	var n nextStep
	switch {
	case strings.Contains(reason, "does not let the task open windows of"):
		n = nextStep{Tool: "request_access", Args: map[string]any{"plan": "open " + appName, "open": []map[string]any{{"app": appName}}},
			Then: "call open_window again once the person granted it"}
	case strings.Contains(reason, "no app named"):
		n = nextStep{Tool: "describe_app", Args: map[string]any{"search": appName}, Then: "open the app by the id it lists"}
	default:
		return ""
	}
	b, _ := json.Marshal(n)
	return "\nnext: " + string(b)
}

// launched is what the model reads of a window it opened, and the
// transcript line; a mount error is the app's text (ADR-0269 §SD7).
func (inst *coordinator) launched(appName string, got agent.Launched) (content string, activity string) {
	b, _ := json.Marshal(struct {
		Window     uint64 `json:"window"`
		Load       string `json:"load,omitempty"`
		LoadReason string `json:"load_reason,omitempty"`
	}{got.Instance, got.Load, got.LoadReason})
	content = string(b)
	activity = "opened " + appName + " as window " + strconv.FormatUint(got.Instance, 10)
	switch got.Load {
	case "opening":
		activity += " · still opening"
	case "failed":
		activity = appName + " in window " + strconv.FormatUint(got.Instance, 10) + " failed to open"
	}
	if got.LoadReason != "" {
		inst.markTainted()
		content = wrapUntrusted("the app's mount error", content)
	}
	return
}

func (inst *coordinator) requestAccess(ctx context.Context, asked agent.Asked, plan string, args map[string]any) (content string, activity string) {
	ceiling := inst.ceilingNow()
	req := agent.GrantRequest{Plan: plan, Conversation: inst.conversation, Handle: inst.handle(), Ceiling: &ceiling, Asked: asked}
	launches, bad := openArg(args["open"])
	if bad != "" {
		// Nothing is asked of the person for a request it cannot read.
		inst.refuse(bad)
		return "error: " + bad, "access not asked: " + bad
	}
	for _, l := range launches {
		// A window the model opens is its to work in, as far as the
		// settings let it work in any.
		req.Launches = append(req.Launches, agent.GrantLaunch{App: l.app, Mode: max(min(agent.ModeAct, ceiling.Mode), agent.ModeObserve), Count: l.count})
	}
	if desktop, _ := args["desktop"].(bool); desktop {
		req.Desktop = agent.ModeAct
	}
	if dests, ok := args["destinations"].([]any); ok {
		for _, d := range dests {
			if s, isStr := d.(string); isStr && s != "" {
				req.Destinations = append(req.Destinations, s)
			}
		}
	}
	// The request waits for the person's decision in the host's dialog.
	done := inst.awaitPerson()
	defer done()
	g, err := inst.cli.Request(ctx, req)
	if err != nil && req.Handle != "" && agent.TaskGone(err.Error()) && inst.dropGrant() {
		// A widening of a task that ended asks for a new task instead.
		req.Handle = ""
		g, err = inst.cli.Request(ctx, req)
	}
	if err != nil {
		inst.refuse(err.Error())
		content = "the person did not grant access: " + err.Error()
		if inst.handle() == "" && len(req.Launches) == 0 {
			// Nothing is shared until asked for; name the app to open.
			n, _ := json.Marshal(nextStep{Tool: "request_access",
				Args: map[string]any{"plan": plan, "open": []map[string]any{{"app": "<an app id from describe_app>"}}},
				Then: "no window is shared with you yet: ask to open the app you need"})
			content += "\nnext: " + string(n)
		}
		return content, "access not granted: " + err.Error()
	}
	inst.mu.Lock()
	if g.Task != "" {
		inst.grant = g
		if !slices.Contains(inst.tasks, g.Task) {
			inst.tasks = append(inst.tasks, g.Task)
		}
	}
	inst.mu.Unlock()
	// Part of the request's answer, not a call the model made: no row of its own.
	listing, _ := inst.listWindows(ctx, agent.Asked{})
	content = "access granted.\n" + grantTermsLine(g.Terms, time.Now())
	if g.Unattended {
		content += unattendedNote + "\n"
	}
	if len(launches) > 0 {
		names := make([]string, 0, len(launches))
		for _, l := range launches {
			names = append(names, l.app)
		}
		content += "open_window may now open: " + strings.Join(names, ", ") + "\n"
	}
	return content + listing, "access granted for task " + inst.grantTask()
}

// openItem is one app request_access asks to open windows of.
type openItem struct {
	app   string
	count uint32
}

// openArg reads request_access's open: a list of {"app": id, "count": n},
// where a bare string stands for {"app": it} — the shape models write as
// often as the declared one. An item that is neither is named, not
// dropped, so a request never goes to the person without what was asked.
func openArg(v any) (items []openItem, bad string) {
	if v == nil {
		return
	}
	list, ok := v.([]any)
	if !ok {
		list = []any{v}
	}
	for _, o := range list {
		it := openItem{count: 1}
		switch x := o.(type) {
		case string:
			it.app = x
		case map[string]any:
			it.app, _ = x["app"].(string)
			if n, isNum := x["count"].(float64); isNum && n >= 1 {
				it.count = uint32(n)
			}
		}
		if strings.TrimSpace(it.app) == "" {
			b, _ := json.Marshal(o)
			return nil, `open takes a list of {"app": "<app id from describe_app>", "count": n}; this item names no app: ` + string(b)
		}
		items = append(items, it)
	}
	return
}

func (inst *coordinator) grantTask() (t string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.grant.Task
}

func (inst *coordinator) listWindows(ctx context.Context, asked agent.Asked) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "no task yet; call request_access first", "list: no task"
	}
	insts, err := inst.cli.ListAsked(ctx, h, asked)
	if err != nil {
		return "error: " + err.Error(), "list: " + err.Error()
	}
	for _, w := range insts {
		inst.noteWindowApp(w.Instance, w.App)
	}
	b, _ := json.Marshal(insts)
	// Window titles are untrusted text (ADR-0269 §SD7).
	inst.markTainted()
	return wrapUntrusted("the host's window list", string(b)), "listed " + strconv.Itoa(len(insts)) + " window(s)"
}

func (inst *coordinator) markTainted() {
	inst.mu.Lock()
	inst.tainted, inst.lastUntrusted = true, true
	inst.mu.Unlock()
}

// wrapUntrusted delimits content an attacker could influence and names
// where it came from (ADR-0269 §SD7).
func wrapUntrusted(source string, text string) (s string) {
	return untrustedOpen + source + "\">>\n" + text + "\n" + untrustedClose
}

// callOutcome is what the model reads of one call.
type callOutcome struct {
	Phase     string            `json:"phase"`
	Reason    string            `json:"reason,omitempty"`
	Revisions map[string]uint64 `json:"revisions,omitempty"`
	Result    jsontext.Value    `json:"result,omitempty"`
	// DataHandle stands in for confined content the model may pass and
	// not read.
	DataHandle string `json:"data_handle,omitempty"`
	// Next, on a refusal, is what would let the call through.
	Next *nextStep `json:"next,omitempty"`
}

func (inst *coordinator) call(ctx context.Context, o toolOrigin, toolCall string, args map[string]any) (content string, activity string) {
	h := inst.handle()
	if h == "" {
		return "error: no task yet; call request_access first", "call_operation: no task"
	}
	window, _ := args["window"].(float64)
	op, _ := args["operation"].(string)
	reason, _ := args["reason"].(string)
	var stray []string
	for k := range args {
		switch k {
		case "window", "operation", "args", "reason":
		default:
			stray = append(stray, k)
		}
	}
	if len(stray) > 0 {
		slices.Sort(stray)
		return "error: call_operation takes window, operation, args and reason; put " + strings.Join(stray, ", ") +
			" under args", op + ": arguments outside args"
	}
	opArgs := "{}"
	a, hasArgs := args["args"]
	if hasArgs && a != nil {
		b, err := json.Marshal(a)
		if err == nil {
			opArgs = string(b)
		}
	}
	key := o.key()
	instance := uint64(window)
	inst.mu.Lock()
	title := inst.curTitle
	inst.mu.Unlock()
	out, err := inst.cli.Call(ctx, agent.CallRequest{Handle: h, Instance: instance, Operation: op, Args: opArgs, Key: key, Reason: reason,
		Title: title, Turn: o.turn, ModelCall: o.modelCall, ToolCall: toolCall, ToolIndex: uint32(max(o.index, 0))})
	where := op + " in window " + strconv.FormatUint(instance, 10)
	if err != nil {
		return "error: " + err.Error(), where + ": " + err.Error()
	}
	if out, err = inst.settle(ctx, h, key, out); err != nil {
		return "error: " + err.Error(), where + ": " + err.Error()
	}
	co := callOutcome{Phase: out.Phase, Reason: out.Reason, Revisions: out.Revisions, Next: inst.nextFor(instance, op, out.Remedy)}
	switch out.Phase {
	case "refused", "denied", "conflict":
		inst.refuse(out.Reason)
	}
	if out.Phase == "refused" && (!hasArgs || a == nil) {
		co.Reason = strings.TrimSpace(co.Reason + "; you sent no args: the operation's arguments go under args")
	}
	untrustedSource := ""
	if out.ResultRef != "" {
		res, rerr := inst.cli.Read(ctx, h, out.ResultRef)
		switch {
		case rerr != nil:
			co.Reason = strings.TrimSpace(co.Reason + "; the result could not be read: " + rerr.Error())
		case res.DataHandle != "":
			co.DataHandle = res.DataHandle
		default:
			co.Result = jsontext.Value(res.Text)
			if res.Confined {
				inst.mu.Lock()
				inst.confined = true
				inst.mu.Unlock()
			}
			if res.Untrusted {
				untrustedSource = res.Source
				inst.markTainted()
			}
		}
	}
	b, err := json.Marshal(co)
	if err != nil {
		return "error: " + err.Error(), where + ": " + err.Error()
	}
	content = string(b)
	if untrustedSource != "" {
		content = wrapUntrusted(untrustedSource, content)
	}
	activity = where + " · " + out.Phase
	if out.Reason != "" && out.Phase != "completed" && out.Phase != "rendered" {
		activity += " · " + out.Reason
	}
	return
}

// turnResult is a finished turn: every message the model saw, the last
// answer, and the activity lines for the transcript. stopped says why a
// turn that had called tools ended without an answer; its calls happened,
// so the transcript shows them.
type turnResult struct {
	// calls are the turn's model calls, for the statistics.
	calls    []callStat
	messages []openaichat.Message
	// omitTo is one past where the last call carried lastRoundNote, which
	// messages leave out; 0 when it carried none.
	omitTo   int
	final    llm.Response
	activity []string
	// steps are the turn's steps, every model and tool call in order.
	steps   []turnStep
	stopped string
	// stoppedErr is the model call's error behind stopped, nil when the
	// rounds ran out.
	stoppedErr error
	// digest is the last request's digest, as the journal recorded it.
	digest string
}
