package chat

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/anchorstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/harness"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The driver's seams (ADR-0310 §SD2): the turn loop is split at the points
// the harness design names, each a declared composition shape of package
// harness, with the compile-date behaviour as the only element until a
// profile selects another. Order is a declared position on each element,
// never the order of registration.
//
//   - pre-step injection, a chain: what the host tells the model before the
//     person's message; the context injector is its one element, and what
//     it composes is written to the journal as note entries.
//   - request configuration, a chain: how one round's request is shaped
//     from the projection; the round's tools and label, then compaction
//     (chat_compact.go), then the last round.
//   - request-error recovery, a first claim: what a failed model call
//     means for the turn; one claimant maps an error after tool activity
//     to a stopped turn.
//   - tool mode, a slot: how a reply's tool calls run; sequential.
//   - turn stopping, a guard: whether the turn goes on after a reply,
//     combined by maximum over stopVerdictContinue < stopVerdictAnswered <
//     stopVerdictStopped.
//
// The journal is the only input (ADR-0308 §SD3): every message the model
// receives is the projection of the entries written so far.

// contextFormE is how a context item reads: a snapshot says where
// something stands, a notice says what changed.
type contextFormE uint8

const (
	contextFormSnapshot contextFormE = iota
	contextFormNotice
)

func (inst contextFormE) String() string {
	if inst == contextFormNotice {
		return "notice"
	}
	return "snapshot"
}

// contextItem is one thing the host tells the model at the start of a
// turn, with the source it came from.
type contextItem struct {
	source string
	form   contextFormE
	text   string
}

// contextSource is one source of mid-turn context: compose returns its
// text when it is news to the model and nothing otherwise. Each source
// keeps its own "told" watermark; the injector decides nothing about news
// beyond asking the sources in order.
//
// deferred: a source's watermark advances when its note is composed and is
// not restored when the turn fails, so a note composed for a turn that got
// no answer is not repeated on the next. The preservation golden
// (failed-turns) records that behaviour; restoring the watermark is a
// visible change for the profile record, not for this refactor.
type contextSource struct {
	name    string
	form    contextFormE
	compose func(ctx context.Context, inst *coordinator, turn string) (text string)
}

// contextSources are the sources in the order the model reads their items.
var contextSources = []contextSource{
	{name: "ceiling", form: contextFormSnapshot, compose: func(_ context.Context, inst *coordinator, _ string) string { return inst.ceilingNote() }},
	{name: "artefact", form: contextFormSnapshot, compose: func(_ context.Context, inst *coordinator, _ string) string { return inst.artefactNote() }},
	{name: "pixels", form: contextFormNotice, compose: func(_ context.Context, inst *coordinator, _ string) string { return inst.pixelsNote() }},
	{name: "changes", form: contextFormNotice, compose: func(ctx context.Context, inst *coordinator, turn string) string { return inst.changesNote(ctx, turn) }},
}

// inject is what the host tells the model before the person's message
// this turn: every source's item that is news, in the sources' order.
func (inst *coordinator) inject(ctx context.Context, turn string) (items []contextItem) {
	for _, src := range contextSources {
		if text := src.compose(ctx, inst, turn); text != "" {
			items = append(items, contextItem{source: src.name, form: src.form, text: text})
		}
	}
	return
}

// injectState is what pre-step injection shapes: the note entries the
// turn writes before the person's message.
type injectState struct {
	ctx   context.Context
	coord *coordinator
	turn  string
	notes []agentrun.Entry
}

// injectContext composes the context items as note entries.
func injectContext(s *injectState) {
	for _, it := range s.coord.inject(s.ctx, s.turn) {
		s.notes = append(s.notes, agentrun.Entry{Kind: agentrun.KindNote, Name: it.source, Form: it.form.String(), Body: it.text, Turn: s.turn, Label: harnessLabel()})
	}
}

// completerI is what a turn's rounds complete through: the host's model
// over the bus (*llm.Client, ADR-0254), a fake in tests.
type completerI interface {
	Complete(ctx context.Context, req llm.Request) (res llm.Response, err error)
}

var _ completerI = (*llm.Client)(nil)

