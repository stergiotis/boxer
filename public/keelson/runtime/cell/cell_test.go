package cell

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/harness"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The sinks are derived from the declaration: a remote model host is a
// sink, a loopback or trusted one is inside the cell; a loopback egress
// destination is not a sink; the clipboard and export always are; every
// peer is one with its clearance. keelson('sinks') serves them.
func TestSinksAreDerivedFromTheDeclaration(t *testing.T) {
	d := Defaults()
	d.ModelHost = "api.example.net:443"
	d.Egress = []EgressDestination{{Name: "osm"}, {Name: "local-tiles", Local: true}}
	d.Peers = ParsePeers(" berlin=AMBER, vienna , =RED")
	sinks := d.Sinks()
	var kinds []string
	for _, s := range sinks {
		kinds = append(kinds, s.Kind.String()+":"+s.Edge+":"+s.Clearance.String())
	}
	assert.Equal(t, []string{"model-route:api.example.net:443:GREEN", "egress:osm:GREEN", "clipboard:clipboard:GREEN", "export:export:GREEN", "peer:berlin:AMBER", "peer:vienna:CLEAR"}, kinds)

	d.TrustedHosts = []string{"api.example.net"}
	assert.Equal(t, agentrun.TierRed, d.RouteClearance("https://api.example.net/v1"), "a trusted host is inside the cell")
	assert.Equal(t, agentrun.TierRed, d.RouteClearance("127.0.0.1:1234"))
	assert.Equal(t, agentrun.TierGreen, d.RouteClearance("other.example.net"))
	assert.NotEqual(t, SinkKindModelRoute, d.Sinks()[0].Kind, "the trusted route is no sink")
	assert.Equal(t, agentrun.TierRed, d.EgressClearance(true))
	assert.Equal(t, agentrun.TierAmber, d.TierOf(agentrun.TierClear, true), "confined maps to the confined tier")
	assert.Equal(t, agentrun.TierRed, d.TierOf(agentrun.TierRed, true), "a higher declared tier stands")

	reg := introspect.NewRegistry()
	require.NoError(t, RegisterIntrospect(reg, func() Declaration { return d }))
	p, ok := reg.Lookup(TableSinks)
	require.True(t, ok)
	rec, err := p.Snapshot(introspect.Projection{})
	require.NoError(t, err)
	defer rec.Release()
	assert.EqualValues(t, 5, rec.NumRows())
}

// The guard allows at or below the clearance and treats the clearance as
// a ceiling above it: the person's path asks, organisation and never
// deny, and a mapping to free above the clearance opens nothing; a tier
// the policy forgets is never.
func TestTheGuardTreatsTheClearanceAsACeiling(t *testing.T) {
	sink := Sink{Kind: SinkKindModelRoute, Edge: "api.example.net", Clearance: agentrun.TierGreen}
	policy := DefaultApprovalPolicy()
	d, tier, reason := Decide(agentrun.Label{Tier: agentrun.TierGreen}, sink, policy)
	assert.Equal(t, harness.DecisionAllow, d)
	assert.Equal(t, ApprovalTierFree, tier)
	assert.Empty(t, reason)
	d, tier, reason = Decide(agentrun.Label{Tier: agentrun.TierAmber}, sink, policy)
	assert.Equal(t, harness.DecisionAsk, d)
	assert.Equal(t, ApprovalTierPerson, tier)
	assert.Contains(t, reason, "recorded justification")
	d, tier, reason = Decide(agentrun.Label{Tier: agentrun.TierAmberStrict}, sink, policy)
	assert.Equal(t, harness.DecisionDeny, d, "no organisation path is wired")
	assert.Equal(t, ApprovalTierOrganisation, tier)
	assert.Contains(t, reason, "organisation")
	d, tier, reason = Decide(agentrun.Label{Tier: agentrun.TierRed}, sink, policy)
	assert.Equal(t, harness.DecisionDeny, d)
	assert.Equal(t, ApprovalTierNever, tier)
	assert.Contains(t, reason, "never leaves")

	low := Sink{Kind: SinkKindModelRoute, Edge: "api.example.net", Clearance: agentrun.TierClear}
	d, tier, reason = Decide(agentrun.Label{Tier: agentrun.TierGreen}, low, policy)
	assert.Equal(t, harness.DecisionDeny, d, "GREEN maps to free, and free above the clearance opens nothing")
	assert.Equal(t, ApprovalTierFree, tier)
	assert.Contains(t, reason, "opens nothing")

	open, err := ParseApprovalPolicy("CLEAR=free")
	require.NoError(t, err)
	d, _, _ = Decide(agentrun.Label{Tier: agentrun.TierAmber}, sink, open)
	assert.Equal(t, harness.DecisionDeny, d, "a tier the policy forgets is never")
	_, err = ParseApprovalPolicy("AMBER=maybe")
	assert.Error(t, err)
	_, err = ParseApprovalPolicy("PINK=free")
	assert.Error(t, err)

	a := Approval{Channel: "clipboard", Tier: "AMBER", Path: "person", Approver: "person", Justification: "for the report"}
	body, err := a.Body()
	require.NoError(t, err)
	back, err := ParseApproval(body)
	require.NoError(t, err)
	assert.Equal(t, "person", back.Path)
	_, err = ParseApproval("{")
	assert.Error(t, err)

	run, ord, ok := ParseApprovalKey(ApprovalKey("chat-a#b", 17))
	require.True(t, ok)
	assert.Equal(t, "chat-a#b", run)
	assert.EqualValues(t, 17, ord)
	for _, bad := range []string{"", "17", "#17", "run#", "run#0", "run#x"} {
		_, _, ok = ParseApprovalKey(bad)
		assert.False(t, ok, bad)
	}
}

