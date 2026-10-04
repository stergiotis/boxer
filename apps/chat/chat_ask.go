package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Questions: with the toggle on, the model may ask the person structured
// questions through ask_user, a tool of the app's own. It reaches no other
// window, so it needs no grant: the call waits inside the turn for the form
// drawn in the transcript, and the person's answer is the call's result.
// Nothing here touches the UI; chat_ask_render.go draws the form.

// QuestionsSeed turns Questions on in a new window (ADR-0009 seed
// variable).
var QuestionsSeed = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_QUESTIONS",
	Default:     "false",
	Description: "turn on Questions in a new chat window: the model may ask the person structured questions as an inline form (ask_user); for scenes and demos",
	Category:    env.CategoryE("boxer-chat"),
})

const (
	maxAskQuestions = 4
	minAskOptions   = 2
	maxAskOptions   = 6
	// maxHeaderRunes bounds a question's header as shown.
	maxHeaderRunes = 16
)

// questionsPrompt is the system message's part for a conversation with
// Questions on.
const questionsPrompt = `You can ask the person structured questions with ask_user; it shows a small form and waits for the answer.
- Ask when a choice is the person's to make and the answer changes what you do next: a preference, a scope, which of several readings they meant. When a sensible default exists, take it and say so instead.
- Group related questions into one call, at most 4, each with 2 to 6 options; set multi_select when several options may apply. Put your recommendation first and say so in its description.
- The person may add a note to a chosen option, write an answer of their own under other, or skip the form; after a skip, go on with your best judgement and say what you assumed.`

