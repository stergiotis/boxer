package projection

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/tokenmeter"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Compaction (ADR-0316): the plan is a function over the journal
// (PlanCompaction, the Go twin of the plan view the ADR names; no SQL
// twin is written), the act a producer the driver runs, the apply pass is
// Fold's. The plan reads the
// request the fold projects now, sizes it from the provider's last
// reported usage plus an estimate for what came after, and when it
// reaches the trigger picks a cut on a turn start that leaves the kept
// tail under the trigger with room for the summary and one more round.
// The ladder is cheapest first (§SD2): a derived elision, the span's tool
// results replaced by stubs a `recall` brings back, before any summary.

// Constants are a profile's compaction constants (ADR-0316 §SD3, ADR-0310
// §SD7); every one of them is data on the profile.
type Constants struct {
	// TriggerTokens is the request size the plan compacts at.
	TriggerTokens int64
	// KeepTurns is how many of the latest turns stay whole whatever the
	// size; a cut never enters them.
	KeepTurns int
	// PinBytes bounds the person's messages a summary carries verbatim;
	// past it they are marked summarised (law L25).
	PinBytes int
	// ElideMinBytes is the size a tool result becomes a stub at.
	ElideMinBytes int
	// SummaryTokens is the summariser's answer budget, and the allowance
	// the plan keeps for the summary.
	SummaryTokens int64
	// TailTokens is the allowance the thrash check gives the kept tail,
	// and RoundMarginTokens one round's.
	TailTokens        int64
	RoundMarginTokens int64
}

// Validate is the thrash check (ADR-0316 §SD3): the head, the summary
// with the person's pinned words, the kept tail and one round's margin
// fit under the trigger, or every request would compact.
func (inst Constants) Validate(headTokens int64) (err error) {
	if inst.TriggerTokens <= 0 || inst.KeepTurns < 1 || inst.PinBytes < 0 || inst.ElideMinBytes < 0 || inst.SummaryTokens <= 0 || inst.TailTokens < 0 || inst.RoundMarginTokens < 0 {
		return eb.Build().Int64("trigger", inst.TriggerTokens).Int("keep", inst.KeepTurns).Int64("summary", inst.SummaryTokens).Errorf("the compaction constants are not all positive")
	}
	if need := headTokens + inst.summaryAllowance() + inst.TailTokens + inst.RoundMarginTokens; need >= inst.TriggerTokens {
		return eb.Build().Int64("head", headTokens).Int64("need", need).Int64("trigger", inst.TriggerTokens).Errorf("the profile thrashes: the head, the summary, the tail and a round's margin reach the trigger")
	}
	return
}

// summaryAllowance is what a summary message may take: the summariser's
// answer, the person's words pinned verbatim beside it (law L25), and the
// message's header and band.
func (inst Constants) summaryAllowance() int64 {
	return inst.SummaryTokens + int64(inst.PinBytes)/4 + messageTokens(Message{Content: Recalled("self", "", 0, "")})
}

// VerdictE is what the plan says to do.
type VerdictE uint8

const (
	// VerdictNone: the request fits.
	VerdictNone VerdictE = iota
	// VerdictElide: the derived elision of the span brings the request
	// under the trigger; no model.
	VerdictElide
	// VerdictSummarise: the span must be summarised.
	VerdictSummarise
	// VerdictRefuse: no cut outside the kept turns brings the request
	// under the trigger; the kept tail alone is too large.
	VerdictRefuse
)

func (inst VerdictE) String() string {
	switch inst {
	case VerdictNone:
		return "none"
	case VerdictElide:
		return "elide"
	case VerdictSummarise:
		return "summarise"
	case VerdictRefuse:
		return "refuse"
	}
	return "verdict(" + strconv.Itoa(int(inst)) + ")"
}

// The forms a compact entry takes.
const (
	FormElide   = "elide"
	FormSummary = "summary"
)

// PersonSummarisedMark is what a summary carries where the person's
// messages were not kept verbatim (law L25).
const PersonSummarisedMark = "[the person's earlier messages past the pin budget are summarised above]"