// The exits and approvals the law tests share: GREEN by the route is at
// the clearance; AMBER by the route under a person's approval of the
// same run and sink passes; AMBER by the clipboard under a free-path
// approval does not; an unknown sink is unchecked; AMBER+STRICT under a
// person's approval is on the wrong path; an approval of another run, or
// written after the exit, does not count.
func lawFixture() (sinks []Sink, exits []ExitRow, approvals []ApprovalEntry) {
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	at := func(minute int) time.Time { return t0.Add(time.Duration(minute) * time.Minute) }
	sinks = []Sink{{Kind: SinkKindModelRoute, Edge: "r", Clearance: agentrun.TierGreen}, {Kind: SinkKindClipboard, Edge: "clipboard", Clearance: agentrun.TierGreen}}
	exits = []ExitRow{
		{Run: "x", Ordinal: 10, CallId: "c10", At: at(10), Sink: SinkKindModelRoute, Edge: "r", Tier: agentrun.TierGreen},
		{Run: "x", Ordinal: 11, CallId: "c11", At: at(11), Sink: SinkKindModelRoute, Edge: "r", Tier: agentrun.TierAmber, Approval: ApprovalKey("x", 3)},
		{Run: "x", Ordinal: 12, CallId: "c12", At: at(12), Sink: SinkKindClipboard, Edge: "clipboard", Tier: agentrun.TierAmber, Approval: ApprovalKey("x", 4)},
		{Run: "x", Ordinal: 13, CallId: "c13", At: at(13), Sink: SinkKindEgress, Edge: "unknown", Tier: agentrun.TierClear},
		{Run: "x", Ordinal: 14, CallId: "c14", At: at(14), Sink: SinkKindClipboard, Edge: "clipboard", Tier: agentrun.TierAmberStrict, Approval: ApprovalKey("x", 5)},
		{Run: "x", Ordinal: 15, CallId: "c15", At: at(15), Sink: SinkKindModelRoute, Edge: "r", Tier: agentrun.TierAmber, Approval: ApprovalKey("y", 1)},
		{Run: "x", Ordinal: 16, CallId: "c16", At: at(16), Sink: SinkKindModelRoute, Edge: "r", Tier: agentrun.TierAmber, Approval: ApprovalKey("x", 20)},
		{Run: "x", Ordinal: 17, CallId: "c17", At: at(17), Sink: SinkKindModelRoute, Edge: "r", Tier: agentrun.TierAmber},
		// The trail's counter and the journal's are two counters: a call
		// whose display id is far below its approval's ordinal is fine
		// when the approval was written first.
		{Run: "x", Ordinal: 3, CallId: "c3", At: at(50), Sink: SinkKindModelRoute, Edge: "r", Tier: agentrun.TierAmber, Approval: ApprovalKey("x", 40)},
	}
	approvals = []ApprovalEntry{
		{Run: "x", Ordinal: 3, At: at(2), Approval: Approval{Channel: "model-route", Edge: "r", Path: "person"}},
		{Run: "x", Ordinal: 4, At: at(2), Approval: Approval{Channel: "clipboard", Path: "free"}},
		{Run: "x", Ordinal: 5, At: at(2), Approval: Approval{Channel: "clipboard", Path: "person"}},
		{Run: "y", Ordinal: 1, At: at(2), Approval: Approval{Channel: "model-route", Edge: "r", Path: "person"}},
		{Run: "x", Ordinal: 20, At: at(30), Approval: Approval{Channel: "model-route", Edge: "r", Path: "person"}},
		{Run: "x", Ordinal: 40, At: at(45), Approval: Approval{Channel: "model-route", Edge: "r", Path: "person"}},
	}
	return
}

