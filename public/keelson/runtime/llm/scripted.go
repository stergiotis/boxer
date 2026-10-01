package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"regexp"
	"strconv"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// ScriptEnv names a scripted model for scenes (ADR-0269 M6): the service
// answers from the script instead of an endpoint. Honoured only on the
// headless host, like the agent's test grants.
var ScriptEnv = env.NewString(env.Spec{
	Name:        "BOXER_LLM_SCRIPT",
	Description: "path to a scripted model (JSON lines, one reply each) the llm service answers from instead of an endpoint; honoured only on the headless host, for scenes",
	Category:    env.CategoryLLM,
})

// ScriptedEndpoint is the endpoint a scripted model reports: loopback, so
// the service treats it as local.
const ScriptedEndpoint = "http://127.0.0.1/scripted"

// ScriptReply is one line of a script: a text answer, or one tool call.
// In Args, the string "$window" stands for the last window number a tool
// result reported ("window":N), since a scene cannot know it in advance.
type ScriptReply struct {
	Content string         `json:"content,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Args    jsontext.Value `json:"args,omitempty"`
}

// ScriptedClient answers the n-th reply of its script to a request that
// already holds n assistant messages: the conversation's position decides
// the reply, so a call that is cancelled and resent repeats its reply
// rather than skipping one. Past the end it answers that the script ended.
type ScriptedClient struct {
	replies []ScriptReply
}

var _ openaichat.ClientI = (*ScriptedClient)(nil)

// ScriptError is a script that does not parse.
type ScriptError struct {
	Line   int
	Reason string
}

func (inst *ScriptError) Error() string {
	return "llm script line " + strconv.Itoa(inst.Line) + ": " + inst.Reason
}

// NewScriptedClient parses a script: one JSON object per line; blank lines
// and lines starting with # are skipped.
func NewScriptedClient(script []byte) (inst *ScriptedClient, err error) {
	inst = &ScriptedClient{}
	sc := bufio.NewScanner(bytes.NewReader(script))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		var r ScriptReply
		if uerr := json.Unmarshal(line, &r, json.RejectUnknownMembers(true)); uerr != nil {
			return nil, &ScriptError{Line: n, Reason: uerr.Error()}
		}
		if (r.Content == "") == (r.Tool == "") {
			return nil, &ScriptError{Line: n, Reason: "a reply has either content or a tool"}
		}
		inst.replies = append(inst.replies, r)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("llm script: %w", err)
	}
	return
}

// LoadScript reads and parses a script file.
func LoadScript(path string) (inst *ScriptedClient, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("llm script: %w", err)
		return
	}
	return NewScriptedClient(b)
}

var windowInResult = regexp.MustCompile(`"window":\s*(\d+)`)

// Complete answers the reply at the request's position.
func (inst *ScriptedClient) Complete(_ context.Context, req openaichat.CompletionRequest) (resp openaichat.CompletionResponse, err error) {
	n := 0
	window := ""
	for _, m := range req.Messages {
		switch m.Role {
		case openaichat.ChatRoleAssistant:
			n++
		case openaichat.ChatRoleTool:
			if all := windowInResult.FindAllStringSubmatch(m.Content, -1); len(all) > 0 {
				window = all[len(all)-1][1]
			}
		}
	}
	if n >= len(inst.replies) {
		resp = openaichat.CompletionResponse{Content: "(the script has ended)", FinishReason: "stop"}
		return
	}
	r := inst.replies[n]
	if r.Tool == "" {
		resp = openaichat.CompletionResponse{Content: r.Content, FinishReason: "stop"}
		return
	}
	args := string(r.Args)
	if args == "" {
		args = "{}"
	}
	if window != "" {
		args = string(bytes.ReplaceAll([]byte(args), []byte(`"$window"`), []byte(window)))
	}
	resp = openaichat.CompletionResponse{FinishReason: "tool_calls",
		ToolCalls: []openaichat.ToolCall{{Id: "s" + strconv.Itoa(n), Name: r.Tool, Arguments: args}}}
	return
}

// Close releases nothing.
func (inst *ScriptedClient) Close() (err error) { return }
