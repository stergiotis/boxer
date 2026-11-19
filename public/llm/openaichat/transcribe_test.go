package openaichat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// parsedForm is a multipart request as a server reads it: the text fields
// in order, and the file part.
type parsedForm struct {
	fields    [][2]string
	fileName  string
	fileType  string
	fileBytes []byte
}

func (inst parsedForm) values(name string) (vs []string) {
	for _, f := range inst.fields {
		if f[0] == name {
			vs = append(vs, f[1])
		}
	}
	return
}

func (inst parsedForm) value(name string) string {
	vs := inst.values(name)
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func parseForm(t *testing.T, contentType string, body []byte) (f parsedForm) {
	t.Helper()
	mt, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mt)
	r := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		p, perr := r.NextPart()
		if errors.Is(perr, io.EOF) {
			break
		}
		require.NoError(t, perr)
		b, rerr := io.ReadAll(p)
		require.NoError(t, rerr)
		if p.FormName() == "file" {
			f.fileName, f.fileType, f.fileBytes = p.FileName(), p.Header.Get("Content-Type"), b
			continue
		}
		f.fields = append(f.fields, [2]string{p.FormName(), string(b)})
	}
	return
}

func audioReq() TranscriptionRequest {
	return TranscriptionRequest{ModelId: "whisper", Audio: []byte("RIFF....WAVEfmt fake"), MediaType: "audio/wav"}
}

// --- encoding ---------------------------------------------------------------

func TestEncodeTranscriptionPlain(t *testing.T) {
	ct, body, err := EncodeTranscription(audioReq(), false)
	require.NoError(t, err)
	f := parseForm(t, ct, body)
	assert.Equal(t, "whisper", f.value("model"))
	assert.Equal(t, "json", f.value("response_format"))
	assert.Equal(t, "audio.wav", f.fileName, "the extension is derived from the media type")
	assert.Equal(t, "audio/wav", f.fileType)
	assert.Equal(t, audioReq().Audio, f.fileBytes)
	for _, absent := range []string{"language", "prompt", "temperature", "stream", "chunking_strategy", "timestamp_granularities[]"} {
		assert.Empty(t, f.values(absent), "a field the caller did not set is not sent: %s", absent)
	}
}

func TestEncodeTranscriptionDiarized(t *testing.T) {
	req := audioReq()
	req.Diarize = true
	req.Timestamps = TimestampsWord
	req.KnownSpeakers = []KnownSpeaker{{Name: "ann", MediaType: "audio/wav", Clip: []byte("clip-a")}, {Name: "bob", Clip: []byte("clip-b")}}
	ct, body, err := EncodeTranscription(req, true)
	require.NoError(t, err)
	f := parseForm(t, ct, body)
	assert.Equal(t, "diarized_json", f.value("response_format"))
	assert.Equal(t, "auto", f.value("chunking_strategy"), "OpenAI requires it over 30 s; vLLM ignores it")
	assert.Equal(t, []string{"ann", "bob"}, f.values("known_speaker_names[]"))
	assert.Equal(t, []string{"data:audio/wav;base64,Y2xpcC1h", "data:application/octet-stream;base64,Y2xpcC1i"}, f.values("known_speaker_references[]"))
	assert.Empty(t, f.values("timestamp_granularities[]"), "a diarized answer has times anyway")
	assert.Equal(t, "true", f.value("stream"))
}

func TestEncodeTranscriptionTimestamps(t *testing.T) {
	req := audioReq()
	req.Timestamps = TimestampsWord
	req.Language = "de"
	temp := float32(0.2)
	req.Temperature = &temp
	ct, body, err := EncodeTranscription(req, false)
	require.NoError(t, err)
	f := parseForm(t, ct, body)
	assert.Equal(t, "verbose_json", f.value("response_format"))
	assert.Equal(t, []string{"word", "segment"}, f.values("timestamp_granularities[]"))
	assert.Equal(t, "de", f.value("language"))
	assert.Equal(t, "0.2", f.value("temperature"))
	assert.False(t, req.Streams(), "vLLM refuses verbose_json with stream")
}

