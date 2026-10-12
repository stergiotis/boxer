package chat

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

// A question the model asks also waits in the host's queue (ADR-0324 §SD1,
// §SD5), so the person sees it from any window and answers a form of
// choices in its card. The form in the window stays; the first answer wins.

// hostQueueI is the host's queue as the asker uses it.
type hostQueueI interface {
	post(q attention.Question) (key string, refused string, err error)
	status(key string) (a attention.Answer, err error)
	withdraw(key string)
}

type busQueue struct{ bus app.BusI }

func (inst busQueue) post(q attention.Question) (key string, refused string, err error) {
	return attention.Post(inst.bus, q)
}

func (inst busQueue) status(key string) (a attention.Answer, err error) {
	return attention.Status(inst.bus, key)
}

func (inst busQueue) withdraw(key string) { _ = attention.Withdraw(inst.bus, key) }

// hostAsk is what the host's card shows of a question beyond its text.
type hostAsk struct {
	kind         string
	conversation string
	title        string
	tainted      bool
	// note is whether an answer needs the person's words; its card only
	// opens the window.
	note bool
}

// hostPollInterval is how often the asker reads the host's answer.
const hostPollInterval = 250 * time.Millisecond

// questionOf is qs as the host's card shows it: the whole form, its first
// question in the Question's own fields.
func questionOf(qs []askQuestion, via hostAsk) (q attention.Question) {
	q = attention.Question{Kind: via.kind, Conversation: via.conversation, Title: via.title, Tainted: via.tainted, Note: via.note}
	for i, aq := range qs {
		fq := attention.FormQuestion{Header: aq.Header, Text: aq.Question, Multi: aq.MultiSelect}
		for _, o := range aq.Options {
			fq.Options = append(fq.Options, attention.QuestionOption{Label: o.Label, Description: o.Description})
		}
		if i == 0 {
			q.Header, q.Text, q.Options, q.Multi = fq.Header, fq.Text, fq.Options, fq.Multi
			continue
		}
		q.Rest = append(q.Rest, fq)
	}
	return
}

// replyOf is the host's answer as the model reads it; ok is false while the
// question waits.
func replyOf(qs []askQuestion, a attention.Answer) (r askReply, ok bool) {
	switch a.Phase {
	case attention.PhaseSkipped:
		return askReply{Skipped: true, Then: skippedThen}, true
	case attention.PhaseAnswered:
		if len(qs) == 0 || len(a.Choices) != len(qs) {
			return
		}
		for i, q := range qs {
			ans := askAnswer{Question: q.Question}
			for _, c := range a.Choices[i] {
				if c < 0 || c >= len(q.Options) {
					return askReply{}, false
				}
				ans.Chosen = append(ans.Chosen, askChoice{Label: q.Options[c].Label})
			}
			r.Answers = append(r.Answers, ans)
		}
		return r, true
	}
	return
}

// watchHost posts qs to the host's queue and hands its answer to reply,
// until ctx ends; it withdraws the question when it returns. The host's
// refusal is an answer of its own: the model reads why and goes on. A host
// without the queue leaves the question to the window.
func watchHost(ctx context.Context, host hostQueueI, qs []askQuestion, via hostAsk, reply chan<- askReply) {
	send := func(r askReply) {
		select {
		case reply <- r:
		default:
		}
	}
	key, refused, err := host.post(questionOf(qs, via))
	switch {
	case err != nil:
		return
	case refused != "":
		send(askReply{Skipped: true, Then: refused})
		return
	}
	defer host.withdraw(key)
	t := time.NewTicker(hostPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a, serr := host.status(key)
		if serr != nil || a.Phase == attention.PhaseGone || a.Phase == attention.PhaseWithdrawn {
			return
		}
		if r, ok := replyOf(qs, a); ok {
			send(r)
			return
		}
	}
}
