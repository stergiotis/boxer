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
	assert.True(t, q.Quick(), "a multiple choice is answered in the card")
	q = quickQuestion()
	q.Rest = []FormQuestion{{Text: "Colour?", Options: []QuestionOption{{Label: "red"}, {Label: "blue"}}, Multi: true}}
	assert.True(t, q.Quick(), "a form of several questions is answered in the card")
	q.Rest = append(q.Rest, q.Rest[0], q.Rest[0], q.Rest[0])
	assert.False(t, q.Quick(), "more questions than ask_user allows open the window")
	q = quickQuestion()
	q.Rest = []FormQuestion{{Text: "Why?", Options: []QuestionOption{{Label: "only one"}}}}
	assert.False(t, q.Quick(), "every question of the form is bounded")
	q = quickQuestion()
	q.Truncated = true
	assert.False(t, q.Quick(), "a form the host cut opens the window")
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
	require.True(t, qs.Decide(items[0].Key, false, [][]int{{1}}))
	assert.False(t, qs.Decide(items[0].Key, true, nil), "the first answer wins")
	a := qs.Status("chat", 3, key)
	assert.Equal(t, PhaseAnswered, a.Phase)
	assert.Equal(t, [][]int{{1}}, a.Choices)
	assert.Equal(t, PhaseGone, qs.Status("chat", 3, key).Phase, "a settled question is forgotten once read")
}

// Choices answer every question of the form: one for a single choice, at
// least one for a multiple, distinct and in range.
func TestAFormIsAnsweredWhole(t *testing.T) {
	q := quickQuestion()
	q.Rest = []FormQuestion{{Text: "Layers?", Options: []QuestionOption{{Label: "wind"}, {Label: "waves"}, {Label: "rain"}}, Multi: true}}
	for _, tc := range []struct {
		choices [][]int
		ok      bool
	}{
		{[][]int{{0}, {0, 2}}, true},
		{[][]int{{0}}, false},
		{[][]int{{0, 1}, {0}}, false},
		{[][]int{{0}, {}}, false},
		{[][]int{{0}, {1, 1}}, false},
		{[][]int{{0}, {3}}, false},
	} {
		assert.Equal(t, tc.ok, q.ValidChoices(tc.choices), "%v", tc.choices)
	}
	qs := NewQuestions(nil)
	key, _ := qs.Post("chat", 3, q, t0)
	assert.False(t, qs.Decide(questionKeyPrefix+key, false, [][]int{{0}}), "half a form is not an answer")
	require.True(t, qs.Decide(questionKeyPrefix+key, false, [][]int{{1}, {0, 2}}))
	assert.Equal(t, [][]int{{1}, {0, 2}}, qs.Status("chat", 3, key).Choices)
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
		assert.True(t, got.Truncated)
		assert.False(t, got.Quick(), "clipped options are not answered in place")
	}
}

func TestQuestionsOverTheBus(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	qs := NewQuestions(nil)
	svc, err := NewBusService(bus, &Service{Questions: qs, Levels: &Levels{}}, zerolog.Nop())
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
		require.True(t, qs.Decide(it.Key, true, nil))
	}
	a, err = Status(cli, key)
	require.NoError(t, err)
	assert.Equal(t, PhaseSkipped, a.Phase)
	_, refused, err = Post(cli, Question{Kind: "nope", Text: "x"})
	require.NoError(t, err)
	assert.NotEmpty(t, refused)
}
