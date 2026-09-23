package orchestrator_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql2/orchestrator"
)

// toolTurn scripts one ChatTools answer: calls, or content.
type toolTurn struct {
	calls   []orchestrator.ToolCall
	content string
}

// mockToolLLM answers ChatTools from a script and records every turn's
// messages and the tools it was offered.
type mockToolLLM struct {
	turns   []toolTurn
	idx     int
	offered [][]orchestrator.Tool
	seen    [][]orchestrator.Message
}

func (inst *mockToolLLM) Chat(_ context.Context, _ string, _ []orchestrator.Message) (string, error) {
	return "", assert.AnError
}

func (inst *mockToolLLM) ChatTools(_ context.Context, _ string, messages []orchestrator.Message, tools []orchestrator.Tool) (string, []orchestrator.ToolCall, error) {
	inst.offered = append(inst.offered, tools)
	inst.seen = append(inst.seen, append([]orchestrator.Message(nil), messages...))
	if inst.idx >= len(inst.turns) {
		return "", nil, assert.AnError
	}
	t := inst.turns[inst.idx]
	inst.idx++
	return t.content, t.calls, nil
}

// mockTools answers describe_table with a canned column list and records
// the calls it ran.
type mockTools struct{ ran []orchestrator.ToolCall }

func (inst *mockTools) Tools() []orchestrator.Tool {
	return []orchestrator.Tool{{Name: "describe_table", Description: "d", Parameters: `{"type":"object"}`}}
}

func (inst *mockTools) Call(_ context.Context, call orchestrator.ToolCall) (string, error) {
	inst.ran = append(inst.ran, call)
	return "id UInt64\nname String", nil
}

// The loop: the model asks for a table, the executor answers in the
// caller's process, the tool turn is appended with its call id, and the
// model's SQL then compiles. The tool history stays in the conversation.
func TestCompileToolLoop(t *testing.T) {
	llm := &mockToolLLM{turns: []toolTurn{
		{calls: []orchestrator.ToolCall{{Id: "c1", Name: "describe_table", Arguments: `{"table":"t"}`}}},
		{content: sqlResponse("SELECT id FROM t")},
	}}
	tools := &mockTools{}
	orch := orchestrator.New(orchestrator.Config{DefaultModel: "m", Tools: tools}, llm, NewMockCH(), NewMockCache(), NewRecordingObserver())
	result, err := orch.Compile(context.Background(), "the ids")
	require.NoError(t, err)
	assert.Contains(t, result.SQL, "SELECT")
	assert.Equal(t, 1, result.Attempts)
	require.Len(t, tools.ran, 1)
	assert.Equal(t, "c1", tools.ran[0].Id)

	require.Len(t, llm.seen, 2)
	last := llm.seen[1]
	require.GreaterOrEqual(t, len(last), 4)
	assert.Equal(t, "assistant", last[len(last)-2].Role)
	assert.Len(t, last[len(last)-2].ToolCalls, 1)
	assert.Equal(t, "tool", last[len(last)-1].Role)
	assert.Equal(t, "c1", last[len(last)-1].ToolCallId)
	assert.Contains(t, last[len(last)-1].Content, "UInt64")
	assert.Len(t, llm.offered[0], 1, "tools were offered")
}

// A spent budget: calls past it are answered with an error the model reads,
// the next turn is asked without tools, and the question still ends in SQL.
func TestCompileToolBudget(t *testing.T) {
	llm := &mockToolLLM{turns: []toolTurn{
		{calls: []orchestrator.ToolCall{{Id: "c1", Name: "describe_table"}, {Id: "c2", Name: "describe_table"}}},
		{content: sqlResponse("SELECT 1")},
	}}
	tools := &mockTools{}
	orch := orchestrator.New(orchestrator.Config{DefaultModel: "m", Tools: tools, MaxToolCalls: 1}, llm, NewMockCH(), NewMockCache(), NewRecordingObserver())
	result, err := orch.Compile(context.Background(), "one")
	require.NoError(t, err)
	assert.Contains(t, result.SQL, "SELECT")
	assert.Len(t, tools.ran, 1, "the second call is over budget and never runs")
	last := llm.seen[1]
	assert.Contains(t, last[len(last)-1].Content, "budget")
	assert.Empty(t, llm.offered[1], "past the budget the model is asked without tools")
}

// A client that cannot carry tools keeps today's single-shot path even when
// an executor is configured.
func TestCompileToolsNeedAToolClient(t *testing.T) {
	llm := NewMockLLM(MockLLMResponse{Content: sqlResponse("SELECT 1")})
	tools := &mockTools{}
	orch := orchestrator.New(orchestrator.Config{DefaultModel: "m", Tools: tools}, llm, NewMockCH(), NewMockCache(), NewRecordingObserver())
	_, err := orch.Compile(context.Background(), "one")
	require.NoError(t, err)
	assert.Empty(t, tools.ran)
}

func TestValidate(t *testing.T) {
	canonical, err := orchestrator.Validate("SELECT a FROM t WHERE b = 1")
	require.NoError(t, err)
	assert.True(t, strings.Contains(strings.ToUpper(canonical), "SELECT"))
	_, err = orchestrator.Validate("SELECT FROM WHERE")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "syntax")
}
