// Package llm is the host-side command for the model routes (ADR-0383
// §SD8): it calls the transcription route directly, as configured by the
// BOXER_LLM_TRANSCRIBE_* variables, so a deployment can try its route
// before any app uses it. It is host-side code and goes around the bus,
// the policy point and the call record, which govern apps.
package llm

import (
	"context"
	"encoding/json/v2"
	"mime"
	"os"
	"path/filepath"
	"strings"

	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The output formats of `llm transcribe`.
const (
	formatText = "text"
	formatJson = "json"
	formatSrt  = "srt"
	formatVtt  = "vtt"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "llm",
		Usage: "host-side commands for the model routes",
		Commands: []*cli.Command{
			{
				Name:      "transcribe",
				Usage:     "transcribe a recording through the transcription route (BOXER_LLM_TRANSCRIBE_*) and write the transcript to stdout",
				ArgsUsage: "<recording>",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "diarize", Usage: "label segments by speaker, with BOXER_LLM_TRANSCRIBE_DIARIZE_MODEL"},
					&cli.StringSliceFlag{Name: "speaker", Usage: "a known speaker for a diarized request, name=clip-file; at most 4"},
					&cli.StringFlag{Name: "timestamps", Value: "none", Usage: "none, segment or word; word times come from OpenAI's whisper-1 only"},
					&cli.StringFlag{Name: "language", Usage: "spoken language as ISO-639-1; empty detects it"},
					&cli.StringFlag{Name: "prompt", Usage: "text the model continues from: spellings, a previous segment"},
					&cli.StringFlag{Name: "format", Value: formatText, Usage: "text, json, srt or vtt; srt and vtt are rendered from the segments, which OpenAI's whisper-1, a Whisper-style vLLM model or --diarize give"},
					&cli.BoolFlag{Name: "stream", Usage: "write the text to stderr as it arrives"},
				},
				Action: transcribe,
			},
		},
	}
}

