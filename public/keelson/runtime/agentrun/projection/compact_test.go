package projection

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

var testConstants = Constants{TriggerTokens: 600, KeepTurns: 1, KeepRounds: 2, PinBytes: 200, ElideMinBytes: 100, SummaryTokens: 50, TailTokens: 100, RoundMarginTokens: 50}

// longRun is turns of one call each with a result of resultBytes.
func longRun(turns int, resultBytes int) (es []agentrun.Entry) {
	return roundsRun(turns, 1, resultBytes)
}

// roundsRun is turns of rounds calls each, one a round, with a result of
// resultBytes each, and an answer.
func roundsRun(turns int, rounds int, resultBytes int) (es []agentrun.Entry) {
	es = append(es, agentrun.Entry{Ordinal: 1, Kind: agentrun.KindSection, Name: "coordinator", Body: "Work in windows."})
	for t := 1; t <= turns; t++ {
		es = appendTurn(es, "t"+string(rune('0'+t)), rounds, resultBytes)
	}
	return
}

// appendTurn appends a turn of rounds calls and an answer after es.
func appendTurn(es []agentrun.Entry, turn string, rounds int, resultBytes int) []agentrun.Entry {
	ord := uint32(len(es) + 1)
	if n := len(es); n > 0 {
		ord = es[n-1].Ordinal + 1
	}
	add := func(e agentrun.Entry) {
		e.Ordinal = ord
		ord++
		es = append(es, e)
	}
	add(agentrun.Entry{Kind: agentrun.KindUser, Role: RoleUser, Body: "question " + turn, Turn: turn})
	for r := 0; r <= rounds; r++ {
		key := turn + "#" + strconv.Itoa(r)
		add(agentrun.Entry{Kind: agentrun.KindRequest, Key: key, Turn: turn})
		if r == rounds {
			add(agentrun.Entry{Kind: agentrun.KindReply, Key: key, Role: RoleAssistant, Body: "answer " + turn, Turn: turn, Label: agentrun.Label{Level: agentrun.LevelOwnModel}})
			break
		}
		callId := "c" + turn + "r" + strconv.Itoa(r)
		add(agentrun.Entry{Kind: agentrun.KindReply, Key: key, Role: RoleAssistant, Calls: CallsBody([]openaichat.ToolCall{{Id: callId, Name: "read_window_tree"}}), Turn: turn, Label: agentrun.Label{Level: agentrun.LevelOwnModel}})
		add(agentrun.Entry{Kind: agentrun.KindIntent, Key: key + "#0", Name: callId, Turn: turn})
		add(agentrun.Entry{Kind: agentrun.KindOutcome, Key: key + "#0", Name: callId, Role: RoleTool, Body: strings.Repeat("x", resultBytes), Turn: turn, Label: agentrun.Label{Level: agentrun.LevelExternal}})
	}
	add(agentrun.Entry{Kind: agentrun.KindTurn, Turn: turn, Body: "answered"})
	return es
}

func TestThePlanIsNoneUnderTheTrigger(t *testing.T) {
	p := PlanCompaction(longRun(2, 100), order, testConstants, 0, 0)
	assert.Equal(t, VerdictNone, p.Verdict)
	assert.Less(t, p.Estimated, testConstants.TriggerTokens)
}

