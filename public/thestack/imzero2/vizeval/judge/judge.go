// Package judge asks a vision model the scenario's questions about a rendering
// and checks its answers against the ones computed from the data (ADR-0257,
// proposed, §SD6 second layer). Accuracy over a scenario's questions measures
// whether the rendering lets a reader get at what the scenario is about: an
// encoding the model cannot answer from has failed, however it looks.
//
// The model is reached through the host's llm service (ADR-0254), so every
// call is recorded and passes the sensitivity point; this package only needs
// something that completes a request. A reply is cached on disk under a key
// over what the picture shows, the question, the model and PromptVersion, so a
// re-run agrees with its first answer and costs nothing.
package judge

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"lukechampine.com/blake3"
)

// PromptVersion names the prompt below; bump it when the prompt changes, and
// every cached reply is asked again.
const PromptVersion = "task-v1"

// Purpose is what the llm call table records the calls as.
const Purpose = "vizeval: answer a scenario question from a rendering"

const systemPrompt = `You are shown one rendering of a dataset — a table, a chart or a diagram — cropped to the rendering itself.
Answer the question from what the rendering shows and from nothing else.
If the rendering does not let you answer — the value is cut off, hidden, unlabelled or not shown — say so instead of guessing.
Reply with one JSON object and nothing else: {"answer": ["..."], "unreadable": false}.
Put each item of the answer in the array as a string: one element for a single value or name, several for a list. Write numbers and names exactly as the rendering shows them.`

// CompleterI completes one request; *llm.Client is the one used.
type CompleterI interface {
	Complete(ctx context.Context, r llm.Request) (llm.Response, error)
}

// Question is what the judge is asked, with the answer computed from the data.
type Question struct {
	ID     string
	Prompt string
	// Expected is the answer's values, one per row of the answer query's first
	// column.
	Expected []string
	// Compare is eq (in order), set (order-free) or approx (one number within
	// Tol); empty means eq.
	Compare string
	Tol     float64
}

// Verdict is one question's outcome.
type Verdict struct {
	ID         string   `json:"id"`
	Given      []string `json:"given"`
	Expected   []string `json:"expected"`
	Correct    bool     `json:"correct"`
	Unreadable bool     `json:"unreadable,omitempty"`
	// Error is why no answer was obtained: the call failed, the reply did not
	// parse, the call budget ran out. Such a verdict is not correct.
	Error  string `json:"error,omitempty"`
	Cached bool   `json:"cached,omitempty"`
}

// Judge asks questions of one model.
type Judge struct {
	Client CompleterI
	// Model is the host's model id, part of the cache key: another model's
	// answer is another measurement.
	Model string
	// CacheDir keeps replies; empty disables the cache.
	CacheDir string
	// MaxCalls bounds the model calls one Judge makes; cached answers are
	// free. Zero means no bound.
	MaxCalls int
	calls    int
}

// Calls is how many model calls this judge has made.
func (inst *Judge) Calls() int { return inst.calls }

// reply is the model's answer as cached.
type reply struct {
	Answer     []string `json:"answer"`
	Unreadable bool     `json:"unreadable"`
}

// Ask asks every question about one PNG. drawing identifies what the PNG
// shows for the cache — the geometry digest of the drawing, which two renders
// of one candidate share although their bytes differ by rasterizer noise;
// empty keys on the PNG's bytes instead. context is the scenario's intent,
// given to the model as the reason the rendering exists.
func (inst *Judge) Ask(ctx context.Context, png []byte, drawing string, context string, qs []Question) (vs []Verdict) {
	if drawing == "" {
		sum := blake3.Sum256(png)
		drawing = "png:" + hex.EncodeToString(sum[:])
	}
	vs = make([]Verdict, 0, len(qs))
	for _, q := range qs {
		v := Verdict{ID: q.ID, Expected: q.Expected}
		key := inst.key(drawing, context, q)
		r, cached, err := inst.cached(key)
		if err == nil && !cached {
			r, err = inst.call(ctx, png, context, q)
			if err == nil {
				err = inst.store(key, r)
			}
		}
		if err != nil {
			v.Error = err.Error()
			vs = append(vs, v)
			continue
		}
		v.Given, v.Unreadable, v.Cached = r.Answer, r.Unreadable, cached
		v.Correct = !r.Unreadable && Matches(q, r.Answer)
		vs = append(vs, v)
	}
	return vs
}