// Plan is the plan view's row (ADR-0316 §SD1).
type Plan struct {
	Verdict VerdictE
	// Cut is the ordinal the projection keeps from, From the previous cut
	// (law L22); Span the messages in [From, Cut) as projected now.
	Cut  uint32
	From uint32
	Span []Message
	// Previous is the standing summary, empty without one.
	Previous string
	// Label is the least label the compaction may carry: the join of the
	// span's and the previous summary's (law L3).
	Label agentrun.Label
	// Person is the person's messages in the span, kept verbatim.
	Person []Message
	// Estimated is the request's size the plan saw, After what the verdict
	// leaves it at.
	Estimated int64
	After     int64
	// Elided is the elide stage's body; CutText the person's message at
	// the cut, what a summariser is told to stop before.
	Elided  string
	CutText string
}

// Tokens estimates a batch's size the way the one meter does
// (tokenmeter.Estimate: four bytes a token, tokenmeter.ImageTokens an
// image), plus a header per message and ToolTokens per declared tool,
// which the meter has no schema for. It is the size of a request no
// provider has reported; Anchored is the size of one that was.
func Tokens(b Batch) (n int64) {
	for _, m := range b.Messages {
		n += messageTokens(m)
	}
	n += int64(len(b.Tools)) * ToolTokens
	return
}

// ToolTokens is the allowance per declared tool for its schema, which the
// journal holds by name only.
const ToolTokens = 64

// messageHeaderTokens is what a message costs beyond its text.
const messageHeaderTokens = 4

func messageTokens(m Message) (n int64) {
	n = int64(len(m.Content)+len(m.Calls)+len(m.ToolCallId))/4 + messageHeaderTokens
	n += int64(len(ParseAttachments(m.Attachments))) * tokenmeter.ImageTokens
	return
}

// Anchored is a batch's size anchored on the provider's report for an
// earlier batch (ADR-0316 §SD3): the estimate of this batch scaled by
// what the provider counted for the base against the base's estimate, so
// a tokenizer denser or sparser than the estimator's four bytes a token
// is followed in either direction, and never under the batch's own
// estimate, so the anchoring only ever raises what the plan sizes. A
// report of zero, or a base with nothing to estimate, leaves the
// estimate alone.
func Anchored(b Batch, base Batch, reported int64) (n int64) {
	n = Tokens(b)
	if reported <= 0 {
		return
	}
	baseTokens := Tokens(base)
	if baseTokens <= 0 {
		return
	}
	if scaled := reported * n / baseTokens; scaled > n {
		n = scaled
	}
	return
}

// ReportedFn reads the provider's last reported prompt tokens out of a
// journal prefix and the ordinal of the reply that reported them, zero
// where none did. The chat keeps the report in its reply entries; the
// projection does not know their shape.
type ReportedFn func(entries []agentrun.Entry) (tokens int64, at uint32)

// before is the entries with an ordinal under at.
func before(es []agentrun.Entry, at uint32) []agentrun.Entry {
	i, _ := slices.BinarySearchFunc(es, at, func(e agentrun.Entry, o uint32) int { return int(e.Ordinal) - int(o) })
	return es[:i]
}

