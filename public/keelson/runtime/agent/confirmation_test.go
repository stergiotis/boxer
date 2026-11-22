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

// A request's card lists the windows the model asked for and folds the
// others; a widening leaves out what its task already holds, and lists only
// the destinations it adds, each in words.
func TestARequestCardSaysWhatIsNew(t *testing.T) {
	svc := &Service{}
	held := &task{entries: map[uint64]*entry{2: {instance: 2, alias: "play", mode: ModeAct}},
		destinations: []string{"clickhouse:localhost:8123"}}
	r := &request{task: held, wanted: map[uint64]ModeE{3: ModeAct}, share: map[uint64]bool{3: true},
		destinations: []string{"clickhouse:localhost:8123", "keelson:apps"}}
	asked, others := svc.splitWindows(r, []windowRow{{key: 2}, {key: 3}, {key: 4}})
	require.Len(t, asked, 1)
	assert.Equal(t, uint64(3), asked[0].key)
	require.Len(t, others, 1, "window 2 is the task's already")
	assert.Equal(t, uint64(4), others[0].key)
	assert.Equal(t, []string{"keelson:apps"}, svc.newDestinations(r))
	assert.Equal(t, []string{"clickhouse:localhost:8123", "keelson:apps"}, svc.newDestinations(&request{destinations: r.destinations}), "a new task's are all new")

	assert.Equal(t, "query the ClickHouse server at localhost:8123", destinationText("clickhouse:localhost:8123"))
	assert.Equal(t, "read this host's table keelson('apps')", destinationText("keelson:apps"))
	assert.Contains(t, destinationText("publish:seco_"), "start with seco_")
	assert.Contains(t, destinationText("expose:result"), "let programs on this machine read the ad-hoc datasets whose names start with result")
	assert.Equal(t, "weird:thing", destinationText("weird:thing"), "a class it does not know is shown by name")
}
