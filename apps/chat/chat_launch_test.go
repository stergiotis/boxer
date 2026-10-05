package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
)

// The model hears how far a window it opened has loaded, and a mount error
// reaches it as the app's text (ADR-0269 §SD3, §SD7).
func TestALaunchedWindowIsReportedAsFarAsItLoaded(t *testing.T) {
	co := &coordinator{}
	content, activity := co.launched("notes", agent.Launched{Instance: 3, Load: "ready"})
	assert.JSONEq(t, `{"window":3,"load":"ready"}`, content)
	assert.Equal(t, "opened notes as window 3", activity)

	content, activity = co.launched("notes", agent.Launched{Instance: 4, Load: "opening"})
	assert.JSONEq(t, `{"window":4,"load":"opening"}`, content)
	assert.Contains(t, activity, "still opening")

	content, activity = co.launched("notes", agent.Launched{Instance: 5, Load: "failed", LoadReason: "no database"})
	assert.Contains(t, content, untrustedOpen, "a mount error is the app's text")
	assert.Contains(t, content, `"load_reason":"no database"`)
	assert.Equal(t, "notes in window 5 failed to open", activity)
	_, tainted, _ := co.state()
	assert.True(t, tainted)
}
