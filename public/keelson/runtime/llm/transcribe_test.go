package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// fakeTranscriber scripts one transcript and records what it was asked.
type fakeTranscriber struct {
	mu     sync.Mutex
	resp   openaichat.Transcript
	err    error
	seen   []openaichat.TranscriptionRequest
	deltas []openaichat.TranscriptDelta
}

func (f *fakeTranscriber) Transcribe(_ context.Context, req openaichat.TranscriptionRequest) (openaichat.Transcript, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, req)
	return f.resp, f.err
}

func (f *fakeTranscriber) TranscribeStream(ctx context.Context, req openaichat.TranscriptionRequest, onDelta func(openaichat.TranscriptDelta)) (openaichat.Transcript, error) {
	for _, d := range f.deltas {
		onDelta(d)
	}
	return f.Transcribe(ctx, req)
}

func (f *fakeTranscriber) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func (f *fakeTranscriber) last() openaichat.TranscriptionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[len(f.seen)-1]
}

func speechCfg(f *fakeTranscriber) Config {
	return Config{Transcribe: TranscribeConfig{Endpoint: "http://127.0.0.1:8000/v1", Model: "whisper", DiarizeModel: "diarizer", Client: f}}
}

var wav = []byte("RIFF\x24\x00\x00\x00WAVEfmt fake audio")

// A transcription goes to the transcription route's model with the
// caller's options, and comes back as the transcript the provider gave,
// with its call on the record: the verb, the audio's type, digest and
// length, and never the audio.
func TestTranscribeThroughTheHostsRoute(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "hello there", Language: "en", Duration: option.Some(2.5), Format: "verbose_json",
		Segments: []openaichat.TranscriptSegment{{Id: "0", Start: 0, End: 2.5, Text: "hello there"}},
		Usage:    openaichat.TranscriptionUsage{Seconds: option.Some(3.0)}}}
	cfg := speechCfg(f)
	cfg.Retain = RetainRing
	cli, svc, _ := serve(t, cfg)
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.False(t, d.Configured, "no chat model is configured")
	assert.True(t, d.Transcription.Configured, "the transcription route stands on its own")
	assert.True(t, d.Transcription.Diarizes())
	assert.Equal(t, "127.0.0.1:8000", d.Transcription.EndpointHost)
	assert.EqualValues(t, DefaultTranscribeMaxBytes, d.Transcription.MaxBytes)

	res, err := cli.Transcribe(context.Background(), TranscriptionRequest{Purpose: "notes/transcribe", Audio: wav, MediaType: "audio/wav",
		Language: "en", Prompt: "Boxer, keelson", Timestamps: openaichat.TimestampsSegment})
	require.NoError(t, err)
	assert.Equal(t, "hello there", res.Transcript.Text)
	assert.Equal(t, "whisper", res.Model)
	assert.Equal(t, f.resp.Segments, res.Transcript.Segments)
	assert.InDelta(t, 2.5, res.Transcript.Duration.Val, 1e-9)
	assert.InDelta(t, 3.0, res.Transcript.Usage.Seconds.Val, 1e-9)
	assert.False(t, res.Transcript.Usage.InputTokens.Has, "unreported stays unreported across the bus")
	seen := f.last()
	assert.Equal(t, "whisper", seen.ModelId)
	assert.Equal(t, wav, seen.Audio)
	assert.Equal(t, "Boxer, keelson", seen.Prompt)
	assert.Equal(t, openaichat.TimestampsSegment, seen.Timestamps)

	calls := svc.Calls()
	require.Len(t, calls, 1)
	c := calls[0]
	assert.Equal(t, VerbTranscribe, c.Verb)
	assert.Equal(t, "audio/wav", c.AudioMediaType)
	assert.Equal(t, agentrun.ContentDigest(string(wav)), c.AudioDigest)
	assert.EqualValues(t, 2500, c.AudioMs.Val, "the answer's duration")
	assert.False(t, c.Speakers.Has, "not a diarized request")
	assert.Equal(t, "hello there", c.Completion)
	assert.Equal(t, "Boxer, keelson", c.Prompt)
	assert.NotContains(t, c.Prompt+c.Completion, string(wav))
	assert.Greater(t, c.PromptBytes, len(wav), "the multipart body that left")
	row := RowOf(c, false)
	assert.Equal(t, "transcribe", row.Verb.Val)
	assert.EqualValues(t, 2500, row.AudioMs.Val)
	back := RecordOf(entityOf(c, false))
	assert.Equal(t, c.Verb, back.Verb)
	assert.Equal(t, c.AudioMs, back.AudioMs)
	assert.Equal(t, c.AudioDigest, back.AudioDigest)
}

