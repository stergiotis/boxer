package chat

import (
	"context"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

const twoQuestions = `{"title":"Asking about the report","questions":[` +
	`{"question":"Which sections?","header":"Sections","multi_select":true,"options":[{"label":"Summary"},{"label":"Costs","description":"the table by month"},{"label":"Risks"}]},` +
	`{"question":"Which format?","header":"Format","options":[{"label":"Markdown"},{"label":"CSV"}]}]}`

func argsOf(t *testing.T, s string) (args map[string]any) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(s), &args))
	return
}

func TestParseQuestionsSaysWhatToFix(t *testing.T) {
	qs, reason := parseQuestions(argsOf(t, twoQuestions))
	require.Empty(t, reason)
	require.Len(t, qs, 2)
	assert.True(t, qs[0].MultiSelect)
	assert.Equal(t, "the table by month", qs[0].Options[1].Description)

	for args, want := range map[string]string{
		`{}`:                     "ask_user takes questions",
		`{"questions":[]}`:       "ask 1 to 4 questions",
		`{"questions":"which?"}`: "do not fit the schema",
		`{"questions":[{"question":" ","options":[{"label":"a"},{"label":"b"}]}]}`:  "question 1 has no text",
		`{"questions":[{"question":"q?","options":[{"label":"a"}]}]}`:               "needs 2 to 6 options, not 1",
		`{"questions":[{"question":"q?","options":[{"label":"a"},{"label":" "}]}]}`: "option 2 has no label",
		`{"questions":[{"question":"q?","options":[{"label":"a"},{"label":"a"}]}]}`: `the option "a" twice`,
	} {
		_, reason = parseQuestions(argsOf(t, args))
		assert.Contains(t, reason, want, args)
	}
	assert.Equal(t, "Very long heade…", cleanHeader("Very long header text"))
}

// An answer needs a choice or the person's own words for every question; a
// note travels only with a chosen option.
func TestTheFormAnswersOnlyWhenComplete(t *testing.T) {
	qs, _ := parseQuestions(argsOf(t, twoQuestions))
	o := &openAsk{questions: qs, reply: make(chan askReply, 1), form: newAskForm(qs)}
	o.form.chosen[0][1], o.form.notes[0][1] = true, " by quarter "
	o.form.notes[0][2] = "not chosen, not sent"
	o.submit()
	assert.True(t, o.form.incomplete)
	assert.False(t, o.form.sent)

	o.form.other[1] = "plain text"
	o.submit()
	require.True(t, o.form.sent)
	r := <-o.reply
	require.Len(t, r.Answers, 2)
	assert.Equal(t, []askChoice{{Label: "Costs", Note: "by quarter"}}, r.Answers[0].Chosen)
	assert.Equal(t, "plain text", r.Answers[1].Other)
	assert.Equal(t, `asked 2 questions · Sections: Costs (by quarter); Format: “plain text”`, askActivity(qs, r))

	o.skip()
	assert.Empty(t, o.reply, "a sent form sends nothing more")
}

// questionsRig is a conversation with Questions on and Apps off: the llm
// service over the scripted model, and no agent service at all.
func questionsRig(t *testing.T, model *scriptedModel) (coord *coordinator, cli *llm.Client, req llm.Request) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: model})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chatBus := bus.NewClient(ManifestId, manifest.Caps)
	cli = llm.NewClient(chatBus)
	cli.Timeout = 10 * time.Second
	conv := newConversation()
	coord = newCoordinator(agent.NewClient(chatBus), conv.id)
	coord.offer(false, true)
	req = conv.request("write me the report")
	req.Messages = append([]openaichat.Message{{Role: openaichat.ChatRoleSystem, Content: systemPrompt(false, true)}}, req.Messages...)
	return
}

// waitAsk is the render goroutine's view: the question once it is open.
func waitAsk(t *testing.T, coord *coordinator) (o *openAsk) {
	t.Helper()
	require.Eventually(t, func() bool { o = coord.ask.current(); return o != nil }, 5*time.Second, 5*time.Millisecond)
	return
}

// With Questions alone, ask_user is the only tool; the call waits for the
// form, and the answer is the call's result. A window tool is not there.
func TestAskUserWaitsForTheFormInsideTheTurn(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("w1", "request_access", `{"plan":"look around"}`),
		toolCall("a1", "ask_user", twoQuestions),
		{Content: "Here is the report.", FinishReason: "stop"},
	}}
	coord, cli, req := questionsRig(t, model)
	go func() {
		o := waitAsk(t, coord)
		o.form.single[1], o.form.chosen[1][0] = 0, true
		o.form.chosen[0][0], o.form.chosen[0][2], o.form.notes[0][2] = true, true, "short"
		o.submit()
	}()
	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, "Here is the report.", res.final.Content)
	require.Len(t, model.seen[0].Tools, 1)
	assert.Equal(t, "ask_user", model.seen[0].Tools[0].Name)

	replies := toolReplies(res.messages)
	assert.Equal(t, "error: no tool request_access", replies["w1"])
	var got askReply
	require.NoError(t, json.Unmarshal([]byte(replies["a1"]), &got))
	require.Len(t, got.Answers, 2)
	assert.Equal(t, []askChoice{{Label: "Summary"}, {Label: "Risks", Note: "short"}}, got.Answers[0].Chosen)
	assert.Equal(t, "Which format?", got.Answers[1].Question)
	assert.Equal(t, []askChoice{{Label: "Markdown"}}, got.Answers[1].Chosen)
	assert.Equal(t, []string{"unknown tool request_access",
		"Asking about the report · asked 2 questions · Sections: Summary, Risks (short); Format: Markdown"}, res.activity)
	assert.Nil(t, coord.ask.current(), "an answered question is closed")
}

// A skip tells the model to go on; a cancelled turn withdraws the question.
func TestAskUserSkippedOrWithdrawn(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{toolCall("a1", "ask_user", twoQuestions)}}
	coord, cli, req := questionsRig(t, model)
	go func() { waitAsk(t, coord).skip() }()
	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	assert.Contains(t, toolReplies(res.messages)["a1"], skippedThen)
	assert.Equal(t, "Asking about the report · asked 2 questions · skipped", res.activity[0])

	model.replies = []openaichat.CompletionResponse{toolCall("a2", "ask_user", twoQuestions)}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitAsk(t, coord)
		cancel()
	}()
	_, err = runTurn(ctx, cli, coord, req, nil)
	require.Error(t, err)
	assert.Nil(t, coord.ask.current())
}

// With both on, ask_user comes first beside the window tools.
func TestBothTogglesOfferEveryTool(t *testing.T) {
	coord := newCoordinator(nil, "c")
	coord.offer(true, true)
	tools := coord.tools(context.Background())
	require.Len(t, tools, 8)
	assert.Equal(t, "ask_user", tools[0].Name)
	assert.Contains(t, systemPrompt(true, true), "ask_user")
	assert.NotContains(t, systemPrompt(true, false), "ask_user")
}
