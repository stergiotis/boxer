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
	Description: "path to a scripted model (JSON lines, one reply each; \"$name\" in an argument takes the last value a tool result gave name) the llm service answers from instead of an endpoint; honoured only on the headless host, for scenes",
	Category:    env.CategoryLLM,
})

// ScriptedEndpoint is the endpoint a scripted model reports: loopback, so
// the service treats it as local.
const ScriptedEndpoint = "http://127.0.0.1/scripted"

// ScriptReply is one line of a script: a text answer, or one tool call.
// In Args, a string "$name" stands for the last value a tool result gave
// "name" — "$window" for the window a launch reported, "$destination" for
// what play's get_state names — since a scene cannot know them in advance.
type ScriptReply struct {
	Content string         `json:"content,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Args    jsontext.Value `json:"args,omitempty"`
}

// ScriptedClient answers the n-th reply of its script to a request that
// already holds n assistant messages: the conversation's position decides
// the reply, so a call that is cancelled and resent repeats its reply
// rather than skipping one. Past the end it answers that the script ended.
// Its token counts are the request's message count and one, so a client's
// count shows that a turn landed.
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

// placeholder is a "$name" argument.
var placeholder = regexp.MustCompile(`"\$([A-Za-z_][A-Za-z0-9_]*)"`)

// lastValue is the JSON token the latest tool result gave name: a string,
// a number or a boolean.
func lastValue(msgs []openaichat.Message, name string) (tok string, ok bool) {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(name) + `":\s*("(?:[^"\\]|\\.)*"|-?[0-9]+(?:\.[0-9]+)?|true|false)`)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != openaichat.ChatRoleTool {
			continue
		}
		if all := re.FindAllStringSubmatch(msgs[i].Content, -1); len(all) > 0 {
			return all[len(all)-1][1], true
		}
	}
	return
}

// Complete answers the reply at the request's position.
func (inst *ScriptedClient) Complete(_ context.Context, req openaichat.CompletionRequest) (resp openaichat.CompletionResponse, err error) {
	n := 0
	for _, m := range req.Messages {
		if m.Role == openaichat.ChatRoleAssistant {
			n++
		}
	}
	resp.InputTokens, resp.OutputTokens = int32(len(req.Messages)), 1
	if n >= len(inst.replies) {
		resp.Content, resp.FinishReason = "(the script has ended)", "stop"
		return
	}
	r := inst.replies[n]
	if r.Tool == "" {
		resp.Content, resp.FinishReason = r.Content, "stop"
		return
	}
	args := string(r.Args)
	if args == "" {
		args = "{}"
	}
	args = placeholder.ReplaceAllStringFunc(args, func(m string) string {
		if tok, ok := lastValue(req.Messages, m[2:len(m)-1]); ok {
			return tok
		}
		return m
	})
	resp.FinishReason = "tool_calls"
	resp.ToolCalls = []openaichat.ToolCall{{Id: "s" + strconv.Itoa(n), Name: r.Tool, Arguments: args}}
	return
}

// Close releases nothing.
func (inst *ScriptedClient) Close() (err error) { return }
