package judge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scripted answers each question by its prompt and counts calls.
type scripted struct {
	answers map[string]string
	calls   int
	seen    []llm.Request
}

func (inst *scripted) Complete(_ context.Context, r llm.Request) (llm.Response, error) {
	inst.calls++
	inst.seen = append(inst.seen, r)
	for prompt, a := range inst.answers {
		if len(r.Messages) == 2 && strings.Contains(r.Messages[1].Content, prompt) {
			return llm.Response{Content: a}, nil
		}
	}
	return llm.Response{}, errors.New("no scripted answer")
}

func TestAskChecksAndCaches(t *testing.T) {
	m := &scripted{answers: map[string]string{
		"busiest?":  "Sure. ```json\n{\"answer\": [\"HOST-03\"], \"unreadable\": false}\n```",
		"over 80?":  `{"answer": ["host-05", "host-01"]}`,
		"mem of 3?": `{"answer": 41.25}`,
		"hidden?":   `{"answer": [], "unreadable": true}`,
	}}
	j := &Judge{Client: m, Model: "vision-1", CacheDir: t.TempDir()}
	qs := []Question{
		{ID: "busiest", Prompt: "busiest?", Expected: []string{"host-03"}},
		{ID: "over", Prompt: "over 80?", Expected: []string{"host-01", "host-05"}, Compare: "set"},
		{ID: "mem", Prompt: "mem of 3?", Expected: []string{"41.2"}, Compare: "approx", Tol: 0.1},
		{ID: "hidden", Prompt: "hidden?", Expected: []string{"x"}},
		{ID: "none", Prompt: "no script", Expected: []string{"x"}},
	}
	png := []byte{0x89, 'P', 'N', 'G'}
	vs := j.Ask(context.Background(), png, "", "compare hosts", qs)
	require.Len(t, vs, 5)
	assert.True(t, vs[0].Correct, "case-insensitive, fenced JSON parsed")
	assert.True(t, vs[1].Correct, "set comparison ignores order")
	assert.True(t, vs[2].Correct, "a bare number within tolerance")
	assert.False(t, vs[3].Correct)
	assert.True(t, vs[3].Unreadable)
	assert.NotEmpty(t, vs[4].Error, "a failed call is recorded, not guessed")
	assert.Equal(t, 5, m.calls)

	req := m.seen[0]
	require.Len(t, req.Messages[1].Images, 1, "the rendering travels as an image")
	assert.Equal(t, "image/png", req.Messages[1].Images[0].MediaType)
	assert.Equal(t, Purpose, req.Purpose)
	require.NotNil(t, req.Temperature)
	assert.Zero(t, *req.Temperature)

	again := j.Ask(context.Background(), png, "", "compare hosts", qs[:4])
	assert.Equal(t, 5, m.calls, "answered questions are served from the cache")
	for _, v := range again {
		assert.True(t, v.Cached)
	}

	other := &Judge{Client: m, Model: "vision-2", CacheDir: j.CacheDir}
	other.Ask(context.Background(), png, "", "compare hosts", qs[:1])
	assert.Equal(t, 6, m.calls, "another model is another measurement")
}

func TestBudget(t *testing.T) {
	m := &scripted{answers: map[string]string{"q": `{"answer": ["a"]}`}}
	j := &Judge{Client: m, Model: "m", MaxCalls: 1}
	vs := j.Ask(context.Background(), []byte{1}, "", "", []Question{
		{ID: "1", Prompt: "q", Expected: []string{"a"}},
		{ID: "2", Prompt: "q", Expected: []string{"a"}},
	})
	assert.True(t, vs[0].Correct)
	assert.Contains(t, vs[1].Error, "budget")
	assert.Equal(t, 1, j.Calls())
}

func TestMatches(t *testing.T) {
	assert.True(t, Matches(Question{Expected: []string{"12.5"}}, []string{"12.50"}))
	assert.True(t, Matches(Question{Expected: []string{"1,234"}}, []string{"1234"}))
	assert.False(t, Matches(Question{Expected: []string{"a", "b"}}, []string{"b", "a"}), "eq keeps order")
	assert.False(t, Matches(Question{Expected: []string{"a"}}, []string{"a", "b"}))
	assert.False(t, Matches(Question{Expected: []string{"5"}, Compare: "approx", Tol: 0.1}, []string{"five"}))
}

func TestDrawingKeysTheCache(t *testing.T) {
	m := &scripted{answers: map[string]string{"q": `{"answer": ["a"]}`}}
	j := &Judge{Client: m, Model: "m", CacheDir: t.TempDir()}
	q := []Question{{ID: "1", Prompt: "q", Expected: []string{"a"}}}
	j.Ask(context.Background(), []byte{1, 2, 3}, "drawing-1", "", q)
	j.Ask(context.Background(), []byte{1, 2, 4}, "drawing-1", "", q)
	assert.Equal(t, 1, m.calls, "rasterizer noise in the bytes, same drawing: one call")
	j.Ask(context.Background(), []byte{1, 2, 3}, "drawing-2", "", q)
	assert.Equal(t, 2, m.calls)
}
