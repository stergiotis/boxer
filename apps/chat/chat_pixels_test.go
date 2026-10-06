package chat

import (
	"context"
	"encoding/json/v2"
	"strings"
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

// pixelRig is imageRig with one capture in the set, screenshot-1.png, and
// the Pixels level p on a local model.
func pixelRig(t *testing.T, p agent.PixelsE) (*coordinator, *artefact) {
	t.Helper()
	shot := testPNG(t, 32, 24, 7)
	coord, art := imageRig(t, func([]uint64, *[4]float32) ([]byte, string) { return shot, "" })
	content, _ := callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{1.0}})
	require.NotContains(t, content, "error", content)
	coord.setPixels(pixelPolicy{level: p, modelLocal: true, endpoint: "m · 127.0.0.1 · on this machine"})
	return coord, art
}

func view(t *testing.T, coord *coordinator, name string) (r viewResult, content string) {
	t.Helper()
	content, _ = callTool(t, coord, "artefact_view_image", map[string]any{"name": name})
	if !strings.HasPrefix(content, "error:") {
		require.NoError(t, json.Unmarshal([]byte(content), &r), content)
	}
	return
}

// viewAsync runs a view whose answer waits on the person.
func viewAsync(t *testing.T, coord *coordinator, name string) <-chan viewResult {
	t.Helper()
	out := make(chan viewResult, 1)
	go func() {
		r, _ := view(t, coord, name)
		out <- r
	}()
	return out
}

func waitPixelAsk(t *testing.T, coord *coordinator) (a *pixelAsk) {
	t.Helper()
	require.Eventually(t, func() bool { a = coord.pixelAskNow(); return a != nil }, 5*time.Second, 5*time.Millisecond)
	return
}

func TestViewToolIsOfferedUnderTheSetting(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsNone)
	assert.NotContains(t, toolNames(coord.tools(context.Background())), "artefact_view_image")
	_, content := view(t, coord, "screenshot-1.png")
	assert.Contains(t, content, "show you no screenshot's pixels")

	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	assert.Contains(t, toolNames(coord.tools(context.Background())), "artefact_view_image")

	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, localOnly: true})
	assert.NotContains(t, toolNames(coord.tools(context.Background())), "artefact_view_image", "a remote model under Only to a local model")
	_, content = view(t, coord, "screenshot-1.png")
	assert.Contains(t, content, "only to a model on this machine")
	assert.Nil(t, coord.pixelAskNow(), "refused before the person is asked")

	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	_, content = view(t, coord, "nope.png")
	assert.Contains(t, content, "no screenshot is named")
	var v any
	require.NoError(t, json.Unmarshal(viewTool().Parameters, &v))
}

// This chat's captures shows without asking; the round's message carries
// the pixels after an untrusted preamble, and a second view in the turn
// does not attach it twice.
func TestCapturesLevelShowsWithoutAsking(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsCaptures)
	coord.beginPixelTurn()
	r, _ := view(t, coord, "screenshot-1.png")
	assert.Equal(t, "shown", r.Status)
	assert.Equal(t, []int{32, 24}, []int{r.Width, r.Height})
	r, _ = view(t, coord, "screenshot-1.png")
	assert.Contains(t, r.Then, "already attached")
	m, ok := coord.takeShown()
	require.True(t, ok)
	assert.Equal(t, openaichat.ChatRoleUser, m.Role)
	require.Len(t, m.Images, 1)
	assert.Equal(t, "image/png", m.Images[0].MediaType)
	assert.Contains(t, m.Content, "untrusted content")
	assert.Contains(t, m.Content, "screenshot-1.png (32×24, allowed by the person's setting: this chat's captures)")
	_, ok = coord.takeShown()
	assert.False(t, ok, "taken once")
}

// Ask each time asks for every view, in a new turn too; a decline is the
// model's to read and attaches nothing.
func TestAskEachTimeAsksEveryView(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsAskEach)
	for range 2 {
		coord.beginPixelTurn()
		done := viewAsync(t, coord, "screenshot-1.png")
		a := waitPixelAsk(t, coord)
		assert.Equal(t, stagePerson, coord.stageNow())
		assert.Equal(t, "screenshot-1.png", a.name)
		assert.Equal(t, "m · 127.0.0.1 · on this machine", a.endpoint)
		assert.NotEmpty(t, a.preview.Pixels)
		a.decide(pixelAllowed)
		assert.Equal(t, "shown", (<-done).Status)
	}
	coord.beginPixelTurn()
	done := viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord).decide(pixelDeclined)
	assert.Equal(t, "declined", (<-done).Status)
	_, ok := coord.takeShown()
	assert.False(t, ok)
}