// PlanCompaction is the plan view: reported is the provider's prompt
// tokens for the last request and reportedAt the ordinal of its reply,
// zero where none was reported. The request the provider saw is the
// projection before that reply; the request now is sized against it
// (Anchored), so the reply and whatever came after it, a compaction
// included, are counted on top of what the provider counted.
func PlanCompaction(entries []agentrun.Entry, order Order, c Constants, reported int64, reportedAt uint32) (p Plan) {
	es := slices.Clone(entries)
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	b := Fold(es, order)
	var previousTokens int64
	for _, m := range b.Messages {
		if m.Part == PartSummary {
			previousTokens = messageTokens(m)
			p.Label = p.Label.Join(m.Label)
		}
	}
	// The previous cut and the standing summary are the latest compact
	// entry's; the summary as the entry holds it, not as the request
	// carries it in band.
	var cut uint32
	run := ""
	for _, e := range es {
		run = e.Run
		if e.Kind == agentrun.KindCompact {
			cut, p.Previous = e.Target, e.Body
		}
	}
	p.From = cut
	if reported > 0 && reportedAt > 0 {
		p.Estimated = Anchored(b, Fold(before(es, reportedAt), order), reported)
	} else {
		p.Estimated = Tokens(b)
	}
	if c.TriggerTokens <= 0 || p.Estimated < c.TriggerTokens {
		return
	}
	// Candidates: the first live user entry of each turn after the cut,
	// the kept turns excluded.
	var starts []uint32
	lastTurn := ""
	for _, e := range es {
		if e.Kind != agentrun.KindUser || e.Ordinal <= cut {
			continue
		}
		if e.Turn != lastTurn || lastTurn == "" {
			starts = append(starts, e.Ordinal)
		}
		lastTurn = e.Turn
	}
	live := make(map[uint32]bool, len(b.Messages))
	for _, m := range b.Messages {
		if m.Part == PartBody {
			live[m.Ordinal] = true
		}
	}
	starts = slices.DeleteFunc(starts, func(o uint32) bool { return !live[o] })
	keep := c.KeepTurns
	if keep < 1 {
		keep = 1
	}
	if len(starts) < keep {
		p.Verdict = VerdictRefuse
		return
	}
	// A cut at the first kept turn's start keeps exactly the kept turns.
	starts = starts[:len(starts)-keep+1]
	// Cheapest first: the earliest cut whose elision fits keeps the most
	// without a model; failing every cut, the earliest whose summary fits
	// is summarised. A cut that removes nothing is no compaction. The
	// elision's size is summed from pieces rendered once per message; the
	// elision itself is rendered for the chosen cut alone.
	previous := p.Previous
	label := p.Label
	pieces := make(map[uint32]string, len(b.Messages))
	for _, m := range b.Messages {
		if m.Part == PartBody {
			pieces[m.Ordinal] = elidePiece(m, c.ElideMinBytes)
		}
	}
	band := messageTokens(Message{Content: Recalled("self", run, 0, "")})
	var summarise Plan
	for _, s := range starts {
		q := Plan{Cut: s, From: cut, Previous: previous, Label: label, Estimated: p.Estimated}
		var removed int64
		elidedBytes := len(elidePrefix(previous))
		for _, m := range b.Messages {
			if m.Part == PartBody && m.Ordinal == s {
				q.CutText = m.Content
			}
			if m.Part != PartBody || m.Ordinal >= s {
				continue
			}
			if len(q.Span) > 0 {
				elidedBytes += len(elideSeparator)
			}
			q.Span = append(q.Span, m)
			q.Label = q.Label.Join(m.Label)
			removed += messageTokens(m)
			elidedBytes += len(pieces[m.Ordinal])
			if m.Role == RoleUser {
				q.Person = append(q.Person, m)
			}
		}
		if len(q.Span) == 0 {
			continue
		}
		// What the compaction leaves: the request less the span and the
		// standing summary, plus the new summary message.
		rest := p.Estimated - removed - previousTokens
		q.After = rest + band + int64(elidedBytes)/4
		if q.After+c.RoundMarginTokens <= c.TriggerTokens {
			q.Verdict = VerdictElide
			q.Elided = Elide(previous, q.Span, c.ElideMinBytes)
			return q
		}
		if summarise.Verdict == VerdictNone {
			if after := rest + c.summaryAllowance(); after+c.RoundMarginTokens <= c.TriggerTokens {
				q.Verdict, q.After = VerdictSummarise, after
				summarise = q
			}
		}
	}
	if summarise.Verdict == VerdictNone {
		p.Verdict = VerdictRefuse
		return
	}
	summarise.Elided = Elide(previous, summarise.Span, c.ElideMinBytes)
	return summarise
}

