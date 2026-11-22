package play

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/launchrequest"
	"github.com/stergiotis/boxer/public/keelson/runtime/sessiondata"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// runAll runs sql through ExecuteArrowStream and drains it, as a lane does.
func runAll(t *testing.T, ctx context.Context, c *Client, sql string, opts *ExecOptions) (rows int64, confined bool, err error) {
	t.Helper()
	var agent *app.OnBehalfOf
	if opts != nil {
		agent = opts.Agent
	}
	dec := c.dispatchFor(agent, sql, "")
	rdr, rs, _, err := c.ExecuteArrowStream(ctx, sql, memory.NewGoAllocator(), opts, nil, dec)
	if err != nil {
		return
	}
	defer func() {
		rdr.Release()
		_ = rs.Close()
	}()
	batches, _, err := drainRun(rdr, rs)
	for _, b := range batches {
		rows += b.NumRows()
		b.Release()
	}
	return rows, rs.Confined(), err
}

// quickly bounds a run that would wait on a lease left held.
func quickly(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestDispatchPlacesTheFourKindsOfStatement(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	c.SetResolver(keelsonResolver{localEndpoint: func() string { return "http://127.0.0.1:1/query" }})

	dec := c.Dispatch("SELECT * FROM keelson('items')", "")
	assert.Equal(t, dispatchClassIntrospection, dec.class, "keelson tables alone stay on the plane")

	dec = c.Dispatch("SELECT * FROM db.message", "")
	assert.Equal(t, dispatchClassManual, dec.class)

	dec = c.Dispatch("SELECT * FROM session('msgs') JOIN db.message USING id", "")
	assert.Equal(t, dispatchClassSession, dec.class)
	assert.Equal(t, r.stub.url(), dec.targetURL)

	dec = c.Dispatch("SELECT * FROM keelson('items') JOIN db.message USING id", "")
	assert.Equal(t, dispatchClassSession, dec.class, "an ad-hoc dataset joined with a server table runs on the server")

	bare := NewClient(ClientConfig{URL: r.stub.url()}, nil)
	bare.SetResolver(autoResolver)
	dec = bare.Dispatch("SELECT * FROM keelson('items') JOIN db.message USING id", "")
	assert.Equal(t, dispatchClassRefused, dec.class)
	assert.Contains(t, dec.reason, "no endpoint serves")
	assert.Contains(t, dec.reason, "not")
	dec = bare.Dispatch("SELECT * FROM session('msgs')", "")
	assert.Equal(t, dispatchClassRefused, dec.class, "a client without the service cannot read a data set")
	assert.Contains(t, dec.reason, "session data service")
}

func TestARunReadsADataSetInItsSessionAndReleasesItsLease(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	res, err := c.defineSession(t.Context(), nil, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), res.Dataset.Revision)
	assert.NotEmpty(t, c.windowTicket(), "the window's first definition opened its data session")

	for range 3 {
		// A lease left held would make the next run wait out the timeout.
		rows, confined, err := runAll(t, quickly(t), c, "SELECT * FROM session('msgs') WHERE x > 0", nil)
		require.NoError(t, err)
		assert.Equal(t, int64(3), rows)
		assert.False(t, confined)
	}
	run := r.stub.lastRun()
	assert.Contains(t, run.SQL, "`__sd_msgs_r1`")
	assert.NotContains(t, run.SQL, "session(")
	assert.Equal(t, "1", run.Params["session_check"])
	assert.NotEmpty(t, run.Params["session_id"])
	assert.Equal(t, "2", run.Params["readonly"])

	// A failed run releases its lease too.
	r.stub.mu.Lock()
	r.stub.failRuns = true
	r.stub.mu.Unlock()
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", nil)
	require.Error(t, err)
	r.stub.mu.Lock()
	r.stub.failRuns = false
	r.stub.mu.Unlock()
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", nil)
	require.NoError(t, err, "the failed run released its lease")

	// So does a cancelled one.
	hold := make(chan struct{})
	r.stub.mu.Lock()
	r.stub.hold = hold
	r.stub.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, _, err := runAll(t, ctx, c, "SELECT * FROM session('msgs')", nil)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	require.Error(t, <-done)
	r.stub.mu.Lock()
	r.stub.hold = nil
	r.stub.mu.Unlock()
	close(hold)
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", nil)
	require.NoError(t, err, "the cancelled run released its lease")

	// A statement naming a data set the session lacks names what it holds.
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('nope')", nil)
	require.ErrorContains(t, err, "msgs")
}

