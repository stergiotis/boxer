package hostboot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// A pause and a resume are audit events within the trail's bounds, as the
// recorder stamps them (ADR-0354 §SD5).
func TestPauseRowsAreValidAuditEvents(t *testing.T) {
	ev := windowhost.PauseEvent{Key: 7, AppId: "github.com/example/app", Paused: true, From: windowhost.PauseFromWindow, At: time.Now()}
	for _, paused := range []bool{true, false} {
		ev.Paused, ev.For = paused, 90*time.Second
		row := pauseRow(ev)
		row.OriginBy, row.PrincipalBy = evidence.OriginByHost.String(), evidence.PrincipalByNone
		require.NoError(t, row.Validate(), "paused %v", paused)
	}
	row := pauseRow(ev)
	assert.Equal(t, "resume", row.Action)
	assert.Equal(t, []string{"by", "from", "paused-for"}, row.AttrKeys)
	assert.Equal(t, []string{"person", "window", "1m30s"}, row.AttrValues)
	assert.Equal(t, []string{"github.com/example/app", "7"}, row.RefValues)
}