// Over the trigger the plan cuts at a turn start outside the kept turns,
// the elision stubs the span's results and keeps the person verbatim, and
// applying it brings the request under the trigger with the laws holding.
func TestThePlanElidesAtATurnStart(t *testing.T) {
	es := longRun(4, 800)
	p := PlanCompaction(es, order, testConstants, 0, 0)
	require.Equal(t, VerdictElide, p.Verdict, "estimated %d after %d", p.Estimated, p.After)
	assert.EqualValues(t, 0, p.From)
	cutEntry := es[p.Cut-1]
	assert.Equal(t, agentrun.KindUser, cutEntry.Kind)
	assert.Equal(t, "t4", cutEntry.Turn, "the earliest cut whose elision fits with a round's margin")
	assert.Len(t, p.Person, 3)
	assert.Contains(t, p.Elided, "The person said:\nquestion t1")
	assert.Contains(t, p.Elided, "was elided: 800 bytes")
	assert.Contains(t, p.Elided, "recall(")
	assert.NotContains(t, p.Elided, strings.Repeat("x", 100))
	assert.Equal(t, agentrun.LevelOwnModel, p.Label.Level, "the elision's label is the join of the span's")

	compact := agentrun.Entry{Ordinal: uint32(len(es) + 1), Kind: agentrun.KindCompact, Target: p.Cut, From: p.From, Form: FormElide, Body: p.Elided, Label: p.Label}
	es = append(es, compact)
	b := Fold(es, order)
	assert.LessOrEqual(t, Tokens(b), testConstants.TriggerTokens)
	assert.Empty(t, CheckCompaction(es))
	assert.Empty(t, CheckBound(es, order, testConstants, nil))
	again := PlanCompaction(es, order, testConstants, 0, 0)
	assert.Equal(t, VerdictNone, again.Verdict)

	// A fifth turn: the next plan's span starts at the previous cut.
	more := longRun(5, 800)[len(longRun(4, 800)):]
	for i := range more {
		more[i].Ordinal = uint32(len(es) + 1 + i)
	}
	es = append(es, more...)
	next := PlanCompaction(es, order, testConstants, 0, 0)
	require.Equal(t, VerdictElide, next.Verdict)
	assert.Equal(t, p.Cut, next.From)
	assert.Greater(t, next.Cut, p.Cut)
	assert.True(t, strings.HasPrefix(next.Elided, p.Elided), "the elision is rolling")
}

func TestThePlanSummarisesWhenElisionIsNotEnough(t *testing.T) {
	// Long words from the person: elision keeps them, so only a summary
	// fits.
	es := longRun(6, 200)
	for i := range es {
		if es[i].Kind == agentrun.KindUser {
			es[i].Body = strings.Repeat("w ", 400)
		}
	}
	p := PlanCompaction(es, order, testConstants, 0, 0)
	assert.Equal(t, VerdictSummarise, p.Verdict)
	assert.NotEmpty(t, p.Span)
	assert.LessOrEqual(t, p.After+testConstants.RoundMarginTokens, testConstants.TriggerTokens, "the summary's allowance fits with a round's margin")
	assert.Greater(t, p.Estimated-tokensOf(p.Span)+int64(len(p.Elided))/4, testConstants.TriggerTokens, "the elision would not have")
}

func tokensOf(ms []Message) (n int64) {
	for _, m := range ms {
		n += messageTokens(m)
	}
	return
}

func TestThePlanRefusesWhenTheKeptTurnsAreTooLarge(t *testing.T) {
	es := longRun(2, 4000)
	p := PlanCompaction(es, order, testConstants, 0, 0)
	assert.Equal(t, VerdictRefuse, p.Verdict)
}

// The provider's report anchors the estimate: what the provider counted
// stays counted, and only what the journal shows removed or added moves
// it. A request the journal cannot account for is refused, not compacted
// to nothing.
func TestTheReportedUsageAnchorsTheEstimate(t *testing.T) {
	es := longRun(3, 100)
	small := PlanCompaction(es, order, testConstants, 0, 0)
	assert.Equal(t, VerdictNone, small.Verdict)
	var lastReply, user3 uint32
	for _, e := range es {
		if e.Kind == agentrun.KindReply {
			lastReply = e.Ordinal
		}
		if e.Kind == agentrun.KindUser && e.Turn == "t3" {
			user3 = e.Ordinal
		}
	}
	// The provider says the last request was large: the plan believes it,
	// and since removing every turn but the kept one takes off a few
	// hundred tokens at most, no cut fits: the plan refuses rather than
	// compacting a span that leaves the size where it was (law L24).
	big := PlanCompaction(es, order, testConstants, 5000, lastReply)
	assert.Equal(t, VerdictRefuse, big.Verdict)
	assert.GreaterOrEqual(t, big.Estimated, int64(5000), "the report, plus the reply it does not count")
	// A compaction after the report stays anchored: the span's estimate
	// comes off the report, the summary's goes on.
	es = append(es, agentrun.Entry{Ordinal: uint32(len(es) + 1), Kind: agentrun.KindCompact, Target: user3, Body: "s"})
	after := PlanCompaction(es, order, testConstants, 5000, lastReply)
	assert.Less(t, after.Estimated, int64(5000))
	blind := Tokens(Fold(es, order))
	assert.Greater(t, after.Estimated, 4*blind, "scaled by the report's density, not the estimator's")
	assert.NotEmpty(t, CheckBound(es, order, testConstants, func([]agentrun.Entry) (int64, uint32) { return 5000, lastReply }), "L24 sees the anchored size")
	assert.Empty(t, CheckBound(es, order, testConstants, nil), "the blind estimate would not")
}