// elideHeading opens an elided span.
const elideHeading = "Earlier in this conversation, compacted:"

// Elide renders the span after the standing summary with every tool
// result at or over minBytes replaced by a stub naming its ordinal, size
// and digest; the person's and the model's words stay verbatim. No model
// touches it, but it holds model output and tool results that re-enter,
// so the apply pass carries it at the model's level in the recalled band
// (ADR-0311 §SD4, ADR-0316 §SD3), whatever the inputs' levels were.
func Elide(previous string, span []Message, minBytes int) string {
	var sb strings.Builder
	sb.WriteString(elidePrefix(previous))
	for i, m := range span {
		if i > 0 {
			sb.WriteString(elideSeparator)
		}
		sb.WriteString(elidePiece(m, minBytes))
	}
	return sb.String()
}

// elideSeparator parts the pieces of an elision.
const elideSeparator = "\n\n"

// elidePrefix opens an elision: the standing summary, or the heading.
func elidePrefix(previous string) string {
	if previous != "" {
		return previous + elideSeparator
	}
	return elideHeading + elideSeparator
}

// elidePiece is one message as the elision carries it.
func elidePiece(m Message, minBytes int) string {
	var sb strings.Builder
	switch m.Role {
	case RoleUser:
		sb.WriteString("The person said:\n")
		sb.WriteString(m.Content)
	case RoleAssistant:
		sb.WriteString("You said:\n")
		sb.WriteString(m.Content)
		if names := callNames(m.Calls); len(names) > 0 {
			if m.Content != "" {
				sb.WriteString("\n")
			}
			sb.WriteString("You called: ")
			sb.WriteString(strings.Join(names, ", "))
		}
	case RoleTool:
		if minBytes > 0 && len(m.Content) >= minBytes {
			sb.WriteString(Stub(m))
		} else {
			sb.WriteString("The result of call ")
			sb.WriteString(callName(m))
			sb.WriteString(":\n")
			sb.WriteString(m.Content)
		}
	default:
		sb.WriteString("A note from the host:\n")
		sb.WriteString(m.Content)
	}
	return sb.String()
}

// Stub is what an elided tool result becomes: the call, the ordinal a
// recall names, the size and the digest.
func Stub(m Message) string {
	return "[the result of call " + callName(m) + " at ordinal " + strconv.FormatUint(uint64(m.Ordinal), 10) + " was elided: " +
		strconv.Itoa(len(m.Content)) + " bytes, digest " + agentrun.ContentDigest(m.Content)[:16] + "; recall(" + strconv.FormatUint(uint64(m.Ordinal), 10) + ") brings it back]"
}

// callName names a call as the journal does: the dispatcher's key
// (ADR-0316 §SD2), the provider's id where the entry carries none.
func callName(m Message) string {
	if m.Key != "" {
		return m.Key
	}
	return m.ToolCallId
}

func callNames(calls string) (names []string) {
	for _, c := range ParseCalls(calls) {
		names = append(names, c.Name)
	}
	return
}

// PersonVerbatim is the person's messages of a span rendered after a
// summary, verbatim up to pinBytes, then the mark (law L25).
func PersonVerbatim(person []Message, pinBytes int) string {
	var sb strings.Builder
	used := 0
	for _, m := range person {
		if used+len(m.Content) > pinBytes {
			sb.WriteString("\n\n")
			sb.WriteString(PersonSummarisedMark)
			return sb.String()
		}
		sb.WriteString("\n\nThe person said:\n")
		sb.WriteString(m.Content)
		used += len(m.Content)
	}
	return sb.String()
}

