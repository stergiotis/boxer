package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// A scripted transcriber answers its lines in order, in the shapes the
// servers send, then says the script ended; a line that is not an answer
// fails the parse with its number.
func TestScriptedTranscriberAnswersInOrder(t *testing.T) {
	s, err := NewScriptedTranscriber([]byte(`# a scene's two recordings
{"text":"first","usage":{"type":"duration","seconds":2}}

{"task":"transcribe","duration":3.0,"text":"a b","segments":[{"type":"transcript.text.segment","id":"s0","start":0,"end":1,"text":"a","speaker":"A"},{"type":"transcript.text.segment","id":"s1","start":1,"end":3,"text":"b","speaker":"B"}]}
`))
	require.NoError(t, err)
	cli, _, _ := serve(t, Config{Transcribe: TranscribeConfig{Endpoint: ScriptedTranscribeEndpoint, Model: "scripted", DiarizeModel: "scripted", Client: s}})
	r1, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	require.NoError(t, err)
	assert.Equal(t, "first", r1.Transcript.Text)
	r2, err := cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav, Diarize: true})
	require.NoError(t, err)
	assert.Equal(t, 2, r2.Transcript.Speakers())
	_, err = cli.Transcribe(context.Background(), TranscriptionRequest{Audio: wav})
	var failed *CallError
	require.True(t, errors.As(err, &failed), "%v", err)
	assert.Contains(t, failed.Reason, "script ended after 2 answers")

	_, err = NewScriptedTranscriber([]byte("{\"text\":\"ok\"}\nnot json\n"))
	var se *ScriptError
	require.True(t, errors.As(err, &se), "%v", err)
	assert.Equal(t, 2, se.Line)
	var _ openaichat.TranscriberI = s
}