// Ask once per image asks once per content: a copy shares it, a crop does
// not; lowering the level forgets what was allowed.
func TestAskOnceRemembersTheContent(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsAskOnce)
	done := viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord).decide(pixelAllowed)
	require.Equal(t, "shown", (<-done).Status)

	_, _ = callTool(t, coord, "artefact_copy_image", map[string]any{"base_revision": 1.0, "from": "screenshot-1.png", "name": "copy.png"})
	coord.beginPixelTurn()
	r, _ := view(t, coord, "copy.png")
	assert.Equal(t, "shown", r.Status, "the same bytes under another name do not ask")
	m, _ := coord.takeShown()
	assert.Contains(t, m.Content, "allowed by the person once before")

	_, _ = callTool(t, coord, "artefact_crop_image", map[string]any{"base_revision": 2.0, "from": "screenshot-1.png", "x": 0.0, "y": 0.0, "w": 8.0, "h": 8.0})
	done = viewAsync(t, coord, "screenshot-2.png")
	waitPixelAsk(t, coord).decide(pixelAllowed)
	<-done

	coord.setPixels(pixelPolicy{level: agent.PixelsAskEach, modelLocal: true})
	coord.setPixels(pixelPolicy{level: agent.PixelsAskOnce, modelLocal: true})
	coord.beginPixelTurn()
	done = viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord).decide(pixelAllowed)
	assert.Equal(t, "shown", (<-done).Status, "asked again after the level went below Ask once")
}

// The setting binds at once: a waiting view ends refused when the level no
// longer shows it, and allowed — by the setting, not the person — when it no
// longer asks; a stopped turn ends it declined, and the trail says why.
func TestAWaitingViewFollowsTheSetting(t *testing.T) {
	coord, _, got := discloseRig(t, agent.PixelsAskEach, nil)
	out := make(chan string, 1)
	go func() {
		_, c := view(t, coord, "screenshot-1.png")
		out <- c
	}()
	waitPixelAsk(t, coord)
	coord.setPixels(pixelPolicy{level: agent.PixelsNone, modelLocal: true})
	assert.Contains(t, <-out, "settings changed while the view waited")

	coord.setPixels(pixelPolicy{level: agent.PixelsAskEach, modelLocal: true})
	done := viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord)
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	assert.Equal(t, "shown", (<-done).Status)
	bySetting := (*got)[len(*got)-1]
	assert.Equal(t, agent.DecidedBySetting, bySetting.DecidedBy, "the person did not allow it; the setting did")
	assert.Equal(t, agent.PixelsCaptures, bySetting.Level)
	m, _ := coord.takeShown()
	assert.Contains(t, m.Content, "allowed by the person's setting")

	coord.beginPixelTurn()
	coord.setPixels(pixelPolicy{level: agent.PixelsAskEach, modelLocal: true})
	ctx, cancel := context.WithCancel(context.Background())
	gotContent := make(chan string, 1)
	go func() {
		c, _ := coord.dispatch(ctx, toolOrigin{turn: "t1"}, openaichat.ToolCall{Name: "artefact_view_image"}, map[string]any{"name": "screenshot-1.png"})
		gotContent <- c
	}()
	waitPixelAsk(t, coord)
	cancel()
	assert.Contains(t, <-gotContent, "the turn was stopped", "stopping the turn ends the wait as declined")
	stopped := (*got)[len(*got)-1]
	assert.Equal(t, agent.DisclosureDeclined, stopped.Decision)
	assert.Equal(t, "the turn was stopped", stopped.Reason)
}

// The model hears of pixels when it may see them, and when it no longer
// may; a model that never could is told nothing beyond the system prompt.
func TestThePixelsNoteIsNewsOnly(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsNone)
	assert.Empty(t, coord.pixelsNote(), "the default is not news")
	coord.setPixels(pixelPolicy{level: agent.PixelsAskEach, localOnly: true})
	assert.Empty(t, coord.pixelsNote(), "a remote model under the switch sees none either")
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	assert.Contains(t, coord.pixelsNote(), "without asking")
	assert.Empty(t, coord.pixelsNote(), "told once")
	coord.setPixels(pixelPolicy{level: agent.PixelsNone})
	assert.Contains(t, coord.pixelsNote(), "no longer")
}

// Pixels stripped mid-turn are no longer attached: a later view in the turn
// attaches them again rather than pointing at a placeholder.
func TestAViewAfterAStripAttachesAgain(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsCaptures)
	coord.beginPixelTurn()
	view(t, coord, "screenshot-1.png")
	m, _ := coord.takeShown()
	coord.setPixels(pixelPolicy{level: agent.PixelsNone})
	coord.bindPixels([]openaichat.Message{m})
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	r, _ := view(t, coord, "screenshot-1.png")
	assert.NotContains(t, r.Then, "already attached")
	_, ok := coord.takeShown()
	assert.True(t, ok)
}

func TestPixelsMessagesBecomePlaceholders(t *testing.T) {
	coord, _ := pixelRig(t, agent.PixelsCaptures)
	coord.beginPixelTurn()
	view(t, coord, "screenshot-1.png")
	m, _ := coord.takeShown()
	ms := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "look"}, m}

	out, confine := coord.bindPixels(ms)
	assert.False(t, confine)
	assert.Len(t, out[1].Images, 1)

	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, localOnly: true, modelLocal: true})
	out, confine = coord.bindPixels(ms)
	assert.True(t, confine, "kept to a local model: the request is labelled confined")
	assert.Len(t, out[1].Images, 1)

	coord.setPixels(pixelPolicy{level: agent.PixelsNone})
	out, _ = coord.bindPixels(ms)
	assert.Empty(t, out[1].Images, "the level dropped: the turn's pixels go")
	assert.Len(t, ms[1].Images, 1, "the caller's messages are not changed")

	stripped, did := stripPixels(ms)
	require.True(t, did)
	assert.Empty(t, stripped[1].Images)
	assert.Contains(t, stripped[1].Content, "no longer attached: screenshot-1.png (32×24")
	assert.False(t, isPixelsMessage(stripped[1]))
	assert.Equal(t, "look", stripped[0].Content)
}

// A turn through the host: the model views a capture, the next round sees
// it after the tool result, and once the turn lands the history keeps a
// placeholder.
func TestAViewedScreenshotIsSentForItsTurnOnly(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("v1", "artefact_view_image", `{"name":"screenshot-1.png"}`),
		{Content: "It shows a gradient.", FinishReason: "stop"},
	}}
	coord, cli, req := questionsRig(t, model)
	coord.offer(false, false)
	conv := newConversation()
	conv.art.store = testStore(t, roomy())
	conv.art.setPolicy(artPolicy{write: true})
	coord.offerArtefact(conv.art)
	shot := testPNG(t, 16, 16, 4)
	coord.captureHook = func(context.Context, []uint64, *[4]float32) ([]byte, string) { return shot, "" }
	_, _ = callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{1.0}})
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, localOnly: true, modelLocal: true})
	// No agent service on this bus: the trail's record is a stand-in.
	coord.discloseHook = func(agent.Disclosure) error { return nil }

	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	require.Len(t, model.seen, 2)
	assert.Contains(t, toolNames(model.seen[0].Tools), "artefact_view_image")
	var sawNote bool
	for _, m := range model.seen[0].Messages {
		sawNote = sawNote || strings.Contains(m.Content, "artefact_view_image shows you any screenshot")
	}
	assert.True(t, sawNote, "the model was told what it may see")
	last := model.seen[1].Messages[len(model.seen[1].Messages)-1]
	require.Len(t, last.Images, 1, "the second round carries the pixels, after the tool result")
	assert.Equal(t, shot, last.Images[0].Data)
	assert.Equal(t, openaichat.ChatRoleTool, model.seen[1].Messages[len(model.seen[1].Messages)-2].Role)

	conv.begin("look", time.Now().UnixMilli(), false)
	conv.landTurn(req, res, nil, time.Now().UnixMilli())
	for _, m := range conv.history {
		assert.Empty(t, m.Images, "the history keeps no pixels")
	}
	next := conv.request("and now?")
	var placeholder bool
	for _, m := range next.Messages {
		placeholder = placeholder || strings.Contains(m.Content, "no longer attached: screenshot-1.png")
	}
	assert.True(t, placeholder)
}

// The confined label is the enforcement (ADR-0287 §SD4): were the chat
// wrong about its model, the host still refuses the request carrying
// pixels to a model off this machine.
func TestTheHostRefusesConfinedPixelsToARemoteModel(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("v1", "artefact_view_image", `{"name":"screenshot-1.png"}`),
		{Content: "never", FinishReason: "stop"},
	}}
	bus := inprocbus.NewInst(zerolog.Nop())
	svc, err := llm.NewService(bus, zerolog.Nop(), llm.Config{Endpoint: "http://192.0.2.10:1234/v1", Model: "m", Client: model})
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	chatBus := bus.NewClient(ManifestId, manifest.Caps)
	cli := llm.NewClient(chatBus)
	cli.Timeout = 10 * time.Second
	conv := newConversation()
	conv.art.store = testStore(t, roomy())
	conv.art.setPolicy(artPolicy{write: true})
	coord := newCoordinator(agent.NewClient(chatBus), nil, conv.id)
	coord.offerArtefact(conv.art)
	shot := testPNG(t, 16, 16, 4)
	coord.captureHook = func(context.Context, []uint64, *[4]float32) ([]byte, string) { return shot, "" }
	_, _ = callTool(t, coord, "artefact_capture", map[string]any{"base_revision": 0.0, "windows": []any{1.0}})
	// The chat believes the model local; the host knows better.
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, localOnly: true, modelLocal: true})
	coord.discloseHook = func(agent.Disclosure) error { return nil }

	res, err := runTurn(context.Background(), cli, coord, conv.request("look"), nil)
	require.NoError(t, err, "a turn that called tools ends stopped, not failed")
	assert.Contains(t, res.stopped, "must not leave this box")
	require.Len(t, model.seen, 1, "the request carrying pixels never reached the model")
}