func transcribe(ctx context.Context, cmd *cli.Command) (err error) {
	path := cmd.Args().First()
	if path == "" {
		return eh.Errorf("name the recording to transcribe")
	}
	tc := llm.TranscribeConfigFromEnv()
	if !tc.Configured() {
		return eh.Errorf("no transcription route is configured: set BOXER_LLM_TRANSCRIBE_ENDPOINT and BOXER_LLM_TRANSCRIBE_MODEL")
	}
	audio, err := os.ReadFile(path)
	if err != nil {
		return eb.Build().Str("path", path).Errorf("read the recording: %w", err)
	}
	req := openaichat.TranscriptionRequest{ModelId: tc.Model, Audio: audio, FileName: filepath.Base(path), MediaType: mediaTypeOf(path),
		Language: cmd.String("language"), Prompt: cmd.String("prompt"), Diarize: cmd.Bool("diarize")}
	if req.Diarize {
		if tc.DiarizeModel == "" {
			return eh.Errorf("--diarize needs BOXER_LLM_TRANSCRIBE_DIARIZE_MODEL")
		}
		req.ModelId = tc.DiarizeModel
	}
	for _, spec := range cmd.StringSlice("speaker") {
		name, clipPath, ok := strings.Cut(spec, "=")
		if !ok || name == "" || clipPath == "" {
			return eb.Build().Str("speaker", spec).Errorf("a speaker is name=clip-file")
		}
		clip, rerr := os.ReadFile(clipPath)
		if rerr != nil {
			return eb.Build().Str("path", clipPath).Errorf("read the speaker's clip: %w", rerr)
		}
		req.KnownSpeakers = append(req.KnownSpeakers, openaichat.KnownSpeaker{Name: name, MediaType: mediaTypeOf(clipPath), Clip: clip})
	}
	switch cmd.String("timestamps") {
	case "none":
	case "segment":
		req.Timestamps = openaichat.TimestampsSegment
	case "word":
		req.Timestamps = openaichat.TimestampsWord
	default:
		return eb.Build().Str("timestamps", cmd.String("timestamps")).Errorf("timestamps are none, segment or word")
	}
	format := cmd.String("format")
	switch format {
	case formatText, formatJson, formatSrt, formatVtt:
	default:
		return eb.Build().Str("format", format).Errorf("the format is text, json, srt or vtt")
	}
	if (format == formatSrt || format == formatVtt) && req.Timestamps == openaichat.TimestampsNone && !req.Diarize {
		// Subtitles need segments with times.
		req.Timestamps = openaichat.TimestampsSegment
	}
	// The ceiling over the request as it leaves, as the service counts it.
	maxBytes := tc.MaxBytes
	if maxBytes <= 0 {
		maxBytes = llm.DefaultTranscribeMaxBytes
	}
	_, body, err := openaichat.EncodeTranscription(req, cmd.Bool("stream") && req.Streams())
	if err != nil {
		return eh.Errorf("form the request: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return eb.Build().Int("bytes", len(body)).Int64("max", maxBytes).Errorf("the request is above BOXER_LLM_TRANSCRIBE_MAXBYTES")
	}
	timeout := tc.Timeout
	if timeout <= 0 {
		timeout = llm.DefaultTranscribeTimeout
	}
	client, err := openaichat.NewClient(tc.Endpoint, tc.ApiKey)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var t openaichat.Transcript
	if cmd.Bool("stream") {
		t, err = client.TranscribeStream(ctx, req, func(d openaichat.TranscriptDelta) { _, _ = os.Stderr.WriteString(d.Text) })
		_, _ = os.Stderr.WriteString("\n")
	} else {
		t, err = client.Transcribe(ctx, req)
	}
	if err != nil {
		return eh.Errorf("transcribe: %w", err)
	}
	if (format == formatSrt || format == formatVtt) && len(t.Segments) == 0 {
		return eb.Build().Str("model", req.ModelId).Str("answer", t.Format).
			Errorf("the answer carries no segments to make subtitles from: use a model that gives segment times (whisper-1, a Whisper-style vLLM model) or --diarize")
	}
	var out string
	switch format {
	case formatText:
		out = t.Text + "\n"
	case formatSrt:
		out = openaichat.RenderSRT(t)
	case formatVtt:
		out = openaichat.RenderWebVTT(t)
	case formatJson:
		b, merr := json.Marshal(jsonOf(t))
		if merr != nil {
			return eh.Errorf("encode the transcript: %w", merr)
		}
		out = string(b) + "\n"
	}
	if _, err = os.Stdout.WriteString(out); err != nil {
		return eh.Errorf("write to stdout: %w", err)
	}
	return nil
}

// jsonTranscript is the transcript as `--format json` writes it: the
// answer's own member names, an unreported quantity left out.
type jsonTranscript struct {
	Text     string        `json:"text"`
	Language string        `json:"language,omitempty"`
	Duration *float64      `json:"duration,omitempty"`
	Format   string        `json:"format"`
	Segments []jsonSegment `json:"segments,omitempty"`
	Words    []jsonWord    `json:"words,omitempty"`
	Usage    jsonUsage     `json:"usage"`
}

type jsonSegment struct {
	Id      string  `json:"id"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker string  `json:"speaker,omitempty"`
	Text    string  `json:"text"`
}

type jsonWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type jsonUsage struct {
	InputTokens  *int32   `json:"input_tokens,omitempty"`
	OutputTokens *int32   `json:"output_tokens,omitempty"`
	AudioTokens  *int32   `json:"audio_tokens,omitempty"`
	Seconds      *float64 `json:"seconds,omitempty"`
}

func jsonOf(t openaichat.Transcript) (j jsonTranscript) {
	j = jsonTranscript{Text: t.Text, Language: t.Language, Format: t.Format}
	if t.Duration.Has {
		j.Duration = &t.Duration.Val
	}
	for _, s := range t.Segments {
		j.Segments = append(j.Segments, jsonSegment(s))
	}
	for _, w := range t.Words {
		j.Words = append(j.Words, jsonWord(w))
	}
	if t.Usage.InputTokens.Has {
		j.Usage.InputTokens = &t.Usage.InputTokens.Val
	}
	if t.Usage.OutputTokens.Has {
		j.Usage.OutputTokens = &t.Usage.OutputTokens.Val
	}
	if t.Usage.AudioTokens.Has {
		j.Usage.AudioTokens = &t.Usage.AudioTokens.Val
	}
	if t.Usage.Seconds.Has {
		j.Usage.Seconds = &t.Usage.Seconds.Val
	}
	return
}

// audioTypes are the containers the endpoint takes, by extension: Go's
// own table has none of them, and the system's varies by machine.
var audioTypes = map[string]string{
	".flac": "audio/flac", ".m4a": "audio/mp4", ".mp3": "audio/mpeg", ".mp4": "video/mp4", ".mpeg": "audio/mpeg",
	".mpga": "audio/mpeg", ".oga": "audio/ogg", ".ogg": "audio/ogg", ".wav": "audio/wav", ".webm": "audio/webm",
}

// mediaTypeOf is a file's media type by its extension, empty when unknown;
// the server reads the container from the file name either way.
func mediaTypeOf(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mt, ok := audioTypes[ext]; ok {
		return mt
	}
	mt := mime.TypeByExtension(ext)
	mt, _, _ = strings.Cut(mt, ";")
	return mt
}
