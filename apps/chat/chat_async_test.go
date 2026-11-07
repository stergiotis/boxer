package chat

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// asyncNoteOps are the note's operations with one that runs in the
// background until the test says it finished, kept apart from noteOps so the
// preservation goldens do not see it.
var asyncNoteOps = func() *appops.Set[*note, noteSnap] {
	s := noteOpsSet()
	appops.Command(s, app.OperationSpec{Name: "slow_job", Version: 1, Summary: "work for a while", Effect: app.OperationEffectRun,
		Writes: []string{"text"}, AnyRevision: true, Agents: true, Async: true},
		func(n *note, _ app.OperationCall, _ appops.None) (noteResult, error) { return noteResult{}, nil })
	appops.Settles(s, "slow_job", func(n *note, _ app.OperationCall) (out noteResult, settled bool, err error) {
		return noteResult{Text: "the job's result"}, n.jobDone.Load(), nil
	})
	return s
}()

// asyncScript opens a window and starts the job, then answers.
func asyncScript(replies ...openaichat.CompletionResponse) *scriptedModel {
	return &scriptedModel{replies: append([]openaichat.CompletionResponse{
		toolCall("a", "request_access", `{"plan":"run the job","open":[{"app":"notes"}]}`),
		toolCall("o", "open_window", `{"app":"notes"}`),
		toolCall("j", "call_operation", `{"window":100,"operation":"slow_job","args":{}}`),
		{Content: "it is running", FinishReason: "stop"},
	}, replies...)}
}

// shortWait is the default profile under a wait the job outlasts.
func shortWait(t *testing.T, change ...func(p *ProfileT)) {
	t.Helper()
	p := defaultProfile()
	p.CallWait = 20 * time.Millisecond
	for _, f := range change {
		f(&p)
	}
	setProfileForTest(t, p)
}

func entriesOf(w *agentrun.Writer, kind agentrun.KindE) (out []agentrun.Entry) {
	for _, e := range w.Entries() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return
}

// A call still running when the wait ends is pending; a completed entry
// records its end as soon as it is known, and the model reads it in a note at
// the next round. It runs against the host's own checks of the journal, so the
// tool loop and the model calls are held to them too (ADR-0357, ADR-0358).
func TestALongCallIsPendingAndTheModelIsToldWhenItSettles(t *testing.T) {
	shortWait(t)
	store := agentrun.NewMemStore()
	model := asyncScript(openaichat.CompletionResponse{Content: "it finished", FinishReason: "stop"})
	bus := inprocbus.NewInst(zerolog.Nop())
	host, coord, cli, req, ctx := coordRigWith(t, bus, model, false, asyncNoteOps, store)

	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, "it is running", res.final.Content)
	w := coord.journalNow()

	var co callOutcome
	reply := toolReplies(res.messages)["j"]
	require.NoError(t, json.Unmarshal([]byte(reply), &co), reply)
	assert.Equal(t, "running", co.Phase)
	assert.Contains(t, co.Reason, "do not poll")
	var pendings []agentrun.Entry
	for _, e := range entriesOf(w, agentrun.KindOutcome) {
		if e.Form == agentrun.FormPending {
			pendings = append(pendings, e)
		}
	}
	require.Len(t, pendings, 1, "the outcome is in form pending")
	assert.Empty(t, entriesOf(w, agentrun.KindCompleted), "nothing settled yet")
	running, _ := coord.pendingNow()
	assert.Equal(t, 1, running)
	require.Len(t, agentrun.Fold(w.Entries()).Unsettled, 1, "a turn entry does not settle work meant to outlive it")

	host.notes[100].jobDone.Store(true)
	require.Eventually(t, func() bool { _, waiting := coord.pendingNow(); return waiting == 1 }, 5*time.Second, 5*time.Millisecond)
	completed := entriesOf(w, agentrun.KindCompleted)
	require.Len(t, completed, 1)
	assert.Equal(t, pendings[0].Key, completed[0].Key)
	assert.Equal(t, "completed", completed[0].Form)
	assert.Contains(t, completed[0].Body, "the job's result")
	assert.Empty(t, agentrun.Fold(w.Entries()).Unsettled)

	second := newConversation().request("what happened?")
	second.Turn, second.Conversation = "turn-2", req.Conversation
	res2, err := runTurn(ctx, cli, coord, second, nil)
	require.NoError(t, err)
	assert.Equal(t, "it finished", res2.final.Content)
	model.mu.Lock()
	last := model.seen[len(model.seen)-1]
	model.mu.Unlock()
	var told int
	for _, m := range last.Messages {
		if m.Role == openaichat.ChatRoleSystem && strings.Contains(m.Content, "Work you started earlier has settled") {
			told++
			assert.Contains(t, m.Content, "the job's result")
		}
	}
	assert.Equal(t, 1, told, "the model is told once")
	_, waiting := coord.pendingNow()
	assert.Zero(t, waiting)
	notes := entriesOf(w, agentrun.KindNote)
	require.NotEmpty(t, notes)
	for _, n := range notes {
		if n.Name == completionNoteName {
			assert.Empty(t, n.Turn, "a failed turn's drop leaves it standing")
			assert.Equal(t, pendings[0].Key, n.Key)
		}
	}
}

