package chat

// Token and answer statistics of a chat window, and their handover to play
// as ad-hoc datasets (ADR-0240): one row per model call, one per turn, for
// every conversation the window held. The records are kept on the render
// thread; a handover encodes a copy off it.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// AdvancedSeed shows the Analytics panel: token and answer statistics of
// the window's conversations, the conversation's agent surface
// (ADR-0283), and Open in play on them.
var AdvancedSeed = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_ADVANCED",
	Default:     "true",
	Description: "show the chat app's Analytics panel — token and answer statistics of the window's conversations, the conversation's agent surface, and Open in play on them as ad-hoc datasets; false hides it",
	Category:    env.CategoryE("boxer-chat"),
})

// The aliases the statistics are published under. Every chat window
// publishes under the same two; a play window follows the newest.
const (
	aliasCalls = "chat_calls"
	aliasTurns = "chat_turns"
)

// Turn outcomes.
const (
	outcomeAnswered  = "answered"
	outcomeStopped   = "stopped"
	outcomeFailed    = "failed"
	outcomeCancelled = "cancelled"
)

// callStat is one model call: a turn without Apps makes one, a turn with
// Apps one per round of its tool loop.
type callStat struct {
	conversation string
	// turnId is the turn's id and callId the call's, as the host's call
	// record names them: what joins a row here to keelson('llm_calls').
	turnId       string
	callId       string
	turn         int
	round        int
	atMs         int64
	inputTokens  int32
	outputTokens int32
	elapsedMs    int64
	finish       string
	toolCalls    int
	incomplete   bool
}

// callStatOf is the record of one answered call.
func callStatOf(round int, res llm.Response) (s callStat) {
	return callStat{round: round, callId: res.CallId, atMs: time.Now().UnixMilli(), inputTokens: res.InputTokens, outputTokens: res.OutputTokens,
		elapsedMs: res.Elapsed.Milliseconds(), finish: res.FinishReason, toolCalls: len(res.ToolCalls), incomplete: res.Incomplete}
}

// turnStat is one turn, from the person's send to its landing.
type turnStat struct {
	conversation string
	turnId       string
	turn         int
	atMs         int64
	rounds       int
	toolCalls    int
	inputTokens  int64
	outputTokens int64
	elapsedMs    int64
	answerChars  int
	outcome      string
}

// chatStats are a window's records, across its conversations.
type chatStats struct {
	calls []callStat
	turns []turnStat
}

// addTurn records a landed turn and its calls. res is nil when the turn
// failed or was cancelled; err says which.
func (inst *chatStats) addTurn(conversation string, turnId string, started time.Time, landedMs int64, res *turnResult, err error) {
	t := turnStat{conversation: conversation, turnId: turnId, atMs: started.UnixMilli(), elapsedMs: landedMs - started.UnixMilli(), outcome: outcomeAnswered}
	for _, prev := range inst.turns {
		if prev.conversation == conversation {
			t.turn = prev.turn + 1
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		t.outcome = outcomeCancelled
	case err != nil || res == nil:
		t.outcome = outcomeFailed
	case res.stopped != "":
		t.outcome = outcomeStopped
	default:
		t.answerChars = len(res.final.Content)
	}
	if res != nil {
		for _, cs := range res.calls {
			cs.conversation, cs.turnId, cs.turn = conversation, turnId, t.turn
			inst.calls = append(inst.calls, cs)
			t.rounds++
			t.toolCalls += cs.toolCalls
			t.inputTokens += int64(cs.inputTokens)
			t.outputTokens += int64(cs.outputTokens)
		}
	}
	inst.turns = append(inst.turns, t)
}

// clone is a copy a handover may encode off the render thread.
func (inst *chatStats) clone() (out chatStats) {
	return chatStats{calls: slices.Clone(inst.calls), turns: slices.Clone(inst.turns)}
}

// answerSeconds are the answered turns' wall times, sorted.
func (inst *chatStats) answerSeconds() (xs []float64) {
	for _, t := range inst.turns {
		if t.outcome == outcomeAnswered {
			xs = append(xs, float64(t.elapsedMs)/1000)
		}
	}
	slices.Sort(xs)
	return
}

// callTokens are the calls' input or output tokens, sorted.
func (inst *chatStats) callTokens(output bool) (xs []float64) {
	for _, cs := range inst.calls {
		v := cs.inputTokens
		if output {
			v = cs.outputTokens
		}
		xs = append(xs, float64(v))
	}
	slices.Sort(xs)
	return
}

// totals are the summary line's figures.
func (inst *chatStats) totals() (turns int, answered int, calls int, in int64, out int64) {
	for _, t := range inst.turns {
		if t.outcome == outcomeAnswered {
			answered++
		}
	}
	for _, cs := range inst.calls {
		in += int64(cs.inputTokens)
		out += int64(cs.outputTokens)
	}
	return len(inst.turns), answered, len(inst.calls), in, out
}

var tsType = &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}

var callsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "conversation", Type: arrow.BinaryTypes.String},
	{Name: "turn_id", Type: arrow.BinaryTypes.String},
	{Name: "call_id", Type: arrow.BinaryTypes.String},
	{Name: "turn", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "round", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "at", Type: tsType},
	{Name: "input_tokens", Type: arrow.PrimitiveTypes.Int32},
	{Name: "output_tokens", Type: arrow.PrimitiveTypes.Int32},
	{Name: "elapsed_ms", Type: arrow.PrimitiveTypes.Int64},
	{Name: "finish", Type: arrow.BinaryTypes.String},
	{Name: "tool_calls", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "incomplete", Type: arrow.FixedWidthTypes.Boolean},
}, nil)

var turnsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "conversation", Type: arrow.BinaryTypes.String},
	{Name: "turn_id", Type: arrow.BinaryTypes.String},
	{Name: "turn", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "at", Type: tsType},
	{Name: "outcome", Type: arrow.BinaryTypes.String},
	{Name: "rounds", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "tool_calls", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "input_tokens", Type: arrow.PrimitiveTypes.Int64},
	{Name: "output_tokens", Type: arrow.PrimitiveTypes.Int64},
	{Name: "elapsed_ms", Type: arrow.PrimitiveTypes.Int64},
	{Name: "answer_chars", Type: arrow.PrimitiveTypes.Uint32},
}, nil)

// callsArrow is the calls as an Arrow IPC stream.
func callsArrow(calls []callStat) (stream []byte, err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, callsSchema)
	defer rb.Release()
	for _, cs := range calls {
		rb.Field(0).(*array.StringBuilder).Append(cs.conversation)
		rb.Field(1).(*array.StringBuilder).Append(cs.turnId)
		rb.Field(2).(*array.StringBuilder).Append(cs.callId)
		rb.Field(3).(*array.Uint32Builder).Append(uint32(cs.turn))
		rb.Field(4).(*array.Uint32Builder).Append(uint32(cs.round))
		rb.Field(5).(*array.TimestampBuilder).Append(arrow.Timestamp(cs.atMs * 1000))
		rb.Field(6).(*array.Int32Builder).Append(cs.inputTokens)
		rb.Field(7).(*array.Int32Builder).Append(cs.outputTokens)
		rb.Field(8).(*array.Int64Builder).Append(cs.elapsedMs)
		rb.Field(9).(*array.StringBuilder).Append(cs.finish)
		rb.Field(10).(*array.Uint32Builder).Append(uint32(cs.toolCalls))
		rb.Field(11).(*array.BooleanBuilder).Append(cs.incomplete)
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	return adhocdata.EncodeRecord(rec)
}

// turnsArrow is the turns as an Arrow IPC stream.
func turnsArrow(turns []turnStat) (stream []byte, err error) {
	rb := array.NewRecordBuilder(memory.DefaultAllocator, turnsSchema)
	defer rb.Release()
	for _, t := range turns {
		rb.Field(0).(*array.StringBuilder).Append(t.conversation)
		rb.Field(1).(*array.StringBuilder).Append(t.turnId)
		rb.Field(2).(*array.Uint32Builder).Append(uint32(t.turn))
		rb.Field(3).(*array.TimestampBuilder).Append(arrow.Timestamp(t.atMs * 1000))
		rb.Field(4).(*array.StringBuilder).Append(t.outcome)
		rb.Field(5).(*array.Uint32Builder).Append(uint32(t.rounds))
		rb.Field(6).(*array.Uint32Builder).Append(uint32(t.toolCalls))
		rb.Field(7).(*array.Int64Builder).Append(t.inputTokens)
		rb.Field(8).(*array.Int64Builder).Append(t.outputTokens)
		rb.Field(9).(*array.Int64Builder).Append(t.elapsedMs)
		rb.Field(10).(*array.Uint32Builder).Append(uint32(t.answerChars))
	}
	rec := rb.NewRecordBatch()
	defer rec.Release()
	return adhocdata.EncodeRecord(rec)
}

