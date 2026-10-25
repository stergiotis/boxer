package cell

import (
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// The laws of the exits (ADR-0312 §SD2, §SD3), each as a Go check over
// rows and as SQL for the integration lane: a law returns the rows that
// break it. The exits are the outbound rows the trail records (ADR-0277
// §SD7) joined on the sink they left by; the approvals are the journal's
// approval entries, joined by the entry id the exit row carries (ADR-0325
// §SD5). An exit and its approval live in two records with two counters,
// the trail's call id and the journal's ordinal, so "before" is a question
// of time, never of one ordinal against the other.

// ExitRow is one row the trail records as leaving the cell: by which sink,
// at which tier, in which conversation, under which approval entry if
// any. Ordinal is the row's own identity in the trail (the call's display
// id); At is when it left.
type ExitRow struct {
	Conversation  string
	Ordinal       uint32
	CallId        string
	At            time.Time
	Sink          SinkKindE
	Edge          string
	Tier          agentrun.TierE
	ApprovalEntry uint64
}

// ApprovalEntry is one approval entry of the journal, as the laws see it:
// its id, where it is (run, ordinal) and the conversation its run is of,
// when it was written, and what it says.
type ApprovalEntry struct {
	EntryId      uint64
	Run          uint64
	Conversation string
	Ordinal      uint32
	At           time.Time
	Approval     Approval
}

func clearancesOf(sinks []Sink) (clearances map[string]agentrun.TierE) {
	clearances = make(map[string]agentrun.TierE, len(sinks))
	for _, s := range sinks {
		clearances[s.Kind.String()+"\x00"+s.Edge] = s.Clearance
	}
	return
}

func approvalsById(approvals []ApprovalEntry) (byId map[uint64]ApprovalEntry) {
	byId = make(map[uint64]ApprovalEntry, len(approvals))
	for _, a := range approvals {
		byId[a.EntryId] = a
	}
	return
}

// L14 is the clearance law: every exit is by a sink the declaration
// names, and an exit above the sink's clearance carries an approval entry
// on the person's path. An exit by a sink the declaration does not name
// is a violation, since nothing checked it. The caller passes the rows
// that left the cell: a loopback or trusted route is inside it and is no
// exit (ExitsSQL joins the declared sinks for the same reason).
func L14(sinks []Sink, exits []ExitRow, approvals []ApprovalEntry) (violations []ExitRow) {
	clearances := clearancesOf(sinks)
	byId := approvalsById(approvals)
	for _, e := range exits {
		c, named := clearances[e.Sink.String()+"\x00"+e.Edge]
		if !named {
			violations = append(violations, e)
			continue
		}
		if e.Tier <= c {
			continue
		}
		a, ok := byId[e.ApprovalEntry]
		if path, parsed := ParseApprovalTier(a.Approval.Path); !ok || !parsed || path != ApprovalTierPerson {
			violations = append(violations, e)
		}
	}
	return
}

// L33 is the approval law: the approval an exit above the clearance names
// is an entry of the exit's conversation, written before the exit, for the same sink,
// and on the path the policy requires for the payload's tier; a path the
// policy never opens is a violation whatever the entry says. "Before" is
// read off the two timestamps where both are known.
func L33(policy ApprovalPolicy, sinks []Sink, exits []ExitRow, approvals []ApprovalEntry) (violations []ExitRow) {
	clearances := clearancesOf(sinks)
	byId := approvalsById(approvals)
	for _, e := range exits {
		c := clearances[e.Sink.String()+"\x00"+e.Edge]
		if e.Tier <= c {
			continue
		}
		need := policy.Path(e.Tier)
		a, ok := byId[e.ApprovalEntry]
		got, parsed := ParseApprovalTier(a.Approval.Path)
		sameSink := a.Approval.Channel == e.Sink.String() && (a.Approval.Edge == "" || a.Approval.Edge == e.Edge)
		after := !a.At.IsZero() && !e.At.IsZero() && a.At.After(e.At)
		if !ok || !parsed || need == ApprovalTierFree || need == ApprovalTierNever || got != need ||
			a.Conversation != e.Conversation || after || !sameSink {
			violations = append(violations, e)
		}
	}
	return
}

// ModelRouteExitsSQL is the model-route exits as the trail shows them:
// every answered call of keelson('llm_calls') by a route the declaration
// lists as a sink, with the tier the wall saw and the approval key it went
// under. A call refused never left; a call to a loopback or trusted route
// stayed inside the cell and is no exit, which the join to keelson('sinks')
// says. The ordinal is the call's display id, an identity and not a
// position in any journal.
const ModelRouteExitsSQL = `
SELECT c.conversation AS conversation, c.id AS ordinal, c.call_id AS call_id, c.at AS at,
       'model-route' AS sink, c.endpoint_host AS edge, toUInt8(c.tier) AS tier, c.approval_entry AS approval_entry
FROM keelson('llm_calls') AS c
INNER JOIN keelson('sinks') AS s ON s.kind = 'model-route' AND s.edge = c.endpoint_host
WHERE NOT c.refused`

// FetchExitsSQL is the egress exits in the same columns: every fetch of
// keelson('http_calls') answered to a destination the declaration lists as
// a sink, with the tier the wall saw — the higher of the sender's
// declaration and the run's tier the dispatcher attested (ADR-0312 §SD4).
// A fetch has no approval path, so its approval entry is 0: L14 holds for
// it only while its tier is at or below the destination's clearance. The
// conversation is the dispatcher's word, empty for an app's own fetch.
const FetchExitsSQL = `
SELECT f.conversation AS conversation, f.id AS ordinal, f.task_call AS call_id, f.at AS at,
       'egress' AS sink, f.destination AS edge, toUInt8(f.tier) AS tier, toUInt64(0) AS approval_entry
FROM keelson('http_calls') AS f
INNER JOIN keelson('sinks') AS s ON s.kind = 'egress' AND s.edge = f.destination
WHERE NOT f.refused`

// ExitsSQL is every exit the trail shows, model-route and egress, in the
// columns L14SQLOver and L33SQLOver read.
const ExitsSQL = ModelRouteExitsSQL + `
UNION ALL` + FetchExitsSQL

// ApprovalsSQL is the approval entries of the journal with their id, the
// conversation of their run, path, sink and time, over
// keelson('agent_run_entries') and keelson('agent_runs').
const ApprovalsSQL = `
SELECT e.entry_id AS entry_id, e.run AS run, r.conversation AS conversation, e.ordinal AS ordinal, e.at_ms AS at_ms,
       JSONExtractString(e.body, 'path') AS path, JSONExtractString(e.body, 'channel') AS channel, JSONExtractString(e.body, 'edge') AS edge
FROM keelson('agent_run_entries') AS e
INNER JOIN keelson('agent_runs') AS r ON r.id = e.run
WHERE e.kind = 'approval'`

// L14SQLOver is L14 over the named relations: exits with the ExitsSQL
// columns, sinks with keelson('sinks')'s, approvals with ApprovalsSQL's.
// The tier compares against the sink's rank, never its name.
func L14SQLOver(exits string, sinks string, approvals string) string {
	return strings.NewReplacer("{exits}", exits, "{sinks}", sinks, "{approvals}", approvals).Replace(`
SELECT x.conversation AS conversation, x.ordinal AS ordinal, x.sink AS sink, x.edge AS edge, x.tier AS tier
FROM {exits} AS x
LEFT JOIN {sinks} AS s ON s.kind = x.sink AND s.edge = x.edge
LEFT JOIN {approvals} AS a ON a.entry_id = x.approval_entry
WHERE s.kind = '' OR (x.tier > s.clearance_rank AND a.path != 'person')`)
}

// L33SQLOver is L33 over the named relations, with the policy as a
// (tier, path) relation of the deployment's mapping. The exit's time is
// the trail's RFC 3339 text and the approval's the journal's millisecond
// epoch; the two are compared as times.
func L33SQLOver(exits string, sinks string, approvals string, policy string) string {
	return strings.NewReplacer("{exits}", exits, "{sinks}", sinks, "{approvals}", approvals, "{policy}", policy).Replace(`
SELECT x.conversation AS conversation, x.ordinal AS ordinal, x.sink AS sink, x.edge AS edge, x.tier AS tier
FROM {exits} AS x
INNER JOIN {sinks} AS s ON s.kind = x.sink AND s.edge = x.edge
LEFT JOIN {approvals} AS a ON a.entry_id = x.approval_entry
LEFT JOIN {policy} AS p ON p.tier = x.tier
WHERE x.tier > s.clearance_rank
  AND (a.entry_id = 0 OR a.conversation != x.conversation OR fromUnixTimestamp64Milli(a.at_ms) > parseDateTime64BestEffort(x.at, 3)
       OR a.channel != x.sink OR (a.edge != '' AND a.edge != x.edge)
       OR p.path IN ('', 'free', 'never') OR a.path != p.path)`)
}

// L14SQL is L14 over the trail and the journal on the server, for the
// integration lane.
var L14SQL = L14SQLOver("("+ExitsSQL+")", "keelson('sinks')", "("+ApprovalsSQL+")")

// L33SQLFor is L33 over the trail and the journal on the server under the
// deployment's approval policy; L33SQL is it under the default policy.
func L33SQLFor(p ApprovalPolicy) string {
	return L33SQLOver("("+ExitsSQL+")", "keelson('sinks')", "("+ApprovalsSQL+")", "("+PolicySQL(p)+")")
}

var L33SQL = L33SQLFor(DefaultApprovalPolicy())

// PolicySQL is the approval policy as a VALUES relation of (tier, path)
// for L33SQL's policy input.
func PolicySQL(p ApprovalPolicy) string {
	var b strings.Builder
	b.WriteString("SELECT * FROM VALUES('tier UInt8, path String'")
	for t := agentrun.TierClear; t <= agentrun.TierRed; t++ {
		b.WriteString(", (" + itoa(uint32(t)) + ", '" + p.Path(t).String() + "')")
	}
	b.WriteString(")")
	return b.String()
}
