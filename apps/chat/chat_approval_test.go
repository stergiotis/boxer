package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// appOverRemote is appOver on a remote route whose clearance is GREEN,
// with the model service verifying approvals against the run journal.
func appOverRemote(t *testing.T, model openaichat.ClientI, store agentrun.StoreI) (inst *App, svc *llm.Service) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	decl := cell.Defaults()
	var err error
	svc, err = llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: model, Retain: llm.RetainRing, Cell: &decl, Approvals: llm.StoreApprovals(store)})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	inst = newApp()
	inst.bus = bus.NewClient(ManifestId, manifest.Caps)
	inst.cli = llm.NewClient(inst.bus)
	inst.cli.Timeout = 5 * time.Second
	inst.coord = inst.newCoordinator(inst.conv.id)
	inst.coord.store = store
	inst.coord.setRoute("api.example.net", false, 0)
	return
}

// amberNote seals an AMBER fact into the run, so every request of the
// conversation is AMBER content bound for a GREEN route. The fact is not
// confined data (that wall fires first and admits no approval): it is a
// labelled body the journal holds, as a synced peer's would be.
func amberNote(t *testing.T, inst *App) (w *agentrun.Writer) {
	t.Helper()
	w, err := inst.coord.openJournal(context.Background(), false)
	require.NoError(t, err)
	_, err = w.Append(context.Background(), agentrun.Entry{Kind: agentrun.KindNote, Name: "sealed", Form: contextFormNotice.String(), Body: "an AMBER fact", Label: agentrun.Label{Level: agentrun.LevelSystem, Tier: agentrun.TierAmber}})
	require.NoError(t, err)
	return
}

// kinds counts the journal's entries by kind.
func kinds(w *agentrun.Writer) (n map[agentrun.KindE][]agentrun.Entry) {
	n = make(map[agentrun.KindE][]agentrun.Entry)
	for _, e := range w.Entries() {
		n[e.Kind] = append(n[e.Kind], e)
	}
	return
}

// AMBER content bound for a GREEN route is held for the person (ADR-0312
// §SD3): they are asked with the tier and the route named, the question
// and their answer are journaled, a yes with a justification is journaled
// as an approval naming the refused call, and the request goes out under
// the approval's key — the run and the ordinal — which the call's row
// carries (L33); a no fails the turn with the refusal.
func TestAnExitAboveTheClearanceAsksThePerson(t *testing.T) {
	model := &fakeModel{}
	store := agentrun.NewMemStore()
	inst, svc := appOverRemote(t, model, store)
	w := amberNote(t, inst)
	require.True(t, inst.startTurn("q1"))
	var o *openAsk
	require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o != nil }, 5*time.Second, 5*time.Millisecond)
	require.Len(t, o.questions, 1)
	assert.Contains(t, o.questions[0].Question, "AMBER content to model-route api.example.net")
	o.form.single[0], o.form.chosen[0][0], o.form.notes[0][0] = 0, true, "the fact is the customer's own"
	o.submit()
	drainUntil(t, inst)
	require.Len(t, inst.conv.entries, 2, "%+v", inst.conv.entries)
	assert.Equal(t, "a1", inst.conv.entries[1].text)
	byKind := kinds(w)
	require.Len(t, byKind[agentrun.KindAsked], 1, "the question is journaled")
	require.Len(t, byKind[agentrun.KindAnswered], 1, "and the answer")
	assert.Contains(t, byKind[agentrun.KindAnswered][0].Body, `"approved":true`)
	approvals := byKind[agentrun.KindApproval]
	require.Len(t, approvals, 1)
	a, err := cell.ParseApproval(approvals[0].Body)
	require.NoError(t, err)
	assert.Equal(t, "model-route", a.Channel)
	assert.Equal(t, "api.example.net", a.Edge)
	assert.Equal(t, "AMBER", a.Tier)
	assert.Equal(t, "person", a.Approver)
	assert.Equal(t, "person", a.Path)
	assert.Equal(t, "the fact is the customer's own", a.Justification)
	assert.Equal(t, "model-route api.example.net", approvals[0].Name, "the entry names the sink")
	var sent, refused []llm.CallRecord
	for _, c := range svc.Calls() {
		if c.Purpose != purpose {
			continue
		}
		if c.Refused {
			refused = append(refused, c)
		} else {
			sent = append(sent, c)
		}
	}
	require.Len(t, refused, 1)
	assert.Equal(t, refused[0].CallId, approvals[0].Key, "the approval names the refused call")
	require.Len(t, sent, 1)
	assert.Equal(t, cell.ApprovalKey(w.Run().Id, approvals[0].Ordinal), sent[0].Approval, "the row names the approval entry (L33)")
	assert.EqualValues(t, agentrun.TierAmber, sent[0].Tier)
	assert.Contains(t, sent[0].Profile, "default@")

	// Declined: the refusal stands.
	require.True(t, inst.startTurn("q2"))
	require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o != nil }, 5*time.Second, 5*time.Millisecond)
	o.form.single[0], o.form.chosen[0][1] = 1, true
	o.submit()
	drainUntil(t, inst)
	last := inst.conv.entries[len(inst.conv.entries)-1]
	assert.Contains(t, last.fail.detail, "above the clearance")
	assert.Contains(t, kinds(w)[agentrun.KindAnswered][1].Body, `"approved":false`)
}