// L14 names an exit above its sink's clearance without a person's
// approval, or by a sink the declaration omits; L33 names an exit whose
// approval is not of the same run, before the exit in time, for the
// sink, and on the path the policy requires.
func TestTheExitLawsReturnTheRowsThatBreakThem(t *testing.T) {
	sinks, exits, approvals := lawFixture()
	assert.Equal(t, []uint32{12, 13, 17}, ordinals(L14(sinks, exits, approvals)), "L14 reads the path alone")
	assert.Equal(t, []uint32{12, 14, 15, 16, 17}, ordinals(L33(DefaultApprovalPolicy(), sinks, exits, approvals)))
	assert.Contains(t, L14SQL, "keelson('sinks')")
	assert.Contains(t, L33SQL, "'approval'")
	assert.Contains(t, L14SQL, "clearance_rank")
	assert.NotContains(t, L33SQL, "policy AS p", "the policy relation is substituted, never a bare name")
	assert.NotContains(t, strings.ReplaceAll(L33SQL, "clearance_rank", ""), "s.clearance", "the tier compares against the rank, never the name")
}

// The same laws as SQL, run on clickhouse local: the fixture forms over
// the law relations, and the production compositions over tables shaped
// as keelson('llm_calls'), keelson('sinks') and keelson('agent_run_entries')
// serve them, with a loopback call that is no exit. The rows they return
// are the rows the Go checks return.
func TestTheExitLawsSQLAgreeWithTheGoChecks(t *testing.T) {
	bin, err := chlocalpool.LookupBinary()
	if err != nil {
		t.Skipf("clickhouse not installed: %v", err)
	}
	sinks, exits, approvals := lawFixture()
	rfc := func(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }
	var ddl strings.Builder
	ddl.WriteString("CREATE TABLE sinks (kind String, edge String, clearance String, clearance_rank UInt64) ENGINE = Memory;\n")
	ddl.WriteString("CREATE TABLE exits (run String, ordinal UInt32, call_id String, at String, sink String, edge String, tier UInt8, approval String) ENGINE = Memory;\n")
	ddl.WriteString("CREATE TABLE approvals (run String, ordinal UInt32, key String, at_ms Int64, path String, channel String, edge String) ENGINE = Memory;\n")
	ddl.WriteString("CREATE TABLE llm_calls (conversation String, id UInt32, call_id String, at String, endpoint_host String, tier UInt8, refused Bool, approval String) ENGINE = Memory;\n")
	ddl.WriteString("CREATE TABLE agent_run_entries (run String, ordinal UInt32, at_ms Int64, kind String, body String) ENGINE = Memory;\n")
	for _, s := range sinks {
		ddl.WriteString("INSERT INTO sinks VALUES ('" + s.Kind.String() + "', '" + s.Edge + "', '" + s.Clearance.String() + "', " + itoa(uint32(s.Clearance)) + ");\n")
	}
	for _, e := range exits {
		ddl.WriteString("INSERT INTO exits VALUES ('" + e.Run + "', " + itoa(e.Ordinal) + ", '" + e.CallId + "', '" + rfc(e.At) + "', '" + e.Sink.String() + "', '" + e.Edge + "', " + itoa(uint32(e.Tier)) + ", '" + e.Approval + "');\n")
		if e.Sink == SinkKindModelRoute {
			ddl.WriteString("INSERT INTO llm_calls VALUES ('" + e.Run + "', " + itoa(e.Ordinal) + ", '" + e.CallId + "', '" + rfc(e.At) + "', '" + e.Edge + "', " + itoa(uint32(e.Tier)) + ", false, '" + e.Approval + "');\n")
		}
	}
	// A refused call never left, and a loopback call is inside the cell:
	// neither is an exit.
	ddl.WriteString("INSERT INTO llm_calls VALUES ('x', 90, 'c90', '" + rfc(exits[0].At) + "', 'r', 4, true, '');\n")
	ddl.WriteString("INSERT INTO llm_calls VALUES ('x', 91, 'c91', '" + rfc(exits[0].At) + "', '127.0.0.1:1234', 4, false, '');\n")
	for _, a := range approvals {
		body, berr := a.Approval.Body()
		require.NoError(t, berr)
		ddl.WriteString("INSERT INTO approvals VALUES ('" + a.Run + "', " + itoa(a.Ordinal) + ", '" + a.Key() + "', " + strconv.FormatInt(a.At.UnixMilli(), 10) + ", '" + a.Approval.Path + "', '" + a.Approval.Channel + "', '" + a.Approval.Edge + "');\n")
		ddl.WriteString("INSERT INTO agent_run_entries VALUES ('" + a.Run + "', " + itoa(a.Ordinal) + ", " + strconv.FormatInt(a.At.UnixMilli(), 10) + ", 'approval', '" + strings.ReplaceAll(body, "'", "\\'") + "');\n")
	}
	run := func(law string) (ords []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		q := ddl.String() + "SELECT ordinal FROM (" + law + ") ORDER BY ordinal FORMAT TSV"
		out, rerr := exec.CommandContext(ctx, bin, "local", "--multiquery", "--query", q).CombinedOutput()
		require.NoError(t, rerr, "%s", out)
		return strings.Fields(string(out))
	}
	assert.Equal(t, []string{"12", "13", "17"}, run(L14SQLOver("exits", "sinks", "approvals")))
	assert.Equal(t, []string{"12", "14", "15", "16", "17"}, run(L33SQLOver("exits", "sinks", "approvals", "("+PolicySQL(DefaultApprovalPolicy())+")")))
	served := strings.NewReplacer("keelson('llm_calls')", "llm_calls", "keelson('sinks')", "sinks", "keelson('agent_run_entries')", "agent_run_entries")
	assert.Equal(t, []string{"17"}, run(served.Replace(L14SQL)), "over the served shapes: model-route rows only")
	assert.Equal(t, []string{"15", "16", "17"}, run(served.Replace(L33SQLFor(DefaultApprovalPolicy()))))
}