// A diarized request goes to the diarizing model, its known speakers with
// it, and the call records how many speakers came back; a host with no
// diarizing model refuses it with the variable to set.
func TestDiarizationGoesToTheDiarizingModel(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "a b a", Format: "diarized_json", Segments: []openaichat.TranscriptSegment{
		{Id: "s0", Speaker: "ann", Text: "a"}, {Id: "s1", Speaker: "B", Text: "b"}, {Id: "s2", Speaker: "ann", Text: "a"}}}}
	cli, svc, _ := serve(t, speechCfg(f))
	res, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Diarize: true,
		KnownSpeakers: []openaichat.KnownSpeaker{{Name: "ann", MediaType: "audio/wav", Clip: []byte("clip")}}})
	require.NoError(t, err)
	assert.Equal(t, "diarizer", res.Model)
	assert.Equal(t, 2, res.Transcript.Speakers())
	seen := f.last()
	assert.Equal(t, "diarizer", seen.ModelId)
	assert.True(t, seen.Diarize)
	assert.Equal(t, "ann", seen.KnownSpeakers[0].Name)
	c := svc.Calls()[0]
	assert.Equal(t, "diarizer", c.Model)
	assert.EqualValues(t, 2, c.Speakers.Val)

	cfg := speechCfg(&fakeTranscriber{})
	cfg.Transcribe.DiarizeModel = ""
	cli2, _, _ := serve(t, cfg)
	d, err := cli2.Describe(context.Background())
	require.NoError(t, err)
	assert.False(t, d.Transcription.Diarizes())
	_, err = cli2.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Diarize: true})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "BOXER_LLM_TRANSCRIBE_DIARIZE_MODEL")
}

// What the service refuses before anything leaves: no route, a recording
// above the ceiling, a request the encoder cannot form.
func TestTranscribeRefusals(t *testing.T) {
	cli, _, _ := serve(t, Config{})
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.False(t, d.Transcription.Configured)
	assert.Contains(t, d.Transcription.Reason, "BOXER_LLM_TRANSCRIBE_ENDPOINT")
	var refused *RefusedError
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "no transcription model")

	f := &fakeTranscriber{}
	cfg := speechCfg(f)
	cfg.Transcribe.MaxBytes = 8
	cli, svc, _ := serve(t, cfg)
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "BOXER_LLM_TRANSCRIBE_MAXBYTES")
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "malformed request")
	assert.Zero(t, f.calls())
	for _, c := range svc.Calls() {
		assert.True(t, c.Refused)
		assert.Equal(t, VerbTranscribe, c.Verb, "a refused transcription is on the record as one")
	}
}

// The walls hold for recordings as for text (ADR-0383 §SD5): confined
// audio stays on the box, a tier above a remote route's clearance needs
// the person's approval, and the canaries read the request's text fields.
func TestTheWallsHoldForRecordings(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}}
	decl := cell.Defaults()
	decl.Canaries = []cell.Canary{{Scope: "hr", Token: "zq-7731", Tier: agentrun.TierAmber}}
	cfg := Config{Approvals: journal, Cell: &decl,
		Transcribe: TranscribeConfig{Endpoint: "https://api.example.net/v1", Model: "whisper", Client: f}}
	cli, svc, _ := serve(t, cfg)
	var refused *RefusedError
	_, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Sensitivity: queryengine.SensitivityConfined})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "sealed data")
	assert.Contains(t, refused.Reason, "api.example.net", "the transcription route's own host")

	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Prompt: "see zq-7731"})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "canary of scope hr")

	req := TranscriptionRequest{Audio: wav, Tier: uint8(agentrun.TierAmber), Run: 1}
	_, err = cli.Transcribe(context.Background(), req)
	require.True(t, errors.As(err, &refused), "%v", err)
	require.True(t, refused.NeedsApproval())
	assert.Equal(t, "model-route api.example.net", refused.Sink)
	assert.Zero(t, f.calls())
	req.Approval = journal.plant(t, 1, 9, refused.CallId, refused.Sink, "person")
	res, err := cli.Transcribe(context.Background(), req)
	require.NoError(t, err, "the approval covers the same recording resent")
	assert.Equal(t, "x", res.Transcript.Text)
	calls := svc.Calls()
	assert.Equal(t, plantedId(1, 9), calls[len(calls)-1].ApprovalEntry)

	req.Approval = ""
	_, err = cli.Transcribe(context.Background(), req)
	require.True(t, errors.As(err, &refused), "%v", err)
	other := req
	other.Audio = append([]byte(nil), wav...)
	other.Audio[len(other.Audio)-1] ^= 1
	other.Approval = journal.plant(t, 1, 10, refused.CallId, refused.Sink, "person")
	_, err = cli.Transcribe(context.Background(), other)
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "bytes changed", "an approval binds to the recording it was given for")

	sinks := svc.Cell().Sinks()
	var edges []string
	for _, s := range sinks {
		edges = append(edges, s.Edge)
	}
	assert.Contains(t, edges, "api.example.net", "the transcription route is a sink of the cell")
}

// recordingJournal is a journal checker that keeps the digest each call
// named and accepts a list of them.
type recordingJournal struct {
	allowed map[string]bool
	seen    []string
}

func (j *recordingJournal) CheckRequest(_ context.Context, _ string, _ uint64, _ string, digest string) (uint64, error) {
	j.seen = append(j.seen, digest)
	if !j.allowed[digest] {
		return 0, errors.New("the call is not the request the entry recorded")
	}
	return 42, nil
}

// The journal entry a transcription names records the request's content
// digest — the audio's digest and every option — and the service computes
// the same one: a request the entry did not record is refused.
func TestTheJournalCoversATranscription(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "x"}}
	req := TranscriptionRequest{Audio: wav, MediaType: "audio/wav", Language: "de", Diarize: true,
		KnownSpeakers: []openaichat.KnownSpeaker{{Name: "ann", Clip: []byte("c")}}, Run: 3, Journal: "3#1"}
	digest, err := req.ContentDigest()
	require.NoError(t, err)
	j := &recordingJournal{allowed: map[string]bool{digest: true}}
	cfg := speechCfg(f)
	cfg.Journal = j
	cli, svc, _ := serve(t, cfg)
	_, err = cli.Transcribe(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{digest}, j.seen)
	assert.EqualValues(t, 42, svc.Calls()[0].RequestEntry)

	changed := req
	changed.Language = "fr"
	var refused *RefusedError
	_, err = cli.Transcribe(context.Background(), changed)
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "journal")

	clip := req
	clip.KnownSpeakers = []openaichat.KnownSpeaker{{Name: "ann", Clip: []byte("d")}}
	d2, err := clip.ContentDigest()
	require.NoError(t, err)
	assert.NotEqual(t, digest, d2, "a speaker's clip is part of the content")
	assert.Equal(t, 1, f.calls())
}

