package llm

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// Speech to text as a verb of the family (ADR-0383): llm.transcribe sends
// one recording to the host's transcription route — its own endpoint and
// model, since the speech server is rarely the chat server — through the
// same policy point as llm.complete, and answers one transcript shape. The
// `llm.*` grant covers it, as the wildcard reads.

// SubjectTranscribe is one transcription.
const SubjectTranscribe = "llm.transcribe"

// DefaultTranscribeTimeout bounds one transcription when the config names
// none: a long recording takes longer than a chat answer.
const DefaultTranscribeTimeout = 300 * time.Second

// DefaultTranscribeMaxBytes is the most audio a transcription carries when
// the config names no ceiling: the upload limit OpenAI and vLLM's default
// share.
const DefaultTranscribeMaxBytes = 25 << 20

// The transcription route's variables (ADR-0383 §SD2). Neither endpoint
// nor model has a default, for ADR-0254 §SD2's reason.
var (
	// TranscribeEndpoint is the transcription route's base URL.
	TranscribeEndpoint = env.NewString(env.Spec{
		Name:        "BOXER_LLM_TRANSCRIBE_ENDPOINT",
		Description: "OpenAI-compatible base URL (OpenAI, vLLM) the host's llm service sends llm.transcribe to, e.g. http://localhost:8000/v1; unset means no transcription is offered to apps",
		Category:    env.CategoryLLM,
	})
	// TranscribeModel is the model a plain transcription uses.
	TranscribeModel = env.NewString(env.Spec{
		Name:        "BOXER_LLM_TRANSCRIBE_MODEL",
		Description: "speech-to-text model id llm.transcribe uses (e.g. whisper-1, gpt-4o-transcribe, or the Whisper model a vLLM serves); unset means no transcription is offered even with the endpoint set",
		Category:    env.CategoryLLM,
	})
	// TranscribeDiarizeModel is the model a diarized transcription uses.
	TranscribeDiarizeModel = env.NewString(env.Spec{
		Name:        "BOXER_LLM_TRANSCRIBE_DIARIZE_MODEL",
		Description: "model id a diarized llm.transcribe uses (e.g. gpt-4o-transcribe-diarize, or a diarizing model a vLLM serves); unset refuses requests that ask for speakers",
		Category:    env.CategoryLLM,
	})
	// TranscribeApiKey is the transcription route's key, its own variable
	// for ApiKey's reason.
	TranscribeApiKey = env.NewString(env.Spec{
		Name:        "BOXER_LLM_TRANSCRIBE_APIKEY",
		Description: "API key the host's llm service sends to the transcription endpoint; empty for local endpoints that take none",
		Category:    env.CategoryLLM,
		Sensitive:   true,
	})
	// TranscribeTimeout bounds one transcription on the service side.
	TranscribeTimeout = env.NewDuration(env.Spec{
		Name:        "BOXER_LLM_TRANSCRIBE_TIMEOUT",
		Default:     "300s",
		Description: "wall-clock bound on one llm.transcribe; the service cancels the provider call at it, and app clients that name no timeout of their own wait that long plus a few seconds for the reply",
		Category:    env.CategoryLLM,
	})
	// TranscribeMaxBytes is the most audio one transcription carries.
	TranscribeMaxBytes = env.NewInt(env.Spec{
		Name:        "BOXER_LLM_TRANSCRIBE_MAXBYTES",
		Default:     strconv.Itoa(DefaultTranscribeMaxBytes),
		Description: "most bytes one llm.transcribe request may carry as it leaves, the recording and any known-speaker clips together; larger requests are refused before anything leaves (OpenAI and vLLM's default both take 25 MiB)",
		Category:    env.CategoryLLM,
	})
)