func ordinals(rows []ExitRow) (out []uint32) {
	for _, r := range rows {
		out = append(out, r.Ordinal)
	}
	return
}

// An exact canary finds its tokens and nothing else; shingles find a
// verbatim copy of four words and miss a paraphrase; the declaration's
// Leaks names the scopes above the sink's clearance only.
func TestCanariesFindVerbatimLeaks(t *testing.T) {
	assert.Equal(t, []string{"zq-7731"}, ExactCanary("the report, zq-7731, is attached", []string{"zq-7731", "zq-0000", ""}))
	assert.Empty(t, ExactCanary("nothing here", []string{"zq-7731"}))
	protected := "The quarterly revenue fell by twelve percent in Vienna"
	assert.True(t, ShingleLeak("he said the quarterly revenue fell by twelve percent, sadly", protected))
	assert.False(t, ShingleLeak("revenue in Vienna dropped twelve percent last quarter", protected), "a paraphrase is not found")
	assert.Equal(t, []string{"a b"}, Shingles("A  b", 4), "a short body is one shingle")
	assert.Empty(t, Shingles("   ", 4))
	assert.Len(t, Shingles("w1 w2 w3 w4 w5 w1 w2 w3 w4 w5", 4), 5, "repeated windows are one shingle each")

	d := Defaults()
	d.Canaries, _ = ParseCanaries("hr=zq-7731@AMBER, finance=qf-11, ")
	d.Protected = []Protected{{Scope: "board", Body: protected, Tier: agentrun.TierAmber}}
	assert.Equal(t, agentrun.TierRed, d.Canaries[1].Tier, "no tier is RED")
	assert.Equal(t, []string{"board", "hr"}, d.Leaks("zq-7731: the quarterly revenue fell by twelve percent", agentrun.TierGreen))
	assert.Empty(t, d.Leaks("zq-7731: the quarterly revenue fell by twelve percent", agentrun.TierAmber), "at the clearance the AMBER scopes may go")
	assert.Equal(t, []string{"finance"}, d.Leaks("qf-11", agentrun.TierAmberStrict))
	_, err := ParseCanaries("hr=")
	assert.Error(t, err)
	_, err = ParseCanaries("hr=x@PINK")
	assert.Error(t, err)
	l10 := L10SQLOver("payloads", "keelson('sinks')", "canary")
	assert.Contains(t, l10, "position(")
	assert.Contains(t, l10, "clearance_rank")
}

