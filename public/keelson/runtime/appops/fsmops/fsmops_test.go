package fsmops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

type fakeMachine struct {
	current string
	asked   int
}

func (inst *fakeMachine) OpsCurrent() string { return inst.current }
func (inst *fakeMachine) OpsView(history int) opfsm.View {
	inst.asked = history
	return opfsm.View{Current: inst.current, States: []string{"idle", "running", "done"},
		Edges:   []opfsm.Edge{{From: "idle", To: "running", Label: "Run"}, {From: "running", To: "done"}, {From: "done", To: "running", Label: "Run"}},
		History: []opfsm.Step{{From: "idle", To: "running"}}}
}

type host struct{ m *fakeMachine }

func newSet() *appops.Set[*host, struct{}] {
	s := appops.NewSet(func(*host) struct{} { return struct{}{} })
	Mount(s, "work", "the work", func(h *host) opfsm.SourceI {
		if h.m == nil {
			return nil
		}
		return h.m
	}, Options{History: 4})
	return s
}

func query[T any](t *testing.T, s *appops.Set[*host, struct{}], h *host, op string) (out T) {
	t.Helper()
	raw, err := s.Bind(h).Snapshot().Query(op, nil)
	require.NoError(t, err)
	out, err = buscodec.Decode[T](raw)
	require.NoError(t, err)
	return
}

func TestAMountedMachineOffersItsStateAndItsGraph(t *testing.T) {
	s := newSet()
	require.NoError(t, s.Catalog().Validate())
	assert.Equal(t, []string{"work_state", "work_machine"}, s.Names())
	h := &host{m: &fakeMachine{current: "idle"}}
	assert.Equal(t, "idle", s.Bind(h).ResourceValue("work_state"), "the current state is a resource, so a move is a change")

	st := query[opfsm.State](t, s, h, "work_state")
	assert.Equal(t, "idle", st.Current)
	assert.Equal(t, []opfsm.Edge{{From: "idle", To: "running", Label: "Run"}}, st.Next, "only the transitions allowed from the current state")
	assert.Len(t, st.History, 1)
	assert.Equal(t, 4, h.m.asked, "the history bound reaches the machine")

	m := query[opfsm.Machine](t, s, h, "work_machine")
	assert.Len(t, m.States, 3)
	assert.Len(t, m.Edges, 3)
}

func TestAnInstanceWithoutAMachineReportsNoState(t *testing.T) {
	s := newSet()
	h := &host{}
	assert.Equal(t, "", s.Bind(h).ResourceValue("work_state"))
	st := query[opfsm.State](t, s, h, "work_state")
	assert.Empty(t, st.Current)
	assert.Empty(t, st.Next)
}

// Two apps mounting a machine offer operations of the same shape.
func TestEveryMountOffersTheSameOperations(t *testing.T) {
	a := newSet()
	b := appops.NewSet(func(*host) int { return 0 })
	Mount(b, "job", "a job", func(h *host) opfsm.SourceI { return nil }, Options{})
	ca, cb := a.Catalog(), b.Catalog()
	for i := range ca.Operations {
		assert.Equal(t, ca.Operations[i].Args, cb.Operations[i].Args)
		assert.Equal(t, ca.Operations[i].Result, cb.Operations[i].Result)
	}
}
