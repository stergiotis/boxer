package chat

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

func answered(content string, calls ...llm.Response) *turnResult {
	res := &turnResult{final: llm.Response{Content: content}}
	for i, c := range calls {
		res.calls = append(res.calls, callStatOf(i, c))
	}
	return res
}

func TestTheStatisticsRecordTurnsAndTheirCalls(t *testing.T) {
	var s chatStats
	t0 := time.Unix(1700000000, 0)
	s.addTurn("c1", "turn-x", t0, t0.Add(2*time.Second).UnixMilli(), answered("hello",
		llm.Response{InputTokens: 100, OutputTokens: 20, ToolCalls: []openaichat.ToolCall{{Id: "x"}}},
		llm.Response{InputTokens: 140, OutputTokens: 30, FinishReason: "stop"}), nil)
	s.addTurn("c1", "turn-x", t0, t0.Add(500*time.Millisecond).UnixMilli(), nil, errors.New("boom"))
	s.addTurn("c1", "turn-x", t0, t0.UnixMilli(), nil, context.Canceled)
	s.addTurn("c2", "turn-x", t0, t0.Add(time.Second).UnixMilli(), &turnResult{stopped: "rounds"}, nil)
	s.addTurn("c2", "turn-x", t0, t0.Add(3*time.Second).UnixMilli(), answered("ok", llm.Response{InputTokens: 10, OutputTokens: 5}), nil)

	require.Len(t, s.turns, 5)
	first := s.turns[0]
	assert.Equal(t, 0, first.turn)
	assert.Equal(t, outcomeAnswered, first.outcome)
	assert.Equal(t, 2, first.rounds)
	assert.Equal(t, 1, first.toolCalls)
	assert.Equal(t, int64(240), first.inputTokens)
	assert.Equal(t, int64(50), first.outputTokens)
	assert.Equal(t, int64(2000), first.elapsedMs)
	assert.Equal(t, 5, first.answerChars)
	assert.Equal(t, []string{outcomeAnswered, outcomeFailed, outcomeCancelled, outcomeStopped, outcomeAnswered},
		[]string{s.turns[0].outcome, s.turns[1].outcome, s.turns[2].outcome, s.turns[3].outcome, s.turns[4].outcome})
	assert.Equal(t, 2, s.turns[2].turn, "turns count per conversation")
	assert.Equal(t, 1, s.turns[4].turn)

	require.Len(t, s.calls, 3)
	assert.Equal(t, "c1", s.calls[1].conversation)
	assert.Equal(t, 1, s.calls[1].round)
	assert.Equal(t, "c2", s.calls[2].conversation)
	assert.Equal(t, 1, s.calls[2].turn)

	assert.Equal(t, []float64{2, 3}, s.answerSeconds(), "answered turns only, sorted")
	assert.Equal(t, []float64{5, 20, 30}, s.callTokens(true))
	turns, ok, calls, in, out := s.totals()
	assert.Equal(t, []int64{5, 2, 3, 250, 55}, []int64{int64(turns), int64(ok), int64(calls), in, out})
}

func decodeStream(t *testing.T, stream []byte) arrow.RecordBatch {
	t.Helper()
	rdr, err := ipc.NewReader(bytes.NewReader(stream))
	require.NoError(t, err)
	t.Cleanup(rdr.Release)
	require.True(t, rdr.Next())
	rec := rdr.RecordBatch()
	rec.Retain()
	t.Cleanup(rec.Release)
	return rec
}

// The tables are what play reads: one row per call and per turn, in types
// the publish gate takes.
func TestTheStatisticsTablesPassThePublishGate(t *testing.T) {
	for _, sc := range []*arrow.Schema{callsSchema, turnsSchema} {
		_, err := adhocdata.StructureFor(sc)
		require.NoError(t, err)
	}
	var s chatStats
	t0 := time.Unix(1700000000, 0)
	s.addTurn("c1", "turn-x", t0, t0.Add(time.Second).UnixMilli(), answered("hi", llm.Response{InputTokens: 7, OutputTokens: 3, FinishReason: "stop"}), nil)
	calls, err := callsArrow(s.calls)
	require.NoError(t, err)
	rec := decodeStream(t, calls)
	assert.Equal(t, int64(1), rec.NumRows())
	assert.Equal(t, "call_id", rec.ColumnName(2), "what joins a row to keelson('llm_calls')")
	assert.Equal(t, "input_tokens", rec.ColumnName(6))
	turns, err := turnsArrow(s.turns)
	require.NoError(t, err)
	rec = decodeStream(t, turns)
	assert.Equal(t, int64(1), rec.NumRows())
	assert.Equal(t, "turn_id", rec.ColumnName(1))
	assert.Equal(t, "outcome", rec.ColumnName(4))
}

func TestNothingIsHandedOverBeforeATurn(t *testing.T) {
	_, err := openStatsInPlay(nil, newStatsPublishers(), chatStats{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no turn")
}

func TestBoxerChatAdvancedFalseHidesTheStatistics(t *testing.T) {
	assert.True(t, newApp().advanced, "on by default")
	AdvancedSeed.SetForTest(t, "false")
	assert.False(t, newApp().advanced)
}