func TestEncodeTranscriptionRefuses(t *testing.T) {
	cases := map[string]func(r *TranscriptionRequest){
		"no model":                 func(r *TranscriptionRequest) { r.ModelId = "" },
		"no audio":                 func(r *TranscriptionRequest) { r.Audio = nil },
		"speakers without diarize": func(r *TranscriptionRequest) { r.KnownSpeakers = []KnownSpeaker{{Name: "a", Clip: []byte("x")}} },
		"too many speakers": func(r *TranscriptionRequest) {
			r.Diarize = true
			for range MaxKnownSpeakers + 1 {
				r.KnownSpeakers = append(r.KnownSpeakers, KnownSpeaker{Name: "a", Clip: []byte("x")})
			}
		},
		"speaker without clip": func(r *TranscriptionRequest) {
			r.Diarize = true
			r.KnownSpeakers = []KnownSpeaker{{Name: "a"}}
		},
		"unknown granularity":            func(r *TranscriptionRequest) { r.Timestamps = TimestampsWord + 1 },
		"a line break in the file name":  func(r *TranscriptionRequest) { r.FileName = "a.wav\r\nX-Injected: 1" },
		"a line break in the media type": func(r *TranscriptionRequest) { r.MediaType = "audio/wav\nX: 1" },
		"a line break in a clip's type": func(r *TranscriptionRequest) {
			r.Diarize = true
			r.KnownSpeakers = []KnownSpeaker{{Name: "a", MediaType: "audio/wav\r", Clip: []byte("x")}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := audioReq()
			mutate(&req)
			_, _, err := EncodeTranscription(req, false)
			require.Error(t, err)
		})
	}
}

// TestEncodeTranscriptionDeterministic: the same request encodes to the
// same bytes, the property an approval's digest rests on, and the body
// always parses back to the audio it carries — even audio that contains
// boundary-like text.
func TestEncodeTranscriptionDeterministic(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		req := TranscriptionRequest{
			ModelId:   rapid.StringMatching(`[a-z0-9-]{1,12}`).Draw(rt, "model"),
			Audio:     rapid.SliceOfN(rapid.Byte(), 1, 256).Draw(rt, "audio"),
			MediaType: rapid.SampledFrom([]string{"", "audio/wav", "audio/mpeg", "audio/ogg"}).Draw(rt, "type"),
			Prompt:    rapid.String().Draw(rt, "prompt"),
			Diarize:   rapid.Bool().Draw(rt, "diarize"),
		}
		if rapid.Bool().Draw(rt, "plant") {
			// Plant the text a boundary starts with into the audio.
			req.Audio = append(req.Audio, []byte("\r\n--boxer-")...)
		}
		ct1, b1, err := EncodeTranscription(req, false)
		require.NoError(rt, err)
		ct2, b2, err := EncodeTranscription(req, false)
		require.NoError(rt, err)
		require.Equal(rt, ct1, ct2)
		require.Equal(rt, b1, b2)
		_, params, err := mime.ParseMediaType(ct1)
		require.NoError(rt, err)
		r := multipart.NewReader(bytes.NewReader(b1), params["boundary"])
		var got []byte
		for {
			p, perr := r.NextPart()
			if errors.Is(perr, io.EOF) {
				break
			}
			require.NoError(rt, perr)
			b, rerr := io.ReadAll(p)
			require.NoError(rt, rerr)
			if p.FormName() == "file" {
				got = b
			}
		}
		require.Equal(rt, req.Audio, got)
	})
}

// --- decoding ---------------------------------------------------------------

