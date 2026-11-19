package openaichat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Speech to text (ADR-0383): POST /audio/transcriptions as OpenAI and
// vLLM implement it. The request is multipart; the answer is asked for in
// the richest JSON form the request needs and normalised into one
// [Transcript], whatever the server sent back. Other servers that claim
// the endpoint are not targeted; doc/adr-background-work's
// speech-to-text-api-survey records where they differ.

// TimestampsE is how fine the times on a transcript are asked to be.
type TimestampsE uint8

const (
	// TimestampsNone asks for the text alone.
	TimestampsNone TimestampsE = iota
	// TimestampsSegment asks for segments with start and end times.
	TimestampsSegment
	// TimestampsWord asks for word times as well; vLLM answers segments
	// without them.
	TimestampsWord
)

// AllTimestamps lists the values of TimestampsE.
var AllTimestamps = []TimestampsE{TimestampsNone, TimestampsSegment, TimestampsWord}

func (inst TimestampsE) String() (s string) {
	switch inst {
	case TimestampsNone:
		s = "none"
	case TimestampsSegment:
		s = "segment"
	case TimestampsWord:
		s = "word"
	}
	return
}

// MaxKnownSpeakers is the most known speakers a diarized request names.
const MaxKnownSpeakers = 4

// KnownSpeaker is a speaker a diarized request names in advance: the label
// the transcript should use, and a short clip of their voice, sent as a
// data URL.
type KnownSpeaker struct {
	Name      string
	MediaType string
	Clip      []byte
}

// TranscriptionRequest is one recording to transcribe. FileName tells the
// server the container (its extension is what servers read); an empty one
// is derived from MediaType.
type TranscriptionRequest struct {
	ModelId   string
	Audio     []byte
	FileName  string
	MediaType string
	// Language is the spoken language as ISO-639-1; empty lets the model
	// detect it.
	Language string
	// Prompt is text the model continues from: spellings, a previous
	// segment. The diarizing model refuses one.
	Prompt      string
	Temperature *float32
	// Diarize asks for segments labelled by speaker, in diarized_json, with
	// the chunking strategy OpenAI requires for input over 30 s; vLLM
	// ignores the strategy. The model must be one that diarizes.
	Diarize       bool
	KnownSpeakers []KnownSpeaker
	// Timestamps asks for verbose_json with the granularities named; it is
	// ignored on a diarized request, whose segments carry times anyway.
	Timestamps TimestampsE
}

// The response formats a request asks for.
const (
	transcriptFormatJson     = "json"
	transcriptFormatVerbose  = "verbose_json"
	transcriptFormatDiarized = "diarized_json"
	// TranscriptFormatText is a Transcript whose answer was not JSON: a
	// server that ignored the format asked for.
	TranscriptFormatText = "text"
)

// responseFormat is the format the request asks for.
func (inst TranscriptionRequest) responseFormat() (f string) {
	switch {
	case inst.Diarize:
		f = transcriptFormatDiarized
	case inst.Timestamps != TimestampsNone:
		f = transcriptFormatVerbose
	default:
		f = transcriptFormatJson
	}
	return
}

// Streams says the request can stream: vLLM refuses verbose_json with
// stream, so a request for times is answered whole.
func (inst TranscriptionRequest) Streams() (yes bool) {
	return inst.responseFormat() != transcriptFormatVerbose
}