// TranscribeConfig is the host's transcription route.
type TranscribeConfig struct {
	Endpoint string
	Model    string
	// DiarizeModel is the model a diarized request uses: OpenAI and vLLM
	// both diarize with a model of its own. Empty refuses diarization.
	DiarizeModel string
	ApiKey       string
	Timeout      time.Duration
	MaxBytes     int64
	// Client, when set, replaces the client the service would build — a
	// test's fake, or the scripted transcriber.
	Client openaichat.TranscriberI
}

// Configured says a transcription is offered: endpoint and model.
func (inst TranscribeConfig) Configured() (yes bool) { return inst.Endpoint != "" && inst.Model != "" }

// TranscribeConfigFromEnv resolves the route from the ADR-0009 registry.
func TranscribeConfigFromEnv() (cfg TranscribeConfig) {
	return TranscribeConfig{Endpoint: TranscribeEndpoint.Get(), Model: TranscribeModel.Get(), DiarizeModel: TranscribeDiarizeModel.Get(),
		ApiKey: TranscribeApiKey.Get(), Timeout: TranscribeTimeout.Get(), MaxBytes: TranscribeMaxBytes.Get()}
}

// TranscriptionDescription is what llm.describe says of the transcription
// route.
type TranscriptionDescription struct {
	Configured   bool
	Model        string
	DiarizeModel string
	EndpointHost string
	Local        bool
	Trusted      bool
	MaxBytes     int64
	Timeout      time.Duration
	Reason       string
}

// Diarizes says a diarized request has a model to go to.
func (inst TranscriptionDescription) Diarizes() bool {
	return inst.Configured && inst.DiarizeModel != ""
}

// --- the app's side ---------------------------------------------------------

// TranscriptionRequest is one transcription as a Go value: the caller's
// declaration of purpose, sensitivity and tier, the journal entry written
// before it, and the recording with its options. The model is the host's.
type TranscriptionRequest struct {
	Purpose     string
	OnBehalfOf  *app.OnBehalfOf
	Sensitivity queryengine.SensitivityE
	// Tier is the recording's TLP tier, the caller's declaration (ADR-0312
	// §SD2); the audio itself cannot be read for canaries.
	Tier     uint8
	Approval string
	Run      uint64
	Journal  string

	Audio         []byte
	FileName      string
	MediaType     string
	Language      string
	Prompt        string
	Temperature   *float32
	Diarize       bool
	KnownSpeakers []openaichat.KnownSpeaker
	Timestamps    openaichat.TimestampsE
}

// TranscriptionResponse is a transcription's reply.
type TranscriptionResponse struct {
	Transcript openaichat.Transcript
	// Model is the model the host sent the recording to.
	Model     string
	Elapsed   time.Duration
	CallId    string
	Admission string
	Rule      string
	Queued    time.Duration
	Remaining []ration.Remaining
}

// transcriptionContent is what a transcription asks of the model: what its
// journal digest is over. The audio counts by its digest.
type transcriptionContent struct {
	AudioDigest   string                 `json:"audio"`
	FileName      string                 `json:"file_name,omitempty"`
	MediaType     string                 `json:"media_type,omitempty"`
	Language      string                 `json:"language,omitempty"`
	Prompt        string                 `json:"prompt,omitempty"`
	Temperature   *float32               `json:"temperature,omitempty"`
	Diarize       bool                   `json:"diarize,omitempty"`
	KnownSpeakers []transcriptionSpeaker `json:"speakers,omitempty"`
	Timestamps    uint8                  `json:"timestamps,omitempty"`
}

type transcriptionSpeaker struct {
	Name       string `json:"name"`
	MediaType  string `json:"media_type,omitempty"`
	ClipDigest string `json:"clip"`
}

func (inst transcriptionContent) digest() (d string, err error) {
	b, err := buscodec.Encode(inst)
	if err != nil {
		return "", eh.Errorf("encode the transcription's content: %w", err)
	}
	return agentrun.ContentDigest(string(b)), nil
}