func TestDecodeTranscriptShapes(t *testing.T) {
	t.Run("json from vLLM, integer seconds", func(t *testing.T) {
		tr, err := DecodeTranscript([]byte(`{"text":"hello there","usage":{"type":"duration","seconds":3}}`))
		require.NoError(t, err)
		assert.Equal(t, "hello there", tr.Text)
		assert.Equal(t, "json", tr.Format)
		require.True(t, tr.Usage.Seconds.Has)
		assert.InDelta(t, 3.0, tr.Usage.Seconds.Val, 1e-9)
		assert.True(t, tr.AudioSeconds().Has)
		assert.False(t, tr.Usage.InputTokens.Has)
	})
	t.Run("json from OpenAI, tokens", func(t *testing.T) {
		tr, err := DecodeTranscript([]byte(`{"text":"hi","usage":{"type":"tokens","input_tokens":14,"output_tokens":45,"total_tokens":59,"input_token_details":{"text_tokens":0,"audio_tokens":14}}}`))
		require.NoError(t, err)
		assert.Equal(t, int32(14), tr.Usage.InputTokens.Val)
		assert.Equal(t, int32(45), tr.Usage.OutputTokens.Val)
		assert.Equal(t, int32(14), tr.Usage.AudioTokens.Val)
		assert.False(t, tr.Usage.Seconds.Has)
		assert.False(t, tr.AudioSeconds().Has, "tokens say nothing of the audio's length")
	})
	t.Run("verbose_json with integer segment ids and words", func(t *testing.T) {
		tr, err := DecodeTranscript([]byte(`{"task":"transcribe","language":"english","duration":8.47,"text":"The beach was a popular spot.",
			"segments":[{"id":0,"seek":0,"start":0.0,"end":3.3,"text":"The beach","tokens":[1,2],"temperature":0.0,"avg_logprob":-0.2,"compression_ratio":1.2,"no_speech_prob":0.0}],
			"words":[{"word":"The","start":0.0,"end":0.24}],"usage":{"type":"duration","seconds":9}}`))
		require.NoError(t, err)
		assert.Equal(t, "verbose_json", tr.Format)
		assert.Equal(t, "english", tr.Language)
		assert.InDelta(t, 8.47, tr.Duration.Val, 1e-9)
		assert.InDelta(t, 8.47, tr.AudioSeconds().Val, 1e-9, "the duration wins over the charged seconds")
		require.Len(t, tr.Segments, 1)
		assert.Equal(t, "0", tr.Segments[0].Id)
		assert.Equal(t, []TranscriptWord{{Word: "The", Start: 0, End: 0.24}}, tr.Words)
		assert.False(t, tr.Diarized())
	})
	t.Run("diarized_json", func(t *testing.T) {
		tr, err := DecodeTranscript([]byte(`{"task":"transcribe","duration":27.4,"text":"Agent: Thanks. Customer: Hi.",
			"segments":[{"type":"transcript.text.segment","id":"seg_001","start":0.0,"end":4.7,"text":"Thanks.","speaker":"agent"},
			{"type":"transcript.text.segment","id":"seg_002","start":4.7,"end":11.8,"text":"Hi.","speaker":"A"},
			{"type":"transcript.text.segment","id":"seg_003","start":11.8,"end":12.0,"text":"Yes.","speaker":"agent"}],
			"usage":{"type":"duration","seconds":28}}`))
		require.NoError(t, err)
		assert.Equal(t, "diarized_json", tr.Format)
		assert.True(t, tr.Diarized())
		assert.Equal(t, 2, tr.Speakers())
		assert.Equal(t, "seg_002", tr.Segments[1].Id)
		assert.Equal(t, "A", tr.Segments[1].Speaker)
	})
	t.Run("a server that ignored the format", func(t *testing.T) {
		tr, err := DecodeTranscript([]byte("  just the words\n"))
		require.NoError(t, err)
		assert.Equal(t, TranscriptFormatText, tr.Format)
		assert.Equal(t, "just the words", tr.Text)
	})
	t.Run("malformed JSON is an error", func(t *testing.T) {
		_, err := DecodeTranscript([]byte(`{"text":`))
		require.Error(t, err)
	})
}

// --- the round trip ---------------------------------------------------------

func TestTranscribeRoundTrip(t *testing.T) {
	var gotPath, gotAuth string
	var form parsedForm
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		form = parseForm(t, r.Header.Get("Content-Type"), body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok","usage":{"type":"duration","seconds":1}}`))
	})
	tr, err := c.Transcribe(context.Background(), audioReq())
	require.NoError(t, err)
	assert.Equal(t, "/audio/transcriptions", gotPath)
	assert.Empty(t, gotAuth, "no key, no header")
	assert.Equal(t, "whisper", form.value("model"))
	assert.Equal(t, "ok", tr.Text)
}

func TestTranscribeClassifiesErrors(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Currently do not support verbose_json for this model","type":"BadRequestError"}}`))
	})
	req := audioReq()
	req.Timestamps = TimestampsSegment
	_, err := c.Transcribe(context.Background(), req)
	require.ErrorIs(t, err, ErrBadRequest)
	assert.Contains(t, err.Error(), "verbose_json", "the provider's message travels in the text")
}

// sseEvents writes events as a server-sent stream, then [DONE].
func sseEvents(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		_, _ = w.Write([]byte("data: " + e + "\n\n"))
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

func TestTranscribeStreamOpenAIEvents(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseEvents(w,
			`{"type":"transcript.text.delta","delta":"Hello"}`,
			`{"type":"transcript.text.delta","delta":" world"}`,
			`{"type":"transcript.text.done","text":"Hello world.","usage":{"type":"tokens","input_tokens":10,"output_tokens":3,"total_tokens":13}}`)
	})
	var deltas []string
	tr, err := c.TranscribeStream(context.Background(), audioReq(), func(d TranscriptDelta) { deltas = append(deltas, d.Text) })
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello", " world"}, deltas)
	assert.Equal(t, "Hello world.", tr.Text, "the done event's text is the transcript")
	assert.Equal(t, int32(10), tr.Usage.InputTokens.Val)
}

