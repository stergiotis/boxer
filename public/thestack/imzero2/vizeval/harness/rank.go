package harness

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/judge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/vizevalfacts"
	"github.com/zeebo/xxh3"
)

// RankOptions configures a ranking run.
type RankOptions struct {
	// OutDir is a score run's output directory: its scorecards.jsonl says
	// what was scored and its candidate directories hold the artifacts.
	OutDir string
	Judge  *judge.Judge
	// Facts, when set, files every comparison as a vizevalJudgement row.
	Facts  *vizevalfacts.VizevalStore
	Logger zerolog.Logger
}

// RankedCandidate is one candidate's place in a ranking.
type RankedCandidate struct {
	CandidateID string            `json:"candidateId"`
	Candidate   vizeval.Candidate `json:"candidate"`
	// Strength is the Bradley–Terry log-strength over every criterion;
	// ByCriterion the same fitted on one criterion at a time.
	Strength    float64            `json:"strength"`
	ByCriterion map[string]float64 `json:"byCriterion"`
	// Accuracy is task.accuracy when the candidate was judged on questions.
	Accuracy *float64 `json:"accuracy,omitempty"`
	Dir      string   `json:"dir"`
}

// Ranking is a scenario's scored candidates ordered by pairwise preference.
type Ranking struct {
	Scenario    string              `json:"scenario"`
	BatchDigest string              `json:"batchDigest"`
	Model       string              `json:"model"`
	Prompt      string              `json:"prompt"`
	Candidates  []RankedCandidate   `json:"candidates"`
	Verdicts    []judge.PairVerdict `json:"verdicts"`
	Skipped     []string            `json:"skipped,omitempty"`
	At          string              `json:"at"`
}

// Rank compares every pair of the scenario's scored candidates, in both
// orders, and fits a ranking (ADR-0257 §SD6, third layer). Only candidates
// that passed their gates take part — a gated candidate is not ranked — and
// only those drawn from one batch: the scenario's most recent digest.
func Rank(ctx context.Context, sc *vizeval.Scenario, opts RankOptions) (r Ranking, err error) {
	cards, skipped, err := rankable(opts.OutDir, sc.Name)
	if err != nil {
		return r, err
	}
	r = Ranking{Scenario: sc.Name, Model: opts.Judge.Model, Prompt: judge.PairPromptVersion, Skipped: skipped}
	if len(cards) < 2 {
		return r, eb.Build().Str("scenario", sc.Name).Int("candidates", len(cards)).
			Errorf("fewer than two scored candidates to compare; score more, or relax the gates")
	}
	r.BatchDigest = cards[0].BatchDigest
	pics := make([]judge.Picture, 0, len(cards))
	for _, c := range cards {
		png, e := os.ReadFile(filepath.Join(opts.OutDir, c.Dir, "artifact.png"))
		if e != nil {
			return r, eb.Build().Str("candidate", c.CandidateID).Errorf("unable to read the artifact: %w", e)
		}
		pics = append(pics, judge.Picture{ID: c.CandidateID, PNG: png, Drawing: c.DrawingDigest})
	}
	for i := range pics {
		for k := i + 1; k < len(pics); k++ {
			var v judge.PairVerdict
			if pics[i].Drawing != "" && pics[i].Drawing == pics[k].Drawing {
				// Two candidates that drew the same thing: a comparison
				// would only measure the model's noise.
				v = judge.PairVerdict{A: pics[i].ID, B: pics[k].ID, Prefer: make(map[string]string, len(judge.Criteria))}
				for _, c := range judge.Criteria {
					v.Prefer[c.Name] = judge.PreferTie
				}
			} else {
				v = opts.Judge.Compare(ctx, pics[i], pics[k], sc.Spec.Intent)
			}
			if v.Error != "" {
				opts.Logger.Warn().Str("a", v.A).Str("b", v.B).Str("error", v.Error).Msg("comparison failed; left out of the fit")
			}
			r.Verdicts = append(r.Verdicts, v)
		}
	}
	ids := make([]string, 0, len(cards))
	for _, c := range cards {
		ids = append(ids, c.CandidateID)
	}
	overall := judge.Fit(ids, r.Verdicts, "")
	byCrit := make(map[string]judge.Strengths, len(judge.Criteria))
	for _, c := range judge.Criteria {
		byCrit[c.Name] = judge.Fit(ids, r.Verdicts, c.Name)
	}
	byID := make(map[string]Scorecard, len(cards))
	for _, c := range cards {
		byID[c.CandidateID] = c
	}
	for _, id := range overall.Order() {
		c := byID[id]
		rc := RankedCandidate{CandidateID: id, Candidate: c.Candidate, Strength: overall[id], Dir: c.Dir,
			ByCriterion: make(map[string]float64, len(byCrit))}
		for name, s := range byCrit {
			rc.ByCriterion[name] = s[id]
		}
		if a, ok := c.Metrics[MetricTaskAccuracy]; ok {
			rc.Accuracy = &a
		}
		r.Candidates = append(r.Candidates, rc)
	}
	r.At = time.Now().UTC().Format(time.RFC3339)
	if err = writeRanking(filepath.Join(opts.OutDir, sc.Name), r); err != nil {
		return r, err
	}
	if opts.Facts != nil {
		if err = writeJudgements(ctx, opts.Facts, r, pics); err != nil {
			return r, err
		}
	}
	return r, nil
}

