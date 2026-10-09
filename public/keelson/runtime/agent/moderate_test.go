package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

const moderatorApp app.AppIdT = "test.moderator"

func (inst *rig) moderator(listed bool) (cli *Client) {
	if listed {
		inst.svc.SetModerators([]string{string(moderatorApp)})
	}
	return NewClient(inst.bus.NewClient(moderatorApp, ModeratorCaps("test: moderate")))
}

func (inst *rig) revoked(g Grant) (why string) {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()
	return inst.svc.tasks[g.Handle].revoked
}

// A listed moderator stops a task by its id, and the record says who; an
// app that is not listed is refused.
func TestModeratorStopsATask(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g := r.grant(ModeAct)

	_, err := r.moderator(false).ModerateStop(ctx, ModerateTarget{Task: g.Task}, "runaway")
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "BOXER_LLM_MODERATORS")
	assert.Empty(t, r.revoked(g))

	tasks, err := r.moderator(true).ModerateStop(ctx, ModerateTarget{Task: g.Task}, "runaway")
	require.NoError(t, err)
	assert.Equal(t, []string{g.Task}, tasks)
	assert.Contains(t, r.revoked(g), "a moderator (test.moderator): runaway")

	tasks, err = r.moderator(true).ModerateStop(ctx, ModerateTarget{Task: g.Task}, "again")
	require.NoError(t, err)
	assert.Empty(t, tasks, "an ended task is not stopped twice")
}

// A moderator names a coordinator's window and reaches every live task it
// drives: the account it sees the loop's model calls charged to.
func TestModeratorStopsByCoordinatorWindow(t *testing.T) {
	r := newRig(t, true)
	bc := r.bus.NewClient("test.coordinator", ClientCaps("test: drive apps"))
	bc.SetInstanceKey(3)
	r.cli = NewClient(bc)
	g := r.grant(ModeAct)
	tasks, err := r.moderator(true).ModerateStop(context.Background(), ModerateTarget{Instance: 3}, "")
	require.NoError(t, err)
	assert.Equal(t, []string{g.Task}, tasks)
	assert.NotEmpty(t, r.revoked(g))
}

// A moderator lowers a ceiling and cannot raise one: what it names above
// the ceiling in force is left as it is.
func TestModeratorLowersNeverRaises(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	act := Ceiling{Mode: ModeAct, Effect: app.OperationEffectDocument}
	g, err := r.cli.Request(ctx, GrantRequest{Plan: "edit the doc", Ceiling: &act,
		Entries: []GrantEntry{{Instance: r.docKey, Mode: ModeAct}}})
	require.NoError(t, err)
	mod := r.moderator(true)

	_, err = mod.ModerateCeiling(ctx, ModerateTarget{Task: g.Task}, Ceiling{Mode: ModeObserve}, "watch only")
	require.NoError(t, err)
	a, err := r.cli.Authority(ctx, g.Handle, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeObserve, a.Ceiling.Mode)

	_, err = mod.ModerateCeiling(ctx, ModerateTarget{Task: g.Task}, Unlimited(), "")
	require.NoError(t, err)
	a, err = r.cli.Authority(ctx, g.Handle, nil)
	require.NoError(t, err)
	assert.Equal(t, ModeObserve, a.Ceiling.Mode, "a moderator cannot raise it back")
	assert.False(t, a.Ceiling.Unpaced)
}

// A moderator's question waits for the person's answer; one nobody
// answers expires undecided.
func TestAskPersonWaitsForTheAnswer(t *testing.T) {
	r := newRig(t, true)
	type answer struct{ granted, decided bool }
	got := make(chan answer, 1)
	go func() {
		granted, decided, err := r.svc.AskPerson(context.Background(), moderatorApp, "allow 100 more calls?", "budget rule")
		assert.NoError(t, err)
		got <- answer{granted, decided}
	}()
	var q *question
	require.Eventually(t, func() bool {
		r.svc.moderation.mu.Lock()
		defer r.svc.moderation.mu.Unlock()
		if len(r.svc.moderation.questions) == 1 {
			q = r.svc.moderation.questions[0]
		}
		return q != nil
	}, time.Second, time.Millisecond)
	assert.Equal(t, "allow 100 more calls?", q.text)
	r.svc.answer(q, true)
	assert.Equal(t, answer{granted: true, decided: true}, <-got)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	granted, decided, err := r.svc.AskPerson(ctx, moderatorApp, "anyone?", "rule")
	require.NoError(t, err)
	assert.False(t, granted || decided)
	r.svc.moderation.mu.Lock()
	assert.Empty(t, r.svc.moderation.questions, "an expired question leaves the queue")
	r.svc.moderation.mu.Unlock()
}

// Each action record is published as it is recorded, without the model's
// words, for a moderator to follow (ADR-0302 §SD2).
func TestActionRecordsArePublished(t *testing.T) {
	r := newRig(t, true)
	bc := r.bus.NewClient(moderatorApp, ModeratorCaps("test: follow actions"))
	got := make(chan ActionRecord, 16)
	unsub, err := bc.Subscribe(SubjectActionRecorded, func(m *app.Msg) {
		rec, derr := DecodeActionEvent(m.Payload)
		assert.NoError(t, derr)
		got <- rec
	})
	require.NoError(t, err)
	t.Cleanup(unsub)
	g := r.grant(ModeAct)
	// A query completes as it is dispatched.
	require.Equal(t, "completed", r.call(g, "q1", "get_text", "{}").Phase)
	var final *ActionRecord
	deadline := time.After(2 * time.Second)
	for final == nil {
		select {
		case rec := <-got:
			if p, ok := opwire.ParsePhase(rec.Phase); ok && p.Result() == opwire.ResultDone {
				final = &rec
			}
		case <-deadline:
			t.Fatal("no action event for the done call")
		}
	}
	assert.Equal(t, g.Task, final.Task)
	assert.Equal(t, "get_text", final.Operation)
	assert.Equal(t, r.docKey, final.Instance)
	assert.NotEmpty(t, final.ArgsDigest)
	assert.Equal(t, "q1", final.Key)
	assert.Empty(t, final.CallTitle, "the model's words stay off the event")
}