// askTool is ask_user's definition.
func askTool() (t openaichat.Tool) {
	return openaichat.Tool{Name: "ask_user",
		Description: "Ask the person one to " + strconv.Itoa(maxAskQuestions) + " questions as a form of options, shown inline in the conversation. Waits for the person's answer.",
		Parameters: toolSchema(`{"type":"object","properties":{"questions":{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxAskQuestions) + `,"items":{"type":"object","properties":{` +
			`"question":{"type":"string","description":"the question, a full sentence ending in a question mark"},` +
			`"header":{"type":"string","description":"a label of one or two words shown as a chip, e.g. \"Scope\""},` +
			`"multi_select":{"type":"boolean","description":"true when several options may be chosen"},` +
			`"options":{"type":"array","minItems":` + strconv.Itoa(minAskOptions) + `,"maxItems":` + strconv.Itoa(maxAskOptions) + `,"items":{"type":"object","properties":{` +
			`"label":{"type":"string","description":"a few words, the choice itself"},` +
			`"description":{"type":"string","description":"one line: what choosing it means"}},"required":["label"],"additionalProperties":false}}},` +
			`"required":["question","options"],"additionalProperties":false}}},"required":["questions"],"additionalProperties":false}`)}
}

type askOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type askQuestion struct {
	Question    string      `json:"question"`
	Header      string      `json:"header,omitempty"`
	MultiSelect bool        `json:"multi_select,omitempty"`
	Options     []askOption `json:"options"`
}

// parseQuestions reads ask_user's arguments; reason, when not empty, says
// what is wrong with them, for the model to fix.
func parseQuestions(args map[string]any) (qs []askQuestion, reason string) {
	raw, has := args["questions"]
	if !has {
		return nil, "ask_user takes questions, a list of 1 to " + strconv.Itoa(maxAskQuestions)
	}
	b, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(b, &qs)
	}
	if err != nil {
		return nil, "questions do not fit the schema: " + err.Error()
	}
	if len(qs) < 1 || len(qs) > maxAskQuestions {
		return nil, "ask 1 to " + strconv.Itoa(maxAskQuestions) + " questions in one call, not " + strconv.Itoa(len(qs))
	}
	for i := range qs {
		q := &qs[i]
		where := "question " + strconv.Itoa(i+1)
		q.Question = strings.TrimSpace(q.Question)
		if q.Question == "" {
			return nil, where + " has no text"
		}
		q.Header = cleanHeader(q.Header)
		if len(q.Options) < minAskOptions || len(q.Options) > maxAskOptions {
			return nil, where + " needs " + strconv.Itoa(minAskOptions) + " to " + strconv.Itoa(maxAskOptions) + " options, not " + strconv.Itoa(len(q.Options))
		}
		seen := make(map[string]bool, len(q.Options))
		for j := range q.Options {
			o := &q.Options[j]
			o.Label, o.Description = strings.TrimSpace(o.Label), strings.TrimSpace(o.Description)
			switch {
			case o.Label == "":
				return nil, where + ", option " + strconv.Itoa(j+1) + " has no label"
			case seen[o.Label]:
				return nil, where + " has the option " + strconv.Quote(o.Label) + " twice"
			}
			seen[o.Label] = true
		}
	}
	return
}

// cleanHeader is a header as shown: one line, at most maxHeaderRunes.
func cleanHeader(s string) (h string) {
	h = strings.Join(strings.Fields(s), " ")
	if r := []rune(h); len(r) > maxHeaderRunes {
		h = string(r[:maxHeaderRunes-1]) + "…"
	}
	return
}

// askChoice is one chosen option and the person's note on it.
type askChoice struct {
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

type askAnswer struct {
	Question string      `json:"question"`
	Chosen   []askChoice `json:"chosen"`
	// Other is an answer the person wrote instead of, or beside, the options.
	Other string `json:"other,omitempty"`
}

// askReply is what the model reads of ask_user.
type askReply struct {
	Answers []askAnswer `json:"answers,omitempty"`
	Skipped bool        `json:"skipped,omitempty"`
	Then    string      `json:"then,omitempty"`
}

const skippedThen = "the person skipped these questions: go on with your best judgement and say what you assumed"

// openAsk is a question waiting for the person. questions and reply are
// fixed when it is made; form is the render goroutine's alone.
type openAsk struct {
	seq       uint64
	questions []askQuestion
	reply     chan askReply
	form      askForm
}

// asker hands a question from the turn's goroutine to the render goroutine
// and the answer back. One turn runs at a time and its tool calls run in
// order, so at most one question is open.
type asker struct {
	mu   sync.Mutex
	open *openAsk
	seq  uint64
}

// ask shows qs and waits for the answer, or for the turn to end.
func (inst *asker) ask(ctx context.Context, qs []askQuestion) (r askReply, err error) {
	o := &openAsk{questions: qs, reply: make(chan askReply, 1), form: newAskForm(qs)}
	inst.mu.Lock()
	inst.seq++
	o.seq = inst.seq
	inst.open = o
	inst.mu.Unlock()
	defer func() {
		inst.mu.Lock()
		if inst.open == o {
			inst.open = nil
		}
		inst.mu.Unlock()
	}()
	select {
	case r = <-o.reply:
	case <-ctx.Done():
		err = ctx.Err()
	}
	return
}

// current is the open question, nil when none is.
func (inst *asker) current() (o *openAsk) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.open
}

// askUser runs one ask_user call.
func (inst *coordinator) askUser(ctx context.Context, args map[string]any) (content string, activity string) {
	qs, reason := parseQuestions(args)
	if reason != "" {
		return "error: " + reason, "ask_user: " + reason
	}
	r, err := inst.ask.ask(ctx, qs)
	if err != nil {
		return "error: the question was withdrawn: " + err.Error(), "question withdrawn"
	}
	b, _ := json.Marshal(r)
	return string(b), askActivity(qs, r)
}

// askActivity is the transcript's line for an answered ask_user: each
// question by its header, and what the person chose.
func askActivity(qs []askQuestion, r askReply) (s string) {
	if r.Skipped {
		return "asked " + plural(len(qs), "question") + " · skipped"
	}
	parts := make([]string, 0, len(r.Answers))
	for i, a := range r.Answers {
		name := a.Question
		if i < len(qs) && qs[i].Header != "" {
			name = qs[i].Header
		}
		picks := make([]string, 0, len(a.Chosen)+1)
		for _, c := range a.Chosen {
			p := c.Label
			if c.Note != "" {
				p += " (" + c.Note + ")"
			}
			picks = append(picks, p)
		}
		if a.Other != "" {
			picks = append(picks, "“"+a.Other+"”")
		}
		parts = append(parts, name+": "+strings.Join(picks, ", "))
	}
	return "asked " + plural(len(qs), "question") + " · " + strings.Join(parts, "; ")
}

func plural(n int, noun string) (s string) {
	s = strconv.Itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return
}

// systemPrompt is the first message of a conversation with Apps or
// Questions on.
func systemPrompt(apps bool, questions bool) (s string) {
	switch {
	case apps && questions:
		return coordinatorPrompt + "\n\n" + questionsPrompt
	case apps:
		return coordinatorPrompt
	default:
		return questionsPrompt
	}
}

// askForm is the person's side of an open question: per question, which
// options are chosen and the note on each, and the answer of their own.
type askForm struct {
	chosen [][]bool
	notes  [][]string
	other  []string
	// single is the chosen option of a single-choice question, -1 for none;
	// chosen mirrors it.
	single []int
	// incomplete is set by a Submit that left a question unanswered.
	incomplete bool
	sent       bool
}

func newAskForm(qs []askQuestion) (f askForm) {
	f = askForm{chosen: make([][]bool, len(qs)), notes: make([][]string, len(qs)), other: make([]string, len(qs)), single: make([]int, len(qs))}
	for i, q := range qs {
		f.chosen[i], f.notes[i], f.single[i] = make([]bool, len(q.Options)), make([]string, len(q.Options)), -1
	}
	return
}

// reply is the form as the model reads it; ok is false while a question
// has neither a chosen option nor an answer of the person's own.
func (inst *askForm) reply(qs []askQuestion) (r askReply, ok bool) {
	ok = true
	for i, q := range qs {
		a := askAnswer{Question: q.Question, Chosen: []askChoice{}, Other: strings.TrimSpace(inst.other[i])}
		for j, o := range q.Options {
			if inst.chosen[i][j] {
				a.Chosen = append(a.Chosen, askChoice{Label: o.Label, Note: strings.TrimSpace(inst.notes[i][j])})
			}
		}
		if len(a.Chosen) == 0 && a.Other == "" {
			ok = false
		}
		r.Answers = append(r.Answers, a)
	}
	return
}

// submit sends the form once it answers every question.
func (inst *openAsk) submit() {
	if inst.form.sent {
		return
	}
	r, ok := inst.form.reply(inst.questions)
	if !ok {
		inst.form.incomplete = true
		return
	}
	inst.form.sent = true
	inst.reply <- r
}

// skip answers that the person chose not to.
func (inst *openAsk) skip() {
	if inst.form.sent {
		return
	}
	inst.form.sent = true
	inst.reply <- askReply{Skipped: true, Then: skippedThen}
}
