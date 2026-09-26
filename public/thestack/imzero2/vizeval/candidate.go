package vizeval

import (
	"encoding/hex"
	"encoding/json/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"lukechampine.com/blake3"
)

// Candidate is one rendering to score: a sink and its resolved options
// (ADR-0257 §SD2). Build it with [NewCandidate], which is what makes two
// spellings of the same rendering — an option left at its default, or set to
// it — one candidate.
type Candidate struct {
	Sink    string `json:"sink"`
	Options Values `json:"options"`
}

// NewCandidate resolves raw options against the catalogued sink's space.
func NewCandidate(sink string, raw map[string]any) (cand Candidate, err error) {
	spec, ok := SinkByID(sink)
	if !ok {
		return Candidate{}, eb.Build().Str("sink", sink).Errorf("unknown sink")
	}
	vals, err := spec.Space.Resolve(raw)
	if err != nil {
		return Candidate{}, eb.Build().Str("sink", sink).Errorf("unable to resolve options: %w", err)
	}
	return Candidate{Sink: sink, Options: vals}, nil
}

// UnmarshalCandidate decodes {"sink": …, "options": {…}} and resolves it. A
// member the format does not have is refused rather than ignored.
func UnmarshalCandidate(b []byte) (cand Candidate, err error) {
	var raw struct {
		Sink    string         `json:"sink"`
		Options map[string]any `json:"options"`
	}
	if err = json.Unmarshal(b, &raw, json.RejectUnknownMembers(true)); err != nil {
		return Candidate{}, eh.Errorf("unable to decode candidate: %w", err)
	}
	return NewCandidate(raw.Sink, raw.Options)
}

// Canonical is the candidate as deterministic JSON — members in name order —
// the form its identity is taken over.
func (inst Candidate) Canonical() (b []byte) {
	b, err := json.Marshal(inst, json.Deterministic(true))
	if err != nil {
		// Values holds only strings, int64s, float64s and bools, which
		// always encode; NaN is refused by Resolve.
		panic(err)
	}
	return b
}

// ID is the candidate's identity: a hex blake3 digest of [Candidate.Canonical],
// truncated to 128 bits. It is the cache key a scorecard is filed under.
func (inst Candidate) ID() string {
	sum := blake3.Sum256(inst.Canonical())
	return hex.EncodeToString(sum[:16])
}