// One answer covers a turn: a second round's exit of the same tier to the
// same sink is approved under the person's standing answer without asking
// again, with an approval entry of its own, since the route binds each
// approval to one exit. A yes without a justification is asked again,
// and counts as a no when none comes.
func TestAnApprovalStandsForTheTurn(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r1", "artefact_read", `{}`),
		{Content: "read", FinishReason: "stop"},
		{Content: "again", FinishReason: "stop"},
	}}
	store := agentrun.NewMemStore()
	inst, svc := appOverRemote(t, model, store)
	inst.artefact = true
	w := amberNote(t, inst)
	require.True(t, inst.startTurn("q1"))
	var o *openAsk
	require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o != nil }, 5*time.Second, 5*time.Millisecond)
	o.form.single[0], o.form.chosen[0][0] = 0, true
	o.submit()
	require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o != nil && !o.form.sent }, 5*time.Second, 5*time.Millisecond)
	assert.True(t, strings.HasPrefix(o.questions[0].Question, "A justification is required"), o.questions[0].Question)
	o.form.single[0], o.form.chosen[0][0], o.form.notes[0][0] = 0, true, "needed for the report"
	o.submit()
	drainUntil(t, inst)
	require.Equal(t, "read", inst.conv.entries[len(inst.conv.entries)-1].text)
	byKind := kinds(w)
	assert.Len(t, byKind[agentrun.KindAsked], 1, "one question for the turn")
	assert.Len(t, byKind[agentrun.KindAnswered], 1)
	approvals := byKind[agentrun.KindApproval]
	require.Len(t, approvals, 2, "one approval per exit")
	assert.NotEqual(t, approvals[0].Key, approvals[1].Key, "each names its refused call")
	for _, e := range approvals {
		a, err := cell.ParseApproval(e.Body)
		require.NoError(t, err)
		assert.Equal(t, "needed for the report", a.Justification)
	}
	var sent int
	for _, c := range svc.Calls() {
		if c.Purpose == purpose && !c.Refused {
			sent++
			assert.NotEmpty(t, c.Approval)
		}
	}
	assert.Equal(t, 2, sent)

	// The next turn asks again.
	require.True(t, inst.startTurn("q2"))
	require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o != nil && !o.form.sent }, 5*time.Second, 5*time.Millisecond)
	for range approvalAsks {
		o.form.single[0], o.form.chosen[0][0] = 0, true
		o.submit()
		require.Eventually(t, func() bool { inst.drain(); o = inst.coord.ask.current(); return o == nil || !o.form.sent }, 5*time.Second, 5*time.Millisecond)
	}
	drainUntil(t, inst)
	assert.Contains(t, inst.conv.entries[len(inst.conv.entries)-1].fail.detail, "above the clearance", "three yeses without a reason are a no")
	assert.Len(t, kinds(w)[agentrun.KindApproval], 2)
}