// discloseRig is pixelRig whose reports to the host's trail are collected,
// and refused with fail when it is set.
func discloseRig(t *testing.T, p agent.PixelsE, fail error) (*coordinator, *artefact, *[]agent.Disclosure) {
	t.Helper()
	coord, art := pixelRig(t, p)
	var got []agent.Disclosure
	coord.discloseHook = func(d agent.Disclosure) error {
		got = append(got, d)
		return fail
	}
	return coord, art, &got
}

// Every view is reported (ADR-0287 §SD6): shown with what let it through,
// declined by the person, refused by the chat; a crop names the capture it
// was cut from, so the trail joins it to the capture's row.
func TestEveryViewIsReportedToTheTrail(t *testing.T) {
	coord, art, got := discloseRig(t, agent.PixelsAskOnce, nil)
	capture := art.headImages()[0]
	_, _ = callTool(t, coord, "artefact_crop_image", map[string]any{"base_revision": 1.0, "from": "screenshot-1.png", "x": 0.0, "y": 0.0, "w": 8.0, "h": 8.0})

	done := viewAsync(t, coord, "screenshot-2.png")
	waitPixelAsk(t, coord).decide(pixelAllowed)
	require.Equal(t, "shown", (<-done).Status)
	coord.beginPixelTurn()
	r, _ := view(t, coord, "screenshot-2.png")
	require.Equal(t, "shown", r.Status)
	done = viewAsync(t, coord, "screenshot-1.png")
	waitPixelAsk(t, coord).decide(pixelDeclined)
	<-done
	view(t, coord, "nope.png")

	require.Len(t, *got, 4)
	first := (*got)[0]
	assert.Equal(t, agent.DisclosureShown, first.Decision)
	assert.Equal(t, agent.DecidedByPerson, first.DecidedBy)
	assert.Equal(t, "crop", first.Source)
	assert.Equal(t, capture.hash, first.RootDigest, "a crop names the capture it was cut from")
	assert.NotEqual(t, first.RootDigest, first.Digest)
	assert.Equal(t, agent.PixelsAskOnce, first.Level)
	assert.Equal(t, "conv", first.Conversation)
	assert.Equal(t, "t1", first.Asked.Turn)
	assert.Equal(t, agent.DecidedByConsent, (*got)[1].DecidedBy, "the second view rests on the consent")
	assert.Equal(t, agent.DisclosureDeclined, (*got)[2].Decision)
	assert.Equal(t, capture.hash, (*got)[2].RootDigest)
	assert.Equal(t, agent.DisclosureRefused, (*got)[3].Decision)
	assert.Equal(t, agent.DecidedByChat, (*got)[3].DecidedBy)
	assert.Contains(t, (*got)[3].Reason, "no screenshot is named")
}

// A view the host cannot record is not shown: the audit fails closed.
func TestAViewTheHostCannotRecordIsNotShown(t *testing.T) {
	coord, _, got := discloseRig(t, agent.PixelsCaptures, assert.AnError)
	coord.beginPixelTurn()
	_, content := view(t, coord, "screenshot-1.png")
	assert.Contains(t, content, "could not record this view")
	_, ok := coord.takeShown()
	assert.False(t, ok, "nothing is attached")
	require.Len(t, *got, 1)
}

// The host's cap binds in the coordinator whatever the window sends, and in
// the permissions the panel draws.
func TestTheHostCapsThePixels(t *testing.T) {
	PixelsMaxEnv.SetForTest(t, "ask")
	PixelsLocalRequiredEnv.SetForTest(t, "true")
	coord, _ := pixelRig(t, agent.PixelsCaptures)
	p := coord.pixelsNow()
	assert.Equal(t, agent.PixelsAskEach, p.level)
	assert.True(t, p.localOnly)

	perms := defaultPermissions().withPixels(agent.PixelsCaptures, false)
	assert.Equal(t, agent.PixelsAskEach, perms.pixels)
	assert.True(t, perms.pixelsLocal)

	PixelsMaxEnv.SetForTest(t, "none")
	coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, modelLocal: true})
	assert.NotContains(t, toolNames(coord.tools(context.Background())), "artefact_view_image")
}
