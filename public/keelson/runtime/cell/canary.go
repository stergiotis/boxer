package cell

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// Canaries (ADR-0312 §SD6): two exact checks over an outbound payload. An
// exact canary finds a planted token; shingles find a verbatim copy of a
// protected body with no token inserted. Both are cheap and miss a
// paraphrase, which the label is for. Similarity fingerprints flag and
// never gate, and are the trail's. The model service runs Leaks over
// every payload before it leaves (law L10).

// ExactCanary returns the tokens that occur in payload, in the order
// given.
func ExactCanary(payload string, tokens []string) (found []string) {
	for _, t := range tokens {
		if t != "" && strings.Contains(payload, t) {
			found = append(found, t)
		}
	}
	return
}

// ShingleSize is the word count of a shingle.
const ShingleSize = 4

// Shingles are the n-word windows of body, over its whitespace-split
// words, lower-cased, sorted and without duplicates; a body shorter than
// n words has one shingle, the whole of it.
func Shingles(body string, n int) (out []string) {
	words := strings.Fields(strings.ToLower(body))
	if len(words) == 0 {
		return nil
	}
	if n <= 0 {
		n = ShingleSize
	}
	if len(words) < n {
		return []string{strings.Join(words, " ")}
	}
	out = make([]string, 0, len(words)-n+1)
	for i := 0; i+n <= len(words); i++ {
		out = append(out, strings.Join(words[i:i+n], " "))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ShingleLeak says a shingle of the protected body occurs in the payload:
// a verbatim copy of at least ShingleSize words.
func ShingleLeak(payload string, protected string) bool {
	have := make(map[string]bool)
	for _, s := range Shingles(payload, ShingleSize) {
		have[s] = true
	}
	for _, s := range Shingles(protected, ShingleSize) {
		if have[s] {
			return true
		}
	}
	return false
}

// Leaks is law L10 over one payload about to leave by a sink: the scopes
// whose canary token or protected body the payload carries at a tier
// above the sink's clearance. Empty means the payload may go.
func (inst Declaration) Leaks(payload string, clearance agentrun.TierE) (scopes []string) {
	for _, c := range inst.Canaries {
		if c.Tier > clearance && c.Token != "" && strings.Contains(payload, c.Token) {
			scopes = append(scopes, c.Scope)
		}
	}
	for _, p := range inst.Protected {
		if p.Tier > clearance && ShingleLeak(payload, p.Body) {
			scopes = append(scopes, p.Scope)
		}
	}
	slices.Sort(scopes)
	return slices.Compact(scopes)
}

// L10SQLOver is the canary law for the integration lane over the named
// relations: no outbound payload contains a live canary of a scope its
// sink's clearance excludes. payloads carries (run, ordinal, sink, edge,
// body), sinks keelson('sinks')'s columns, canary (scope, token, tier).
//
// deferred: no served relation carries the outbound bodies (the trail
// retains a call's prompt only under RetainRing, in memory), so there is
// no production composition of this law yet; the model service runs
// Leaks over every payload before it leaves, which is the check that
// gates. Protected bodies likewise have no source but the declaration's
// own field; nothing in the tree fills it yet.
func L10SQLOver(payloads string, sinks string, canary string) string {
	return strings.NewReplacer("{payloads}", payloads, "{sinks}", sinks, "{canary}", canary).Replace(`
SELECT p.run AS run, p.ordinal AS ordinal, p.sink AS sink, c.scope AS scope, c.token AS token
FROM {payloads} AS p
INNER JOIN {sinks} AS s ON s.kind = p.sink AND s.edge = p.edge
INNER JOIN {canary} AS c ON position(p.body, c.token) > 0
WHERE c.tier > s.clearance_rank`)
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }
