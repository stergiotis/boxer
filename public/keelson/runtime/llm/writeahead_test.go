package llm

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// downOnce refuses its first insert and takes every later one.
type downOnce struct {
	mu    sync.Mutex
	tries int
}

func (inst *downOnce) Exec(context.Context, string) error { return nil }

func (inst *downOnce) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(func(arrow.RecordBatch, error) bool) {}
}

func (inst *downOnce) InsertArrow(context.Context, string, []arrow.RecordBatch) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.tries++
	if inst.tries == 1 {
		return errors.New("server down")
	}
	return nil
}

// ADR-0277 §SD3: a call whose write-ahead failed is not durable, even when
// the flush after the answer lands its rows — the request left first.
func TestAFailedWriteAheadLeavesTheCallNotDurable(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "out", FinishReason: "stop"}}
	cfg := localCfg(p)
	cfg.Trail = trail.NewRecorder(&downOnce{}, "run-test", zerolog.Nop())
	t.Cleanup(cfg.Trail.Close)
	cli, svc, _ := serve(t, cfg)
	msgs := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}

	_, err := cli.Complete(context.Background(), Request{Messages: msgs})
	require.NoError(t, err, "unrequired, the call proceeds")
	_, err = cli.Complete(context.Background(), Request{Messages: msgs})
	require.NoError(t, err)
	calls := svc.Calls()
	require.Len(t, calls, 2)
	assert.False(t, calls[0].Durable, "its request was not on the trail when it left")
	assert.True(t, calls[1].Durable)
}
