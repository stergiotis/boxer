package chat

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The apps and desktop notes say where the model stands: told when they
// are news, told again when the desktop changed or the turn that told them
// went unread.
func TestTheDesktopNotesSayWhereTheModelStands(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("c1", "request_access", `{"plan":"look","open":[{"app":"notes"}]}`),
		toolCall("c2", "open_window", `{"app":"notes"}`),
		{Content: "done", FinishReason: "stop"},
	}}
	_, coord, cli, req, ctx := coordRig(t, bus, model, false)

	assert.Contains(t, coord.appsNote(ctx), "- "+notesId+" — Notes: keep a note (help)")
	assert.Empty(t, coord.appsNote(ctx), "the apps are told once")
	assert.Contains(t, coord.desktopNote(ctx), "no task yet")
	assert.Empty(t, coord.desktopNote(ctx), "nothing changed")

	_, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	marks := coord.toldNow()
	desk := coord.desktopNote(ctx)
	assert.Contains(t, desk, "- 100 — "+notesId+", act, ready", "the task holds the window it opened")
	assert.NotContains(t, desk, "title", "a window's title is the app's text")

	// A turn that ends unanswered leaves its notes unread: told again.
	coord.untell(marks)
	assert.Equal(t, desk, coord.desktopNote(ctx))

	// Without Apps the model has no windows to stand among.
	coord.offer(false, false)
	coord.untell(marks)
	assert.Empty(t, coord.desktopNote(ctx))
}