func TestARunThatMetALostSessionIsSentOnceMore(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	_, err := c.defineSession(t.Context(), nil, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	r.stub.mu.Lock()
	r.stub.loseOnRun = 1
	r.stub.mu.Unlock()
	rows, _, err := runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", nil)
	require.NoError(t, err, "the second prepare replays the data set in a new session")
	assert.Equal(t, int64(3), rows)
	r.stub.mu.Lock()
	n := len(r.stub.runs)
	first, second := r.stub.runs[n-2].Params["session_id"], r.stub.runs[n-1].Params["session_id"]
	r.stub.loseOnRun = 2
	r.stub.mu.Unlock()
	assert.NotEqual(t, first, second, "the retry went to the reopened session")

	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", nil)
	require.Error(t, err, "a run is sent once more, not twice")
	assert.True(t, sessiondata.IsSessionLost(err))
	r.stub.mu.Lock()
	r.stub.loseOnRun = 0
	r.stub.mu.Unlock()
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", nil)
	require.NoError(t, err, "and released its lease")
}

func TestAnAgentsRunIsHeldToTheGrantAndMarkedWithItsTask(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: []string{DestinationClickHouse(r.host)}}
	r.grant("t1", DestinationClickHouse(r.host))

	_, err := c.defineSession(t.Context(), obo, "msgs", "SELECT 1 AS x", nil)
	var limit *AgentLimitError
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, sessiondata.DestinationSession(r.host), limit.Destination, "keeping a data set needs its own destination")

	r.grant("t1", sessiondata.DestinationSession(r.host))
	_, err = c.defineSession(t.Context(), obo, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)

	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", &ExecOptions{Agent: obo})
	require.NoError(t, err, "reading needs only the endpoint")

	ungranted := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c2"}
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs')", &ExecOptions{Agent: ungranted})
	require.ErrorAs(t, err, &limit)
	assert.Equal(t, DestinationClickHouse(r.host), limit.Destination, "a missing endpoint names it, as for any run")

	c.reach.record(r.stub.url())
	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs') JOIN keelson('items') USING x", &ExecOptions{Agent: obo})
	require.ErrorAs(t, err, &limit, "an ad-hoc dataset the grant does not list, refused by the dataset service's rule")
	assert.Equal(t, DestinationKeelson("items"), limit.Destination)
	p0 := &PlayApp{client: c, sql: "SELECT * FROM session('msgs') JOIN keelson('items') USING x"}
	err = p0.refuseAgentRun(obo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DestinationKeelson("items"), "the check at the call names it too")

	// The agent's statement is a read, checked before the service binds it.
	p := &PlayApp{client: c, sql: "SELECT * FROM session('msgs')"}
	require.NoError(t, p.refuseAgentRun(obo))
	p.sql = "INSERT INTO t SELECT * FROM session('msgs')"
	require.Error(t, p.refuseAgentRun(obo))
}

func TestAnAdhocDatasetJoinsAServerTableAsConfinedData(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	c.SetResolver(autoResolver)
	_, err := c.defineSession(t.Context(), nil, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)

	_, _, err = runAll(t, quickly(t), c, "SELECT * FROM session('msgs') JOIN keelson('items') USING x", nil)
	require.ErrorContains(t, err, "sealed", "an endpoint that has not shown it may see sealed data is refused")

	c.reach.record(r.stub.url())
	rows, confined, err := runAll(t, quickly(t), c, "SELECT * FROM session('msgs') JOIN keelson('items') USING x", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(3), rows)
	assert.True(t, confined, "the binding's label reaches the result")
	run := r.stub.lastRun()
	assert.Equal(t, []string{"__adhoc_0"}, run.Files)

	// A data set defined from one is confined, and so is what reads it.
	_, err = c.defineSession(t.Context(), nil, "joined", "SELECT * FROM keelson('items')", nil)
	require.NoError(t, err)
	_, confined, err = runAll(t, quickly(t), c, "SELECT * FROM session('joined')", nil)
	require.NoError(t, err)
	assert.True(t, confined)
}

// TestTwoAppsOfOneTaskShareItsDataSession: a second app — a client with
// another window identity, as a downstream app holds — reads what the first
// defined for the same task, through the on-behalf-of context alone.
func TestTwoAppsOfOneTaskShareItsDataSession(t *testing.T) {
	r := newSdRig(t)
	r.grant("t1", DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host))
	dests := []string{DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)}
	play := r.client("github.com/x/play", 1)
	other := r.client("github.com/x/kernel", 9)

	_, err := play.defineSession(t.Context(), &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: dests}, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c7", Destinations: dests}
	rows, _, err := runAll(t, quickly(t), other, "SELECT * FROM session('msgs')", &ExecOptions{Agent: obo, Label: "kernel-input"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), rows)
	infos, err := other.listSession(t.Context(), obo)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "msgs", infos[0].Name)

	// Another task does not see it.
	r.grant("t2", dests...)
	_, _, err = runAll(t, quickly(t), other, "SELECT * FROM session('msgs')", &ExecOptions{Agent: &app.OnBehalfOf{Task: "t2", Epoch: 1, Call: "c1", Destinations: dests}})
	require.Error(t, err)
}