func contentOfTranscription(audioDigest string, fileName, mediaType, language, prompt string, temperature *float32, diarize bool, speakers []wireKnownSpeaker, timestamps uint8) (c transcriptionContent) {
	c = transcriptionContent{AudioDigest: audioDigest, FileName: fileName, MediaType: mediaType,
		Language: language, Prompt: prompt, Temperature: temperature, Diarize: diarize, Timestamps: timestamps}
	for _, s := range speakers {
		c.KnownSpeakers = append(c.KnownSpeakers, transcriptionSpeaker{Name: s.Name, MediaType: s.MediaType, ClipDigest: agentrun.ContentDigest(string(s.Clip))})
	}
	return
}

// ContentDigest is the digest a request entry records under its name
// (ADR-0357 §SD3) for this transcription: the audio's digest and every
// option, taken from the request as it will be sent.
func (inst TranscriptionRequest) ContentDigest() (digest string, err error) {
	return contentOfTranscription(agentrun.ContentDigest(string(inst.Audio)), inst.FileName, inst.MediaType, inst.Language, inst.Prompt, inst.Temperature,
		inst.Diarize, wireOfSpeakers(inst.KnownSpeakers), uint8(inst.Timestamps)).digest()
}

// content is the request's content, its audio counted by the digest the
// service already took.
func (inst wireTranscribeRequest) content(audioDigest string) transcriptionContent {
	return contentOfTranscription(audioDigest, inst.FileName, inst.MediaType, inst.Language, inst.Prompt, inst.Temperature,
		inst.Diarize, inst.KnownSpeakers, inst.Timestamps)
}

// rawSize is the recording's and the clips' bytes before encoding: what a
// request far over the ceiling is refused on before anything is copied
// or hashed.
func (inst wireTranscribeRequest) rawSize() (n int64) {
	n = int64(len(inst.Audio))
	for _, s := range inst.KnownSpeakers {
		n += int64(len(s.Clip))
	}
	return
}

// maxAudioSeconds bounds a reported audio length: past it, or below zero,
// the provider's figure is not taken.
const maxAudioSeconds = 1e9

// audioMsOf is the audio's length the answer reported, in milliseconds,
// when it is a length: what the record carries and the ledger charges,
// the one value for both.
func audioMsOf(t openaichat.Transcript) (ms option.Option[int64]) {
	if s := t.AudioSeconds(); s.Has && s.Val >= 0 && s.Val < maxAudioSeconds {
		ms = option.Some(int64(s.Val * 1000))
	}
	return
}

// Transcribe sends one recording through the host's transcription route.
// A refusal is a *RefusedError, a provider failure a *CallError, as on
// Complete.
func (inst *Client) Transcribe(ctx context.Context, r TranscriptionRequest) (res TranscriptionResponse, err error) {
	return inst.transcribe(ctx, r, nil)
}

// TranscribeStreaming is Transcribe with the transcript handed to onDelta
// as the provider writes it: Content is new text, Segments the segments a
// diarized stream finished. A request for times (verbose_json) is answered
// whole and gives no deltas.
func (inst *Client) TranscribeStreaming(ctx context.Context, r TranscriptionRequest, onDelta func(Delta)) (res TranscriptionResponse, err error) {
	return inst.transcribe(ctx, r, onDelta)
}