func (inst *Judge) key(drawing string, context string, q Question) string {
	h := blake3.New(32, nil)
	for _, part := range []string{PromptVersion, inst.Model, drawing, context, q.ID, q.Prompt} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func (inst *Judge) cached(key string) (r reply, found bool, err error) {
	if inst.CacheDir == "" {
		return r, false, nil
	}
	b, err := os.ReadFile(filepath.Join(inst.CacheDir, key+".json"))
	if os.IsNotExist(err) {
		return r, false, nil
	}
	if err != nil {
		return r, false, eh.Errorf("unable to read a cached reply: %w", err)
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, false, eh.Errorf("unable to decode a cached reply: %w", err)
	}
	return r, true, nil
}

func (inst *Judge) store(key string, r reply) (err error) {
	if inst.CacheDir == "" {
		return nil
	}
	if err = os.MkdirAll(inst.CacheDir, 0o755); err != nil {
		return eh.Errorf("unable to create the reply cache: %w", err)
	}
	b, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		return eh.Errorf("unable to encode a reply: %w", err)
	}
	if err = os.WriteFile(filepath.Join(inst.CacheDir, key+".json"), b, 0o644); err != nil {
		return eh.Errorf("unable to cache a reply: %w", err)
	}
	return nil
}

func (inst *Judge) call(ctx context.Context, png []byte, context string, q Question) (r reply, err error) {
	if inst.MaxCalls > 0 && inst.calls >= inst.MaxCalls {
		return r, eh.Errorf("the run's model-call budget is spent")
	}
	inst.calls++
	zero := float32(0)
	user := "Question: " + q.Prompt
	if context != "" {
		user = "What the rendering is for: " + context + "\n\n" + user
	}
	res, err := inst.Client.Complete(ctx, llm.Request{
		Purpose:     Purpose,
		Temperature: &zero,
		Messages: []openaichat.Message{
			{Role: openaichat.ChatRoleSystem, Content: systemPrompt},
			{Role: openaichat.ChatRoleUser, Content: user, Images: []openaichat.Image{{MediaType: "image/png", Data: png}}},
		},
	})
	if err != nil {
		return r, eh.Errorf("the model call failed: %w", err)
	}
	return ParseReply(res.Content)
}

// ParseReply reads the first JSON object in a reply. Models asked for JSON
// alone still wrap it in prose or a code fence often enough that insisting on
// the bare object would lose answers to formatting.
func ParseReply(content string) (r reply, err error) {
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return r, eh.Errorf("the reply holds no JSON object")
	}
	var raw struct {
		Answer     any  `json:"answer"`
		Unreadable bool `json:"unreadable"`
	}
	if err = json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
		return r, eh.Errorf("the reply's JSON does not parse: %w", err)
	}
	r.Unreadable = raw.Unreadable
	switch a := raw.Answer.(type) {
	case []any:
		for _, x := range a {
			r.Answer = append(r.Answer, scalarString(x))
		}
	case nil:
	default:
		r.Answer = []string{scalarString(a)}
	}
	return r, nil
}

func scalarString(x any) string {
	switch v := x.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	b, _ := json.Marshal(x)
	return string(b)
}

// Matches compares an answer with the expected values under the question's
// comparison. Strings compare case- and space-insensitively; two values that
// both read as numbers compare as numbers, so "12.50" answers "12.5".
func Matches(q Question, given []string) bool {
	switch q.Compare {
	case "approx":
		if len(given) != 1 || len(q.Expected) != 1 {
			return false
		}
		g, okG := number(given[0])
		e, okE := number(q.Expected[0])
		return okG && okE && math.Abs(g-e) <= q.Tol
	case "set":
		a, b := normAll(given), normAll(q.Expected)
		slices.Sort(a)
		slices.Sort(b)
		return slices.EqualFunc(slices.Compact(a), slices.Compact(b), same)
	default:
		return len(given) == len(q.Expected) && slices.EqualFunc(normAll(given), normAll(q.Expected), same)
	}
}

func normAll(ss []string) (out []string) {
	out = make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, norm(s))
	}
	return out
}

func norm(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func same(a, b string) bool {
	if x, ok := number(a); ok {
		if y, ok2 := number(b); ok2 {
			return x == y
		}
	}
	return a == b
}

func number(s string) (f float64, ok bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	s = strings.TrimSuffix(s, "%")
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}