// requestState is one round's request as the configuration chain shapes
// it: the projected messages under the Pixels setting, the tools, the
// label, and what the next turn declares left out.
type requestState struct {
	ctx context.Context
	// jctx is the context journal writes run under (journalCtx).
	jctx    context.Context
	coord   *coordinator
	cli     completerI
	w       *agentrun.Writer
	req     llm.Request
	round   int
	limit   int
	parent  string
	omitTo  int
	msgs    []openaichat.Message
	batch   projection.Batch
	digest  string
	confine bool
	err     error
	// lastRound is the ordinal of the last-round note the round wrote,
	// to drop after the call.
	lastRound uint32
}

// configureRound binds the Pixels setting (ADR-0287 §SD1), declares the
// tools, projects the journal and gives the request the round's messages,
// label, parent and round.
func configureRound(s *requestState) {
	if s.confine, s.err = s.coord.bindPixelsJournal(s.jctx, s.w); s.err != nil {
		return
	}
	tools := s.coord.tools(s.ctx)
	if s.err = s.coord.syncTools(s.jctx, s.w, tools); s.err != nil {
		return
	}
	if s.batch, s.msgs, s.digest, s.err = s.coord.project(s.ctx, s.w); s.err != nil {
		return
	}
	s.req.Messages, s.req.Tools, s.req.Sensitivity, s.req.ParentCallId, s.req.Round = s.msgs, tools, s.coord.sensitivity(), s.parent, uint32(s.round)
	if s.confine {
		s.req.Sensitivity = queryengine.SensitivityConfined
	}
	// The request's tier, for the route's clearance (ADR-0312 §SD2), and
	// the profile and route ids for the call's row (ADR-0310 §SD4).
	s.req.Tier = uint8(s.coord.requestLabel(s.batch).Tier)
	s.req.Profile, s.req.Route = s.coord.profileRef()
}

// configureLastRound asks the model to answer on the last round: a note
// written for this call only, dropped after it, so it does not join the
// history the next turn resends; no tool may be chosen. The host saw the
// note, so the next turn declares it left out (ADR-0264 §SD3).
func configureLastRound(s *requestState) {
	if s.err != nil || s.round != s.limit-1 {
		return
	}
	written, err := s.coord.append(s.jctx, s.w, agentrun.Entry{Kind: agentrun.KindNote, Name: "last-round", Form: "once", Body: lastRoundNote, Turn: s.req.Turn, Label: harnessLabel()})
	if err != nil {
		s.err = err
		return
	}
	s.lastRound = written[0].Ordinal
	if s.batch, s.msgs, s.digest, s.err = s.coord.project(s.ctx, s.w); s.err != nil {
		return
	}
	s.req.Messages = s.msgs
	if len(s.req.Tools) > 0 {
		// A tool choice names the tools it is about; without any the
		// wire form refuses it.
		s.req.ToolChoice = "none"
	}
	s.omitTo = len(s.msgs)
}

// recoverState is a failed model call: the error and how many tool calls
// the turn had made.
type recoverState struct {
	err      error
	activity int
}

// recovery is what a claimant makes of the failure: a stopped turn with
// the reason, which keeps the calls it made shown.
type recovery struct {
	stopped string
}

// stopAfterActivity claims a model error after tool activity: the turn is
// stopped, not failed, since its calls happened; a cancel is the person's
// and fails the turn as before.
func stopAfterActivity(s recoverState) (r recovery, claimed bool) {
	if s.activity == 0 || errors.Is(s.err, context.Canceled) {
		return
	}
	return recovery{stopped: failureReason(s.err)}, true
}

// stopVerdictE is the turn-stopping guard's lattice.
type stopVerdictE uint8

const (
	stopVerdictContinue stopVerdictE = iota
	// stopVerdictAnswered is a reply without tool calls: the turn's answer.
	stopVerdictAnswered
	// stopVerdictStopped ends the turn without an answer.
	stopVerdictStopped
)

// stopState is a reply as the guard sees it.
type stopState struct {
	toolCalls int
	lastRound bool
}

func stopWhenAnswered(s stopState) (v stopVerdictE, err error) {
	if s.toolCalls == 0 {
		v = stopVerdictAnswered
	}
	return
}

