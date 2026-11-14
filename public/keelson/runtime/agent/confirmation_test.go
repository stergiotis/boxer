package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A confirmation shows what the call carries: its arguments, indented,
// whole up to a bound and counted past it.
func TestAConfirmationShowsTheCallsArguments(t *testing.T) {
	assert.Equal(t, "{\n  \"text\": \"rm -rf ~\"\n}", confirmationArgs(`{"text":"rm -rf ~"}`))
	assert.Equal(t, "(none)", confirmationArgs(""))
	assert.Equal(t, "(none)", confirmationArgs("{}"))
	assert.Equal(t, "not json", confirmationArgs("not json"), "what does not parse is shown as it is")
	long := confirmationArgs(`{"text":"` + strings.Repeat("é", maxConfirmationArgs) + `"}`)
	assert.Contains(t, long, "more bytes not shown")
	assert.LessOrEqual(t, len(long), maxConfirmationArgs+64)

	r, g := approvedRig(t, ModeAct)
	require.Equal(t, "proposed", r.call(g, "x1", "publish_text", `{"name":"report-1"}`).Phase)
	r.svc.mu.Lock()
	ps := r.svc.proposals(0, true)
	r.svc.mu.Unlock()
	require.Len(t, ps, 1)
	assert.Contains(t, confirmationArgs(ps[0].rec.req.Args), `"name": "report-1"`, "the dialog's arguments are the call's")
}