// The reviewer's reproduction: a small journal with a large report once
// planned an elision of nothing.
func TestAPlanNeverRemovesNothing(t *testing.T) {
	es := longRun(3, 100)
	var lastReply uint32
	for _, e := range es {
		if e.Kind == agentrun.KindReply {
			lastReply = e.Ordinal
		}
	}
	for _, reported := range []int64{700, 5000, 50000} {
		p := PlanCompaction(es, order, testConstants, reported, lastReply)
		if p.Verdict == VerdictElide || p.Verdict == VerdictSummarise {
			assert.NotEmpty(t, p.Span, "reported %d", reported)
			assert.Less(t, p.After, p.Estimated, "reported %d", reported)
		}
	}
}

// hasLaw says a breach of the law is in the list, by its code alone: L2
// is not L22.
func hasLaw(broken []string, law string) bool {
	for _, b := range broken {
		if strings.Contains(b, ": "+law+": ") {
			return true
		}
	}
	return false
}

func TestTheLawsCatchABrokenJournal(t *testing.T) {
	es := longRun(3, 10)
	user2 := es[1+8].Ordinal
	require.Equal(t, agentrun.KindUser, es[1+8].Kind)
	// From is not the previous cut, the cut does not increase, the person is
	// not verbatim.
	es = append(es,
		agentrun.Entry{Ordinal: uint32(len(es) + 1), Kind: agentrun.KindCompact, Target: user2, From: 0, Body: "nothing"},
		agentrun.Entry{Ordinal: uint32(len(es) + 2), Kind: agentrun.KindCompact, Target: user2, From: 3, Body: "question t1"})
	broken := CheckCompaction(es)
	assert.True(t, hasLaw(broken, "L22"), "%v", broken)
	assert.True(t, hasLaw(broken, "L23"), "%v", broken)
	assert.True(t, hasLaw(broken, "L25"), "%v", broken)
	assert.False(t, hasLaw(broken, "L2"), "%v", broken)
	// A cut inside a call's span: the person interjects between a call and
	// its result, and the cut lands on the interjection.
	es2 := []agentrun.Entry{
		{Ordinal: 1, Kind: agentrun.KindUser, Role: RoleUser, Body: "go", Turn: "t1"},
		{Ordinal: 2, Kind: agentrun.KindReply, Role: RoleAssistant, Calls: CallsBody([]openaichat.ToolCall{{Id: "c1", Name: "read"}}), Turn: "t1"},
		{Ordinal: 3, Kind: agentrun.KindUser, Role: RoleUser, Body: "wait", Turn: "t2"},
		{Ordinal: 4, Kind: agentrun.KindOutcome, Name: "c1", Role: RoleTool, Body: "r", Turn: "t2"},
		{Ordinal: 5, Kind: agentrun.KindCompact, Target: 3, Body: "go"},
	}
	broken2 := CheckCompaction(es2)
	assert.True(t, hasLaw(broken2, "L2"), "%v", broken2)
	assert.Contains(t, strings.Join(broken2, "\n"), "inside the span of call c1")
	// A cut that is not a user entry is the other L2 breach.
	es4 := longRun(2, 10)
	require.Equal(t, agentrun.KindRequest, es4[2].Kind)
	es4 = append(es4, agentrun.Entry{Ordinal: uint32(len(es4) + 1), Kind: agentrun.KindCompact, Target: es4[2].Ordinal, Body: "question t1"})
	assert.True(t, hasLaw(CheckCompaction(es4), "L2"))
	// The mark satisfies L25.
	es3 := longRun(2, 10)
	es3 = append(es3, agentrun.Entry{Ordinal: uint32(len(es3) + 1), Kind: agentrun.KindCompact, Target: 10, Body: "summary " + PersonSummarisedMark})
	assert.Empty(t, CheckCompaction(es3))
	// L25 reads the person's message as the request carried it: its edit.
	es5 := longRun(2, 10)
	es5 = append(es5,
		agentrun.Entry{Ordinal: uint32(len(es5) + 1), Kind: agentrun.KindEdit, Target: 2, Body: "question t1 (edited)"},
		agentrun.Entry{Ordinal: uint32(len(es5) + 2), Kind: agentrun.KindCompact, Target: 10, Body: "summary: question t1 (edited)"})
	assert.Empty(t, CheckCompaction(es5), "the edited body is verbatim")
	es5[len(es5)-1].Body = "summary: question t1"
	assert.True(t, hasLaw(CheckCompaction(es5), "L25"), "the original body is not what was shown")
}

