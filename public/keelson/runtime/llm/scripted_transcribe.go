package llm

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"sync"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TranscribeScriptEnv names a scripted transcriber for scenes (ADR-0383
// §SD8): the service answers llm.transcribe from the script instead of an
// endpoint. Honoured only on the headless host, like BOXER_LLM_SCRIPT.
var TranscribeScriptEnv = env.NewString(env.Spec{
	Name:        "BOXER_LLM_TRANSCRIBE_SCRIPT",
	Description: "path to a scripted transcriber (JSON lines, each an answer body as OpenAI or vLLM sends it: json, verbose_json or diarized_json) the llm service answers llm.transcribe from, in order, instead of an endpoint; honoured only on the headless host, for scenes",
	Category:    env.CategoryLLM,
})

// ScriptedTranscribeEndpoint is the endpoint a scripted transcriber
// reports: loopback, so the service treats it as local.
const ScriptedTranscribeEndpoint = "http://127.0.0.1/scripted-transcribe"

// ScriptedTranscriber answers the n-th transcription with the n-th answer
// of its script; past the end it fails with the script's end named.
type ScriptedTranscriber struct {
	mu      sync.Mutex
	answers []openaichat.Transcript
	next    int
}

var _ openaichat.TranscriberI = (*ScriptedTranscriber)(nil)

// NewScriptedTranscriber parses a script: one answer body per line; blank
// lines and lines starting with # are skipped.
func NewScriptedTranscriber(script []byte) (inst *ScriptedTranscriber, err error) {
	inst = &ScriptedTranscriber{}
	sc := bufio.NewScanner(bytes.NewReader(script))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		if line[0] != '{' {
			return nil, &ScriptError{Line: n, Reason: "an answer is a JSON object"}
		}
		t, derr := openaichat.DecodeTranscript(line)
		if derr != nil {
			return nil, &ScriptError{Line: n, Reason: derr.Error()}
		}
		inst.answers = append(inst.answers, t)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("transcription script: %w", err)
	}
	return
}

// LoadTranscribeScript reads and parses a script file.
func LoadTranscribeScript(path string) (inst *ScriptedTranscriber, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("transcription script: %w", err)
		return
	}
	return NewScriptedTranscriber(b)
}

// Transcribe answers the next line of the script.
func (inst *ScriptedTranscriber) Transcribe(ctx context.Context, _ openaichat.TranscriptionRequest) (t openaichat.Transcript, err error) {
	if err = ctx.Err(); err != nil {
		return t, eh.Errorf("scripted transcription: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.next >= len(inst.answers) {
		return t, eb.Build().Int("answers", len(inst.answers)).Errorf("the transcription script ended after %d answers", len(inst.answers)) //boxer:lint disable=CS013 reason="shape 1: the reason crosses the bus to the scene as text"
	}
	t = inst.answers[inst.next]
	inst.next++
	return
}
