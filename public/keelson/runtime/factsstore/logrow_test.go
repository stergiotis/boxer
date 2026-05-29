//go:build llm_generated_opus47

package factsstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryFactsStore_WriteLog_AssignsId(t *testing.T) {
	s := NewInMemoryFactsStore()
	id1, err := s.WriteLog(LogRow{Level: "info", Message: "first", Ts: time.Now()})
	require.NoError(t, err)
	id2, err := s.WriteLog(LogRow{Level: "info", Message: "second", Ts: time.Now()})
	require.NoError(t, err)
	assert.NotZero(t, id1)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, s.Logs(), 2)
}

func TestInMemoryFactsStore_WriteLog_CapturesFields(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteLog(LogRow{
		AppId:   "play",
		Level:   "warn",
		Message: "slow query",
		Fields: []LogField{
			{Name: "latency_ms", Kind: LogFieldKindInt, Int: 1200},
			{Name: "subject", Kind: LogFieldKindString, Str: "ch.query.boxer"},
		},
		Ts: time.Now(),
	})
	require.NoError(t, err)
	logs := s.Logs()
	require.Len(t, logs, 1)
	assert.Equal(t, "warn", logs[0].Level)
	require.Len(t, logs[0].Fields, 2)
	assert.Equal(t, "latency_ms", logs[0].Fields[0].Name)
	assert.Equal(t, int64(1200), logs[0].Fields[0].Int)
	assert.Equal(t, LogFieldKindInt, logs[0].Fields[0].Kind)
}

// TestInMemoryFactsStore_WriteLog_DefensiveBytesCopy guards the contract
// the chstore relies on: a caller (logbridge's decode loop in particular)
// must be free to recycle its byte slices after WriteLog returns.
func TestInMemoryFactsStore_WriteLog_DefensiveBytesCopy(t *testing.T) {
	s := NewInMemoryFactsStore()
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	_, err := s.WriteLog(LogRow{
		Level:   "info",
		Message: "blob field",
		Fields:  []LogField{{Name: "raw", Kind: LogFieldKindBytes, Bytes: payload}},
		Ts:      time.Now(),
	})
	require.NoError(t, err)
	payload[0] = 0x00
	logs := s.Logs()
	require.Len(t, logs, 1)
	require.Len(t, logs[0].Fields, 1)
	assert.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, logs[0].Fields[0].Bytes)
}

// TestLogErrorContext_Summary covers the flat-text fallback that
// table-column readers rely on. The first non-empty Msg encountered
// in chain order wins — the chain's outermost wrap is what reads as
// the canonical error string in the viewer's Error column. Edge
// cases: nil context returns ""; no-msg facts (the per-frame stubs
// eh's materialize pass produces) are skipped; an entirely empty
// chain returns "" without panicking.
func TestLogErrorContext_Summary(t *testing.T) {
	t.Run("nil receiver returns empty", func(t *testing.T) {
		var ctx *LogErrorContext
		assert.Equal(t, "", ctx.Summary())
	})

	t.Run("first non-empty msg wins across streams", func(t *testing.T) {
		ctx := &LogErrorContext{
			Streams: []LogErrorStream{
				{Name: "stack-0", Facts: []LogErrorFact{
					{Msg: "outer wrap"},
					{Msg: "inner cause"},
				}},
			},
		}
		assert.Equal(t, "outer wrap", ctx.Summary(),
			"chain order picks the outermost message — the most recent %w prefix")
	})

	t.Run("frame-only facts are skipped", func(t *testing.T) {
		ctx := &LogErrorContext{
			Streams: []LogErrorStream{
				{Name: "stack-0", Facts: []LogErrorFact{
					{Source: "x.go", Line: "1", Function: "DoThing"},
					{Msg: "the actual error"},
				}},
			},
		}
		assert.Equal(t, "the actual error", ctx.Summary(),
			"per-frame stubs without msg must not be treated as the summary line")
	})

	t.Run("empty chain returns empty", func(t *testing.T) {
		ctx := &LogErrorContext{}
		assert.Equal(t, "", ctx.Summary())
	})
}
