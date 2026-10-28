package projection

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

func citedRun() []agentrun.Entry {
	v := func(ordinal uint32, id string, outcome string, observed string) agentrun.Entry {
		return agentrun.Entry{Ordinal: ordinal, Kind: agentrun.KindVerdict, Key: "c-2:0", Name: id, Turn: "t1", Body: observed,
			Arguments: `{"expect":"3","id":"` + id + `","says":"three words"}`,
			Verdict:   &agentrun.Verdict{Form: "value", Outcome: outcome, RestsOn: []string{"c-1:0"}, Result: 7, Digest: "d1", Source: "s1", Endpoint: "clickhouse:h"}}
	}
	return []agentrun.Entry{
		{Ordinal: 1, Kind: agentrun.KindUser, Turn: "t1", Body: "how many words?"},
		{Ordinal: 2, Kind: agentrun.KindIntent, Key: "c-1:0", Name: "call_operation", Effect: agentrun.EffectNone, Turn: "t1"},
		{Ordinal: 3, Kind: agentrun.KindOutcome, Key: "c-1:0", Name: "c1", Role: RoleTool, Turn: "t1", Body: `{"result":{"result_id":7,"words":3}}`},
		{Ordinal: 4, Kind: agentrun.KindIntent, Key: "c-2:0", Name: "cite", Effect: agentrun.EffectNone, Turn: "t1"},
		v(5, "a1", VerdictHeld, "3"),
		v(6, "a2", VerdictFailed, "3"),
		{Ordinal: 7, Kind: agentrun.KindOutcome, Key: "c-2:0", Name: "c2", Role: RoleTool, Turn: "t1", Body: `{"assertions":[]}`},
		{Ordinal: 8, Kind: agentrun.KindReply, Turn: "t1", Body: "Three words [a1], not four [a2]; see `[x9]` and [x] done.\n\n1. first\n\nA 2024 note: 1,000 items."},
	}
}

func TestReferencesAreLettersThenDigits(t *testing.T) {
	assert.Equal(t, []string{"a1", "a2"}, References(citedRun()[7].Body), "a fenced [x9], a checkbox [x] and a repeat are not references")
	assert.Nil(t, References("[^1] [text](u) [12] [a1](url)"))
	marked := MarkReferences("see [a1] and `[a1]` and [a1](u)\n```\n[a1]\n```\n[b2]", func(id string) string { return "[" + id + " ✓]" })
	assert.Equal(t, "see [a1 ✓] and `[a1]` and [a1](u)\n```\n[a1]\n```\n[b2 ✓]", marked, "code and links are left alone")
}

func TestTheCitationLawsHoldOnACitedReply(t *testing.T) {
	es := citedRun()
	assert.Empty(t, CheckCitations(es))
	assert.Equal(t, []string{"L38: reply 8 states 2024, which nothing the turn read accounts for", "L38: reply 8 states 1,000, which nothing the turn read accounts for"}, Uncited(es),
		"the list marker and the cited 3 are accounted for; the year and the count are not")
	vs := Verdicts(es, "t1")
	assert.Len(t, vs, 2)
	assert.Equal(t, "a1", vs[0].Name)
}

func TestADanglingReferenceBreaksL36(t *testing.T) {
	es := citedRun()
	es[7].Body = "Three words [a1], maybe [a3]."
	assert.Equal(t, []string{"L36: reply 8 refers to [a3], which no verdict of its turn names before it"}, CheckCitations(es))
	es = citedRun()
	es[7].Turn = "t2"
	assert.Len(t, CheckCitations(es), 2, "a reply of another turn cannot refer to this turn's verdicts")
}

func TestAVerdictWithoutItsSourcesBreaksL37(t *testing.T) {
	es := citedRun()
	es[4].Verdict.RestsOn = []string{"c-9:0"}
	es[5].Verdict.Endpoint = ""
	broken := CheckCitations(es)
	assert.Contains(t, broken, "L37: verdict 5 rests on c-9:0, which is no tool call of the run")
	assert.Contains(t, broken, "L37: verdict 6 ran without its statement digest, its source's or its endpoint")
	es = citedRun()
	es[1].Effect = agentrun.EffectDocument
	assert.Contains(t, CheckCitations(es), "L37: verdict 5 rests on c-1:0, a command")
	es = citedRun()
	es[4].Verdict.Outcome = VerdictUnchecked
	es[4].Verdict.RestsOn = nil
	assert.Empty(t, CheckCitations(es), "a verdict that did not run owes no sources")
}

func TestTheLatestVerdictPerAssertionStands(t *testing.T) {
	es := citedRun()
	es = append(es, agentrun.Entry{Ordinal: 9, Kind: agentrun.KindVerdict, Key: "c-2:0", Name: "a2", Turn: "t1", Form: "recheck", Body: "4",
		Verdict: &agentrun.Verdict{Form: "value", Outcome: VerdictMoved, RestsOn: []string{"c-1:0"}, Digest: "d1", Source: "s1", Endpoint: "clickhouse:h"}})
	vs := Verdicts(es, "t1")
	assert.Len(t, vs, 2)
	assert.Equal(t, VerdictMoved, vs[1].Verdict.Outcome, "the recheck replaces the first verdict in place")
}

func TestTheSelectionRuleOverVerdicts(t *testing.T) {
	cand := func(o uint32, key string) agentrun.Entry {
		return agentrun.Entry{Ordinal: o, Kind: agentrun.KindCandidate, Key: key}
	}
	v := func(o uint32, key string, outcome string) agentrun.Entry {
		return agentrun.Entry{Ordinal: o, Kind: agentrun.KindVerdict, Key: key, Verdict: &agentrun.Verdict{Outcome: outcome}}
	}
	es := []agentrun.Entry{cand(1, "k1"), cand(2, "k2"), cand(3, "k3"),
		v(4, "k1", VerdictHeld), v(5, "k1", VerdictHeld), v(6, "k1", VerdictFailed),
		v(7, "k2", VerdictHeld), v(8, "k2", VerdictUnchecked),
		v(9, "k3", VerdictHeld)}
	o, ok := SelectCandidate(es)
	assert.True(t, ok)
	assert.Equal(t, uint32(3), o, "k1 failed once; k2 and k3 held once each and k3 has fewer unchecked")
	_, ok = SelectCandidate([]agentrun.Entry{cand(1, "k1"), v(2, "k1", VerdictFailed)})
	assert.False(t, ok)
	o, _ = SelectCandidate([]agentrun.Entry{cand(1, "k1"), cand(2, "k2")})
	assert.Equal(t, uint32(1), o, "without verdicts the lowest ordinal stands")
}