// A streaming caller hears the transcript's text and the finished segments
// of a diarized stream; the reply is the transcript all the same.
func TestATranscriptStreams(t *testing.T) {
	seg := openaichat.TranscriptSegment{Id: "s0", Start: 0, End: 1, Speaker: "A", Text: "Hi."}
	f := &fakeTranscriber{
		resp:   openaichat.Transcript{Text: "Hi. Hello.", Format: "diarized_json", Segments: []openaichat.TranscriptSegment{seg}},
		deltas: []openaichat.TranscriptDelta{{Text: "Hi.", SegmentId: "s0"}, {SegmentId: "s0", Segment: option.Some(seg)}, {Text: " Hello."}},
	}
	cli, _, _ := serve(t, speechCfg(f))
	var text strings.Builder
	var segs []openaichat.TranscriptSegment
	res, err := cli.TranscribeStreaming(context.Background(), TranscriptionRequest{Audio: wav, Diarize: true}, func(d Delta) {
		text.WriteString(d.Content)
		segs = append(segs, d.Segments...)
	})
	require.NoError(t, err)
	assert.Equal(t, "Hi. Hello.", text.String())
	assert.Equal(t, []openaichat.TranscriptSegment{seg}, segs)
	assert.Equal(t, "Hi. Hello.", res.Transcript.Text)
}

// A provider failure keeps its kind across the bus, and the call is on the
// record as failed, not refused.
func TestATranscriptionFailureKeepsItsKind(t *testing.T) {
	f := &fakeTranscriber{err: &openaichat.HTTPError{Status: 400, Message: "Currently do not support verbose_json for this model"}}
	f.err = errors.Join(f.err, openaichat.ErrBadRequest)
	cli, svc, _ := serve(t, speechCfg(f))
	_, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Timestamps: openaichat.TimestampsWord})
	require.ErrorIs(t, err, openaichat.ErrBadRequest)
	var failed *CallError
	require.True(t, errors.As(err, &failed))
	assert.Contains(t, failed.Reason, "verbose_json")
	c := svc.Calls()[0]
	assert.False(t, c.Refused)
	assert.NotEmpty(t, c.Error)
}

// The ledger charges a transcription the audio's length where the answer
// reported it, and its tokens where it reported them; a rule on audio
// milliseconds counts it.
func TestTheLedgerChargesAudioLength(t *testing.T) {
	f := &fakeTranscriber{resp: openaichat.Transcript{Text: "x", Usage: openaichat.TranscriptionUsage{Seconds: option.Some(4.0),
		InputTokens: option.Some(int32(12)), OutputTokens: option.Some(int32(3))}}}
	cli, svc, _ := serve(t, speechCfg(f))
	require.NoError(t, svc.Ledger().Set(ration.Rule{Id: "audio", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)},
		Kind: ration.RuleKindBudget, Quantity: ration.QuantityAudioMs, Limit: 5000, Window: time.Hour}))
	_, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.NoError(t, err)
	var app ration.AccountUsage
	for _, a := range svc.Ledger().Accounts() {
		if a.Account.Kind == ration.AccountKindApp {
			app = a
		}
	}
	assert.EqualValues(t, 4000, app.Totals[ration.QuantityAudioMs])
	assert.EqualValues(t, 15, app.Totals[ration.QuantityTotalTokens])
	c := svc.Calls()[0]
	assert.EqualValues(t, 12, c.InputTokens)
	assert.EqualValues(t, ration.QuantityAudioMs, "audio_ms")
	assert.Equal(t, usageOfTranscript(f.resp, c.Elapsed)[ration.QuantityAudioMs], usageOfRecord(c)[ration.QuantityAudioMs], "the rebuilt ledger charges what the call did")

	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.NoError(t, err, "the first call's 4000 ms are under the budget when the second is admitted")
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, "audio", refused.Rule)
}

// The grant covers the verb as the wildcard reads: ClientCaps's llm.*
// reaches llm.transcribe, and an app without it is refused by the bus.
func TestTheClientGrantCoversTranscribe(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	cfg := speechCfg(&fakeTranscriber{resp: openaichat.Transcript{Text: "x"}})
	cfg.Journal = AcceptAnyJournal{}
	svc, err := NewService(bus, zerolog.Nop(), cfg)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	granted := NewClient(bus.NewClient(appId, ClientCaps("test: transcribe")))
	_, err = granted.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.NoError(t, err)
	ungranted := NewClient(bus.NewClient("test.other", nil))
	ungranted.Timeout = time.Second
	_, err = ungranted.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.Error(t, err)
	var refused *RefusedError
	assert.False(t, errors.As(err, &refused), "the bus refused it, not the service")
}