// statsSql is the buffer the play window opens with.
const statsSql = `-- This chat window's turns, from the person's send to the answer.
-- keelson('chat_calls') holds one row per model call, and
-- keelson('chat_surface') the agent surface: one row per window operation,
-- launchable app and desktop verb, with its status and calls.
SELECT
    conversation,
    turn,
    outcome,
    rounds,
    tool_calls,
    input_tokens,
    output_tokens,
    elapsed_ms / 1000 AS seconds
FROM keelson('chat_turns')
ORDER BY at`

// statsPublishers hold one window's two datasets; each republishes onto
// the handle it holds.
type statsPublishers struct {
	calls   *adhocdata.Publisher
	turns   *adhocdata.Publisher
	surface *adhocdata.Publisher
}

func newStatsPublishers() (p statsPublishers) {
	// Each window publishes under its own aliases (ADR-0288 §SD3); the play
	// window it opens reads them under the base names.
	return statsPublishers{calls: adhocdata.NewWindowPublisher(aliasCalls), turns: adhocdata.NewWindowPublisher(aliasTurns),
		surface: adhocdata.NewWindowPublisher(aliasSurface)}
}

// openStatsInPlay publishes s, and the surface's cells when there are any,
// and opens a play window on them. It makes bus round trips, so it runs off
// the render thread.
func openStatsInPlay(bus app.BusI, pubs statsPublishers, s chatStats, cells []surfaceCell) (note string, err error) {
	if len(s.turns) == 0 {
		err = eh.Errorf("no turn to show yet")
		return
	}
	datasets := []string{aliasTurns, aliasCalls}
	if len(cells) > 0 {
		var surface []byte
		surface, err = surfaceArrow(cells)
		if err != nil {
			return
		}
		if _, err = pubs.surface.Publish(bus, surface); err != nil {
			err = eh.Errorf("publish %s: %w", aliasSurface, err)
			return
		}
		datasets = append(datasets, aliasSurface)
	}
	calls, err := callsArrow(s.calls)
	if err != nil {
		return
	}
	turns, err := turnsArrow(s.turns)
	if err != nil {
		return
	}
	if _, err = pubs.calls.Publish(bus, calls); err != nil {
		err = eh.Errorf("publish %s: %w", aliasCalls, err)
		return
	}
	if _, err = pubs.turns.Publish(bus, turns); err != nil {
		err = eh.Errorf("publish %s: %w", aliasTurns, err)
		return
	}
	scoped := make([]string, 0, len(datasets))
	for _, base := range datasets {
		switch base {
		case aliasTurns:
			scoped = append(scoped, pubs.turns.Alias())
		case aliasCalls:
			scoped = append(scoped, pubs.calls.Alias())
		default:
			scoped = append(scoped, pubs.surface.Alias())
		}
	}
	cfg, err := buscodec.Encode(launchcfg.PlayLaunch{Sql: statsSql, AutoRun: true, Endpoint: launchcfg.EndpointIntrospection,
		Datasets: scoped, DatasetNames: datasets})
	if err != nil {
		err = eh.Errorf("encode the play launch config: %w", err)
		return
	}
	if _, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfg); err != nil {
		err = eh.Errorf("open play: %w", err)
		return
	}
	note = fmt.Sprintf("opened in play: %d turns as %s, %d calls as %s", len(s.turns), aliasTurns, len(s.calls), aliasCalls)
	if len(cells) > 0 {
		note += fmt.Sprintf(", %d surface cells as %s", len(cells), aliasSurface)
	}
	return
}
