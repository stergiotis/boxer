package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Marking a window needs suggest on it and a viewport rect the desktop in
// suggest; an accepted mark lands on the host's scene under the task,
// and ending the task retires it (ADR-0297 §SD8).
func TestMarkNeedsSuggestAndEndsWithTheTask(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	scene := r.host.Marks()

	g := r.grant(ModeObserve)
	ask := func(key string, targets ...Target) Outcome {
		out, err := r.cli.Mark(ctx, MarkRequest{Handle: g.Handle, Asked: Asked{Key: key}, Id: "save",
			Op: "callout", Targets: targets, Text: "press this"})
		require.NoError(t, err)
		return out
	}
	local := [4]float32{10, 20, 40, 18}
	out := ask("observe", Target{Window: r.docKey, Rect: &local})
	assert.Equal(t, "input_required", out.Phase)
	assert.Contains(t, out.Reason, "suggest")
	assert.Equal(t, 0, scene.Len())

	// A test grant is observe or act; act covers suggest.
	g = r.grant(ModeAct)
	out = ask("suggest", Target{Window: r.docKey, Rect: &local})
	require.Equal(t, "completed", out.Phase, out.Reason)
	items := scene.Snapshot()
	require.Len(t, items, 1)
	assert.Equal(t, g.Task, items[0].Task)
	assert.Equal(t, "press this", items[0].Text)
	require.NotNil(t, items[0].Targets[0].Local)
	assert.Equal(t, float32(10), items[0].Targets[0].Local.X)

	out = ask("other", Target{Window: 99})
	assert.Equal(t, "input_required", out.Phase)
	assert.Contains(t, out.Reason, "window 99")

	vp := [4]float32{0, 0, 10, 10}
	out = ask("desktop", Target{Viewport: true, Rect: &vp})
	assert.Equal(t, "input_required", out.Phase)
	assert.Contains(t, out.Reason, "desktop")

	out, err := r.cli.Mark(ctx, MarkRequest{Handle: g.Handle, Asked: Asked{Key: "badop"}, Id: "x", Op: "scribble",
		Targets: []Target{{Window: r.docKey}}})
	require.NoError(t, err)
	assert.Equal(t, "refused", out.Phase)

	require.NoError(t, r.cli.StopWith(ctx, g.Handle, StopRequest{Reason: "done"}))
	assert.Equal(t, 0, scene.Len(), "the task's marks end with it")
}

// A task clears its own marks, by id or all; another task's stay.
func TestClearRemovesOnlyTheTasksOwn(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	scene := r.host.Marks()
	require.NoError(t, scene.Put(inscribeMark("someone-else")))
	g := r.grant(ModeAct)
	for _, id := range []string{"a", "b"} {
		out, err := r.cli.Mark(ctx, MarkRequest{Handle: g.Handle, Asked: Asked{Key: id}, Id: id, Op: "highlight",
			Targets: []Target{{Window: r.docKey}}})
		require.NoError(t, err)
		require.Equal(t, "completed", out.Phase, out.Reason)
	}
	out, err := r.cli.Unmark(ctx, g.Handle, Asked{Key: "clear-a"}, "a")
	require.NoError(t, err)
	assert.Equal(t, "completed", out.Phase)
	assert.Equal(t, 2, scene.Len())
	out, err = r.cli.Unmark(ctx, g.Handle, Asked{Key: "clear-all"}, "")
	require.NoError(t, err)
	assert.Contains(t, out.Reason, "1 mark")
	assert.Equal(t, []string{"someone-else"}, scene.Tasks())
}
