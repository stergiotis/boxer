package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

type captureMinter struct {
	mu   sync.Mutex
	recs []applink.Record
}

func (inst *captureMinter) MintTask(rec applink.Record) (link string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.recs = append(inst.recs, rec)
	return "boxer:l/minted", nil
}

func (inst *captureMinter) last() applink.Record {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.recs[len(inst.recs)-1]
}

func linkRig(t *testing.T) (*rig, *captureMinter) {
	r := newRig(t, true)
	m := &captureMinter{}
	r.svc.SetLinks(m)
	return r, m
}

func (inst *rig) makeLink(g Grant, key string, op string, args string) (string, error) {
	return inst.cli.MakeLink(context.Background(), MakeLinkRequest{CallRequest: CallRequest{Handle: g.Handle, Key: key, Operation: op, Args: args},
		Target: LinkTargetWindow, Window: inst.docKey})
}

// followFramed follows a click while the window keeps drawing frames, as
// the window host would.
func (inst *rig) followFramed(c LinkClick) LinkFollowed {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				inst.host.frame(inst.docKey)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()
	out := inst.svc.FollowLink(context.Background(), c)
	close(stop)
	<-done
	return out
}

func TestMakeLink_ChecksTheCallWithoutRunningIt(t *testing.T) {
	r, m := linkRig(t)
	g := r.grant(ModeAct)
	require.Equal(t, "completed", r.call(g, "k1", "get_text", "{}").Phase)
	used := r.svc.tasks[g.Handle].callsUsed

	link, err := r.makeLink(g, "k2", "set_text", `{"text":"linked"}`)
	require.NoError(t, err)
	assert.Equal(t, "boxer:l/minted", link)
	rec := m.last()
	assert.Equal(t, g.Task, rec.Task)
	assert.Equal(t, uint64(7), rec.Window)
	assert.Equal(t, "set_text", rec.Operation)
	assert.Equal(t, applink.LevelOwnModel, rec.AuthorLevel)
	assert.Contains(t, rec.Expects, "text", "the revision the task read")
	assert.Equal(t, "start", r.host.docs[7].text, "minting runs nothing")
	assert.Equal(t, used, r.svc.tasks[g.Handle].callsUsed, "minting spends no call")
}

func TestMakeLink_RefusesWhatTheGrantDoesNotRun(t *testing.T) {
	r, _ := linkRig(t)
	g := r.grant(ModeObserve)
	_, err := r.makeLink(g, "k1", "set_text", `{"text":"x"}`)
	assert.Error(t, err, "observe mode does not run a document command")

	g = r.grant(ModeAct)
	_, err = r.makeLink(g, "k2", "export", `{}`)
	assert.Error(t, err, "a consequential command")
	_, err = r.makeLink(g, "k3", "get_text", `{}`)
	assert.Error(t, err, "a query")
	_, err = r.makeLink(g, "k4", "set_text", `{"nope":1}`)
	assert.Error(t, err, "arguments outside the schema")
	_, err = r.makeLink(g, "k5", "wipe", `{}`)
	assert.Error(t, err, "an operation not exposed to agents")
}

func TestFollowLink_RunsUnderTheGrant(t *testing.T) {
	r, m := linkRig(t)
	g := r.grant(ModeAct)
	require.Equal(t, "completed", r.call(g, "k1", "get_text", "{}").Phase)
	_, err := r.makeLink(g, "k2", "set_text", `{"text":"linked"}`)
	require.NoError(t, err)

	out := r.followFramed(LinkClick{Record: m.last(), Surface: "test.surface", Click: 1})
	require.True(t, out.Authorised, out.Reason)
	assert.Equal(t, uint64(7), out.Window)
	assert.True(t, out.Outcome.Phase == opwire.PhaseApplied || out.Outcome.Phase == opwire.PhaseRendered, out.Outcome.Phase.String())
	assert.Equal(t, "linked", r.host.docs[7].text)

	// The first click moved the text past the revision the link recorded.
	out = r.followFramed(LinkClick{Record: m.last(), Surface: "test.surface", Click: 2})
	require.True(t, out.Authorised, out.Reason)
	assert.Equal(t, opwire.PhaseConflict, out.Outcome.Phase, "the window changed since the link was made")
}

func TestFollowLink_NotAuthorisedOnceTheGrantStopsCovering(t *testing.T) {
	r, m := linkRig(t)
	g := r.grant(ModeAct)
	require.Equal(t, "completed", r.call(g, "k1", "get_text", "{}").Phase)
	_, err := r.makeLink(g, "k2", "set_text", `{"text":"linked"}`)
	require.NoError(t, err)
	rec := m.last()

	r.svc.mu.Lock()
	r.svc.tasks[g.Handle].entries[7].mode = ModeObserve
	r.svc.mu.Unlock()
	out := r.svc.FollowLink(context.Background(), LinkClick{Record: rec, Click: 1})
	assert.False(t, out.Authorised, "observe mode")
	assert.Equal(t, "start", r.host.docs[7].text)

	require.NoError(t, r.cli.Stop(context.Background(), g.Handle))
	out = r.svc.FollowLink(context.Background(), LinkClick{Record: rec, Click: 2})
	assert.False(t, out.Authorised)
	assert.Contains(t, out.Reason, "ended")
}