// TestAHandOverCarriesThePersonsDataSession: the opener's window mints a
// token, the opened window claims it and reads the opener's data sets.
// Neither ticket nor token reaches the log or an error.
func TestAHandOverCarriesThePersonsDataSession(t *testing.T) {
	var logged bytes.Buffer
	prev := log.Logger
	log.Logger = zerolog.New(&logged).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log.Logger = prev })

	r := newSdRig(t)
	opener := r.client("github.com/x/play", 1)
	opened := r.client("github.com/x/play", 2)
	_, err := opener.defineSession(t.Context(), nil, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	ticket := opener.windowTicket()

	_, _, err = runAll(t, quickly(t), opened, "SELECT * FROM session('msgs')", nil)
	require.ErrorContains(t, err, "holds no data session")
	assert.NotContains(t, err.Error(), ticket)

	token, err := opener.handOverToken(t.Context())
	require.NoError(t, err)
	require.True(t, sessiondata.IsHandOver(token))
	require.NoError(t, opened.claimHandOver(t.Context(), token))
	_, _, err = runAll(t, quickly(t), opened, "SELECT * FROM session('msgs')", nil)
	require.NoError(t, err)
	err = opened.claimHandOver(t.Context(), token)
	require.Error(t, err, "a token is claimed once")
	assert.NotContains(t, err.Error(), token)

	assert.NotContains(t, logged.String(), ticket)
	assert.NotContains(t, logged.String(), token)
}

func TestOpenInPlayHandsTheWindowsDataSessionOver(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	_, err := c.defineSession(t.Context(), nil, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	bus := &fakeOpenBus{}
	inst := newOpenTestApp(t, bus)
	inst.client = c
	require.NoError(t, inst.openPlayground(playLaunchForTest("SELECT * FROM session('msgs')")))
	cfg := decodeOpenedLaunch(t, bus)
	require.True(t, cfg.SessionHandOver.Has)
	assert.True(t, sessiondata.IsHandOver(cfg.SessionHandOver.Val))
	assert.NotEqual(t, c.windowTicket(), cfg.SessionHandOver.Val, "the token goes, not the ticket")

	// A window working for a task hands nothing over.
	c.SetAgentMark(&app.OnBehalfOf{Task: "t1", Epoch: 1})
	require.NoError(t, inst.openPlayground(playLaunchForTest("SELECT 1")))
	assert.False(t, decodeOpenedLaunch(t, bus).SessionHandOver.Has)
}

func TestSessionStandInClassifiesTheMacroAsATable(t *testing.T) {
	assert.Equal(t, "SELECT * FROM `__sd_msgs` AS m", sessionStandIn("SELECT * FROM session('msgs') AS m"))
	assert.Equal(t, "SELECT 1", sessionStandIn("SELECT 1"))
}

func TestStaleDependantsAreTheTransitiveReadersLeftStale(t *testing.T) {
	infos := []sessiondata.DatasetInfo{
		{Name: "a", State: sessiondata.DatasetStateLive},
		{Name: "b", Dependencies: []string{"a"}, State: sessiondata.DatasetStateStale},
		{Name: "c", Dependencies: []string{"b"}, State: sessiondata.DatasetStateStale},
		{Name: "d", State: sessiondata.DatasetStateStale},
	}
	assert.Equal(t, []string{"b", "c"}, staleDependants(infos, "a"))
}

func TestAGrantErrorBecomesAnAgentLimit(t *testing.T) {
	err := sessionError("x", &sessiondata.GrantError{Destination: "clickhouse-session:h", Reason: "no"})
	var limit *AgentLimitError
	require.True(t, errors.As(err, &limit))
	assert.Equal(t, "clickhouse-session:h", limit.Destination)
}

// playLaunchForTest is a launch config seeded with sql.
func playLaunchForTest(sql string) launchcfg.PlayLaunch { return launchcfg.PlayLaunch{Sql: sql} }

// decodeOpenedLaunch is the launch config the last open request carried.
func decodeOpenedLaunch(t *testing.T, bus *fakeOpenBus) (cfg launchcfg.PlayLaunch) {
	t.Helper()
	req, err := buscodec.Decode[launchrequest.LaunchRequest](bus.gotPayload)
	require.NoError(t, err)
	cfg, err = buscodec.Decode[launchcfg.PlayLaunch](req.Config)
	require.NoError(t, err)
	return
}

// TestACheckRestsOnAResultThatReadADataSet: a citation's check over a
// result whose statement read session('msgs') may read the data set, runs
// in the data session, and holds no lease afterwards.
func TestACheckRestsOnAResultThatReadADataSet(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	dests := []string{DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)}
	r.grant("t1", dests...)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: dests}
	_, err := c.defineSession(t.Context(), obo, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	cs := checkSnap{checkSource: checkSource{resultId: 12, source: "SELECT * FROM session('msgs') LIMIT 20"}}
	for range 2 {
		out, err := runCheck(quickly(t), c, cs, obo, CheckArgs{ResultId: 12, Statement: "SELECT count() = 3 FROM session('msgs')"})
		require.NoError(t, err)
		assert.Equal(t, []string{"3"}, out.Cells)
		assert.Equal(t, statementDigest(cs.source), out.SourceDigest)
	}
	assert.Contains(t, r.stub.lastRun().SQL, "`__sd_msgs_r1`")
	_, err = runCheck(quickly(t), c, cs, obo, CheckArgs{ResultId: 12, Statement: "SELECT count() FROM session('other')"})
	require.ErrorContains(t, err, "did not read")
}