// CheckCompaction is laws L22, L23, L25 and the boundary rule of L2 over a
// journal (ADR-0316 §SD4): each compact's From is the previous cut, cuts
// strictly increase, a cut is a user entry no earlier call is answered
// past, and every live user message before a cut is in the summary
// verbatim or the summary is marked. It returns a line per breach.
func CheckCompaction(entries []agentrun.Entry) (broken []string) {
	es := slices.Clone(entries)
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	byOrdinal := make(map[uint32]agentrun.Entry, len(es))
	dropped := make(map[uint32]bool)
	// shown is the person's message as the request carried it: its latest
	// edit's body, the entry's without one.
	shown := make(map[uint32]string)
	for _, e := range es {
		byOrdinal[e.Ordinal] = e
		if e.Kind == agentrun.KindUser {
			shown[e.Ordinal] = e.Body
		}
		if e.Kind == agentrun.KindEdit {
			dropped[e.Target] = e.Form == agentrun.FormDrop
			if e.Form != agentrun.FormDrop {
				shown[e.Target] = e.Body
			}
		}
	}
	at := func(o uint32) string { return "compact at " + strconv.FormatUint(uint64(o), 10) }
	var prev uint32
	first := true
	for _, e := range es {
		if e.Kind != agentrun.KindCompact {
			continue
		}
		if first {
			if e.From != 0 {
				broken = append(broken, at(e.Ordinal)+": L22: the first compaction's from is not 0")
			}
		} else if e.From != prev {
			broken = append(broken, at(e.Ordinal)+": L22: from "+strconv.FormatUint(uint64(e.From), 10)+" is not the previous cut "+strconv.FormatUint(uint64(prev), 10))
		}
		if !first && e.Target <= prev {
			broken = append(broken, at(e.Ordinal)+": L23: the cut does not increase")
		}
		if e.Target == 0 || e.Target >= e.Ordinal {
			broken = append(broken, at(e.Ordinal)+": the cut is not an earlier ordinal")
		}
		if t, ok := byOrdinal[e.Target]; !ok || t.Kind != agentrun.KindUser {
			broken = append(broken, at(e.Ordinal)+": L2: the cut is not a user entry")
		}
		open := make(map[string]bool)
		for _, x := range es {
			if x.Ordinal >= e.Ordinal {
				break
			}
			if x.Kind == agentrun.KindReply && x.Ordinal < e.Target {
				for _, c := range ParseCalls(x.Calls) {
					open[c.Id] = true
				}
			}
			if x.Kind == agentrun.KindOutcome && x.Ordinal >= e.Target && open[x.Name] {
				broken = append(broken, at(e.Ordinal)+": L2: the cut is inside the span of call "+x.Name)
			}
		}
		marked := strings.Contains(e.Body, PersonSummarisedMark)
		for _, x := range es {
			if x.Kind != agentrun.KindUser || x.Ordinal < e.From || x.Ordinal >= e.Target || x.Ordinal >= e.Ordinal || dropped[x.Ordinal] || shown[x.Ordinal] == "" {
				continue
			}
			if !marked && !strings.Contains(e.Body, shown[x.Ordinal]) {
				broken = append(broken, at(e.Ordinal)+": L25: the person's message at "+strconv.FormatUint(uint64(x.Ordinal), 10)+" is neither verbatim nor marked summarised")
			}
		}
		prev, first = e.Target, false
	}
	return
}

// CheckBound is law L24 over a journal: the request projected right after
// each compaction is under the trigger, sized as the plan sized it,
// anchored on the provider's last report before the compaction where
// reported finds one (nil reads none).
func CheckBound(entries []agentrun.Entry, order Order, c Constants, reported ReportedFn) (broken []string) {
	es := slices.Clone(entries)
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	for i, e := range es {
		if e.Kind != agentrun.KindCompact {
			continue
		}
		after := Fold(es[:i+1], order)
		n := Tokens(after)
		if reported != nil {
			if tokens, at := reported(es[:i]); tokens > 0 && at > 0 {
				n = Anchored(after, Fold(before(es, at), order), tokens)
			}
		}
		if n > c.TriggerTokens {
			broken = append(broken, "compact at "+strconv.FormatUint(uint64(e.Ordinal), 10)+": L24: the request after it is "+strconv.FormatInt(n, 10)+" tokens, over the trigger")
		}
	}
	return
}
