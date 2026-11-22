package sessiondata

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// secondStub is another endpoint for r's service.
func (inst *rig) secondStub() (e Endpoint, stub *stubServer) {
	stub = newStub(inst.t)
	return Endpoint{URL: stub.url(), User: "u", Password: "p"}, stub
}

// waitClosed waits until s is marked closed.
func waitClosed(t *testing.T, s *session) {
	t.Helper()
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.closed
	}, 5*time.Second, time.Millisecond)
}

// TestRedefinitionOnAnotherEndpointIsRefused: a data set's table and the
// tables of what reads it live in one server session; a redefinition
// naming another endpoint would leave them there, uncounted.
func TestRedefinitionOnAnotherEndpointIsRefused(t *testing.T) {
	r := newRig(t, Limits{})
	ticket, err := r.svc.Open(context.Background(), person)
	require.NoError(t, err)
	me := Caller{By: person}
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: me, Ticket: ticket, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: me, Ticket: ticket, Endpoint: r.endpoint(), Name: "b", SQL: "SELECT x FROM session('a')"})
	require.NoError(t, err)
	e2, stub2 := r.secondStub()
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: me, Ticket: ticket, Endpoint: e2, Name: "a", SQL: "SELECT 2 AS x"})
	assert.ErrorContains(t, err, "drop it before defining it there")
	assert.Empty(t, stub2.creates())
	assert.Empty(t, r.stub.drops(), "the data set and what reads it stay where they are")
	e3 := r.endpoint()
	e3.Password = "other"
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: me, Ticket: ticket, Endpoint: e3, Name: "a", SQL: "SELECT 2 AS x"})
	assert.ErrorContains(t, err, "another identity", "another identity on the same endpoint is another server session")
}

// TestConcurrentDefinesOnTwoEndpointsHoldTheBounds: definitions on two
// server sessions of one data session run at once; the count and the rows
// are checked again when each is recorded, and the one past a bound is
// refused and its table dropped.
func TestConcurrentDefinesOnTwoEndpointsHoldTheBounds(t *testing.T) {
	for _, limits := range []Limits{{MaxDatasets: 1}, {MaxRows: 15}} {
		r := newRig(t, limits)
		e2, stub2 := r.secondStub()
		r.stub.delay, stub2.delay = 50*time.Millisecond, 50*time.Millisecond
		ticket, err := r.svc.Open(context.Background(), person)
		require.NoError(t, err)
		me := Caller{By: person}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, e := range []Endpoint{r.endpoint(), e2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = r.svc.Define(context.Background(), DefineInput{Caller: me, Ticket: ticket, Endpoint: e, Name: []string{"a", "b"}[i], SQL: "SELECT 1 AS x"})
			}()
		}
		wg.Wait()
		failures := 0
		for _, err := range errs {
			if err != nil {
				failures++
			}
		}
		assert.Equal(t, 1, failures, "%+v: one of the two is past the bound", limits)
		infos, _, err := r.svc.List(context.Background(), me, ticket)
		require.NoError(t, err)
		assert.Len(t, infos, 1)
		live := 0
		for _, st := range []*stubServer{r.stub, stub2} {
			for _, id := range st.sessionIds() {
				live += len(st.tablesOf(id))
			}
		}
		assert.Equal(t, 1, live, "%+v: the refused one's table is dropped", limits)
	}
}

// TestADefineThatMeetsAClosedSessionLeavesNoTable: the data session ends
// while a definition's create runs; the definition is refused and its table
// dropped rather than recorded in a session nobody will close again.
func TestADefineThatMeetsAClosedSessionLeavesNoTable(t *testing.T) {
	r := newRig(t, Limits{})
	ticket, err := r.svc.Open(context.Background(), person)
	require.NoError(t, err)
	s := r.svc.lookupTicket(ticket)
	var once sync.Once
	r.stub.afterCreate = func() {
		once.Do(func() {
			go func() { _ = r.svc.CloseSession(context.Background(), person, ticket) }()
			waitClosed(t, s)
		})
	}
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: Caller{By: person}, Ticket: ticket, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	assert.ErrorContains(t, err, "ended")
	assert.Contains(t, r.stub.drops(), "__sd_a_r1")
}

// TestAPrepareThatMeetsAClosedSessionHoldsNothing: the data session ends
// while a statement is prepared; no binding and no lease come back.
func TestAPrepareThatMeetsAClosedSessionHoldsNothing(t *testing.T) {
	r := newRig(t, Limits{})
	ticket, err := r.svc.Open(context.Background(), person)
	require.NoError(t, err)
	me := Caller{By: person}
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: me, Ticket: ticket, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	s := r.svc.lookupTicket(ticket)
	var once sync.Once
	r.stub.afterProbe = func() {
		once.Do(func() {
			go func() { _ = r.svc.CloseSession(context.Background(), person, ticket) }()
			waitClosed(t, s)
		})
	}
	_, err = r.svc.Prepare(context.Background(), PrepareInput{Caller: me, Ticket: ticket, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	assert.ErrorContains(t, err, "ended")
	assert.Empty(t, r.svc.leasesWhere(func(*lease) bool { return true }))
}

// TestATableACreateLeftBehindDoesNotBlockItsName: the server made a table
// whose create the capability saw fail; the next definition of the name
// drops it rather than failing on it, and a probe drops one no data set
// owns.
func TestATableACreateLeftBehindDoesNotBlockItsName(t *testing.T) {
	r := newRig(t, Limits{})
	c := r.fullAgent("t1")
	r.stub.madeButFailed = 1
	_, err := r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	require.Error(t, err)
	res := r.define(c, "a", "SELECT 1 AS x")
	assert.Equal(t, DatasetStateLive, res.Dataset.State)

	r.stub.madeButFailed = 1
	_, err = r.svc.Define(context.Background(), DefineInput{Caller: c, Endpoint: r.endpoint(), Name: "gone", SQL: "SELECT 1 AS x"})
	require.Error(t, err)
	b, err := r.svc.Prepare(context.Background(), PrepareInput{Caller: c, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)
	require.NoError(t, r.svc.Release(person, b.Lease))
	assert.ElementsMatch(t, []string{"__sd_a_r1"}, r.stub.tablesOf(b.Settings["session_id"]), "the table no data set owns is gone")
}

// TestAClosedWindowsLeaseIsReleased: a window that closes with a binding
// out never releases it; the next statement does not wait for the lease to
// lapse, and the host stopping leaves no lease behind.
func TestAClosedWindowsLeaseIsReleased(t *testing.T) {
	r := newRig(t, Limits{})
	ctx := context.Background()
	ticket, err := r.svc.Open(ctx, person)
	require.NoError(t, err)
	require.NoError(t, r.svc.Attach(ctx, person2, ticket))
	_, err = r.svc.Define(ctx, DefineInput{Caller: Caller{By: person}, Ticket: ticket, Endpoint: r.endpoint(), Name: "a", SQL: "SELECT 1 AS x"})
	require.NoError(t, err)
	_, err = r.svc.Prepare(ctx, PrepareInput{Caller: Caller{By: person}, Ticket: ticket, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err)
	r.svc.instanceClosed(ctx, person)

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = r.svc.Prepare(waitCtx, PrepareInput{Caller: Caller{By: person2}, Ticket: ticket, Endpoint: r.endpoint(), SQL: "SELECT * FROM session('a')"})
	require.NoError(t, err, "the closed window's lease does not hold the session")
	require.NoError(t, r.svc.Close(ctx))
	assert.Empty(t, r.svc.leasesWhere(func(*lease) bool { return true }), "the host stopping releases what is out")
}