// Validate is the check every transcription passes before it is encoded.
func (inst TranscriptionRequest) Validate() (err error) {
	switch {
	case inst.ModelId == "":
		err = eh.Errorf("the transcription names no model")
	case len(inst.Audio) == 0:
		err = eh.Errorf("the transcription carries no audio")
	case len(inst.KnownSpeakers) > 0 && !inst.Diarize:
		err = eh.Errorf("known speakers are named only on a diarized request")
	case len(inst.KnownSpeakers) > MaxKnownSpeakers:
		err = eb.Build().Int("speakers", len(inst.KnownSpeakers)).Int("max", MaxKnownSpeakers).Errorf("too many known speakers")
	case inst.Timestamps > TimestampsWord:
		err = eb.Build().Uint8("timestamps", uint8(inst.Timestamps)).Errorf("unknown timestamp granularity")
	}
	if err != nil {
		return
	}
	for i, s := range inst.KnownSpeakers {
		if s.Name == "" || len(s.Clip) == 0 {
			err = eb.Build().Int("index", i).Errorf("a known speaker needs a name and a clip")
			return
		}
	}
	return
}

// fileNameOf is the request's file name, or one derived from its media
// type: servers read the container from the extension.
func (inst TranscriptionRequest) fileNameOf() (name string) {
	if inst.FileName != "" {
		return inst.FileName
	}
	ext := "bin"
	mt, _, _ := strings.Cut(strings.ToLower(inst.MediaType), ";")
	switch strings.TrimSpace(mt) {
	case "audio/wav", "audio/wave", "audio/x-wav", "audio/vnd.wave":
		ext = "wav"
	case "audio/mpeg", "audio/mp3":
		ext = "mp3"
	case "audio/flac", "audio/x-flac":
		ext = "flac"
	case "audio/ogg", "application/ogg":
		ext = "ogg"
	case "audio/webm", "video/webm":
		ext = "webm"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		ext = "m4a"
	case "video/mp4":
		ext = "mp4"
	}
	return "audio." + ext
}

