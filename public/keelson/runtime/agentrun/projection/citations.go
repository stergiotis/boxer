package projection

// The citation laws (ADR-0340 (proposed) §SD5) and the fan-out selection
// rule (§SD6), as checks over a run's entries: each returns the rows that
// break it, in the shape CheckCompaction has.

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

// referencePattern is a reply's reference to an assertion: its id in
// square brackets, letters then digits, so a checkbox [x], a footnote
// [^1] and a link [text](url) are not references.
var referencePattern = regexp.MustCompile(`\[([a-z]{1,4}[0-9]{1,4})\]`)

// fencePattern is a fenced code block or an inline code span, which the
// laws read past.
var fencePattern = regexp.MustCompile("(?s)```.*?```|`[^`\n]*`")

// References are the assertion ids a reply's text refers to, in order,
// outside code fences, each once.
func References(text string) (ids []string) {
	for _, m := range referencePattern.FindAllStringSubmatch(fencePattern.ReplaceAllString(text, ""), -1) {
		if !slices.Contains(ids, m[1]) {
			ids = append(ids, m[1])
		}
	}
	return
}

// The verdict outcomes a check that ran reports (ADR-0340 §SD1).
const (
	VerdictHeld      = "held"
	VerdictFailed    = "failed"
	VerdictMoved     = "moved"
	VerdictUnchecked = "unchecked"
)

// Verdicts are the latest verdict per assertion id of one turn, from the
// entries, in ordinal order of their first check.
func Verdicts(entries []agentrun.Entry, turn string) (out []agentrun.Entry) {
	at := map[string]int{}
	for _, e := range entries {
		if e.Kind != agentrun.KindVerdict || e.Turn != turn || e.Verdict == nil {
			continue
		}
		if i, ok := at[e.Name]; ok {
			out[i] = e
			continue
		}
		at[e.Name] = len(out)
		out = append(out, e)
	}
	return
}

// CheckCitations is the citation laws over a run's entries (ADR-0340
// §SD5), returning one line per breach:
//
//   - L36 — every reference in a reply names a verdict of the same turn
//     written before the reply;
//   - L37 — a verdict whose check ran rests on query outcomes of the run
//     that precede it, and carries its statement digest, the source's and
//     the endpoint;
//   - L38 — every figure a reply states outside a code fence is accounted
//     for by a result the turn read, an assertion's observed value or
//     expectation, or the person's message of the turn; a report, not a
//     refusal, listed apart by Uncited.
func CheckCitations(entries []agentrun.Entry) (broken []string) {
	es := slices.Clone(entries)
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	ord := func(o uint32) string { return strconv.FormatUint(uint64(o), 10) }
	intents := map[string]agentrun.Entry{}
	outcomes := map[string]agentrun.Entry{}
	for _, e := range es {
		switch e.Kind {
		case agentrun.KindIntent:
			intents[e.Key] = e
		case agentrun.KindOutcome:
			if e.Role == RoleTool {
				outcomes[e.Key] = e
			}
		}
	}
	for _, e := range es {
		switch e.Kind {
		case agentrun.KindReply:
			for _, id := range References(e.Body) {
				found := false
				for _, v := range es {
					if v.Kind == agentrun.KindVerdict && v.Turn == e.Turn && v.Name == id && v.Ordinal < e.Ordinal {
						found = true
						break
					}
				}
				if !found {
					broken = append(broken, "L36: reply "+ord(e.Ordinal)+" refers to ["+id+"], which no verdict of its turn names before it")
				}
			}
		case agentrun.KindVerdict:
			v := e.Verdict
			if v == nil {
				broken = append(broken, "L37: verdict "+ord(e.Ordinal)+" carries no record")
				continue
			}
			ran := v.Outcome == VerdictHeld || v.Outcome == VerdictFailed || v.Outcome == VerdictMoved
			if !ran {
				continue
			}
			if v.Digest == "" || v.Source == "" || v.Endpoint == "" {
				broken = append(broken, "L37: verdict "+ord(e.Ordinal)+" ran without its statement digest, its source's or its endpoint")
			}
			if len(v.RestsOn) == 0 {
				broken = append(broken, "L37: verdict "+ord(e.Ordinal)+" rests on no call")
			}
			for _, k := range v.RestsOn {
				o, ok := outcomes[k]
				in, has := intents[k]
				switch {
				case !ok || !has:
					broken = append(broken, "L37: verdict "+ord(e.Ordinal)+" rests on "+k+", which is no tool call of the run")
				case o.Ordinal >= e.Ordinal:
					broken = append(broken, "L37: verdict "+ord(e.Ordinal)+" rests on "+k+", whose outcome follows it")
				case in.Effect != agentrun.EffectNone && in.Effect != agentrun.EffectView:
					broken = append(broken, "L37: verdict "+ord(e.Ordinal)+" rests on "+k+", a command")
				}
			}
		}
	}
	return
}