func TestTheStubNamesTheDispatcherKey(t *testing.T) {
	m := Message{Ordinal: 7, Key: "llm-1#0", ToolCallId: "c1", Content: "big"}
	assert.Contains(t, Stub(m), "call llm-1#0 at ordinal 7")
	m.Key = ""
	assert.Contains(t, Stub(m), "call c1 at ordinal 7")
}

func TestPersonVerbatimPins(t *testing.T) {
	person := []Message{{Role: RoleUser, Content: "one"}, {Role: RoleUser, Content: strings.Repeat("b", 50)}, {Role: RoleUser, Content: "three"}}
	s := PersonVerbatim(person, 40)
	assert.Contains(t, s, "one")
	assert.NotContains(t, s, "bbbb")
	assert.Contains(t, s, PersonSummarisedMark)
	assert.NotContains(t, PersonVerbatim(person, 1000), PersonSummarisedMark)
}

func TestTheConstantsThrashCheck(t *testing.T) {
	assert.NoError(t, testConstants.Validate(100))
	assert.Error(t, testConstants.Validate(500))
	assert.Error(t, Constants{}.Validate(0))
	// The pinned words count: 600 - (50 + 100 + 50) leaves 400 for the
	// head and the pin; a pin of 1600 bytes is 400 tokens on its own.
	pinned := testConstants
	pinned.PinBytes = 1600
	assert.Error(t, pinned.Validate(10))
	pinned.PinBytes = 0
	assert.NoError(t, pinned.Validate(10))
}

// For any run the plan's cut is a live user entry that starts a turn after
// the previous cut, outside the kept turns, or a reply inside them that
// leaves the kept rounds and keeps its turn's message; the span is exactly
// the live messages in [from, cut) but that one, and the laws hold once
// it is applied.
func TestThePlanCutsOnATurnStartOrARound(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		turns := rapid.IntRange(1, 7).Draw(rt, "turns")
		rounds := rapid.IntRange(1, 6).Draw(rt, "rounds")
		bytes := rapid.IntRange(10, 1500).Draw(rt, "bytes")
		es := roundsRun(turns, rounds, bytes)
		c := testConstants
		c.KeepTurns = rapid.IntRange(1, 3).Draw(rt, "keep")
		c.KeepRounds = rapid.IntRange(1, 3).Draw(rt, "keep_rounds")
		p := PlanCompaction(es, order, c, 0, 0)
		if p.Verdict == VerdictNone || p.Verdict == VerdictRefuse {
			return
		}
		require.NotEmpty(rt, p.Span, "a compaction removes something")
		require.Less(rt, p.After, p.Estimated)
		cut := es[p.Cut-1]
		switch cut.Kind {
		case agentrun.KindUser:
			require.Zero(rt, p.Pinned)
			starts := 0
			for _, e := range es {
				if e.Kind == agentrun.KindUser && e.Ordinal >= p.Cut {
					starts++
				}
			}
			require.GreaterOrEqual(rt, starts, c.KeepTurns)
		case agentrun.KindReply:
			require.Equal(rt, VerdictElide, p.Verdict, "a round is cut by elision only")
			pinned := es[p.Pinned-1]
			require.Equal(rt, agentrun.KindUser, pinned.Kind)
			require.Equal(rt, cut.Turn, pinned.Turn)
			replies := 0
			for _, e := range es {
				if e.Kind == agentrun.KindReply && e.Ordinal >= p.Cut {
					replies++
				}
			}
			require.GreaterOrEqual(rt, replies, c.KeepRounds)
		default:
			rt.Fatalf("a cut on a %s entry", cut.Kind)
		}
		for _, m := range p.Span {
			require.Less(rt, m.Ordinal, p.Cut)
			require.GreaterOrEqual(rt, m.Ordinal, p.From)
			require.NotEqual(rt, p.Pinned, m.Ordinal)
		}
		compact := agentrun.Entry{Ordinal: uint32(len(es) + 1), Kind: agentrun.KindCompact, Target: p.Cut, From: p.From, Form: FormElide, Body: p.Elided, Label: p.Label}
		es = append(es, compact)
		require.Empty(rt, CheckCompaction(es))
		if p.Verdict == VerdictElide {
			// A summary's size is the summariser's; the elision is the plan's.
			require.Empty(rt, CheckBound(es, order, c, nil))
		}
	})
}

