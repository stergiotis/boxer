package harness

import (
	"context"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/vizevalfacts"
	"github.com/zeebo/xxh3"
)

// kindLabel is the facts row's kind label.
const kindLabel = "vizevalScore"

// ScoreKey is the identity of one measurement: the same candidate over the
// same data at the same build is the same measurement, and is filed under the
// same key (ADR-0257 §SD8).
func ScoreKey(scenario string, candidateID string, build string, digest string) (id uint64, nk string) {
	nk = "vizeval/" + scenario + "/" + candidateID + "/" + build + "/" + digest
	return xxh3.HashString(nk), nk
}

// reusable reports whether a stored scorecard may stand in for rendering
// again. A dirty build is never reused, nor one with no revision at all (a
// binary built without VCS stamping, a test binary): neither names the code
// that drew it. A failed attempt is not a measurement.
func reusable(card Scorecard) bool {
	return !strings.HasSuffix(card.Build, "+dirty") && card.Build != unknownBuild &&
		(card.Status == StatusScored || card.Status == StatusGated || card.Status == StatusInadmissible)
}

// RowOf is a scorecard as a facts row.
func RowOf(card Scorecard) (row vizevalfacts.VizevalScore) {
	id, nk := ScoreKey(card.Scenario, card.CandidateID, card.Build, card.BatchDigest)
	at, err := time.Parse(time.RFC3339, card.At)
	if err != nil {
		at = time.Now()
	}
	row = vizevalfacts.VizevalScore{
		Id: id, NaturalKey: []byte(nk), Ts: at.UTC(),
		Kind:     kindLabel,
		Scenario: card.Scenario, CandidateId: card.CandidateID, Sink: card.Candidate.Sink,
		Candidate: []string{string(card.Candidate.Canonical())},
		Build:     card.Build, BatchDigest: card.BatchDigest, Rows: uint64(max(card.Rows, 0)),
		Status: string(card.Status),
	}
	if card.Reason != "" {
		row.Reason = []string{card.Reason}
	}
	if card.Dir != "" {
		row.Dir = []string{card.Dir}
	}
	if card.Metrics != nil {
		row.Area = card.Area[:]
		names := make([]string, 0, len(card.Metrics))
		for n := range card.Metrics {
			names = append(names, n)
		}
		slices.Sort(names)
		row.MetricName = names
		row.MetricValue = make([]float64, 0, len(names))
		for _, n := range names {
			row.MetricValue = append(row.MetricValue, card.Metrics[n])
		}
	}
	for n, pass := range card.Gates {
		if pass {
			row.GatePassed = append(row.GatePassed, n)
		} else {
			row.GateFailed = append(row.GateFailed, n)
		}
	}
	slices.Sort(row.GatePassed)
	slices.Sort(row.GateFailed)
	return row
}

// CardOf is a facts row read back as a scorecard.
func CardOf(row vizevalfacts.VizevalScore) (card Scorecard, err error) {
	card = Scorecard{
		Scenario: row.Scenario, CandidateID: row.CandidateId, Build: row.Build,
		BatchDigest: row.BatchDigest, Rows: int64(row.Rows), Status: StatusE(row.Status),
		At: row.Ts.UTC().Format(time.RFC3339),
	}
	if len(row.Candidate) > 0 {
		if card.Candidate, err = vizeval.UnmarshalCandidate([]byte(row.Candidate[0])); err != nil {
			return Scorecard{}, eh.Errorf("unable to read the stored candidate: %w", err)
		}
	}
	if len(row.Reason) > 0 {
		card.Reason = row.Reason[0]
	}
	if len(row.Dir) > 0 {
		card.Dir = row.Dir[0]
	}
	if len(row.Area) == 4 {
		copy(card.Area[:], row.Area)
	}
	if len(row.MetricName) > 0 {
		card.Metrics = make(map[string]float64, len(row.MetricName))
		for i, n := range row.MetricName {
			if i < len(row.MetricValue) {
				card.Metrics[n] = row.MetricValue[i]
			}
		}
	}
	if len(row.GatePassed)+len(row.GateFailed) > 0 {
		card.Gates = make(map[string]bool, len(row.GatePassed)+len(row.GateFailed))
		for _, n := range row.GatePassed {
			card.Gates[n] = true
		}
		for _, n := range row.GateFailed {
			card.Gates[n] = false
		}
	}
	return card, nil
}

