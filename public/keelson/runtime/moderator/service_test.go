package moderator

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

type answer struct{}

func (answer) Complete(context.Context, openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	return openaichat.CompletionResponse{Content: "ok", InputTokens: 10, OutputTokens: 2}, nil
}
func (answer) Close() (err error) { return }

// actionPayload is an action record as the dispatcher publishes it.
type actionPayload struct {
	V             uint8  `json:"v"`
	AtUnixMs      int64  `json:"at_unix_ms"`
	Task          string `json:"task,omitempty"`
	Actor         string `json:"actor,omitempty"`
	ActorInstance uint64 `json:"actor_instance,omitempty"`
	Key           string `json:"key,omitempty"`
	Instance      uint64 `json:"instance,omitempty"`
	Operation     string `json:"operation,omitempty"`
	ArgsDigest    string `json:"args_digest,omitempty"`
	Decision      string `json:"decision,omitempty"`
	Phase         string `json:"phase,omitempty"`
}

// Over the in-process bus with the model service and the dispatcher: a
// window that repeats a call is noted, then slowed, then — driving no task
// — denied; its model calls are refused and the table shows it stopped.
func TestTheLadderActsThroughTheServices(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	llmSvc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1/v1", Model: "m", MaxTokens: 64,
		Client: answer{}, Moderators: []string{string(AppId)}})
	require.NoError(t, err)
	t.Cleanup(llmSvc.Close)
	agentSvc, err := agent.NewService(bus, zerolog.Nop(), agent.Config{})
	require.NoError(t, err)
	t.Cleanup(agentSvc.Close)
	agentSvc.SetModerators([]string{string(AppId)})
	cfg := DefaultConfig()
	cfg.Tick = time.Hour
	mod, err := NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(mod.Close)

	appBus := bus.NewClient("test.chat", llm.ClientCaps("test: ask"))
	appBus.SetInstanceKey(7)
	cli := llm.NewClient(appBus)
	cli.Timeout = 5 * time.Second
	_, err = cli.Complete(context.Background(), llm.Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}})
	require.NoError(t, err)

	pub := bus.NewClient("test.dispatcher", []app.SubjectFilter{{Pattern: agent.SubjectActionRecorded, Direction: app.CapDirectionPub}})
	n := 0
	repeat := func() {
		for range 3 {
			n++
			payload, perr := buscodec.Encode(actionPayload{V: 1, AtUnixMs: time.Now().UnixMilli(), Task: "task-1", Actor: "test.chat",
				ActorInstance: 7, Key: "k" + strconv.Itoa(n), Instance: 9, Operation: "set_text", ArgsDigest: "d", Decision: "final", Phase: "completed"})
			require.NoError(t, perr)
			require.NoError(t, pub.Publish(agent.SubjectActionRecorded, payload))
		}
	}
	level := func() LevelE {
		for _, st := range mod.States() {
			if st.Window.Instance == 7 {
				return st.Level
			}
		}
		return LevelNone
	}
	rule := func(id string) bool {
		_, ok := llmSvc.Ledger().Rule(id)
		return ok
	}

	repeat()
	require.Eventually(t, func() bool { return level() == LevelNoted }, 2*time.Second, time.Millisecond)
	assert.False(t, rule(RulePrefix+"slow/7"))

	repeat()
	require.Eventually(t, func() bool { return rule(RulePrefix + "slow/7") }, 2*time.Second, time.Millisecond)
	slow, _ := llmSvc.Ledger().Rule(RulePrefix + "slow/7")
	assert.Equal(t, ration.RuleKindRate, slow.Kind)
	assert.Equal(t, string(AppId), slow.Author)
	assert.False(t, slow.Until.IsZero(), "the moderator's rules lapse")

	repeat()
	require.Eventually(t, func() bool { return rule(RulePrefix + "deny/7") }, 2*time.Second, time.Millisecond)
	assert.Equal(t, LevelStopped, level())
	_, err = cli.Complete(context.Background(), llm.Request{Messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}})
	var refused *llm.RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, ration.RefusalStop, refused.Refusal)
	assert.Contains(t, refused.Reason, "repeat")
}