// EncodeTranscription is the multipart body a request leaves as, and its
// content type. The boundary is derived from the content, so the same
// request encodes to the same bytes: what an approval binds to (ADR-0312
// §SD3) and what the exit budget counts. stream adds stream=true.
func EncodeTranscription(req TranscriptionRequest, stream bool) (contentType string, body []byte, err error) {
	if err = req.Validate(); err != nil {
		return
	}
	fields := make([]formField, 0, 12+2*len(req.KnownSpeakers))
	add := func(name, value string) { fields = append(fields, formField{name: name, value: value}) }
	format := req.responseFormat()
	add("model", req.ModelId)
	add("response_format", format)
	if req.Language != "" {
		add("language", req.Language)
	}
	if req.Prompt != "" {
		add("prompt", req.Prompt)
	}
	if req.Temperature != nil {
		add("temperature", strconv.FormatFloat(float64(*req.Temperature), 'g', -1, 32))
	}
	if format == transcriptFormatVerbose {
		if req.Timestamps == TimestampsWord {
			add("timestamp_granularities[]", "word")
		}
		add("timestamp_granularities[]", "segment")
	}
	if req.Diarize {
		add("chunking_strategy", "auto")
		for _, s := range req.KnownSpeakers {
			add("known_speaker_names[]", s.Name)
		}
		for _, s := range req.KnownSpeakers {
			add("known_speaker_references[]", dataUrl(s.MediaType, s.Clip))
		}
	}
	if stream {
		add("stream", "true")
	}
	fileName := req.fileNameOf()
	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	// The boundary: a digest over everything the body carries, salted
	// until it occurs in none of it (RFC 2046 §5.1.1).
	h := blake3.New(32, nil)
	for _, f := range fields {
		h.Write([]byte(f.name))
		h.Write([]byte{0})
		h.Write([]byte(f.value))
		h.Write([]byte{0})
	}
	h.Write([]byte(fileName))
	h.Write([]byte{0})
	h.Write([]byte(mediaType))
	h.Write([]byte{0})
	h.Write(req.Audio)
	seed := h.Sum(nil)
	var boundary string
	for salt := 0; ; salt++ {
		s := blake3.Sum256(append(seed, byte(salt), byte(salt>>8)))
		boundary = "boxer-" + hex.EncodeToString(s[:20])
		if !bytes.Contains(req.Audio, []byte(boundary)) && !fieldsContain(fields, boundary) {
			break
		}
	}

	var buf bytes.Buffer
	buf.Grow(len(req.Audio) + 1024)
	w := multipart.NewWriter(&buf)
	if err = w.SetBoundary(boundary); err != nil {
		err = eh.Errorf("set the multipart boundary: %w", err)
		return
	}
	for _, f := range fields {
		if err = w.WriteField(f.name, f.value); err != nil {
			err = eb.Build().Str("field", f.name).Errorf("write a form field: %w", err)
			return
		}
	}
	hdr := make(textproto.MIMEHeader, 2)
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="`+escapeQuotes(fileName)+`"`)
	hdr.Set("Content-Type", mediaType)
	pw, perr := w.CreatePart(hdr)
	if perr != nil {
		err = eh.Errorf("create the file part: %w", perr)
		return
	}
	if _, err = pw.Write(req.Audio); err != nil {
		err = eh.Errorf("write the file part: %w", err)
		return
	}
	if err = w.Close(); err != nil {
		err = eh.Errorf("close the multipart body: %w", err)
		return
	}
	contentType, body = w.FormDataContentType(), buf.Bytes()
	return
}

// formField is one text field of a multipart body.
type formField struct{ name, value string }

func fieldsContain(fields []formField, s string) bool {
	for _, f := range fields {
		if strings.Contains(f.name, s) || strings.Contains(f.value, s) {
			return true
		}
	}
	return false
}

// escapeQuotes is the escaping mime/multipart applies to a file name.
func escapeQuotes(s string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(s)
}

func dataUrl(mediaType string, b []byte) string {
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// Transcript is a transcription's answer, normalised. Format says which
// form the answer came in: json, verbose_json, diarized_json, or text when
// the server answered with something that was not JSON.
type Transcript struct {
	Text string
	// Language is the detected or given language, when the answer says.
	Language string
	// Duration is the audio's length in seconds, when the answer says.
	Duration option.Option[float64]
	Segments []TranscriptSegment
	Words    []TranscriptWord
	Usage    TranscriptionUsage
	Format   string
}

// Diarized says the answer labelled its segments by speaker.
func (inst Transcript) Diarized() (yes bool) {
	for _, s := range inst.Segments {
		if s.Speaker != "" {
			return true
		}
	}
	return false
}

// Speakers is the number of distinct speaker labels in the answer. Labels
// are opaque and scoped to the one transcript.
func (inst Transcript) Speakers() (n int) {
	seen := make(map[string]struct{}, 4)
	for _, s := range inst.Segments {
		if s.Speaker != "" {
			seen[s.Speaker] = struct{}{}
		}
	}
	return len(seen)
}

// AudioSeconds is the audio's length as the answer reports it: its
// duration, or the seconds its usage was charged in.
func (inst Transcript) AudioSeconds() (s option.Option[float64]) {
	if inst.Duration.Has {
		return inst.Duration
	}
	return inst.Usage.Seconds
}

// TranscriptSegment is a stretch of the transcript with its times in
// seconds. Id is a string whatever the server sent; Speaker is set on a
// diarized answer.
type TranscriptSegment struct {
	Id      string
	Start   float64
	End     float64
	Speaker string
	Text    string
}

// TranscriptWord is one word with its times in seconds.
type TranscriptWord struct {
	Word  string
	Start float64
	End   float64
}

// TranscriptionUsage is what the answer reports it used; every quantity is
// absent when the server did not report it. OpenAI's newer models report
// tokens, whisper-1 and vLLM seconds.
type TranscriptionUsage struct {
	InputTokens  option.Option[int32]
	OutputTokens option.Option[int32]
	AudioTokens  option.Option[int32]
	Seconds      option.Option[float64]
}

// TranscriptDelta is one advance of a streamed transcript: new text, the
// segment it belongs to when the server says, or a finished segment of a
// diarized stream.
type TranscriptDelta struct {
	Text      string
	SegmentId string
	Segment   option.Option[TranscriptSegment]
}

// TranscriberI transcribes recordings.
type TranscriberI interface {
	Transcribe(ctx context.Context, req TranscriptionRequest) (t Transcript, err error)
}

// StreamingTranscriberI is a transcriber that can stream.
type StreamingTranscriberI interface {
	TranscriberI
	// TranscribeStream is Transcribe with onDelta called, on the caller's
	// goroutine, as the transcript arrives. A request that cannot stream
	// (Streams), or a server that answers whole, gives no deltas.
	TranscribeStream(ctx context.Context, req TranscriptionRequest, onDelta func(TranscriptDelta)) (t Transcript, err error)
}

var _ StreamingTranscriberI = (*Client)(nil)

// The answer's wire shapes, every member optional: json, verbose_json and
// diarized_json are one struct, and the usage one struct over both of
// OpenAI's variants and the chat-style tokens vLLM's stream sends.
type wireTranscript struct {
	Text     string                  `json:"text"`
	Language string                  `json:"language"`
	Duration *float64                `json:"duration"`
	Segments []wireTranscriptSegment `json:"segments"`
	Words    []wireTranscriptWord    `json:"words"`
	Usage    *wireTranscriptUsage    `json:"usage"`
}

type wireTranscriptSegment struct {
	Id      jsontext.Value `json:"id"`
	Start   float64        `json:"start"`
	End     float64        `json:"end"`
	Speaker string         `json:"speaker"`
	Text    string         `json:"text"`
}

type wireTranscriptWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type wireTranscriptUsage struct {
	Type              string   `json:"type"`
	Seconds           *float64 `json:"seconds"`
	InputTokens       *int32   `json:"input_tokens"`
	OutputTokens      *int32   `json:"output_tokens"`
	PromptTokens      *int32   `json:"prompt_tokens"`
	CompletionTokens  *int32   `json:"completion_tokens"`
	InputTokenDetails *struct {
		AudioTokens *int32 `json:"audio_tokens"`
	} `json:"input_token_details"`
}

func (inst *wireTranscriptUsage) usage() (u TranscriptionUsage) {
	if inst == nil {
		return
	}
	if inst.Seconds != nil {
		u.Seconds = option.Some(*inst.Seconds)
	}
	switch {
	case inst.InputTokens != nil:
		u.InputTokens = option.Some(*inst.InputTokens)
	case inst.PromptTokens != nil:
		u.InputTokens = option.Some(*inst.PromptTokens)
	}
	switch {
	case inst.OutputTokens != nil:
		u.OutputTokens = option.Some(*inst.OutputTokens)
	case inst.CompletionTokens != nil:
		u.OutputTokens = option.Some(*inst.CompletionTokens)
	}
	if d := inst.InputTokenDetails; d != nil && d.AudioTokens != nil {
		u.AudioTokens = option.Some(*d.AudioTokens)
	}
	return
}

func segmentId(raw jsontext.Value) (id string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &id); err == nil {
			return id
		}
	}
	return string(raw)
}

// DecodeTranscript normalises an answer body as OpenAI or vLLM sends it:
// json, verbose_json or diarized_json. A body that is not a JSON object is
// the text itself: the shape a server gives when it ignores the format
// asked for.
func DecodeTranscript(raw []byte) (t Transcript, err error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		t = Transcript{Text: string(trimmed), Format: TranscriptFormatText}
		return
	}
	var w wireTranscript
	if err = json.Unmarshal(trimmed, &w); err != nil {
		err = eb.Build().Str("rawSnippet", snippet(string(trimmed), 256)).Errorf("decode the transcript: %w", err)
		return
	}
	t = transcriptOf(w)
	return
}

func transcriptOf(w wireTranscript) (t Transcript) {
	t = Transcript{Text: w.Text, Language: w.Language, Usage: w.Usage.usage(), Format: transcriptFormatJson}
	if w.Duration != nil {
		t.Duration = option.Some(*w.Duration)
	}
	if len(w.Segments) > 0 {
		t.Segments = make([]TranscriptSegment, 0, len(w.Segments))
		for _, s := range w.Segments {
			t.Segments = append(t.Segments, TranscriptSegment{Id: segmentId(s.Id), Start: s.Start, End: s.End, Speaker: s.Speaker, Text: s.Text})
		}
	}
	if len(w.Words) > 0 {
		t.Words = make([]TranscriptWord, 0, len(w.Words))
		for _, x := range w.Words {
			t.Words = append(t.Words, TranscriptWord(x))
		}
	}
	switch {
	case t.Diarized():
		// Only speaker labels make an answer diarized: diarized_json
		// whose model labelled no one reads as segments with times.
		t.Format = transcriptFormatDiarized
	case len(t.Segments) > 0 || len(t.Words) > 0 || w.Duration != nil:
		t.Format = transcriptFormatVerbose
	}
	return
}

// Transcribe sends one recording and returns its transcript.
func (inst *Client) Transcribe(ctx context.Context, req TranscriptionRequest) (t Transcript, err error) {
	contentType, body, err := EncodeTranscription(req, false)
	if err != nil {
		return
	}
	url := inst.baseUrl + "audio/transcriptions"
	t0 := time.Now()
	var status, attempts int
	if inst.observer != nil {
		defer func() { inst.observeTranscription(req, t, status, attempts, t0, err) }()
	}
	var raw []byte
	status, raw, attempts, err = inst.sendRetrying(ctx, http.MethodPost, url, contentType, body)
	if err != nil {
		return
	}
	if status < 200 || status >= 300 {
		err = inst.classifyHttpError(ctx, url, status, raw)
		return
	}
	t, err = DecodeTranscript(raw)
	return
}

func (inst *Client) observeTranscription(req TranscriptionRequest, t Transcript, status int, attempts int, t0 time.Time, err error) {
	st := RequestStat{Model: req.ModelId, Status: status, Attempts: attempts, Elapsed: time.Since(t0), Err: err}
	if t.Usage.InputTokens.Has {
		st.InputTokens = t.Usage.InputTokens.Val
	}
	if t.Usage.OutputTokens.Has {
		st.OutputTokens = t.Usage.OutputTokens.Val
	}
	inst.observer(st)
}

// TranscribeStream is Transcribe with the transcript's text handed to
// onDelta as it arrives. It reads both stream shapes the targeted servers
// send — OpenAI's typed transcript.text.* events, and the
// chat-completion-style chunks of a plain vLLM stream — and a whole JSON
// answer from a server that did not stream. The transcript is assembled
// from the stream; the deltas are advisory.
func (inst *Client) TranscribeStream(ctx context.Context, req TranscriptionRequest, onDelta func(TranscriptDelta)) (t Transcript, err error) {
	if !req.Streams() {
		return inst.Transcribe(ctx, req)
	}
	if onDelta == nil {
		onDelta = func(TranscriptDelta) {}
	}
	contentType, body, err := EncodeTranscription(req, true)
	if err != nil {
		return
	}
	url := inst.baseUrl + "audio/transcriptions"
	t0 := time.Now()
	var status, attempts int
	if inst.observer != nil {
		defer func() { inst.observeTranscription(req, t, status, attempts, t0, err) }()
	}
	var httpResp *http.Response
	var raw []byte
	status, httpResp, raw, attempts, err = inst.openRetrying(ctx, url, contentType, body)
	if err != nil {
		return
	}
	if httpResp == nil {
		if refusesStreaming(status, raw) {
			return inst.Transcribe(ctx, req)
		}
		err = inst.classifyHttpError(ctx, url, status, raw)
		return
	}
	defer func() { _ = httpResp.Body.Close() }()
	if !strings.HasPrefix(strings.ToLower(httpResp.Header.Get("Content-Type")), "text/event-stream") {
		// The server answered whole.
		raw, err = inst.readLimited(httpResp)
		if err != nil {
			return
		}
		t, err = DecodeTranscript(raw)
		return
	}
	t, err = inst.readTranscriptStream(httpResp, onDelta)
	return
}

func (inst *Client) readLimited(resp *http.Response) (raw []byte, err error) {
	reader := io.Reader(resp.Body)
	if inst.maxResponseBytes > 0 {
		reader = io.LimitReader(resp.Body, inst.maxResponseBytes+1)
	}
	if raw, err = io.ReadAll(reader); err != nil {
		err = eh.Errorf("read body: %w", err)
		return
	}
	if inst.maxResponseBytes > 0 && int64(len(raw)) > inst.maxResponseBytes {
		err = eb.Build().Int64("maxResponseBytes", inst.maxResponseBytes).Errorf("read body: %w", ErrResponseTooLarge)
	}
	return
}

// wireTranscriptEvent is one event of either stream shape.
type wireTranscriptEvent struct {
	Type      string               `json:"type"`
	Delta     string               `json:"delta"`
	SegmentId string               `json:"segment_id"`
	Id        jsontext.Value       `json:"id"`
	Start     float64              `json:"start"`
	End       float64              `json:"end"`
	Speaker   string               `json:"speaker"`
	Text      *string              `json:"text"`
	Language  string               `json:"language"`
	Usage     *wireTranscriptUsage `json:"usage"`
	Choices   []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *wireErrorBlock `json:"error"`
}

func (inst *Client) readTranscriptStream(resp *http.Response, onDelta func(TranscriptDelta)) (t Transcript, err error) {
	var text strings.Builder
	var final *string
	var usage TranscriptionUsage
	var segments []TranscriptSegment
	var language string
	events := 0
	err = inst.readSSE(resp.Body, func(payload []byte) (ferr error) {
		var ev wireTranscriptEvent
		if ferr = json.Unmarshal(payload, &ev); ferr != nil {
			return eb.Build().Str("rawSnippet", snippet(string(payload), 256)).Errorf("decode a transcript event: %w", ferr)
		}
		if ev.Error != nil && ev.Error.Message != "" {
			return eb.Build().Str("type", ev.Error.Type).Errorf("the stream reported an error: %s", ev.Error.Message) //boxer:lint disable=CS013 reason="the provider's message is what the reply carries to the app as text, as the completion stream's does"
		}
		events++
		if ev.Usage != nil {
			usage = ev.Usage.usage()
		}
		switch ev.Type {
		case "transcript.text.delta":
			if ev.Delta != "" {
				text.WriteString(ev.Delta)
				onDelta(TranscriptDelta{Text: ev.Delta, SegmentId: ev.SegmentId})
			}
		case "transcript.text.segment":
			s := TranscriptSegment{Id: segmentId(ev.Id), Start: ev.Start, End: ev.End, Speaker: ev.Speaker}
			if ev.Text != nil {
				s.Text = *ev.Text
			}
			segments = append(segments, s)
			onDelta(TranscriptDelta{SegmentId: s.Id, Segment: option.Some(s)})
		case "transcript.text.done":
			final = ev.Text
			if ev.Language != "" {
				language = ev.Language
			}
		case "":
			// vLLM's chat-completion-style chunk.
			for i, ch := range ev.Choices {
				if i > 0 {
					break
				}
				if c := ch.Delta.Content; c != "" {
					text.WriteString(c)
					onDelta(TranscriptDelta{Text: c})
				}
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	if events == 0 {
		err = eh.Errorf("the transcription stream carried no events")
		return
	}
	t = Transcript{Text: text.String(), Language: language, Segments: segments, Usage: usage, Format: transcriptFormatJson}
	if final != nil {
		t.Text = *final
	}
	if len(segments) > 0 {
		t.Format = transcriptFormatDiarized
	}
	return
}
