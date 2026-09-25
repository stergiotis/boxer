package judge

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"lukechampine.com/blake3"
)

// PairPromptVersion names the comparison prompt; bump it when the prompt or
// the criteria change.
const PairPromptVersion = "pair-v1"

// PairPurpose is what the llm call table records the comparisons as.
const PairPurpose = "vizeval: compare two renderings of the same data"

// Criterion is one question a comparison answers. The five are the Tier 2
// rubric's perception criteria (V1 clutter, V2 colour encoding, V4 density,
// V5 legends, V7 typographic rhythm) restated for two renderings of one
// dataset rather than one screenshot against the design system.
type Criterion struct {
	Name     string
	Question string
}

// Criteria are asked in every comparison, in this order.
var Criteria = []Criterion{
	{"clutter", "Which makes what matters for the stated purpose easiest to find: a clear focal point, secondary detail visibly subordinate, nothing competing for attention without reason?"},
	{"colour", "Which uses colour more purposefully: colour that encodes something and does so consistently, and no more colours than that needs?"},
	{"density", "Which fits the amount of data better: neither cramped nor wasteful of space?"},
	{"labels", "Which labels its content more completely: names, units and headings visible, nothing a reader has to guess or look up?"},
	{"typography", "Which has the more consistent typographic rhythm: regular spacing, a type hierarchy that tells headings from body from captions?"},
}

// Preferences a comparison records per criterion.
const (
	PreferA     = "a"
	PreferB     = "b"
	PreferTie   = "tie"
	PreferSplit = "split" // the two orders disagreed: no preference survived the swap
)

const pairSystemPrompt = `You are shown two renderings of the same dataset, labelled Rendering 1 and Rendering 2, each cropped to the rendering itself.
Compare them on each criterion you are given, and only on those criteria; do not invent other rules or grade general taste.
Where neither is clearly better on a criterion, say tie.
Reply with one JSON object and nothing else, with one member per criterion name: {"<criterion>": {"better": "1" | "2" | "tie", "why": "<one sentence>"}}.`

// Picture is one candidate's artifact as a comparison sees it.
type Picture struct {
	// ID is the candidate id, how the verdict names the two sides.
	ID  string
	PNG []byte
	// Drawing is the geometry digest of what the PNG shows, the cache's
	// identity for it; empty keys on the bytes.
	Drawing string
}

// PairVerdict is a comparison of A and B, asked in both orders.
type PairVerdict struct {
	A string `json:"a"`
	B string `json:"b"`
	// Prefer maps each criterion to a, b, tie or split.
	Prefer map[string]string `json:"prefer"`
	// Why is the model's reason per criterion, from the A-first order.
	Why    map[string]string `json:"why,omitempty"`
	Error  string            `json:"error,omitempty"`
	Cached bool              `json:"cached,omitempty"`
}

// Compare asks which of two renderings is better on each criterion, once
// with A shown first and once with B first. A preference stands only when
// both orders agree; a model that prefers whichever it saw first — the bias
// this is built to cancel — produces splits, not wins.
func (inst *Judge) Compare(ctx context.Context, a Picture, b Picture, intent string) (v PairVerdict) {
	v = PairVerdict{A: a.ID, B: b.ID, Prefer: make(map[string]string, len(Criteria))}
	ab, cachedAB, err := inst.pairOrder(ctx, a, b, intent)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	ba, cachedBA, err := inst.pairOrder(ctx, b, a, intent)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.Cached = cachedAB && cachedBA
	v.Why = make(map[string]string, len(Criteria))
	for _, c := range Criteria {
		first := sideOf(ab[c.Name].Better, PreferA, PreferB)
		second := sideOf(ba[c.Name].Better, PreferB, PreferA)
		switch {
		case first == second:
			v.Prefer[c.Name] = first
		default:
			v.Prefer[c.Name] = PreferSplit
		}
		v.Why[c.Name] = ab[c.Name].Why
	}
	return v
}

// sideOf maps the model's "1"/"2"/"tie" onto the side shown first or second.
func sideOf(better string, first string, second string) string {
	switch strings.TrimSpace(strings.ToLower(better)) {
	case "1", "rendering 1":
		return first
	case "2", "rendering 2":
		return second
	}
	return PreferTie
}

type pairAnswer struct {
	Better string `json:"better"`
	Why    string `json:"why"`
}

func pictureKey(p Picture) string {
	if p.Drawing != "" {
		return p.Drawing
	}
	sum := blake3.Sum256(p.PNG)
	return "png:" + hex.EncodeToString(sum[:])
}

func (inst *Judge) pairOrder(ctx context.Context, first Picture, second Picture, intent string) (ans map[string]pairAnswer, cached bool, err error) {
	h := blake3.New(32, nil)
	for _, part := range []string{PairPromptVersion, inst.Model, pictureKey(first), pictureKey(second), intent} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	key := "pair-" + hex.EncodeToString(h.Sum(nil)[:16])
	if inst.CacheDir != "" {
		if b, e := os.ReadFile(filepath.Join(inst.CacheDir, key+".json")); e == nil {
			if e = json.Unmarshal(b, &ans); e == nil {
				return ans, true, nil
			}
		}
	}
	if inst.MaxCalls > 0 && inst.calls >= inst.MaxCalls {
		return nil, false, eh.Errorf("the run's model-call budget is spent")
	}
	inst.calls++
	var user strings.Builder
	if intent != "" {
		user.WriteString("What the renderings are for: " + intent + "\n\n")
	}
	user.WriteString("Criteria:\n")
	for _, c := range Criteria {
		user.WriteString("- " + c.Name + ": " + c.Question + "\n")
	}
	zero := float32(0)
	res, err := inst.Client.Complete(ctx, llm.Request{
		Purpose:     PairPurpose,
		Temperature: &zero,
		Messages: []openaichat.Message{
			{Role: openaichat.ChatRoleSystem, Content: pairSystemPrompt},
			{Role: openaichat.ChatRoleUser, Content: user.String(), Images: []openaichat.Image{
				{MediaType: "image/png", Data: first.PNG}, {MediaType: "image/png", Data: second.PNG},
			}},
		},
	})
	if err != nil {
		return nil, false, eh.Errorf("the model call failed: %w", err)
	}
	content := res.Content
	start, end := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return nil, false, eh.Errorf("the reply holds no JSON object")
	}
	if err = json.Unmarshal([]byte(content[start:end+1]), &ans); err != nil {
		return nil, false, eh.Errorf("the reply's JSON does not parse: %w", err)
	}
	if inst.CacheDir != "" {
		if err = os.MkdirAll(inst.CacheDir, 0o755); err == nil {
			if b, e := json.Marshal(ans, json.Deterministic(true)); e == nil {
				err = os.WriteFile(filepath.Join(inst.CacheDir, key+".json"), b, 0o644)
			}
		}
		if err != nil {
			return nil, false, eh.Errorf("unable to cache a comparison: %w", err)
		}
	}
	return ans, false, nil
}