// stopOnLastRoundCalls stops a turn whose model calls tools on the last
// round: only a model that ignores tool_choice gets here, and it was told
// no tool can be called, so the calls are not made.
func stopOnLastRoundCalls(s stopState) (v stopVerdictE, err error) {
	if s.toolCalls > 0 && s.lastRound {
		v = stopVerdictStopped
	}
	return
}

// roundState is a reply's tool calls as the tool mode runs them: each
// call's intent before it leaves, its outcome after, and the screenshots
// the round showed after every result.
type roundState struct {
	ctx context.Context
	// jctx is the context journal writes run under (journalCtx).
	jctx     context.Context
	coord    *coordinator
	w        *agentrun.Writer
	turn     string
	round    int
	reply    llm.Response
	progress func(round int, doing string)
	out      *turnResult
	err      error
}

// toolModeI runs a reply's tool calls.
type toolModeI interface {
	run(s *roundState)
}

// sequentialTools runs the calls one after another, in the reply's order.
type sequentialTools struct{}

var _ toolModeI = sequentialTools{}

func (sequentialTools) run(s *roundState) {
	coord := s.coord
	for i, tc := range s.reply.ToolCalls {
		if s.progress != nil {
			s.progress(s.round, coord.peekTitle(tc))
		}
		coord.setStage(stageTool)
		o := toolOrigin{turn: s.turn, modelCall: s.reply.CallId, index: i}
		effect := coord.effectOf(s.ctx, tc.Name, tc.Arguments)
		// The intent names the task the call runs under, so law L15 can
		// match a grant to the calls it admitted.
		if _, s.err = s.coord.append(s.jctx, s.w, agentrun.Entry{Kind: agentrun.KindIntent, Key: o.key(), Name: tc.Name, Body: tc.Arguments,
			Effect: effect, Form: coord.handle(), Turn: s.turn, Label: harnessLabel()}); s.err != nil {
			return
		}
		step := coord.steps.begin(turnStep{kind: stepTool, round: s.round, name: tc.Name, title: coord.peekTitle(tc), args: indentArgs(tc.Arguments)})
		content, activity := coord.exec(s.ctx, o, tc)
		coord.steps.finish(step, func(st *turnStep) {
			st.result, st.activity, st.refused = clip(content, stepResultMax), activity, strings.HasPrefix(content, "error:")
		})
		s.out.activity = append(s.out.activity, activity)
		untrusted, confined := coord.lastResultLabel()
		writer, decided := coord.takeWriter()
		if effect == agentrun.EffectConsequential && decided == endorsedByPerson && callCompleted(content) {
			// The person decided this call in the dispatcher's dialog: that
			// gesture is the endorsement (ADR-0311 §SD3, law L15), recorded
			// before the outcome is. A call the grant admitted without
			// asking has no entry of its own; the grant's answered entry is
			// the consent in force.
			if _, s.err = s.coord.append(s.jctx, s.w, endorseEntry(o.key(), s.turn, tc.Arguments, effect)); s.err != nil {
				return
			}
		}
		if _, s.err = s.coord.append(s.jctx, s.w, agentrun.Entry{Kind: agentrun.KindOutcome, Key: o.key(), Name: tc.Id, Role: projection.RoleTool, Body: content,
			Form: activity, Turn: s.turn, Label: outcomeLabel(untrusted, confined).Join(writer.label), WriterRun: writer.run, WriterTurn: writer.turn, WriterOrdinal: writer.ordinal}); s.err != nil {
			return
		}
	}
	if e, ok := coord.takeShownEntry(s.turn); ok {
		// After every tool result of the round, which must follow the
		// calls directly.
		if _, s.err = s.coord.append(s.jctx, s.w, e); s.err != nil {
			return
		}
	}
}

// turnSeams are the driver's seams with their elements.
type turnSeams struct {
	inject    harness.Chain[injectState]
	configure harness.Chain[requestState]
	recover   harness.FirstClaim[recoverState, recovery]
	toolMode  harness.Slot[toolModeI]
	stop      harness.Guards[stopState, stopVerdictE]
}