// TestASessionRunCarriesTheTaskAndTheServicesStatementsDoNot: a run reading
// a data set leaves marked with the task whose text it is (ADR-0403); the
// service's own create and probe leave with the mark cleared.
func TestASessionRunCarriesTheTaskAndTheServicesStatementsDoNot(t *testing.T) {
	r := newSdRig(t)
	rec := &siteRecorder{}
	t.Cleanup(fairlead.Install(rec))
	dests := []string{DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)}
	r.grant("t1", dests...)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: dests}
	c := r.client("github.com/x/play", 1)
	_, err := c.defineSession(t.Context(), obo, "msgs", "SELECT 1 AS x", nil)
	require.NoError(t, err)
	_, _, err = runAll(t, quickly(t), c, "SELECT x FROM session('msgs')", &ExecOptions{Agent: obo})
	require.NoError(t, err)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	assert.Equal(t, "t1", rec.bySite["chserver.deliver"])
	assert.Contains(t, rec.bySite, sessiondata.SiteCreate)
	assert.Empty(t, rec.bySite[sessiondata.SiteCreate])
	assert.Empty(t, rec.bySite[sessiondata.SiteProbe])
}

// siteRecorder is a fairlead pass that keeps the task of the last statement
// each site led.
type siteRecorder struct {
	mu     sync.Mutex
	bySite map[string]string
}

func (inst *siteRecorder) Lead(_ context.Context, st fairlead.Statement) (string, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.bySite == nil {
		inst.bySite = map[string]string{}
	}
	inst.bySite[st.Site] = st.Task
	return st.SQL, nil
}

// TestACitationRestsOnAKeptDataSet: the result of a keep is the data set's
// first rows, so an assertion citing it reads session('<name>') and holds;
// one naming the definition's base table is refused, and the refusal names
// the tables the result's statement read, as an assertion must name them.
func TestACitationRestsOnAKeptDataSet(t *testing.T) {
	r := newSdRig(t)
	c := r.client("github.com/x/play", 1)
	dests := []string{DestinationClickHouse(r.host), sessiondata.DestinationSession(r.host)}
	r.grant("t1", dests...)
	obo := &app.OnBehalfOf{Task: "t1", Epoch: 1, Call: "c1", Destinations: dests}
	res, err := c.defineSession(t.Context(), obo, "top", "SELECT 1 AS x FROM db.message", nil)
	require.NoError(t, err)
	kept := keptOf(res, nil)
	assert.Contains(t, kept.Cite, "session('top')", "the keep's answer says how to cite it")
	cs := checkSnap{checkSource: checkSource{resultId: 3, source: sampleStatement("top", 50)}}

	out, err := runCheck(quickly(t), c, cs, obo, CheckArgs{ResultId: 3, Statement: "SELECT count() FROM session('top')"})
	require.NoError(t, err)
	assert.Equal(t, []string{"3"}, out.Cells, "the stub's data set holds three rows")

	_, err = runCheck(quickly(t), c, cs, obo, CheckArgs{ResultId: 3, Statement: "SELECT count() FROM db.message"})
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "names db.message")
	assert.Contains(t, refusal.Reason, "it read session('top')", "the refusal says what the statement read")
}
