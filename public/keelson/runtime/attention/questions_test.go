package attention

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func quickQuestion() Question {
	return Question{Kind: KindQuestion, Conversation: "c1", Title: "Wind field", Text: "Threshold on which speed?",
		Options: []QuestionOption{{Label: "10 m wind"}, {Label: "gusts"}}}
}

func TestQuickReplyBounds(t *testing.T) {
	q := quickQuestion()
	assert.True(t, q.Quick())
	q.Multi = true
	assert.False(t, q.Quick(), "multi-select opens the window")
	q = quickQuestion()
	q.More = 1
	assert.False(t, q.Quick(), "a form of several questions opens the window")
	q = quickQuestion()
	q.Options = q.Options[:1]
	assert.False(t, q.Quick())
	q = quickQuestion()
	q.Note = true
	assert.False(t, q.Quick(), "an answer in the person's words opens the window")
}

func TestAPostedQuestionIsAnItemAndTheFirstAnswerWins(t *testing.T) {
	qs := NewQuestions(nil)
	key, why := qs.Post("chat", 3, quickQuestion(), t0)
	require.Empty(t, why)
	var items []Item
	for it := range qs.WaitingItems() {
		items = append(items, it)
	}
	require.Len(t, items, 1)
	assert.Equal(t, KindQuestion, items[0].Kind)
	assert.Equal(t, "c1", items[0].Conversation)
	assert.NotContains(t, items[0].Summary, "Threshold", "the queue's line is host-written")

	assert.Equal(t, PhaseGone, qs.Status("chat", 4, key).Phase, "another window cannot read it")
	assert.Equal(t, PhaseWaiting, qs.Status("chat", 3, key).Phase)
	require.True(t, qs.Decide(items[0].Key, false, 1))
	assert.False(t, qs.Decide(items[0].Key, true, 0), "the first answer wins")
	a := qs.Status("chat", 3, key)
	assert.Equal(t, PhaseAnswered, a.Phase)
	assert.Equal(t, 1, a.Choice)
	assert.Equal(t, PhaseGone, qs.Status("chat", 3, key).Phase, "a settled question is forgotten once read")
}

func TestAWithdrawnQuestionLeavesTheQueue(t *testing.T) {
	qs := NewQuestions(nil)
	key, _ := qs.Post("chat", 3, quickQuestion(), t0)
	qs.Withdraw("chat", 3, key)
	for range qs.WaitingItems() {
		t.Fatal("withdrawn")
	}
}

func TestPostingIsBoundedByTheFloodLimit(t *testing.T) {
	qs := NewQuestions(NewFlood(1, 0))
	_, why := qs.Post("chat", 3, quickQuestion(), t0)
	require.Empty(t, why)
	_, why = qs.Post("chat", 3, quickQuestion(), t0)
	assert.Contains(t, why, "go on and say what you assumed")
}

func TestAPostedQuestionIsClipped(t *testing.T) {
	qs := NewQuestions(nil)
	q := quickQuestion()
	for range 8 {
		q.Options = append(q.Options, QuestionOption{Label: "x"})
	}
	_, why := qs.Post("chat", 3, q, t0)
	require.Empty(t, why)
	for it := range qs.WaitingItems() {
		got, _, _, ok := qs.Lookup(it.Key)
		require.True(t, ok)
		assert.Len(t, got.Options, MaxOptions)
		assert.False(t, got.Quick(), "clipped options are not answered in place")
	}
}

func TestQuestionsOverTheBus(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	qs := NewQuestions(nil)
	svc, err := NewBusService(bus, qs, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()
	cli := bus.NewClient("chat", ClientCaps("test"))
	key, refused, err := Post(cli, quickQuestion())
	require.NoError(t, err)
	require.Empty(t, refused)
	a, err := Status(cli, key)
	require.NoError(t, err)
	assert.Equal(t, PhaseWaiting, a.Phase)
	for it := range qs.WaitingItems() {
		require.True(t, qs.Decide(it.Key, true, 0))
	}
	a, err = Status(cli, key)
	require.NoError(t, err)
	assert.Equal(t, PhaseSkipped, a.Phase)
	_, refused, err = Post(cli, Question{Kind: "nope", Text: "x"})
	require.NoError(t, err)
	assert.NotEmpty(t, refused)
}