// turnSeamsNow are the seams as this build composes them; a composition
// that does not validate is a programming error.
var turnSeamsNow = sync.OnceValue(func() (s turnSeams) {
	var err error
	if s.inject, err = harness.NewChain(harness.Element[func(*injectState)]{Name: "context", Order: 10, Fn: injectContext}); err != nil {
		panic(err)
	}
	if s.configure, err = harness.NewChain(
		harness.Element[func(*requestState)]{Name: "round", Order: 10, Fn: configureRound},
		harness.Element[func(*requestState)]{Name: "compact", Order: 15, Fn: configureCompaction},
		harness.Element[func(*requestState)]{Name: "last-round", Order: 20, Fn: configureLastRound}); err != nil {
		panic(err)
	}
	if s.recover, err = harness.NewFirstClaim(harness.Element[func(recoverState) (recovery, bool)]{Name: "stop-after-activity", Order: 10, Fn: stopAfterActivity}); err != nil {
		panic(err)
	}
	s.toolMode = harness.NewSlot[toolModeI]("sequential", sequentialTools{})
	if s.stop, err = harness.NewGuards(stopVerdictStopped,
		harness.Element[func(stopState) (stopVerdictE, error)]{Name: "answered", Fn: stopWhenAnswered},
		harness.Element[func(stopState) (stopVerdictE, error)]{Name: "last-round-calls", Fn: stopOnLastRoundCalls}); err != nil {
		panic(err)
	}
	return
})