// figurePattern is a figure as a reply states it.
var figurePattern = regexp.MustCompile(`\d[\d,']*(?:\.\d+)?`)

// listMarkerPattern is an ordered list's marker at a line's start, which
// is no figure.
var listMarkerPattern = regexp.MustCompile(`(?m)^\s*\d+[.)]\s`)

// Uncited is law L38 as a report: for each reply, the figures it states
// outside code fences, references and list markers, that no result the
// turn read, no assertion's observed value or expectation and no message
// of the person in that turn accounts for; one line per (reply, figure).
func Uncited(entries []agentrun.Entry) (lines []string) {
	es := slices.Clone(entries)
	slices.SortFunc(es, func(a, b agentrun.Entry) int { return int(a.Ordinal) - int(b.Ordinal) })
	accounted := map[string]*strings.Builder{}
	add := func(turn string, text string) {
		b, ok := accounted[turn]
		if !ok {
			b = &strings.Builder{}
			accounted[turn] = b
		}
		b.WriteString(normalizeFigures(text))
		b.WriteByte('\n')
	}
	for _, e := range es {
		switch e.Kind {
		case agentrun.KindUser:
			add(e.Turn, e.Body)
		case agentrun.KindOutcome:
			if e.Role == RoleTool {
				add(e.Turn, e.Body)
			}
		case agentrun.KindVerdict:
			add(e.Turn, e.Body)
			add(e.Turn, e.Arguments)
		}
	}
	for _, e := range es {
		if e.Kind != agentrun.KindReply {
			continue
		}
		text := referencePattern.ReplaceAllString(fencePattern.ReplaceAllString(e.Body, ""), "")
		text = listMarkerPattern.ReplaceAllString(text, "")
		have := ""
		if b := accounted[e.Turn]; b != nil {
			have = b.String()
		}
		for _, f := range figurePattern.FindAllString(text, -1) {
			n := normalizeFigures(f)
			if strings.Contains(have, n) {
				continue
			}
			lines = append(lines, "L38: reply "+strconv.FormatUint(uint64(e.Ordinal), 10)+" states "+f+", which nothing the turn read accounts for")
		}
	}
	return
}

// normalizeFigures drops thousands separators so a figure matches however
// it was grouped.
func normalizeFigures(s string) string {
	return strings.NewReplacer(",", "", "'", "").Replace(s)
}

// SelectCandidate is the fan-out selection rule as a fold over the
// entries (ADR-0340 §SD6): among the candidate entries, the one with no
// failed verdict and the most held, ties broken by the fewest unchecked,
// then the lowest ordinal; a verdict counts for the candidate whose key
// it carries. ok is false without a candidate that qualifies.
func SelectCandidate(entries []agentrun.Entry) (ordinal uint32, ok bool) {
	type score struct {
		ordinal                 uint32
		held, failed, unchecked int
	}
	var cands []*score
	byKey := map[string]*score{}
	for _, e := range entries {
		if e.Kind == agentrun.KindCandidate {
			s := &score{ordinal: e.Ordinal}
			cands = append(cands, s)
			byKey[e.Key] = s
		}
	}
	for _, e := range entries {
		if e.Kind != agentrun.KindVerdict || e.Verdict == nil {
			continue
		}
		s, has := byKey[e.Key]
		if !has {
			continue
		}
		switch e.Verdict.Outcome {
		case VerdictHeld:
			s.held++
		case VerdictFailed:
			s.failed++
		default:
			s.unchecked++
		}
	}
	var best *score
	for _, s := range cands {
		if s.failed > 0 {
			continue
		}
		if best == nil || s.held > best.held || (s.held == best.held && (s.unchecked < best.unchecked || (s.unchecked == best.unchecked && s.ordinal < best.ordinal))) {
			best = s
		}
	}
	if best == nil {
		return 0, false
	}
	return best.ordinal, true
}