// OpenFacts binds the scorecard store to boxer.facts at the configured
// endpoint, creating the table when it does not exist. chstore is the table's
// only DDL author (ADR-0184 §SD2), so the table is set up through it and the
// generated store runs none.
func OpenFacts(ctx context.Context) (store *vizevalfacts.VizevalStore, err error) {
	cs, err := chstore.New(chstore.ConfigFromEnv())
	if err != nil {
		return nil, eh.Errorf("unable to configure boxer.facts: %w", err)
	}
	if err = cs.SetupTable(ctx, ""); err != nil {
		return nil, eh.Errorf("unable to set up boxer.facts: %w", err)
	}
	exec, err := storeexec.New(chclient.New(chclient.ConfigFromEnv(), nil), nil)
	if err != nil {
		return nil, err
	}
	store = vizevalfacts.NewVizevalStore(exec, nil, vizevalfacts.VizevalStoreConfig{})
	if err = store.VerifySchema(ctx); err != nil {
		return nil, eh.Errorf("boxer.facts does not have the shape the scorecard store decodes: %w", err)
	}
	return store, nil
}

// lookupStored returns the newest stored scorecard for the measurement, if
// one may be reused.
func lookupStored(ctx context.Context, store *vizevalfacts.VizevalStore, card Scorecard) (stored Scorecard, found bool, err error) {
	id, _ := ScoreKey(card.Scenario, card.CandidateID, card.Build, card.BatchDigest)
	ent, found, err := store.Latest(ctx, id)
	if err != nil || !found || ent == nil || !ent.VizevalScore.Has {
		return Scorecard{}, false, err
	}
	if stored, err = CardOf(ent.VizevalScore.Val); err != nil {
		return Scorecard{}, false, err
	}
	return stored, reusable(stored), nil
}

// ReadFacts yields every scorecard filed in boxer.facts, oldest first; with a
// scenario, only that scenario's. The scenario filter is applied after
// decoding: the kind's scan is already narrow, and a membership-value
// predicate would bind this reader to the table's physical column names.
func ReadFacts(ctx context.Context, store *vizevalfacts.VizevalStore, scenario string) iter.Seq2[Scorecard, error] {
	return func(yield func(Scorecard, error) bool) {
		for ent, err := range store.ScanVizevalScore(ctx, recordstore.ScanOpts{}) {
			if err != nil {
				yield(Scorecard{}, eh.Errorf("unable to scan scorecards: %w", err))
				return
			}
			if ent == nil || !ent.VizevalScore.Has {
				continue
			}
			if scenario != "" && ent.VizevalScore.Val.Scenario != scenario {
				continue
			}
			card, e := CardOf(ent.VizevalScore.Val)
			if !yield(card, e) || e != nil {
				return
			}
		}
	}
}

// writeFacts files freshly scored cards and flushes them.
func writeFacts(ctx context.Context, store *vizevalfacts.VizevalStore, cards []Scorecard) (err error) {
	for _, c := range cards {
		if c.ReusedFrom != "" {
			continue
		}
		row := RowOf(c)
		err = store.Begin(row.Id, row.Ts, vizevalfacts.VizevalEnvelope{NaturalKey: row.NaturalKey}).AddVizevalScore(row).Commit()
		if err != nil {
			return eh.Errorf("unable to stage a scorecard row: %w", err)
		}
	}
	if _, err = store.Flush(ctx); err != nil {
		return eh.Errorf("unable to write scorecard rows: %w", err)
	}
	return nil
}
