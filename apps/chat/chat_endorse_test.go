package chat

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// What a model wrote re-enters marked (ADR-0311 §SD4, laws L16 and L17):
// after the model writes the artefact, a read of it is journaled with the
// writer's run and projected in band at the model's level; a read before
// any model write is not.
func TestAnArtefactReadAfterAModelWriteReentersMarked(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("r0", "artefact_read", `{}`),
		toolCall("w1", "artefact_write", `{"base_revision":0,"text":"# Plan\n"}`),
		toolCall("r1", "artefact_read", `{}`),
		{Content: "Read it back.", FinishReason: "stop"},
	}}
	coord, cli, req := questionsRig(t, model)
	coord.offer(false, false)
	conv := newConversation()
	conv.art.setPolicy(artPolicy{write: true})
	coord.offerArtefact(conv.art)
	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	var reads []agentrun.Entry
	for _, e := range coord.journalNow().Entries() {
		if e.Kind == agentrun.KindOutcome && strings.HasPrefix(e.Name, "r") {
			reads = append(reads, e)
		}
	}
	require.Len(t, reads, 2)
	assert.Empty(t, reads[0].WriterRun, "an empty artefact is nobody's")
	assert.Equal(t, coord.conversation, reads[1].WriterRun, "the head is the model's")
	last := model.seen[3].Messages
	wrapped := last[len(last)-1]
	assert.Equal(t, openaichat.ChatRoleTool, wrapped.Role)
	assert.True(t, strings.HasPrefix(wrapped.Content, `<recalled source="self" run="`+coord.conversation+`"`), wrapped.Content)
	assert.True(t, strings.HasSuffix(wrapped.Content, "</recalled>"))
	b := projection.Fold(coord.journalNow().Entries(), sectionOrder())
	for _, m := range b.Messages {
		if m.Ordinal == reads[1].Ordinal {
			assert.Equal(t, agentrun.LevelOwnModel, m.Label.Level, "L16")
		}
	}
}

// The person's endorsement lifts the artefact's head for publishing
// (ADR-0311 §SD3): the entry names the revision and the digest shown; a
// model's later edit voids it, the person's own edit does not, and the
// next endorsement records what changed.
func TestEndorsingTheArtefactIsVoidedByAModelEdit(t *testing.T) {
	coord, _, _ := questionsRig(t, &scriptedModel{})
	conv := newConversation()
	conv.art.setPolicy(artPolicy{write: true})
	coord.offerArtefact(conv.art)
	ctx := context.Background()
	_, err := coord.openJournal(ctx, false)
	require.NoError(t, err)
	_, err = conv.art.commit(0, artRevision{text: "# Plan\n\nFirst.\n", source: revSourceModel, turn: "t1", tool: "artefact_write"})
	require.NoError(t, err)
	assert.False(t, coord.endorsedHead())
	n, text := conv.art.head()
	require.NoError(t, coord.endorseArtefact(ctx, n, text))
	assert.True(t, coord.endorsedHead())
	_, err = conv.art.commit(1, artRevision{text: "# Plan\n\nFirst, mine.\n", source: revSourcePerson})
	require.NoError(t, err)
	assert.True(t, coord.endorsedHead(), "the person's own edit is theirs and leaves the endorsement standing")
	_, err = conv.art.commit(2, artRevision{text: "# Plan\n\nSecond.\n", source: revSourceModel, turn: "t2", tool: "artefact_edit"})
	require.NoError(t, err)
	assert.False(t, coord.endorsedHead(), "a model edit voids it")
	n, text = conv.art.head()
	require.NoError(t, coord.endorseArtefact(ctx, n, text))
	var endorsements []endorseBody
	for _, e := range coord.journalNow().Entries() {
		if e.Kind == agentrun.KindEndorse {
			assert.Equal(t, endorseKeyArtefact, e.Key)
			assert.Equal(t, endorsedByPerson, e.Name)
			assert.Equal(t, agentrun.LevelPerson, e.Label.Level)
			var b endorseBody
			require.NoError(t, jsonUnmarshal(e.Body, &b))
			endorsements = append(endorsements, b)
		}
	}
	require.Len(t, endorsements, 2)
	assert.Equal(t, "r1", endorsements[0].Epoch)
	assert.Equal(t, "none", endorsements[0].Previous)
	assert.Equal(t, "r3", endorsements[1].Epoch)
	assert.Equal(t, endorsements[0].Digest, endorsements[1].Previous)
	assert.Equal(t, "lines 3-3 changed", endorsements[1].Diff)
	assert.Equal(t, []string{"publish"}, endorsements[1].Classes)
}