func TestTranscribeStreamVllmChunks(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseEvents(w,
			`{"id":"trsc-1","object":"transcription.chunk","created":1,"model":"whisper","choices":[{"delta":{"content":"Hello"},"finish_reason":null,"stop_reason":null}]}`,
			`{"id":"trsc-1","object":"transcription.chunk","created":1,"model":"whisper","choices":[{"delta":{"content":" world"},"finish_reason":"stop","stop_reason":null}]}`,
			`{"id":"trsc-1","object":"transcription.chunk","created":1,"model":"whisper","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":4,"total_tokens":24}}`)
	})
	var deltas []string
	tr, err := c.TranscribeStream(context.Background(), audioReq(), func(d TranscriptDelta) { deltas = append(deltas, d.Text) })
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello", " world"}, deltas)
	assert.Equal(t, "Hello world", tr.Text, "without a done event the transcript is what arrived")
	assert.Equal(t, int32(20), tr.Usage.InputTokens.Val)
	assert.Equal(t, int32(4), tr.Usage.OutputTokens.Val)
}

func TestTranscribeStreamDiarizedSegments(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseEvents(w,
			`{"type":"transcript.text.delta","delta":"Hi.","segment_id":"seg_0"}`,
			`{"type":"transcript.text.segment","id":"seg_0","start":0.0,"end":1.5,"speaker":"A","text":"Hi."}`,
			`{"type":"transcript.text.delta","delta":" Hello.","segment_id":"seg_1"}`,
			`{"type":"transcript.text.segment","id":"seg_1","start":1.5,"end":2.5,"speaker":"B","text":"Hello."}`,
			`{"type":"transcript.text.done","text":"Hi. Hello.","usage":{"type":"tokens","input_tokens":5,"output_tokens":2,"total_tokens":7}}`)
	})
	req := audioReq()
	req.Diarize = true
	var segs []TranscriptSegment
	var ids []string
	tr, err := c.TranscribeStream(context.Background(), req, func(d TranscriptDelta) {
		if d.Segment.Has {
			segs = append(segs, d.Segment.Val)
			return
		}
		ids = append(ids, d.SegmentId)
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"seg_0", "seg_1"}, ids)
	require.Len(t, segs, 2)
	assert.Equal(t, "diarized_json", tr.Format)
	assert.Equal(t, 2, tr.Speakers())
	assert.Equal(t, segs, tr.Segments)
}

func TestTranscribeStreamAnsweredWhole(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"whole","usage":{"type":"duration","seconds":2}}`))
	})
	called := false
	tr, err := c.TranscribeStream(context.Background(), audioReq(), func(TranscriptDelta) { called = true })
	require.NoError(t, err)
	assert.False(t, called)
	assert.Equal(t, "whole", tr.Text)
}

func TestTranscribeStreamError(t *testing.T) {
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseEvents(w, `{"error":{"message":"audio too long","type":"BadRequestError"}}`)
	})
	_, err := c.TranscribeStream(context.Background(), audioReq(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audio too long")
}

func TestTranscribeStreamRequestForTimesIsWhole(t *testing.T) {
	var streamed string
	c := newServerClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		streamed = parseForm(t, r.Header.Get("Content-Type"), body).value("stream")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"t","duration":1.0,"segments":[{"id":0,"start":0,"end":1,"text":"t"}]}`))
	})
	req := audioReq()
	req.Timestamps = TimestampsSegment
	tr, err := c.TranscribeStream(context.Background(), req, nil)
	require.NoError(t, err)
	assert.Empty(t, streamed, "verbose_json is never asked to stream")
	assert.Equal(t, "verbose_json", tr.Format)
}

// --- subtitles --------------------------------------------------------------

func TestRenderSubtitles(t *testing.T) {
	tr := Transcript{Segments: []TranscriptSegment{
		{Id: "0", Start: 0, End: 1.5, Text: "Hello"},
		{Id: "1", Start: 3661.0005, End: 3662.25, Speaker: "A", Text: "a < b\n\nnext"},
	}}
	assert.Equal(t, "1\n00:00:00,000 --> 00:00:01,500\nHello\n\n2\n01:01:01,001 --> 01:01:02,250\nA: a < b\nnext\n\n", RenderSRT(tr))
	assert.Equal(t, "WEBVTT\n\n00:00:00.000 --> 00:00:01.500\nHello\n\n01:01:01.001 --> 01:01:02.250\n<v A>a &lt; b\nnext\n\n", RenderWebVTT(tr))
	assert.Equal(t, "", RenderSRT(Transcript{Text: "no segments"}))
	gaps := Transcript{Segments: []TranscriptSegment{{Start: 0, End: 1, Text: "  "}, {Start: 1, End: 2, Text: "a --> b"}}}
	assert.Equal(t, "1\n00:00:01,000 --> 00:00:02,000\na -> b\n\n", RenderSRT(gaps), "an empty segment is no cue, and the arrow is not a timing line")
	assert.Equal(t, "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\na --&gt; b\n\n", RenderWebVTT(gaps))
	assert.True(t, strings.HasPrefix(RenderWebVTT(Transcript{}), "WEBVTT"))
	assert.Equal(t, "00:00:00.000", cueTime(-3, '.'))
}
