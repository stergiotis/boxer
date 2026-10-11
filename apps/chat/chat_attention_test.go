package chat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

// fakeQueue is the host's queue in a test: one question, answered by the
// test.
type fakeQueue struct {
	mu        sync.Mutex
	posted    []attention.Question
	answer    attention.Answer
	refuse    string
	withdrawn int
}

func (inst *fakeQueue) post(q attention.Question) (key string, refused string, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.refuse != "" {
		return "", inst.refuse, nil
	}
	inst.posted = append(inst.posted, q)
	inst.answer = attention.Answer{Phase: attention.PhaseWaiting}
	return "k1", "", nil
}

func (inst *fakeQueue) status(string) (a attention.Answer, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.answer, nil
}

func (inst *fakeQueue) withdraw(string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.withdrawn++
}

func (inst *fakeQueue) decide(a attention.Answer) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.answer = a
}

func twoOptions() []askQuestion {
	return []askQuestion{{Header: "Speed", Question: "Threshold on which speed?", Options: []askOption{{Label: "10 m wind"}, {Label: "gusts"}}}}
}

var asQuestion = hostAsk{kind: attention.KindQuestion, conversation: "c1"}

func TestAQuestionAnsweredInTheHostsCard(t *testing.T) {
	q := &fakeQueue{}
	a := &asker{host: q}
	done := make(chan askReply, 1)
	go func() {
		r, err := a.askVia(context.Background(), twoOptions(), asQuestion)
		assert.NoError(t, err)
		done <- r
	}()
	require.Eventually(t, func() bool { return a.current() != nil }, time.Second, 5*time.Millisecond, "the window shows it too")
	q.decide(attention.Answer{Phase: attention.PhaseAnswered, Choice: 1})
	r := <-done
	require.Len(t, r.Answers, 1)
	assert.Equal(t, "gusts", r.Answers[0].Chosen[0].Label)
	q.mu.Lock()
	defer q.mu.Unlock()
	require.Len(t, q.posted, 1)
	assert.Equal(t, "c1", q.posted[0].Conversation)
	assert.True(t, q.posted[0].Quick())
}

func TestAQuestionAnsweredInTheWindowIsWithdrawn(t *testing.T) {
	q := &fakeQueue{}
	a := &asker{host: q}
	done := make(chan askReply, 1)
	go func() {
		r, _ := a.askVia(context.Background(), twoOptions(), asQuestion)
		done <- r
	}()
	require.Eventually(t, func() bool { return a.current() != nil }, time.Second, 5*time.Millisecond)
	a.current().skip()
	r := <-done
	assert.True(t, r.Skipped)
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return q.withdrawn == 1
	}, time.Second, 5*time.Millisecond, "the card is taken back")
}

func TestARefusedQuestionAnswersAtOnce(t *testing.T) {
	q := &fakeQueue{refuse: "the person is not taking questions now; go on and say what you assumed"}
	a := &asker{host: q}
	r, err := a.askVia(context.Background(), twoOptions(), asQuestion)
	require.NoError(t, err)
	assert.True(t, r.Skipped)
	assert.Contains(t, r.Then, "not taking questions")
	assert.Nil(t, a.current(), "the window's form is taken down")
}

func TestAnExitApprovalOpensItsWindow(t *testing.T) {
	q := questionOf(twoOptions(), hostAsk{kind: attention.KindExitApproval, note: true})
	assert.False(t, q.Quick(), "a justification is the person's words")
}