func (inst *Client) transcribe(ctx context.Context, r TranscriptionRequest, onDelta func(Delta)) (res TranscriptionResponse, err error) {
	if inst == nil || inst.bus == nil {
		return res, eh.Errorf("client without a bus")
	}
	if err = ctx.Err(); err != nil {
		return res, eh.Errorf("before request: %w", err)
	}
	req := wireTranscribeRequest{
		V: wireVersion, Purpose: r.Purpose, Sensitivity: uint8(r.Sensitivity), Tier: r.Tier, Approval: r.Approval, Run: r.Run, Journal: r.Journal,
		Audio: r.Audio, FileName: r.FileName, MediaType: r.MediaType, Language: r.Language, Prompt: r.Prompt, Temperature: r.Temperature,
		Diarize: r.Diarize, KnownSpeakers: wireOfSpeakers(r.KnownSpeakers), Timestamps: uint8(r.Timestamps),
		CancelKey: strconv.FormatUint(rand.Uint64(), 36),
	}
	if r.OnBehalfOf != nil {
		req.OnBehalfTask, req.OnBehalfEpoch, req.OnBehalfCall = r.OnBehalfOf.Task, r.OnBehalfOf.Epoch, r.OnBehalfOf.Call
	}
	if deadline, ok := ctx.Deadline(); ok {
		req.DeadlineUnixNanos = deadline.UnixNano()
	}
	if ib, ok := inst.bus.(app.BusInboxI); ok && onDelta != nil {
		receiver := newDeltaReceiver(req.CancelKey, onDelta)
		inbox, unsubscribe, ierr := ib.SubscribeInbox(receiver.handle)
		if ierr != nil {
			receiver.finish(false)
		} else {
			req.StreamInbox = inbox
			defer func() {
				unsubscribe()
				receiver.finish(err == nil)
			}()
		}
	}
	payload, err := encode(req)
	if err != nil {
		return
	}
	raw, err := inst.request(ctx, SubjectTranscribe, payload, req.CancelKey, inst.transcriptionWait(ctx))
	if err != nil {
		return res, eb.Build().Str("purpose", r.Purpose).Errorf("llm.transcribe request: %w", err)
	}
	w, err := decode[wireTranscribeReply](raw)
	if err != nil {
		return
	}
	if !w.Ok {
		return res, failureOf(w.wireReply)
	}
	res = TranscriptionResponse{Transcript: w.transcript(), Model: w.Model, Elapsed: time.Duration(w.ElapsedNs), CallId: w.CallId,
		Admission: w.Admission, Rule: w.Rule, Queued: time.Duration(w.QueuedNs), Remaining: remainingOfWire(w.Remaining)}
	return
}

// transcriptionWait is the default wait for a transcription: the
// service's bound on one plus ReplyMargin.
func (inst *Client) transcriptionWait(ctx context.Context) (d time.Duration) {
	if inst.Timeout > 0 {
		return inst.Timeout
	}
	st := time.Duration(inst.transcribeTimeout.Load())
	if st <= 0 {
		if desc, err := inst.Describe(ctx); err == nil {
			st = desc.Transcription.Timeout
		}
	}
	if st <= 0 {
		st = DefaultTranscribeTimeout
	}
	return st + ReplyMargin
}

// --- the wire ---------------------------------------------------------------

// wireTranscribeRequest is the envelope on llm.transcribe.
type wireTranscribeRequest struct {
	V           uint8  `json:"v"`
	Purpose     string `json:"purpose,omitempty"`
	Sensitivity uint8  `json:"sensitivity,omitempty"`
	Tier        uint8  `json:"tier,omitempty"`
	Approval    string `json:"approval,omitempty"`
	Run         uint64 `json:"run,omitempty"`
	Journal     string `json:"journal,omitempty"`

	Audio         []byte             `json:"audio"`
	FileName      string             `json:"file_name,omitempty"`
	MediaType     string             `json:"media_type,omitempty"`
	Language      string             `json:"language,omitempty"`
	Prompt        string             `json:"prompt,omitempty"`
	Temperature   *float32           `json:"temperature,omitempty"`
	Diarize       bool               `json:"diarize,omitempty"`
	KnownSpeakers []wireKnownSpeaker `json:"known_speakers,omitempty"`
	Timestamps    uint8              `json:"timestamps,omitempty"`

	DeadlineUnixNanos int64  `json:"deadline_ns,omitempty"`
	CancelKey         string `json:"cancel_key,omitempty"`
	StreamInbox       string `json:"stream_inbox,omitempty"`
	OnBehalfTask      string `json:"obo_task,omitempty"`
	OnBehalfEpoch     uint64 `json:"obo_epoch,omitempty"`
	OnBehalfCall      string `json:"obo_call,omitempty"`
}