// rankable reads the run's scorecards and keeps, per candidate, the latest
// scored card of the scenario, all from the scenario's most recent batch.
func rankable(outDir string, scenario string) (cards []Scorecard, skipped []string, err error) {
	f, err := os.Open(filepath.Join(outDir, "scorecards.jsonl"))
	if err != nil {
		return nil, nil, eh.Errorf("unable to open the run's scorecards: %w", err)
	}
	defer func() { _ = f.Close() }()
	latest := make(map[string]Scorecard, 16)
	var newest Scorecard
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var c Scorecard
		if err = json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, nil, eh.Errorf("unable to decode a scorecard: %w", err)
		}
		if c.Scenario != scenario {
			continue
		}
		if c.At >= newest.At {
			newest = c
		}
		if c.Status != StatusScored {
			continue
		}
		if prev, ok := latest[c.CandidateID]; !ok || c.At >= prev.At {
			latest[c.CandidateID] = c
		}
	}
	if err = sc.Err(); err != nil {
		return nil, nil, eh.Errorf("unable to read the run's scorecards: %w", err)
	}
	for _, c := range latest {
		switch {
		case c.BatchDigest != newest.BatchDigest:
			skipped = append(skipped, c.CandidateID+": scored over other data ("+c.BatchDigest+")")
		case c.Dir == "" || !nonEmpty(filepath.Join(outDir, c.Dir, "artifact.png")):
			skipped = append(skipped, c.CandidateID+": its artifact is not in this output directory")
		default:
			cards = append(cards, c)
		}
	}
	slices.SortFunc(cards, func(a, b Scorecard) int { return strings.Compare(a.CandidateID, b.CandidateID) })
	slices.Sort(skipped)
	return cards, skipped, nil
}

// JudgementKey is the identity of one comparison: the same two drawings,
// compared by the same model under the same prompt, in one scenario.
func JudgementKey(scenario, prompt, model, drawingA, drawingB string) (id uint64, nk string) {
	nk = "vizevalJudgement/" + scenario + "/" + prompt + "/" + model + "/" + drawingA + "/" + drawingB
	return xxh3.HashString(nk), nk
}

func writeJudgements(ctx context.Context, store *vizevalfacts.VizevalStore, r Ranking, pics []judge.Picture) (err error) {
	drawing := make(map[string]string, len(pics))
	for _, p := range pics {
		drawing[p.ID] = p.Drawing
	}
	ts := time.Now().UTC()
	for _, v := range r.Verdicts {
		if v.Error != "" {
			continue
		}
		id, nk := JudgementKey(r.Scenario, r.Prompt, r.Model, drawing[v.A], drawing[v.B])
		row := vizevalfacts.VizevalJudgement{
			Id: id, NaturalKey: []byte(nk), Ts: ts, Kind: "vizevalJudgement",
			Scenario: r.Scenario, BatchDigest: r.BatchDigest, Model: r.Model, Prompt: r.Prompt,
			A: v.A, B: v.B, DrawingA: drawing[v.A], DrawingB: drawing[v.B],
		}
		for _, c := range judge.Criteria {
			row.Criterion = append(row.Criterion, c.Name)
			row.Preference = append(row.Preference, v.Prefer[c.Name])
			row.Why = append(row.Why, v.Why[c.Name])
		}
		err = store.Begin(row.Id, row.Ts, vizevalfacts.VizevalEnvelope{NaturalKey: row.NaturalKey}).AddVizevalJudgement(row).Commit()
		if err != nil {
			return eh.Errorf("unable to stage a judgement row: %w", err)
		}
	}
	if _, err = store.Flush(ctx); err != nil {
		return eh.Errorf("unable to write judgement rows: %w", err)
	}
	return nil
}

func writeRanking(dir string, r Ranking) (err error) {
	b, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		return eh.Errorf("unable to encode the ranking: %w", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "ranking.json"), b, 0o644); err != nil {
		return eh.Errorf("unable to write the ranking: %w", err)
	}
	var md strings.Builder
	md.WriteString("# " + r.Scenario + " — ranking\n\nGenerated by `imzero2 vizeval rank` (ADR-0257) — " + r.At +
		". Model `" + r.Model + "`, prompt `" + r.Prompt + "`, batch `" + r.BatchDigest + "`.\n\n" +
		"Strength is a Bradley–Terry log-strength over pairwise preferences asked in both orders; " +
		"a difference of 1 is odds of e to 1. Criteria: ")
	names := make([]string, 0, len(judge.Criteria))
	for _, c := range judge.Criteria {
		names = append(names, c.Name)
	}
	md.WriteString(strings.Join(names, ", ") + ".\n\n| rank | candidate | strength | " + strings.Join(names, " | ") + " | task accuracy |\n|")
	md.WriteString(strings.Repeat(" --- |", 4+len(names)) + "\n")
	for i, c := range r.Candidates {
		row := "| " + strconv.Itoa(i+1) + " | `" + string(c.Candidate.Canonical()) + "` | " + fmtF(c.Strength) + " |"
		for _, n := range names {
			row += " " + fmtF(c.ByCriterion[n]) + " |"
		}
		acc := "—"
		if c.Accuracy != nil {
			acc = fmtF(*c.Accuracy)
		}
		md.WriteString(row + " " + acc + " |\n")
	}
	md.WriteString("\n")
	for i, c := range r.Candidates {
		md.WriteString("## " + strconv.Itoa(i+1) + ". " + c.Candidate.Sink + "\n\n`" + string(c.Candidate.Canonical()) +
			"`\n\n![" + c.CandidateID + "](" + c.CandidateID + "/artifact.png)\n\n")
	}
	if len(r.Skipped) > 0 {
		md.WriteString("## Not ranked\n\n")
		for _, s := range r.Skipped {
			md.WriteString("- " + s + "\n")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "ranking.md"), []byte(md.String()), 0o644); err != nil {
		return eh.Errorf("unable to write the ranking page: %w", err)
	}
	return nil
}

func fmtF(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
