package judge

import (
	"context"
	"encoding/json/v2"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pairModel answers every criterion by a rule over the two images it is
// shown, the first byte of each standing for the picture.
type pairModel struct {
	rule  func(first, second byte) string
	calls int
}

func (inst *pairModel) Complete(_ context.Context, r llm.Request) (llm.Response, error) {
	inst.calls++
	imgs := r.Messages[1].Images
	better := inst.rule(imgs[0].Data[0], imgs[1].Data[0])
	out := make(map[string]pairAnswer, len(Criteria))
	for _, c := range Criteria {
		out[c.Name] = pairAnswer{Better: better, Why: "because"}
	}
	b, _ := json.Marshal(out)
	return llm.Response{Content: "Here you go: " + string(b)}, nil
}

func pic(id string, b byte) Picture { return Picture{ID: id, PNG: []byte{b}} }

func TestCompareCancelsPositionBias(t *testing.T) {
	biased := &pairModel{rule: func(_, _ byte) string { return "1" }}
	j := &Judge{Client: biased, Model: "m"}
	v := j.Compare(context.Background(), pic("x", 1), pic("y", 2), "")
	require.Empty(t, v.Error)
	assert.Equal(t, 2, biased.calls, "both orders are asked")
	for _, c := range Criteria {
		assert.Equal(t, PreferSplit, v.Prefer[c.Name], "a model that prefers whatever it saw first wins nothing")
	}

	larger := &pairModel{rule: func(a, b byte) string {
		if a > b {
			return "1"
		}
		return "2"
	}}
	j = &Judge{Client: larger, Model: "m", CacheDir: t.TempDir()}
	v = j.Compare(context.Background(), pic("x", 1), pic("y", 2), "")
	for _, c := range Criteria {
		assert.Equal(t, PreferB, v.Prefer[c.Name], "a consistent preference survives the swap")
	}
	assert.Equal(t, "because", v.Why["clutter"])
	_ = j.Compare(context.Background(), pic("x", 1), pic("y", 2), "")
	assert.Equal(t, 2, larger.calls, "the second comparison is served from the cache")
}

func TestFitOrdersAChain(t *testing.T) {
	larger := &pairModel{rule: func(a, b byte) string {
		if a > b {
			return "1"
		}
		return "2"
	}}
	j := &Judge{Client: larger, Model: "m"}
	ps := []Picture{pic("low", 1), pic("mid", 2), pic("high", 3)}
	var vs []PairVerdict
	for i := range ps {
		for k := i + 1; k < len(ps); k++ {
			vs = append(vs, j.Compare(context.Background(), ps[i], ps[k], ""))
		}
	}
	s := Fit([]string{"low", "mid", "high"}, vs, "")
	assert.Equal(t, []string{"high", "mid", "low"}, s.Order())
	assert.False(t, s["high"] > 1e6, "an undefeated candidate keeps a finite strength")
	assert.InDelta(t, 0, s["low"]+s["mid"]+s["high"], 1e-9, "strengths are centred")

	perCrit := Fit([]string{"low", "mid", "high"}, vs, "clutter")
	assert.Equal(t, s.Order(), perCrit.Order())

	tie := Fit([]string{"a", "b"}, []PairVerdict{{A: "a", B: "b", Prefer: map[string]string{"x": PreferSplit}}}, "")
	assert.InDelta(t, tie["a"], tie["b"], 1e-9, "a split favours nobody")
}
