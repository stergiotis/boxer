package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Arranging needs the desktop in act; raising and placing need act on the
// window; each accepted verb reaches the host and is recorded with effect
// view (ADR-0276 §SD3, §SD4).
func TestWindowVerbsFollowTheGrant(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()

	observer := r.grant(ModeObserve)
	out, err := r.cli.Arrange(ctx, observer.Handle, Asked{Key: "a1"}, "tile", nil)
	require.NoError(t, err)
	assert.Equal(t, "input_required", out.Phase)
	assert.Contains(t, out.Reason, "desktop in act mode")
	out, err = r.cli.Raise(ctx, observer.Handle, Asked{Key: "r1"}, r.docKey)
	require.NoError(t, err)
	assert.Equal(t, "input_required", out.Phase, "observe on the window does not raise it")
	out, err = r.cli.Place(ctx, observer.Handle, Asked{Key: "p0"}, 99, 0, 0, 10, 10)
	require.NoError(t, err)
	assert.Contains(t, out.Reason, "does not cover", "a window outside the grant is not placed")

	actor, err := r.cli.Request(ctx, GrantRequest{Plan: "tidy the desktop", Desktop: ModeAct,
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.NoError(t, err)
	out, err = r.cli.Arrange(ctx, actor.Handle, Asked{Key: "a2"}, "columns", []uint64{r.docKey})
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase)
	out, err = r.cli.Raise(ctx, actor.Handle, Asked{Key: "r2"}, r.docKey)
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase)
	out, err = r.cli.Place(ctx, actor.Handle, Asked{Key: "p2"}, r.docKey, 10, 40, 300, 200)
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase)

	// A repeated key answers the first call and does not reach the host again.
	_, err = r.cli.Raise(ctx, actor.Handle, Asked{Key: "r2"}, r.docKey)
	require.NoError(t, err)
	r.host.mu.Lock()
	assert.Equal(t, []string{"arrange:columns", "raise", "place"}, r.host.windowActs)
	r.host.mu.Unlock()

	var verbs []string
	for _, rec := range r.svc.Actions() {
		if rec.Task == "" || rec.Effect != "view" {
			continue
		}
		verbs = append(verbs, rec.Operation+":"+rec.Phase)
	}
	assert.Contains(t, verbs, "arrange:completed")
	assert.Contains(t, verbs, "arrange:input_required", "a refused verb is recorded too")

	var desktops []string
	for _, g := range r.svc.Grants() {
		desktops = append(desktops, g.Desktop)
	}
	assert.ElementsMatch(t, []string{"", "act"}, desktops, "agent_grants shows the desktop mode")
}

// A grant may name the desktop alone.
func TestAGrantMayNameOnlyTheDesktop(t *testing.T) {
	r := newRig(t, true)
	g, err := r.cli.Request(context.Background(), GrantRequest{Plan: "tile everything", Desktop: ModeAct})
	require.NoError(t, err)
	out, err := r.cli.Arrange(context.Background(), g.Handle, Asked{Key: "a"}, "tile", nil)
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase)
}

// A verb the model's tool call asked for carries its turn and model call on
// the record, as an operation's call does (ADR-0277 §SD1).
func TestAWindowVerbNamesTheModelCallThatAsked(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g, err := r.cli.Request(ctx, GrantRequest{Plan: "raise it", Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.NoError(t, err)
	out, err := r.cli.Raise(ctx, g.Handle, Asked{Key: "llm-1#0", Turn: "turn-1", ModelCall: "llm-1", ToolCall: "call_0"}, r.docKey)
	require.NoError(t, err)
	require.Equal(t, "completed", out.Phase)
	var raised int
	for _, a := range r.svc.Actions() {
		if a.Operation == "raise" {
			raised++
			assert.Equal(t, "turn-1", a.Turn)
			assert.Equal(t, "llm-1", a.ModelCall)
			assert.Equal(t, "call_0", a.ToolCallId)
		}
	}
	assert.Positive(t, raised)
}
