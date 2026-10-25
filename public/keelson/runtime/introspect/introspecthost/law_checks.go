package introspecthost

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
)

// The laws beyond the evidence laws that the host can judge over what it
// holds (ADR-0327 §SD7, update of 2026-10-13): the compaction laws per
// journal run (ADR-0316 §SD4), the exits' clearance and approval laws over
// the model calls this process answered (ADR-0312 §SD2, §SD3), and the
// copy law over the clipboard broker's records (ADR-0317 §SD4). Each is
// the Go check the integration lane runs, over live rows instead of a
// fixture; the laws that need an app's own fold (L4, L24) or a store the
// host does not keep (L10's payloads) are not rows here.

// compactionLaws are the laws CheckCompaction judges, in row order.
var compactionLaws = []string{"L2", "L22", "L23", "L25"}

// compactionChecks is one row per compaction law over one run's entries.
// A breach line names its law after the entry it is at ("compact at N:
// L22: …"); the one line without a law — a cut that is not an earlier
// ordinal — is an ordering breach, read as L23.
func compactionChecks(scope string, entries []agentrun.Entry) (rows []evidenceCheck) {
	byLaw := map[string][]string{}
	first := map[string]uint64{}
	for _, line := range projection.CheckCompaction(entries) {
		at, rest, _ := strings.Cut(line, ": ")
		law := "L23"
		if l, tail, ok := strings.Cut(rest, ": "); ok && strings.HasPrefix(l, "L") {
			law, rest = l, tail
		}
		if _, seen := first[law]; !seen {
			first[law], _ = strconv.ParseUint(strings.TrimPrefix(at, "compact at "), 10, 64)
		}
		byLaw[law] = append(byLaw[law], at+": "+rest)
	}
	for _, law := range compactionLaws {
		lines := byLaw[law]
		rows = append(rows, evidenceCheck{Store: "journal", Scope: scope, Law: law, Holds: len(lines) == 0,
			Broken: first[law], Why: strings.Join(lines, "; ")})
	}
	return
}

// approvalsOf is a run's approval entries as the exits' laws read them.
func approvalsOf(r agentrun.Run, entries []agentrun.Entry) (out []cell.ApprovalEntry) {
	for _, e := range entries {
		if e.Kind != agentrun.KindApproval || e.Approval == nil {
			continue
		}
		a := e.Approval
		out = append(out, cell.ApprovalEntry{EntryId: e.EntryId, Run: r.Id, Conversation: r.Conversation, Ordinal: e.Ordinal, At: e.At,
			Approval: cell.Approval{Channel: a.Channel, Edge: a.Edge, Tier: a.Tier, Path: a.Path, Approver: a.Approver, At: a.At}})
	}
	return
}

// exitChecks is L14 and L33 over the exits this process holds, one pair
// of rows per sink kind: the model-route exits are the answered calls of
// the model service's ring by a route the declaration lists as a sink, the
// egress exits the answered fetches of the egress service's ring to a
// destination it lists — as cell.ModelRouteExitsSQL and cell.FetchExitsSQL
// select them. The rings are bounded, so the rows judge the calls they
// still hold, and the approvals the journal store lists.
func (inst evidenceChecksProvider) exitChecks(approvals []cell.ApprovalEntry) (rows []evidenceCheck) {
	if inst.cell == nil {
		return
	}
	decl := inst.cell()
	sinks := decl.Sinks()
	edges := map[cell.SinkKindE]map[string]bool{}
	for _, s := range sinks {
		if edges[s.Kind] == nil {
			edges[s.Kind] = map[string]bool{}
		}
		edges[s.Kind][s.Edge] = true
	}
	policy := decl.Approvals
	if policy == nil {
		policy = cell.DefaultApprovalPolicy()
	}
	if inst.calls != nil {
		var exits []cell.ExitRow
		for _, c := range inst.calls.Calls() {
			if c.Refused || !edges[cell.SinkKindModelRoute][c.EndpointHost] {
				continue
			}
			exits = append(exits, cell.ExitRow{Conversation: c.Conversation, Ordinal: uint32(c.Id), CallId: c.CallId, At: c.At,
				Sink: cell.SinkKindModelRoute, Edge: c.EndpointHost, Tier: agentrun.TierE(c.Tier), ApprovalEntry: c.ApprovalEntry})
		}
		rows = append(rows, exitLawRows(cell.SinkKindModelRoute, policy, sinks, exits, approvals)...)
	}
	if inst.fetches != nil {
		var exits []cell.ExitRow
		for _, f := range inst.fetches.Calls() {
			if f.Refused || !edges[cell.SinkKindEgress][f.Destination] {
				continue
			}
			exits = append(exits, cell.ExitRow{Conversation: f.Conversation, Ordinal: uint32(f.Id), CallId: f.TaskCall, At: f.At,
				Sink: cell.SinkKindEgress, Edge: f.Destination, Tier: f.Tier})
		}
		rows = append(rows, exitLawRows(cell.SinkKindEgress, policy, sinks, exits, approvals)...)
	}
	return
}

// exitLawRows is L14 and L33 over one sink kind's exits, as two rows.
func exitLawRows(kind cell.SinkKindE, policy cell.ApprovalPolicy, sinks []cell.Sink, exits []cell.ExitRow, approvals []cell.ApprovalEntry) (rows []evidenceCheck) {
	for _, law := range []struct {
		name       string
		violations []cell.ExitRow
	}{
		{"L14", cell.L14(sinks, exits, approvals)},
		{"L33", cell.L33(policy, sinks, exits, approvals)},
	} {
		row := evidenceCheck{Store: "exits", Scope: kind.String(), Law: law.name, Holds: len(law.violations) == 0,
			Why: strconv.Itoa(len(exits)) + " exits in this process's ring"}
		if len(law.violations) > 0 {
			v := law.violations[0]
			row.Broken = uint64(v.Ordinal)
			row.Why = strconv.Itoa(len(law.violations)) + " of " + strconv.Itoa(len(exits)) + " exits break it, first call " + v.CallId + " in " + v.Conversation
		}
		rows = append(rows, row)
	}
	return
}

// copyChecks is L20 over the broker's records: labelled text above the
// free tier left as a handle, was refused, or is a recorded exit. The
// forwarded copies the law also covers are drained by the frame and not
// kept, so that clause is not judged here.
func (inst evidenceChecksProvider) copyChecks() (rows []evidenceCheck) {
	if inst.clipboard == nil || inst.cell == nil {
		return
	}
	free := clipboardbroker.PolicyOf(inst.cell()).FreeTier
	records := inst.clipboard.Records()
	broken := clipboardbroker.CheckL20(free, records, nil)
	row := evidenceCheck{Store: "clipboard", Scope: "clipboard", Law: "L20", Holds: len(broken) == 0,
		Why: strconv.Itoa(len(records)) + " copies in the broker's ring; forwarded copies not judged"}
	if len(broken) > 0 {
		row.Why = strings.Join(broken, "; ")
	}
	return append(rows, row)
}