type wireKnownSpeaker struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type,omitempty"`
	Clip      []byte `json:"clip"`
}

func wireOfSpeakers(ss []openaichat.KnownSpeaker) (out []wireKnownSpeaker) {
	for _, s := range ss {
		out = append(out, wireKnownSpeaker(s))
	}
	return
}

func speakersOfWire(ss []wireKnownSpeaker) (out []openaichat.KnownSpeaker) {
	for _, s := range ss {
		out = append(out, openaichat.KnownSpeaker(s))
	}
	return
}

type wireSegment struct {
	Id      string  `json:"id,omitempty"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker string  `json:"speaker,omitempty"`
	Text    string  `json:"text,omitempty"`
}

type wireWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func wireOfSegment(s openaichat.TranscriptSegment) wireSegment { return wireSegment(s) }

func segmentsOfWire(ss []wireSegment) (out []openaichat.TranscriptSegment) {
	if len(ss) == 0 {
		return nil
	}
	out = make([]openaichat.TranscriptSegment, 0, len(ss))
	for _, s := range ss {
		out = append(out, openaichat.TranscriptSegment(s))
	}
	return
}

// wireTranscribeReply is the reply on llm.transcribe: the reply every verb
// gives — the refusal, the failure kind, the call id, the admission — and
// the transcript, its text in Content. A refusal is the plain reply.
type wireTranscribeReply struct {
	wireReply
	Model       string        `json:"model,omitempty"`
	Language    string        `json:"language,omitempty"`
	Duration    *float64      `json:"duration,omitempty"`
	Format      string        `json:"format,omitempty"`
	Segments    []wireSegment `json:"segments,omitempty"`
	Words       []wireWord    `json:"words,omitempty"`
	AudioTokens *int32        `json:"audio_tokens,omitempty"`
	Seconds     *float64      `json:"seconds,omitempty"`
	HasInput    bool          `json:"has_input,omitempty"`
	HasOutput   bool          `json:"has_output,omitempty"`
}

func (inst wireTranscribeReply) transcript() (t openaichat.Transcript) {
	t = openaichat.Transcript{Text: inst.Content, Language: inst.Language, Format: inst.Format, Segments: segmentsOfWire(inst.Segments)}
	if inst.Duration != nil {
		t.Duration = option.Some(*inst.Duration)
	}
	for _, w := range inst.Words {
		t.Words = append(t.Words, openaichat.TranscriptWord(w))
	}
	if inst.HasInput {
		t.Usage.InputTokens = option.Some(inst.InputTokens)
	}
	if inst.HasOutput {
		t.Usage.OutputTokens = option.Some(inst.OutputTokens)
	}
	if inst.AudioTokens != nil {
		t.Usage.AudioTokens = option.Some(*inst.AudioTokens)
	}
	if inst.Seconds != nil {
		t.Usage.Seconds = option.Some(*inst.Seconds)
	}
	return
}

func wireOfTranscript(t openaichat.Transcript) (w wireTranscribeReply) {
	w.Content, w.Language, w.Format = t.Text, t.Language, t.Format
	if t.Duration.Has {
		d := t.Duration.Val
		w.Duration = &d
	}
	for _, s := range t.Segments {
		w.Segments = append(w.Segments, wireOfSegment(s))
	}
	for _, x := range t.Words {
		w.Words = append(w.Words, wireWord(x))
	}
	if t.Usage.InputTokens.Has {
		w.InputTokens, w.HasInput = t.Usage.InputTokens.Val, true
	}
	if t.Usage.OutputTokens.Has {
		w.OutputTokens, w.HasOutput = t.Usage.OutputTokens.Val, true
	}
	if t.Usage.AudioTokens.Has {
		v := t.Usage.AudioTokens.Val
		w.AudioTokens = &v
	}
	if t.Usage.Seconds.Has {
		v := t.Usage.Seconds.Val
		w.Seconds = &v
	}
	return
}

// --- the service's side -----------------------------------------------------

// transcribeRoute is the transcription route as the policy point sees it.
func (inst *Service) transcribeRoute() exitRoute { return inst.speech }

// describeTranscription is the describe reply's transcription block.
func (inst *Service) describeTranscription() (d TranscriptionDescription) {
	tc := inst.cfg.Transcribe
	if !tc.Configured() || inst.transcribeBroken != "" {
		d.Reason = "no transcription model is configured on this host (BOXER_LLM_TRANSCRIBE_ENDPOINT and BOXER_LLM_TRANSCRIBE_MODEL)"
		if inst.transcribeBroken != "" {
			d.Reason = inst.transcribeBroken
		}
		d.Timeout = tc.Timeout
		return
	}
	return TranscriptionDescription{Configured: true, Model: tc.Model, DiarizeModel: tc.DiarizeModel, EndpointHost: inst.speech.host,
		Local: inst.speech.local, Trusted: inst.speech.trusted, MaxBytes: tc.MaxBytes, Timeout: tc.Timeout}
}

// transcriptionDescriptor is the request's message row content: what was
// sent and to which model, by type, size and digest. The row records its
// own size and digest over this text; the audio's are in it.
func transcriptionDescriptor(req wireTranscribeRequest, model string, audioDigest string) string {
	var b strings.Builder
	b.WriteString("transcribe with ")
	b.WriteString(model)
	b.WriteString(": ")
	mt := req.MediaType
	if mt == "" {
		mt = "audio of unnamed type"
	}
	b.WriteString(mt)
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(len(req.Audio)))
	b.WriteString(" bytes, ")
	b.WriteString(audioDigest)
	if req.Diarize {
		b.WriteString(", diarized with ")
		b.WriteString(strconv.Itoa(len(req.KnownSpeakers)))
		b.WriteString(" known speakers")
	}
	return b.String()
}

// transcriptionLeaks is the text of a transcription the canaries read: its
// text fields. The audio cannot be read for a planted token (ADR-0383,
// Consequences).
func transcriptionLeaks(req wireTranscribeRequest) string {
	var b strings.Builder
	for _, s := range []string{req.FileName, req.MediaType, req.Language, req.Prompt} {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	for _, s := range req.KnownSpeakers {
		b.WriteString(s.Name)
		b.WriteByte('\n')
	}
	return b.String()
}

// handleTranscribe answers llm.transcribe: the request is checked and
// encoded as it would leave, passes the policy point, and the transcript
// goes back with the call on the record.
func (inst *Service) handleTranscribe(msg *app.Msg) {
	req, err := decode[wireTranscribeRequest](msg.Payload)
	if err != nil {
		inst.refuse(msg, "malformed request: "+err.Error(), CallRecord{Verb: VerbTranscribe}, nil)
		return
	}
	tc := inst.cfg.Transcribe
	model := tc.Model
	if req.Diarize {
		model = tc.DiarizeModel
	}
	if tc.MaxBytes > 0 && req.rawSize() > tc.MaxBytes {
		inst.refuse(msg, "the request carries "+strconv.FormatInt(req.rawSize(), 10)+" bytes of recording and speaker clips, above this host's ceiling of "+strconv.FormatInt(tc.MaxBytes, 10)+" (BOXER_LLM_TRANSCRIBE_MAXBYTES)",
			CallRecord{Verb: VerbTranscribe, Model: model, EndpointHost: inst.speech.host, Purpose: req.Purpose, AudioMediaType: req.MediaType}, nil)
		return
	}
	rec := CallRecord{
		CallId: inst.mintCallId(), At: time.Now().UTC(), Sender: msg.Sender, SenderInstance: msg.SenderInstance,
		Purpose: req.Purpose, Sensitivity: queryengine.SensitivityE(req.Sensitivity), Model: model, EndpointHost: inst.speech.host,
		Tier: req.Tier, Verb: VerbTranscribe, AudioMediaType: req.MediaType, AudioDigest: agentrun.ContentDigest(string(req.Audio)),
		Messages: 1,
	}
	// The request on the trail (ADR-0277 §SD3): one message row naming the
	// recording — its type, size and digest, never its bytes — written
	// before it leaves, and the transcript as the reply's row after.
	t := &turn{messages: []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: transcriptionDescriptor(req, model, rec.AudioDigest)}}}
	if !tc.Configured() || inst.transcriber == nil {
		inst.refuse(msg, inst.describeTranscription().Reason, rec, t)
		return
	}
	if req.Diarize && tc.DiarizeModel == "" {
		inst.refuse(msg, "the request asks for speakers and no diarizing model is configured on this host (BOXER_LLM_TRANSCRIBE_DIARIZE_MODEL)", rec, t)
		return
	}
	treq := openaichat.TranscriptionRequest{ModelId: model, Audio: req.Audio, FileName: req.FileName, MediaType: req.MediaType,
		Language: req.Language, Prompt: req.Prompt, Temperature: req.Temperature, Diarize: req.Diarize,
		KnownSpeakers: speakersOfWire(req.KnownSpeakers), Timestamps: openaichat.TimestampsE(req.Timestamps)}
	// The bytes that leave, as the request is sent whole: what an approval
	// binds to and the exit budget counts. A streamed request adds one
	// field, not counted, so the approval of a refused call covers its
	// resend whether or not either streams.
	_, body, berr := openaichat.EncodeTranscription(treq, false)
	if berr != nil {
		inst.refuse(msg, "malformed request: "+berr.Error(), rec, t)
		return
	}
	rec.PromptBytes = len(body)
	// The ceiling is over the body as it leaves — the audio and the known
	// speakers' clips — which is what a server's upload limit counts.
	if tc.MaxBytes > 0 && int64(len(body)) > tc.MaxBytes {
		inst.refuse(msg, "the request is "+strconv.Itoa(len(body))+" bytes with its recording and speaker clips, above this host's ceiling of "+strconv.FormatInt(tc.MaxBytes, 10)+" (BOXER_LLM_TRANSCRIBE_MAXBYTES)", rec, t)
		return
	}
	if inst.cfg.Retain.atLeast(RetainRing) {
		rec.Prompt = req.Prompt
	}
	p := inst.gate(msg, gateIn{
		route: inst.transcribeRoute(), run: req.Run, journalKey: req.Journal, approval: req.Approval,
		digestOf:     func() (string, error) { return req.content(rec.AudioDigest).digest() },
		onBehalfTask: req.OnBehalfTask, onBehalfEpoch: req.OnBehalfEpoch, onBehalfCall: req.OnBehalfCall,
		body: body, leaks: transcriptionLeaks(req),
		cancelKey: req.CancelKey, deadline: req.DeadlineUnixNanos, timeout: tc.Timeout,
	}, &rec, t)
	if p == nil {
		return
	}
	defer p.close()
	started := time.Now()
	tr, cerr := inst.transcribeMaybeStreamed(p.ctx, treq, req, msg)
	rec.Elapsed = time.Since(started)
	used := usageOfTranscript(tr, rec.Elapsed)
	p.settle(used)
	if tr.Usage.InputTokens.Has {
		rec.InputTokens = tr.Usage.InputTokens.Val
	}
	if tr.Usage.OutputTokens.Has {
		rec.OutputTokens = tr.Usage.OutputTokens.Val
	}
	rec.AudioMs = audioMsOf(tr)
	if req.Diarize && cerr == nil {
		rec.Speakers = option.Some(int32(tr.Speakers()))
	}
	rec.CompletionBytes = len(tr.Text)
	if inst.cfg.Retain.atLeast(RetainRing) {
		rec.Completion = tr.Text
	}
	rep := wireOfTranscript(tr)
	if cerr != nil {
		rep = wireTranscribeReply{wireReply: wireReply{Reason: cerr.Error(), ErrorKind: kindOf(cerr)}}
		rec.Error = rep.Reason
	} else {
		rep.Ok = true
		t.reply = option.Some(openaichat.Message{Role: openaichat.ChatRoleAssistant, Content: tr.Text})
	}
	// The admission travels on a failure too, as on a completion's.
	rep.V, rep.Model, rep.ElapsedNs, rep.CallId = wireVersion, model, int64(rec.Elapsed), rec.CallId
	rep.Admission, rep.Rule, rep.QueuedNs, rep.Remaining = p.decision.Outcome.String(), p.decision.Rule, int64(p.decision.Queued), wireOfRemaining(p.decision.Remaining)
	inst.record(context.WithoutCancel(msg.Context()), rec, t)
	inst.publishCall(rec, p.chain, p.class, p.decision, used)
	lg := inst.log.Debug()
	if !rep.Ok {
		lg = inst.log.Warn()
	}
	lg.Str("sender", string(msg.Sender)).Str("purpose", req.Purpose).Bool("ok", rep.Ok).Str("reason", rep.Reason).
		Int("audioBytes", len(req.Audio)).Bool("diarize", req.Diarize).Dur("elapsed", rec.Elapsed).Msg("llm: transcribe")
	p.close()
	inst.reply(msg.Reply, rep)
}

// transcribeMaybeStreamed is the provider call, streamed to the request's
// inbox where the inbox is the sender's, the provider streams and the
// request can (ADR-0383 §SD6).
func (inst *Service) transcribeMaybeStreamed(ctx context.Context, treq openaichat.TranscriptionRequest, req wireTranscribeRequest, msg *app.Msg) (tr openaichat.Transcript, err error) {
	sc, ok := inst.transcriber.(openaichat.StreamingTranscriberI)
	if !ok || req.StreamInbox == "" || !treq.Streams() {
		return inst.transcriber.Transcribe(ctx, treq)
	}
	if !inst.busClient.InboxHeldBy(req.StreamInbox, msg.Sender, msg.SenderInstance) {
		inst.log.Warn().Str("sender", string(msg.Sender)).Str("inbox", req.StreamInbox).Msg("llm: stream inbox not the sender's; answering without deltas")
		return inst.transcriber.Transcribe(ctx, treq)
	}
	st := newDeltaStream(req.CancelKey, func(payload []byte) error {
		return inst.busClient.Publish(req.StreamInbox, payload)
	})
	tr, err = sc.TranscribeStream(ctx, treq, st.addTranscript)
	st.close(ctx.Err() == nil)
	return
}

// usageOfTranscript is what a transcription used, as the provider
// reported it: tokens and audio length where it said.
func usageOfTranscript(t openaichat.Transcript, elapsed time.Duration) (u ration.Usage) {
	u = ration.Usage{ration.QuantityCalls: 1, ration.QuantityWallMs: elapsed.Milliseconds()}
	if t.Usage.InputTokens.Has {
		u[ration.QuantityInputTokens] = int64(t.Usage.InputTokens.Val)
	}
	if t.Usage.OutputTokens.Has {
		u[ration.QuantityOutputTokens] = int64(t.Usage.OutputTokens.Val)
	}
	if ms := audioMsOf(t); ms.Has {
		u[ration.QuantityAudioMs] = ms.Val
	}
	return u.WithTotal()
}

// newTranscriber builds the route's client.
func newTranscriber(tc TranscribeConfig) (t openaichat.TranscriberI, err error) {
	if tc.Client != nil {
		return tc.Client, nil
	}
	c, err := openaichat.NewClient(tc.Endpoint, tc.ApiKey)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// closeTranscriber releases the route's client when it holds anything.
func closeTranscriber(t openaichat.TranscriberI) {
	if c, ok := t.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}