// Anchoring scales by the report's density and never undershoots the
// estimate: a base whose one large result the provider counted small
// does not drag what remains below what it is.
func TestAnchoringNeverUndershootsTheEstimate(t *testing.T) {
	kept := Message{Part: PartBody, Ordinal: 1, Role: RoleUser, Content: strings.Repeat("q", 400)}
	big := Message{Part: PartBody, Ordinal: 2, Role: RoleTool, ToolCallId: "c", Content: strings.Repeat("x", 40000)}
	base := Batch{Messages: []Message{kept, big}}
	b := Batch{Messages: []Message{kept}}
	n := Anchored(b, base, 2000)
	assert.GreaterOrEqual(t, n, Tokens(b), "never under the batch's own estimate")
	assert.Positive(t, n)
	dense := Anchored(b, base, 10*Tokens(base))
	assert.Equal(t, 10*Tokens(b), dense, "a denser tokenizer scales the estimate up")
	assert.Equal(t, Tokens(b), Anchored(b, Batch{}, 2000), "nothing to scale by")
}

// applyPlan appends the plan's compact entry as the elide act writes it.
func applyPlan(es []agentrun.Entry, p Plan) []agentrun.Entry {
	return append(es, agentrun.Entry{Ordinal: es[len(es)-1].Ordinal + 1, Kind: agentrun.KindCompact, Target: p.Cut, From: p.From, Form: FormElide, Body: p.Elided, Label: p.Label})
}

// The first turn of a conversation, grown past the trigger by its own
// rounds, is compacted at a round (ADR-0316 Update 2026-11-16): the cut
// sits on a reply, the latest rounds stay whole, the earlier results are
// stubs, and the person's message stays a body message at its own level,
// verbatim, before the rounds kept.
func TestTheFirstTurnIsElidedAtARound(t *testing.T) {
	es := roundsRun(1, 6, 600)
	p := PlanCompaction(es, order, testConstants, 0, 0)
	require.Equal(t, VerdictElide, p.Verdict, "estimated %d after %d", p.Estimated, p.After)
	cut := es[p.Cut-1]
	assert.Equal(t, agentrun.KindReply, cut.Kind)
	require.NotZero(t, p.Pinned)
	assert.Equal(t, "question t1", es[p.Pinned-1].Body)
	assert.NotContains(t, p.Elided, "question t1", "the pinned message is not carried twice")
	assert.Contains(t, p.Elided, "was elided: 600 bytes")
	assert.Empty(t, p.Person)
	// The pinned message is inside [From, Cut): its tier reaches the
	// compaction's label, as law L3 reads the span.
	tiered := slices.Clone(es)
	tiered[p.Pinned-1].Label.Tier = agentrun.TierE(3)
	assert.EqualValues(t, 3, PlanCompaction(tiered, order, testConstants, 0, 0).Label.Tier)

	es = applyPlan(es, p)
	b := Fold(es, order)
	var pinned *Message
	for i, m := range b.Messages {
		if m.Part == PartBody && m.Ordinal == p.Pinned {
			pinned = &b.Messages[i]
			assert.Equal(t, PartSummary, b.Messages[i-1].Part, "after the recalled band")
			assert.Greater(t, b.Messages[i+1].Ordinal, p.Cut-1, "before the entries from the cut")
		}
	}
	require.NotNil(t, pinned, "the person's message is in the body")
	assert.Equal(t, "question t1", pinned.Content, "verbatim, outside any band")
	assert.Equal(t, agentrun.LevelPerson, pinned.Label.Level, "at the person's level")
	assert.LessOrEqual(t, Tokens(b), testConstants.TriggerTokens)
	assert.Empty(t, CheckCompaction(es))
	assert.Empty(t, CheckBound(es, order, testConstants, nil))
	assert.Equal(t, VerdictNone, PlanCompaction(es, order, testConstants, 0, 0).Verdict)
	replies := 0
	for _, m := range b.Messages {
		if m.Part == PartBody && m.Role == RoleAssistant {
			replies++
		}
	}
	assert.GreaterOrEqual(t, replies, testConstants.KeepRounds, "the latest rounds stay whole")
}