// BOXER_CHAT_CALL_WAIT: a call that settles within the wait answers directly,
// with its result, and nothing is left pending.
func TestACallThatSettlesInTheWaitAnswersDirectly(t *testing.T) {
	p := defaultProfile()
	p.CallWait = 5 * time.Second
	setProfileForTest(t, p)
	model := asyncScript()
	bus := inprocbus.NewInst(zerolog.Nop())
	host, coord, cli, req, ctx := coordRigWith(t, bus, model, false, asyncNoteOps, agentrun.NewMemStore())
	go func() {
		for {
			host.mu.Lock()
			n := host.notes[100]
			host.mu.Unlock()
			if n != nil {
				n.jobDone.Store(true)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	res, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	var co callOutcome
	reply := toolReplies(res.messages)["j"]
	require.NoError(t, json.Unmarshal([]byte(reply), &co), reply)
	assert.Equal(t, "completed", co.Phase)
	assert.Contains(t, string(co.Result), "the job's result")
	running, waiting := coord.pendingNow()
	assert.Zero(t, running+waiting)
}

// The wait binds: a poll of the host does not outlast it.
func TestTheCallWaitBinds(t *testing.T) {
	shortWait(t)
	bus := inprocbus.NewInst(zerolog.Nop())
	_, coord, cli, req, ctx := coordRigWith(t, bus, asyncScript(), false, asyncNoteOps, agentrun.NewMemStore())
	started := time.Now()
	_, err := runTurn(ctx, cli, coord, req, nil)
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 3*time.Second, "well under one poll of the host's %s", "5s")
}

func TestTheCallLimitsAreTheProfiles(t *testing.T) {
	p := defaultProfile()
	assert.Equal(t, 10*time.Second, p.CallWait)
	assert.Equal(t, time.Hour, p.PendingMax)
	assert.Equal(t, 4, p.WakesMax)
	require.NoError(t, p.Validate())
	before := p.Fingerprint()
	p.CallWait = time.Minute
	assert.NotEqual(t, before, p.Fingerprint(), "a changed limit changes the fingerprint")
	p.CallWait = 0
	assert.Error(t, p.Validate())
	CallWaitEnv.SetForTest(t, "3s")
	assert.Equal(t, 3*time.Second, defaultProfile().CallWait, "the variable sets it")
}

// asyncApp is a window over the rig, so its frames can start turns.
func asyncApp(t *testing.T, model *scriptedModel, change ...func(p *ProfileT)) (inst *App, host *noteHost) {
	t.Helper()
	shortWait(t, change...)
	bus := inprocbus.NewInst(zerolog.Nop())
	host, coord, cli, _, _ := coordRigWith(t, bus, model, false, asyncNoteOps, agentrun.NewMemStore())
	inst = newApp()
	inst.cli, inst.coord = cli, coord
	inst.apps = true
	coord.conversation = inst.conv.id
	return
}

// startJob runs the turn that starts the job and waits for its answer.
func startJob(t *testing.T, inst *App) {
	t.Helper()
	require.True(t, inst.startTurn("run the job"))
	drainUntil(t, inst)
	// The title call takes the script's next reply; settled, it cannot take
	// the one a later turn expects.
	settleTitle(t, inst)
	require.Eventually(t, func() bool { r, _ := inst.coord.pendingNow(); return r == 1 }, 5*time.Second, 5*time.Millisecond)
}

// A completion with no turn running starts one, with no message of the
// person's: the transcript says why, and it cannot be edited as theirs
// (ADR-0358 §SD5).
func TestACompletionWakesATurn(t *testing.T) {
	model := asyncScript(openaichat.CompletionResponse{Content: "a title", FinishReason: "stop"}, openaichat.CompletionResponse{Content: "it finished", FinishReason: "stop"})
	inst, host := asyncApp(t, model)
	startJob(t, inst)
	host.notes[100].jobDone.Store(true)
	require.Eventually(t, func() bool {
		inst.drain()
		for _, e := range inst.conv.entries {
			if e.woken {
				return inst.pending == nil
			}
		}
		return false
	}, 5*time.Second, 5*time.Millisecond)
	settleTitle(t, inst)

	var user, woken, answered int
	for _, e := range inst.conv.entries {
		switch {
		case e.woken:
			woken++
			assert.Contains(t, e.text, "work finished in the background")
		case e.speaker == speakerUser:
			user++
		case e.speaker == speakerModel && e.text == "it finished":
			answered++
		}
	}
	assert.Equal(t, 1, user, "nobody said anything new")
	assert.Equal(t, 1, woken)
	assert.Equal(t, 1, answered)
	assert.False(t, inst.conv.canRewind(), "there is no message of the person's to take back")
	for _, e := range entriesOf(inst.coord.journalNow(), agentrun.KindUser) {
		assert.Equal(t, "run the job", e.Body, "the journal attributes nothing to the person that they did not say")
	}
	m, _ := transcriptModel(inst.conv, -1, false, 0)
	var shown bool
	for i, b := range m.Body {
		if strings.Contains(b, "work finished in the background") {
			shown = true
			assert.Equal(t, int32(-1), m.Sender[i], "the host's line, not the person's")
		}
	}
	assert.True(t, shown)
}

// Each condition that withholds a wake leaves the completion for the person's
// next message (ADR-0358 §SD5).
func TestWakesAreWithheld(t *testing.T) {
	due := func(c *coordinator) bool { return c.wakeDue() }
	waiting := func(c *coordinator) {
		c.inbox = []completion{{key: "k", body: "b"}}
		c.grant.Handle = "h"
	}
	t.Run("due", func(t *testing.T) {
		c := newCoordinator(nil, nil, "c")
		waiting(c)
		assert.True(t, due(c))
	})
	t.Run("nothing waits", func(t *testing.T) {
		c := newCoordinator(nil, nil, "c")
		c.grant.Handle = "h"
		assert.False(t, due(c))
	})
	t.Run("the task ended", func(t *testing.T) {
		c := newCoordinator(nil, nil, "c")
		waiting(c)
		c.grant.Handle = ""
		assert.False(t, due(c))
	})
	t.Run("the person pressed Stop, until they speak", func(t *testing.T) {
		c := newCoordinator(nil, nil, "c")
		waiting(c)
		c.holdWakes()
		assert.False(t, due(c))
		c.personSpoke()
		assert.True(t, due(c))
	})
	t.Run("past the limit, until they speak", func(t *testing.T) {
		WakesMaxEnv.SetForTest(t, "2")
		c := newCoordinator(nil, nil, "c")
		waiting(c)
		c.noteWake()
		assert.True(t, due(c))
		c.noteWake()
		assert.False(t, due(c))
		c.personSpoke()
		assert.True(t, due(c))
	})
	t.Run("zero means never", func(t *testing.T) {
		WakesMaxEnv.SetForTest(t, "0")
		c := newCoordinator(nil, nil, "c")
		waiting(c)
		assert.False(t, due(c))
	})
}

// The person editing a message they took back is not interrupted: the turn
// they send next carries the completion.
func TestNoWakeWhileThePersonEdits(t *testing.T) {
	inst, host := asyncApp(t, asyncScript())
	startJob(t, inst)
	inst.editing = &rewound{}
	host.notes[100].jobDone.Store(true)
	require.Eventually(t, func() bool { _, w := inst.coord.pendingNow(); return w == 1 }, 5*time.Second, 5*time.Millisecond)
	for range 5 {
		inst.drain()
	}
	assert.Nil(t, inst.pending)
	inst.editing = nil
	inst.drain()
	assert.NotNil(t, inst.pending, "the wake was only held")
	drainUntil(t, inst)
}

// A withheld completion reaches the model with the person's next message.
func TestAWithheldCompletionWaitsForThePerson(t *testing.T) {
	model := asyncScript(openaichat.CompletionResponse{Content: "noted", FinishReason: "stop"})
	inst, host := asyncApp(t, model, func(p *ProfileT) { p.WakesMax = 0 })
	startJob(t, inst)
	host.notes[100].jobDone.Store(true)
	require.Eventually(t, func() bool { _, w := inst.coord.pendingNow(); return w == 1 }, 5*time.Second, 5*time.Millisecond)
	for range 5 {
		inst.drain()
	}
	assert.Nil(t, inst.pending, "no turn started")
	model.mu.Lock()
	before := len(model.seen)
	model.mu.Unlock()
	require.True(t, inst.startTurn("how did it go?"))
	drainUntil(t, inst)
	model.mu.Lock()
	defer model.mu.Unlock()
	require.Greater(t, len(model.seen), before)
	var told bool
	for _, m := range model.seen[len(model.seen)-1].Messages {
		told = told || strings.Contains(m.Content, "Work you started earlier has settled")
	}
	assert.True(t, told)
}

// A conversation that ends while work runs records that its end was not seen,
// and the run still closes (ADR-0358 §SD6).
func TestRetiringEndsWatchersAsUnknown(t *testing.T) {
	inst, _ := asyncApp(t, asyncScript())
	startJob(t, inst)
	w := inst.coord.journalNow()
	inst.coord.retire()
	require.Eventually(t, func() bool { return len(entriesOf(w, agentrun.KindCompleted)) == 1 }, 2*time.Second, 5*time.Millisecond,
		"well under the host's five-second poll: a watcher does not hold a drain up")
	e := entriesOf(w, agentrun.KindCompleted)[0]
	assert.Equal(t, completedFormUnknown, e.Form)
	assert.Contains(t, e.Body, "conversation ended")
	_, waiting := inst.coord.pendingNow()
	assert.Zero(t, waiting, "nobody is left to tell")
}

// A resume owes the model what the record leaves open: a call still pending
// ends as unknown and is told, and so is a completion never told. What was
// told already is not told again (ADR-0358 §SD6).
func TestAResumeSettlesPendingWork(t *testing.T) {
	ctx := context.Background()
	store := agentrun.NewMemStore()
	w, err := agentrun.Open(ctx, store, agentrun.Run{Conversation: "c", OwnerApp: string(ManifestId), Coordinator: "w"})
	require.NoError(t, err)
	_, err = w.Append(ctx,
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "gone", Name: "run", Effect: agentrun.EffectRun, Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "gone", Role: "tool", Name: "x", Form: agentrun.FormPending, Body: "running", Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "untold", Name: "run", Effect: agentrun.EffectRun, Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "untold", Role: "tool", Name: "y", Form: agentrun.FormPending, Body: "running", Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindCompleted, Key: "untold", Form: "completed", Body: "its result", Label: agentrun.Label{Tier: agentrun.TierE(2)}},
		agentrun.Entry{Kind: agentrun.KindIntent, Key: "told", Name: "run", Effect: agentrun.EffectRun, Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindOutcome, Key: "told", Role: "tool", Name: "z", Form: agentrun.FormPending, Body: "running", Turn: "t1"},
		agentrun.Entry{Kind: agentrun.KindCompleted, Key: "told", Form: "completed", Body: "known"},
		agentrun.Entry{Kind: agentrun.KindNote, Key: "told", Name: completionNoteName, Body: "known"},
		agentrun.Entry{Kind: agentrun.KindTurn, Turn: "t1"},
	)
	require.NoError(t, err)
	assert.Len(t, agentrun.Fold(w.Entries()).Unsettled, 1, "only the call with no end recorded")

	lines, err := settlePending(ctx, w)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "gone")
	assert.Empty(t, agentrun.Fold(w.Entries()).Unsettled)
	bodies := map[string]agentrun.Entry{}
	counts := map[string]int{}
	for _, n := range entriesOf(w, agentrun.KindNote) {
		bodies[n.Key] = n
		counts[n.Key]++
	}
	assert.Contains(t, bodies["gone"].Body, "its end was not seen")
	assert.Contains(t, bodies["untold"].Body, "its result")
	assert.Equal(t, agentrun.TierE(2), bodies["untold"].Label.Tier, "the note keeps the content's label")
	assert.Equal(t, 1, counts["told"], "told once")
	again, err := settlePending(ctx, w)
	require.NoError(t, err)
	assert.Empty(t, again, "settling is idempotent")
}

// The status line says a result waits for the person only when no turn will
// read it first.
func TestTheBackgroundLineSaysWhoReadsAResult(t *testing.T) {
	assert.Equal(t, "1 operation running in the background", backgroundLine(1, 0, false))
	assert.Equal(t, "1 result waiting for your next message", backgroundLine(0, 1, false))
	assert.Equal(t, "2 results on their way to the model, 1 operation running in the background", backgroundLine(1, 2, true))
	assert.Equal(t, "1 result on its way to the model", backgroundLine(0, 1, true))
}