// Law L15 over a journal: a consequential act that completed with
// neither an endorsement before its outcome nor a grant in force is a
// breach; an endorse entry under its key written before the outcome, or
// the person's grant earlier in the journal, is the consent; an
// endorsement written after the outcome is not.
func TestLawL15FindsAnUnendorsedConsequentialAct(t *testing.T) {
	intent := func(ord uint32, key string) agentrun.Entry {
		return agentrun.Entry{Ordinal: ord, Kind: agentrun.KindIntent, Key: key, Name: "call_operation", Effect: agentrun.EffectConsequential, Body: `{"operation":"publish_result"}`}
	}
	done := func(ord uint32, key string) agentrun.Entry {
		return agentrun.Entry{Ordinal: ord, Kind: agentrun.KindOutcome, Key: key, Body: `{"phase":"completed"}`}
	}
	endorse := endorseEntry("k1", "t1", `{"operation":"publish_result"}`, agentrun.EffectConsequential)
	assert.Equal(t, endorsedByPerson, endorse.Name)
	assert.Equal(t, agentrun.KindEndorse, endorse.Kind)

	broken := checkEndorsed([]agentrun.Entry{intent(1, "k1"), done(2, "k1"),
		intent(3, "k2"), {Ordinal: 4, Kind: agentrun.KindOutcome, Key: "k2", Body: `{"phase":"refused","reason":"no"}`}})
	require.Len(t, broken, 1, "a completed act without consent; a refused one is no act")
	assert.Contains(t, broken[0], "k1")
	assert.Contains(t, broken[0], "L15")

	before := endorse
	before.Ordinal = 2
	assert.Empty(t, checkEndorsed([]agentrun.Entry{intent(1, "k1"), before, done(3, "k1")}), "endorsed before the outcome")
	after := endorse
	after.Ordinal = 3
	assert.Len(t, checkEndorsed([]agentrun.Entry{intent(1, "k1"), done(2, "k1"), after}), 1, "an endorsement after the outcome is not consent")

	grant := agentrun.Entry{Ordinal: 1, Kind: agentrun.KindAnswered, Key: "h1", Name: endorsedByPerson, Form: grantForm, Body: `{"task":"t","handle":"h1"}`}
	under := func(ord uint32, key string, handle string) agentrun.Entry {
		e := intent(ord, key)
		e.Form = handle
		return e
	}
	assert.Empty(t, checkEndorsed([]agentrun.Entry{grant, under(2, "k1", "h1"), done(3, "k1")}), "the person's grant for the intent's task is the consent")
	assert.Len(t, checkEndorsed([]agentrun.Entry{grant, under(2, "k1", "h2"), done(3, "k1")}), 1, "a grant for another task is not")
	assert.Len(t, checkEndorsed([]agentrun.Entry{grant, intent(2, "k1"), done(3, "k1")}), 1, "an intent under no task is not covered by any grant")
	hostGrant := grant
	hostGrant.Name = grantedByHost
	assert.Len(t, checkEndorsed([]agentrun.Entry{hostGrant, under(2, "k1", "h1"), done(3, "k1")}), 1, "a grant the host gave in the person's place consents to nothing")
	assert.Len(t, checkEndorsed([]agentrun.Entry{under(1, "k1", "h1"), done(2, "k1"), {Ordinal: 3, Kind: agentrun.KindAnswered, Key: "h1", Name: endorsedByPerson, Form: grantForm}}), 1, "a grant given later is not")

	other := endorseEntry("k1", "t1", `{"operation":"publish_result","name":"other"}`, agentrun.EffectConsequential)
	other.Ordinal = 2
	broken = checkEndorsed([]agentrun.Entry{intent(1, "k1"), other, done(3, "k1")})
	require.Len(t, broken, 1, "an endorsement over other arguments than the act ran with is not consent")
	assert.Contains(t, broken[0], "not over the arguments")
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
