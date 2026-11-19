package llm

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Each route meets the walls with its own host (ADR-0383 §SD2): with the
// chat route on this box and the transcription route remote, confined
// content may be completed and may not be transcribed, and the other way
// round.
func TestEachRouteMeetsTheWallsWithItsOwnHost(t *testing.T) {
	confined := queryengine.SensitivityConfined
	var refused *RefusedError

	chat := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	speech := &fakeTranscriber{resp: openaichat.Transcript{Text: "x"}}
	cfg := localCfg(chat)
	cfg.Transcribe = TranscribeConfig{Endpoint: "https://speech.example.net/v1", Model: "whisper", Client: speech}
	cli, _, _ := serve(t, cfg)
	_, err := cli.Complete(context.Background(), Request{Sensitivity: confined, Messages: userMessage()})
	require.NoError(t, err, "the chat route is on this box")
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Sensitivity: confined, Audio: wav})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "speech.example.net")
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Tier: uint8(agentrun.TierAmber), Audio: wav, Run: 1})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.True(t, refused.NeedsApproval(), "AMBER above the remote route's clearance")
	_, err = cli.Complete(context.Background(), Request{Tier: uint8(agentrun.TierAmber), Messages: userMessage()})
	require.NoError(t, err, "inside the cell nothing is withheld")

	chat2 := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	speech2 := &fakeTranscriber{resp: openaichat.Transcript{Text: "x"}}
	cfg2 := Config{Endpoint: "https://chat.example.net/v1", Model: "m", Client: chat2,
		Transcribe: TranscribeConfig{Endpoint: "http://127.0.0.1:8000/v1", Model: "whisper", Client: speech2}}
	cli2, _, _ := serve(t, cfg2)
	_, err = cli2.Transcribe(context.Background(), TranscriptionRequest{Sensitivity: confined, Audio: wav})
	require.NoError(t, err, "the transcription route is on this box")
	_, err = cli2.Complete(context.Background(), Request{Sensitivity: confined, Messages: userMessage()})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "chat.example.net")
}

// A transcription's deltas go only to an inbox the sender holds.
func TestATranscriptionStreamInboxMustBeTheSenders(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "steered"}, deltas: []openaichat.TranscriptDelta{{Text: "steered"}}}
	bus := inprocbus.NewInst(zerolog.Nop())
	cfg := speechCfg(f)
	cfg.Journal = AcceptAnyJournal{}
	svc, err := NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	var hits atomic.Int32
	inbox, unsubscribe, err := bus.NewClient("victim", nil).SubscribeInbox(func(*app.Msg) { hits.Add(1) })
	require.NoError(t, err)
	defer unsubscribe()
	other := bus.NewClient(appId, ClientCaps("test: transcribe"))
	raw, err := other.RequestWithTimeout(SubjectTranscribe, mustEncode(t, wireTranscribeRequest{V: wireVersion, Audio: wav, CancelKey: "k", StreamInbox: inbox}), 5*time.Second)
	require.NoError(t, err)
	rep, err := decode[wireTranscribeReply](raw)
	require.NoError(t, err)
	assert.True(t, rep.Ok, rep.Reason)
	assert.Equal(t, "steered", rep.Content)
	assert.Zero(t, hits.Load(), "nothing reaches an inbox the sender does not hold")
}

// blockingTranscriber answers when its context ends, with the context's
// error, and records the deadline it ran under.
type blockingTranscriber struct {
	started  chan struct{}
	deadline time.Time
	has      bool
}

func (f *blockingTranscriber) Transcribe(ctx context.Context, _ openaichat.TranscriptionRequest) (openaichat.Transcript, error) {
	f.deadline, f.has = ctx.Deadline()
	if f.started != nil {
		f.started <- struct{}{}
	}
	<-ctx.Done()
	return openaichat.Transcript{}, ctx.Err()
}

// The caller's cancel stops the provider call, the route's timeout bounds
// it, and the caller's deadline narrows it.
func TestATranscriptionIsCancelledTimedAndBounded(t *testing.T) {
	b := &blockingTranscriber{started: make(chan struct{}, 1)}
	cfg := Config{Transcribe: TranscribeConfig{Endpoint: "http://127.0.0.1:8000/v1", Model: "whisper", Client: b, Timeout: time.Hour}}
	cli, svc, _ := serve(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := cli.Transcribe(ctx, TranscriptionRequest{Audio: wav})
		errc <- err
	}()
	<-b.started
	cancel()
	require.ErrorIs(t, <-errc, context.Canceled)
	require.Eventually(t, func() bool {
		calls := svc.Calls()
		return len(calls) == 1 && calls[0].Error != ""
	}, 5*time.Second, 10*time.Millisecond, "llm.cancel stopped the provider call")
	assert.WithinDuration(t, time.Now().Add(time.Hour), b.deadline, time.Minute, "the route's timeout")

	soon := time.Now().Add(30 * time.Second)
	dctx, dcancel := context.WithDeadline(context.Background(), soon)
	go func() {
		_, err := cli.Transcribe(dctx, TranscriptionRequest{Audio: wav})
		errc <- err
	}()
	<-b.started
	assert.WithinDuration(t, soon, b.deadline, time.Second, "the caller's deadline")
	dcancel()
	<-errc

	short := &blockingTranscriber{}
	scfg := Config{Transcribe: TranscribeConfig{Endpoint: "http://127.0.0.1:8000/v1", Model: "whisper", Client: short, Timeout: 50 * time.Millisecond}}
	scli, _, _ := serve(t, scfg)
	_, err := scli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.ErrorIs(t, err, context.DeadlineExceeded, "BOXER_LLM_TRANSCRIBE_TIMEOUT bounds the call")
}

// Under the default retention the record keeps no text: neither the
// prompt nor the transcript.
func TestATranscriptsTextFollowsRetain(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "secret words"}}
	cli, svc, _ := serve(t, speechCfg(f))
	_, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Prompt: "names"})
	require.NoError(t, err)
	c := svc.Calls()[0]
	assert.Empty(t, c.Prompt)
	assert.Empty(t, c.Completion)
	assert.Equal(t, len("secret words"), c.CompletionBytes)
}

// The trail holds the request as one message row naming the recording —
// the model and the audio's digest — and the transcript as the reply's
// row: the call's history is those two.
func TestTheTranscriptIsTheReplyRow(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "hello"}}
	cli, svc, _ := serve(t, speechCfg(f))
	_, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, MediaType: "audio/wav"})
	require.NoError(t, err)
	c := svc.Calls()[0]
	descriptor := transcriptionDescriptor(wireTranscribeRequest{Audio: wav, MediaType: "audio/wav"}, "whisper", c.AudioDigest)
	assert.Contains(t, descriptor, c.AudioDigest)
	assert.Contains(t, descriptor, "whisper")
	assert.Contains(t, descriptor, "audio/wav")
	want := historyHash(messageHashes([]openaichat.Message{
		{Role: openaichat.ChatRoleUser, Content: descriptor},
		{Role: openaichat.ChatRoleAssistant, Content: "hello"},
	}))
	assert.Equal(t, want, c.HistoryHash)
}

// A failure's reply carries what each rule on the call's accounts leaves,
// as a success's does.
func TestATranscriptionFailureKeepsWhatIsLeft(t *testing.T) {
	f := &fakeTranscriber{err: openaichat.ErrServer}
	cli, svc, _ := serve(t, speechCfg(f))
	require.NoError(t, svc.Ledger().Set(ration.Rule{Id: "calls", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityCalls, Limit: 100, Window: time.Hour}))
	raw, err := cli.bus.RequestWithTimeout(SubjectTranscribe, mustEncode(t, wireTranscribeRequest{V: wireVersion, Audio: wav}), 5*time.Second)
	require.NoError(t, err)
	w, err := decode[wireTranscribeReply](raw)
	require.NoError(t, err)
	assert.False(t, w.Ok)
	require.NotEmpty(t, w.Remaining)
	assert.Equal(t, "calls", w.Remaining[0].Rule)
}

// The canaries read every text field a transcription carries: the file
// name and the known speakers' names as well as the prompt.
func TestCanariesReadATranscriptionsTextFields(t *testing.T) {
	decl := cell.Defaults()
	decl.Canaries = []cell.Canary{{Scope: "hr", Token: "zq-7731", Tier: agentrun.TierAmber}}
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "x"}}
	cfg := Config{Cell: &decl, Transcribe: TranscribeConfig{Endpoint: "https://api.example.net/v1", Model: "whisper", DiarizeModel: "d", Client: f}}
	cli, _, _ := serve(t, cfg)
	var refused *RefusedError
	for name, req := range map[string]TranscriptionRequest{
		"file name":    {Audio: wav, FileName: "zq-7731.wav"},
		"speaker name": {Audio: wav, Diarize: true, KnownSpeakers: []openaichat.KnownSpeaker{{Name: "zq-7731", Clip: []byte("c")}}},
		"language":     {Audio: wav, Language: "zq-7731"},
	} {
		_, err := cli.Transcribe(context.Background(), req)
		require.True(t, errors.As(err, &refused), "%s: %v", name, err)
		assert.Contains(t, refused.Reason, "canary of scope hr", name)
	}
	assert.Zero(t, f.calls())
}

// The content digest covers every option: changing any one changes it,
// and the service computes the client's digest from the wire.
func TestTheContentDigestCoversEveryOption(t *testing.T) {
	temp := float32(0.3)
	base := TranscriptionRequest{Audio: wav, FileName: "a.wav", MediaType: "audio/wav", Language: "en", Prompt: "p", Diarize: true,
		KnownSpeakers: []openaichat.KnownSpeaker{{Name: "ann", MediaType: "audio/wav", Clip: []byte("c")}}}
	variants := map[string]func(r *TranscriptionRequest){
		"audio":       func(r *TranscriptionRequest) { r.Audio = append(append([]byte(nil), wav...), 0) },
		"file name":   func(r *TranscriptionRequest) { r.FileName = "b.wav" },
		"media type":  func(r *TranscriptionRequest) { r.MediaType = "audio/mpeg" },
		"language":    func(r *TranscriptionRequest) { r.Language = "de" },
		"prompt":      func(r *TranscriptionRequest) { r.Prompt = "q" },
		"temperature": func(r *TranscriptionRequest) { r.Temperature = &temp },
		"diarize":     func(r *TranscriptionRequest) { r.Diarize, r.KnownSpeakers = false, nil },
		"speaker name": func(r *TranscriptionRequest) {
			r.KnownSpeakers = []openaichat.KnownSpeaker{{Name: "bob", MediaType: "audio/wav", Clip: []byte("c")}}
		},
		"speaker clip": func(r *TranscriptionRequest) {
			r.KnownSpeakers = []openaichat.KnownSpeaker{{Name: "ann", MediaType: "audio/wav", Clip: []byte("d")}}
		},
		"clip type": func(r *TranscriptionRequest) {
			r.KnownSpeakers = []openaichat.KnownSpeaker{{Name: "ann", MediaType: "audio/ogg", Clip: []byte("c")}}
		},
		"timestamps": func(r *TranscriptionRequest) { r.Timestamps = openaichat.TimestampsSegment },
	}
	d0, err := base.ContentDigest()
	require.NoError(t, err)
	seen := map[string]string{d0: "base"}
	for name, mutate := range variants {
		r := base
		mutate(&r)
		d, derr := r.ContentDigest()
		require.NoError(t, derr)
		prev, dup := seen[d]
		assert.False(t, dup, "%s gives %s's digest", name, prev)
		seen[d] = name
	}
	w := wireTranscribeRequest{Audio: base.Audio, FileName: base.FileName, MediaType: base.MediaType, Language: base.Language, Prompt: base.Prompt,
		Diarize: base.Diarize, KnownSpeakers: wireOfSpeakers(base.KnownSpeakers)}
	ds, err := w.content(agentrun.ContentDigest(string(wav))).digest()
	require.NoError(t, err)
	assert.Equal(t, d0, ds, "client and service agree")
}

// The ceiling admits a request of exactly its size and refuses one byte
// more.
func TestTheCeilingIsInclusive(t *testing.T) {
	treq := openaichat.TranscriptionRequest{ModelId: "whisper", Audio: wav}
	_, body, err := openaichat.EncodeTranscription(treq, false)
	require.NoError(t, err)
	for _, tc := range []struct {
		max int64
		ok  bool
	}{{int64(len(body)), true}, {int64(len(body)) - 1, false}} {
		f := &fakeTranscriber{resp: openaichat.Transcript{Text: "x"}}
		cfg := speechCfg(f)
		cfg.Transcribe.MaxBytes = tc.max
		cli, _, _ := serve(t, cfg)
		_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
		if tc.ok {
			require.NoError(t, err, "a body of exactly the ceiling")
		} else {
			var refused *RefusedError
			require.True(t, errors.As(err, &refused), "%v", err)
		}
	}
}