// runTurn is one turn: the person's message and the host's notes are
// written to the journal, then model calls and tool calls until the model
// answers without a tool or the rounds run out; the last round offers no
// tools, so the model answers with what it has. req carries the turn's
// ids and the person's message as its last message; every message the
// model receives is projected from the journal. progress, when set, hears
// each round as it starts and each tool call, by its title, as it runs.
func runTurn(ctx context.Context, cli completerI, coord *coordinator, req llm.Request, progress func(round int, doing string)) (out *turnResult, err error) {
	seams := turnSeamsNow()
	out = &turnResult{}
	coord.steps.reset()
	coord.mu.Lock()
	coord.answered = nil
	coord.mu.Unlock()
	coord.beginPixelTurn()
	defer func() {
		out.steps, _, _ = coord.steps.snapshot(0)
	}()
	// The journal is written under a context a Stop does not end: what
	// happened is recorded whether or not the person waited for it.
	jctx := journalCtx(ctx)
	if !coord.enterTurn() {
		// The conversation moved on before the turn began: nothing is
		// written for a turn that will not be shown.
		err = eb.Build().Str("conversation", coord.conversation).Errorf("the conversation was replaced")
		return
	}
	defer coord.leaveTurn()
	w, err := coord.openJournal(jctx, req.Retain)
	if err != nil {
		return
	}
	if err = coord.syncSections(jctx, w); err != nil {
		return
	}
	in := injectState{ctx: ctx, coord: coord, turn: req.Turn}
	seams.inject.Apply(&in)
	text := ""
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == openaichat.ChatRoleUser {
		text = req.Messages[n-1].Content
	}
	entries := append(in.notes, agentrun.Entry{Kind: agentrun.KindUser, Role: projection.RoleUser, Body: text, Turn: req.Turn, Label: harnessLabel()})
	if _, err = coord.append(jctx, w, entries...); err != nil {
		return
	}
	// A turn that ends without an answer is not resent (ADR-0265 §SD3):
	// its entries are dropped from the projection, kept in the record.
	defer func() {
		state := "answered"
		switch {
		case err != nil:
			state = "failed"
		case out.stopped != "":
			state = "stopped"
		}
		if state != "answered" {
			if _, derr := coord.dropTurn(jctx, w, req.Turn); derr != nil && err == nil {
				err = derr
			}
		}
		// A screenshot shown this turn stays for this turn: the next turn
		// sends a placeholder in its place (ADR-0287 §SD5).
		if _, serr := coord.stripPixelsJournal(jctx, w); serr != nil && err == nil {
			err = serr
		}
		if _, terr := coord.append(jctx, w, agentrun.Entry{Kind: agentrun.KindTurn, Turn: req.Turn, Body: turnBody(state, out.stopped), Label: harnessLabel()}); terr != nil && err == nil {
			err = terr
		}
		// A chain head per turn end, covering the turn's end (ADR-0314
		// §SD3).
		coord.shipHead(jctx, w, func(c anchorstore.Cadence) bool { return c.PerTurn })
	}()
	parent := req.ParentCallId
	limit := coord.roundLimit()
	_, mode := seams.toolMode.Get()
	for round := 0; round < limit; round++ {
		if progress != nil {
			progress(round, "")
		}
		rs := requestState{ctx: ctx, jctx: jctx, coord: coord, cli: cli, w: w, req: req, round: round, limit: limit, parent: parent}
		seams.configure.Apply(&rs)
		if err = rs.err; err != nil {
			return
		}
		if rs.omitTo > 0 {
			out.omitTo = rs.omitTo
		}
		key := requestKey(req.Turn, round)
		reqLabel := coord.requestLabel(rs.batch)
		body, _ := jsonMarshal(requestBody{Digest: rs.digest, Messages: len(rs.msgs), Tools: len(rs.req.Tools), Round: round, Label: reqLabel.String()})
		if _, err = coord.append(jctx, w, agentrun.Entry{Kind: agentrun.KindRequest, Key: key, Body: body, Turn: req.Turn, Label: reqLabel}); err != nil {
			return
		}
		out.digest = rs.digest
		coord.setStage(stageModel)
		step := coord.steps.begin(turnStep{kind: stepModel, round: round})
		var res llm.Response
		res, err = coord.complete(ctx, cli, w, rs.req)
		coord.steps.finish(step, func(s *turnStep) {
			s.content, s.reasoning = clip(res.Content, stepContentMax), clipTail(res.Reasoning, stepReasoningMax)
			s.tools, s.inTokens, s.outTokens, s.failed = len(res.ToolCalls), res.InputTokens, res.OutputTokens, err != nil
		})
		if rs.lastRound != 0 {
			// The note was for this call only.
			if _, derr := coord.append(jctx, w, agentrun.Entry{Kind: agentrun.KindEdit, Target: rs.lastRound, Form: agentrun.FormDrop, Turn: req.Turn, Label: harnessLabel()}); derr != nil && err == nil {
				err = derr
				return
			}
		}
		if err != nil {
			if _, rec, claimed := seams.recover.Decide(recoverState{err: err, activity: len(out.activity)}); claimed {
				out.stopped, out.stoppedErr, err = rec.stopped, err, nil
			}
			return
		}
		parent = res.CallId
		out.final = res
		cs := callStatOf(round, res)
		out.calls = append(out.calls, cs)
		coord.noteAnswered(cs)
		replyMeta, _ := jsonMarshal(replyBody{CallId: res.CallId, FinishReason: res.FinishReason, InputTokens: res.InputTokens, OutputTokens: res.OutputTokens})
		if _, err = coord.append(jctx, w, agentrun.Entry{Kind: agentrun.KindReply, Key: key, Role: projection.RoleAssistant, Body: res.Content, Calls: projection.CallsBody(res.ToolCalls),
			Name: replyMeta, Turn: req.Turn, Label: coord.replyLabel(reqLabel), WriterRun: w.Run().Id, WriterTurn: req.Turn}); err != nil {
			return
		}
		sent := rs.msgs
		if rs.lastRound != 0 {
			// The note was for the call alone; what the turn keeps is the
			// rest.
			sent = sent[:len(sent)-1]
		}
		out.messages = append(slices.Clip(sent), openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: res.Content, ToolCalls: res.ToolCalls})
		verdict, _, _ := seams.stop.Decide(stopState{toolCalls: len(res.ToolCalls), lastRound: round == limit-1})
		if verdict == stopVerdictAnswered {
			break
		}
		if verdict == stopVerdictStopped {
			out.stopped = "the model kept calling tools past " + strconv.Itoa(limit) + " rounds"
			return
		}
		rs2 := roundState{ctx: ctx, jctx: jctx, coord: coord, w: w, turn: req.Turn, round: round, reply: res, progress: progress, out: out}
		mode.run(&rs2)
		if err = rs2.err; err != nil {
			return
		}
	}
	return
}