// A later round cut in the same turn rolls the elision on and keeps the
// same message; one in the next turn moves the pin, and the earlier
// turn's message goes into the elision verbatim (law L25).
func TestRoundCutsRollAndMoveThePin(t *testing.T) {
	// A trigger large enough for the rolling elision's stubs: under
	// testConstants' 600 they alone would pass it.
	c := testConstants
	c.TriggerTokens = 2000
	es := roundsRun(1, 6, 1600)
	first := PlanCompaction(es, order, c, 0, 0)
	require.Equal(t, VerdictElide, first.Verdict)
	es = applyPlan(es, first)

	es = appendTurn(es, "t2", 6, 1600)
	next := PlanCompaction(es, order, c, 0, 0)
	require.Equal(t, VerdictElide, next.Verdict, "estimated %d after %d", next.Estimated, next.After)
	assert.Equal(t, first.Cut, next.From)
	assert.Greater(t, next.Cut, first.Cut)
	assert.True(t, strings.HasPrefix(next.Elided, first.Elided), "the elision is rolling")
	assert.Equal(t, "question t2", es[next.Pinned-1].Body)
	assert.Contains(t, next.Elided, "The person said:\nquestion t1", "the earlier pin is elided verbatim")
	es = applyPlan(es, next)
	assert.Empty(t, CheckCompaction(es))
	assert.Empty(t, CheckBound(es, order, c, nil))
	b := Fold(es, order)
	for _, m := range b.Messages {
		if m.Part == PartBody && m.Role == RoleUser {
			assert.Equal(t, "question t2", m.Content, "only the current turn's message is pinned")
		}
	}
}

// A call answered after a later reply makes that reply no cut: the cut
// would part the call from its result (law L2).
func TestARoundCutNeverPartsACallFromItsResult(t *testing.T) {
	es := roundsRun(1, 6, 600)
	// Move the first round's result after the second round's reply.
	var firstOutcome, secondReply int
	replies := 0
	for i, e := range es {
		switch {
		case e.Kind == agentrun.KindOutcome && firstOutcome == 0:
			firstOutcome = i
		case e.Kind == agentrun.KindReply:
			replies++
			if replies == 2 {
				secondReply = i
			}
		}
	}
	blocked := es[firstOutcome].Ordinal
	es[firstOutcome].Ordinal, es[secondReply].Ordinal = es[secondReply].Ordinal, blocked
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	require.Equal(t, agentrun.KindReply, es[blocked-1].Kind, "the second reply now comes before the first result")
	cuts := roundCuts(es, liveOf(Fold(es, order)), 0, testConstants.KeepRounds)
	require.NotEmpty(t, cuts, "the later rounds are still cuts")
	assert.NotContains(t, cuts, blocked, "the reply between a call and its result is no cut")
	p := PlanCompaction(es, order, testConstants, 0, 0)
	if p.Verdict == VerdictElide {
		assert.Empty(t, CheckCompaction(applyPlan(es, p)))
	}
}

func liveOf(b Batch) map[uint32]bool {
	live := make(map[uint32]bool)
	for _, m := range b.Messages {
		if m.Part == PartBody {
			live[m.Ordinal] = true
		}
	}
	return live
}

// The laws read a round cut: a reply is a cut (L2), and the message it
// keeps in the body is not asked of the elision (L25); a message of the
// same turn that is not the first is.
func TestTheLawsReadARoundCut(t *testing.T) {
	es := roundsRun(1, 3, 10)
	var reply2 uint32
	n := 0
	for _, e := range es {
		if e.Kind == agentrun.KindReply {
			if n++; n == 2 {
				reply2 = e.Ordinal
			}
		}
	}
	ok := append(slices.Clone(es), agentrun.Entry{Ordinal: es[len(es)-1].Ordinal + 1, Kind: agentrun.KindCompact, Target: reply2, Body: "elided"})
	assert.Empty(t, CheckCompaction(ok))
	// A second message of the person's in the turn, before the cut, is
	// not pinned: the elision must carry it.
	es[3] = agentrun.Entry{Ordinal: es[3].Ordinal, Kind: agentrun.KindUser, Role: RoleUser, Body: "and also", Turn: "t1"}
	bad := append(slices.Clone(es), agentrun.Entry{Ordinal: es[len(es)-1].Ordinal + 1, Kind: agentrun.KindCompact, Target: reply2, Body: "elided"})
	assert.True(t, hasLaw(CheckCompaction(bad), "L25"), "%v", CheckCompaction(bad))
}