// FromEnv reads the policy data with the shipped defaults; a set value
// that does not parse fails closed, and tier names read in any case.
func TestFromEnvDefaultsAndFailsClosed(t *testing.T) {
	d := FromEnv([]string{"a.lan"})
	assert.Equal(t, []string{"a.lan"}, d.TrustedHosts)
	assert.Equal(t, agentrun.TierAmber, d.ConfinedTier)
	assert.Equal(t, agentrun.TierGreen, d.Clearances.ModelRoute)
	assert.Equal(t, ApprovalTierPerson, d.Approvals.Path(agentrun.TierAmber))
	assert.False(t, d.Managed)
	assert.Empty(t, d.Canaries)
	RouteClearanceEnv.SetForTest(t, "RED")
	ApprovalsEnv.SetForTest(t, "RED=person")
	CanariesEnv.SetForTest(t, "hr=zq@AMBER")
	t.Setenv(EgressClearanceEnv.Spec().Name, "amber")
	d = FromEnv(nil)
	assert.Equal(t, agentrun.TierRed, d.Clearances.ModelRoute)
	assert.Equal(t, agentrun.TierAmber, d.Clearances.Egress, "case-insensitive")
	assert.Equal(t, ApprovalTierPerson, d.Approvals.Path(agentrun.TierRed))
	assert.Equal(t, ApprovalTierNever, d.Approvals.Path(agentrun.TierClear), "a tier the deployment's mapping forgets is never")
	assert.Equal(t, []Canary{{Scope: "hr", Token: "zq", Tier: agentrun.TierAmber}}, d.Canaries)

	t.Setenv(ApprovalsEnv.Spec().Name, "AMBER=never,RED=nevr")
	t.Setenv(ClipboardClearanceEnv.Spec().Name, "PINK")
	t.Setenv(ConfinedTierEnv.Spec().Name, "AMBR")
	t.Setenv(CanariesEnv.Spec().Name, "hr")
	d = FromEnv(nil)
	assert.Equal(t, ApprovalTierNever, d.Approvals.Path(agentrun.TierAmber), "a mapping that does not parse opens nothing")
	assert.Equal(t, ApprovalTierNever, d.Approvals.Path(agentrun.TierClear))
	assert.Equal(t, agentrun.TierClear, d.Clearances.Clipboard, "a clearance that does not parse fails low: CLEAR")
	assert.Equal(t, agentrun.TierRed, d.ConfinedTier, "a content tier that does not parse fails high: RED")
	assert.Empty(t, d.Canaries)
}
