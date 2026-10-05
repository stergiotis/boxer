package harness

import (
	"bufio"
	"cmp"
	"encoding/json/v2"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/geometry"
)

// DefaultTableMetrics are the columns `vizeval table` prints without
// --metrics: the gate-worthy faults, then what a round was most often
// compared on — how much was cut, how legible, how much of the batch is
// named, how well a reader answered.
var DefaultTableMetrics = []string{
	geometry.MetricTextOverlapPairs, geometry.MetricTextClipped, geometry.MetricTextElidedShare,
	geometry.MetricTextMinContrast, MetricRowsLabelledShare, MetricTaskAccuracy,
}

// TableOptions selects and orders what WriteTable prints.
type TableOptions struct {
	// Metrics are the metric columns, in order.
	Metrics []string
	// Scenario, when set, keeps that scenario's cards only.
	Scenario string
	// All keeps every card; otherwise a candidate measured more than once
	// over the same batch keeps its latest card.
	All bool
	// Sort names a metric to order each group by, ascending; a leading '-'
	// descends. A card without the metric sorts last either way.
	Sort string
}

// ReadScorecards reads every card appended to outDir/scorecards.jsonl, oldest
// first.
func ReadScorecards(outDir string) (cards []Scorecard, err error) {
	p := filepath.Join(outDir, "scorecards.jsonl")
	f, err := os.Open(p)
	if err != nil {
		return nil, eb.Build().Str("path", p).Errorf("unable to open the scorecards: %w", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		var c Scorecard
		if err = json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, eb.Build().Int("line", line).Errorf("unable to decode a scorecard: %w", err)
		}
		cards = append(cards, c)
	}
	if err = sc.Err(); err != nil {
		return nil, eh.Errorf("unable to read the scorecards: %w", err)
	}
	return cards, nil
}

// tableGroup is the cards of one scenario over one batch: the unit within
// which scorecards compare (ADR-0266 §SD4).
type tableGroup struct {
	scenario, digest, frame string
	rows                    int64
	cards                   []Scorecard
}

// WriteTable prints one aligned line per card, grouped by scenario, batch
// digest and frame in order of first appearance, under a header naming the group. A
// group whose cards span builds gets a build column: cards of two builds are
// two versions of the code, which is what a reader comparing them needs to
// see.
func WriteTable(w io.Writer, cards []Scorecard, opts TableOptions) (err error) {
	groups := groupCards(cards, opts)
	for gi, g := range groups {
		if gi > 0 {
			if _, err = io.WriteString(w, "\n"); err != nil {
				return err
			}
		}
		builds := make([]string, 0, 2)
		for _, c := range g.cards {
			if !slices.Contains(builds, c.Build) {
				builds = append(builds, c.Build)
			}
		}
		head := "# " + g.scenario + "  batch " + g.digest + " (" + strconv.FormatInt(g.rows, 10) + " rows)"
		if g.frame != "" {
			head += "  box " + g.frame
		}
		if len(builds) == 1 {
			head += "  build " + builds[0]
		}
		if _, err = io.WriteString(w, head+"\n"); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		cols := []string{"id", "sink", "options", "status"}
		if len(builds) > 1 {
			cols = append(cols, "build")
		}
		cols = append(cols, opts.Metrics...)
		_, _ = io.WriteString(tw, strings.Join(cols, "\t")+"\n")
		for _, c := range g.cards {
			f := []string{c.CandidateID[:min(10, len(c.CandidateID))], c.Candidate.Sink,
				strings.Join(OptionTokens(c.Candidate.Options), " "), string(c.Status)}
			if len(builds) > 1 {
				f = append(f, c.Build)
			}
			for _, m := range opts.Metrics {
				if v, ok := c.Metrics[m]; ok {
					f = append(f, strconv.FormatFloat(v, 'g', 4, 64))
				} else {
					f = append(f, "-")
				}
			}
			_, _ = io.WriteString(tw, strings.Join(f, "\t")+"\n")
		}
		if err = tw.Flush(); err != nil {
			return eh.Errorf("unable to write the table: %w", err)
		}
	}
	return nil
}

func groupCards(cards []Scorecard, opts TableOptions) (groups []*tableGroup) {
	type key struct{ scenario, digest, frame string }
	idx := make(map[key]*tableGroup)
	for _, c := range cards {
		if opts.Scenario != "" && c.Scenario != opts.Scenario {
			continue
		}
		k := key{c.Scenario, c.BatchDigest, c.Frame}
		g, ok := idx[k]
		if !ok {
			g = &tableGroup{scenario: c.Scenario, digest: c.BatchDigest, frame: c.Frame, rows: c.Rows}
			idx[k] = g
			groups = append(groups, g)
		}
		if !opts.All {
			// The later card replaces the earlier in its place, so a round's
			// order survives a re-run of one of its candidates.
			if i := slices.IndexFunc(g.cards, func(o Scorecard) bool { return o.CandidateID == c.CandidateID }); i >= 0 {
				g.cards[i] = c
				continue
			}
		}
		g.cards = append(g.cards, c)
	}
	if opts.Sort != "" {
		name, desc := strings.CutPrefix(opts.Sort, "-")
		for _, g := range groups {
			slices.SortStableFunc(g.cards, func(a, b Scorecard) int {
				va, oka := a.Metrics[name]
				vb, okb := b.Metrics[name]
				switch {
				case !oka && !okb:
					return 0
				case !oka:
					return 1
				case !okb:
					return -1
				case desc:
					return cmp.Compare(vb, va)
				}
				return cmp.Compare(va, vb)
			})
		}
	}
	return groups
}
