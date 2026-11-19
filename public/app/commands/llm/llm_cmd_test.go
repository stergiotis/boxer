package llm

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// --format json writes the answer's own member names and leaves out what
// the answer did not report.
func TestJsonOutputUsesTheAnswersNames(t *testing.T) {
	tr := openaichat.Transcript{Text: "hi", Format: "diarized_json", Duration: option.Some(1.5),
		Segments: []openaichat.TranscriptSegment{{Id: "s0", Start: 0, End: 1.5, Speaker: "A", Text: "hi"}},
		Usage:    openaichat.TranscriptionUsage{Seconds: option.Some(2.0)}}
	b, err := json.Marshal(jsonOf(tr))
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"hi","duration":1.5,"format":"diarized_json","segments":[{"id":"s0","start":0,"end":1.5,"speaker":"A","text":"hi"}],"usage":{"seconds":2}}`, string(b))
}

func TestMediaTypeOf(t *testing.T) {
	assert.Equal(t, "audio/mpeg", mediaTypeOf("x/talk.MP3"))
	assert.Equal(t, "audio/wav", mediaTypeOf("a.WAV"))
	assert.Empty(t, mediaTypeOf("x/talk.nope"))
}
