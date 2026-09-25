// Package harness scores candidates over a scenario (ADR-0257, proposed):
// for each candidate it runs the scenario's dataset through play's
// Experiments pane on the headless host, captures the artifact with its SVG
// and tree sidecars, and measures the geometry of what was drawn.
//
// Results are files: one directory per scenario and candidate holding the
// capture, the cropped artifact and the scorecard, a gallery per scenario, and
// every scorecard appended to one JSONL file. The facts store is a later
// milestone (§SD8); the scorecard is its row shape.
package harness

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json/v2"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/geometry"
	"lukechampine.com/blake3"
)

// ArtifactNode is the accessibility node the Experiments pane wraps its output
// in; kept in step with play's experimentsArtifactName.
const ArtifactNode = "experiments.artifact"

// captureName is the file stem of every candidate's capture; the candidate's
// directory is what tells them apart.
const captureName = "capture"

// StatusE is how far a candidate got.
type StatusE string

const (
	// StatusScored passed every gate and has metrics.
	StatusScored StatusE = "scored"
	// StatusGated has metrics and failed at least one gate.
	StatusGated StatusE = "gated"
	// StatusInadmissible was not rendered: the scenario does not admit its
	// sink, or its batch exceeds the sink's row cap.
	StatusInadmissible StatusE = "inadmissible"
	// StatusFailed was attempted and produced nothing to measure.
	StatusFailed StatusE = "failed"
)

// Answer is one question with its computed answer rows.
type Answer struct {
	ID      string     `json:"id"`
	Prompt  string     `json:"prompt"`
	Rows    [][]string `json:"rows"`
	Compare string     `json:"compare,omitempty"`
	Tol     float64    `json:"tol,omitempty"`
}

// Scorecard is one candidate scored over one scenario at one build.
type Scorecard struct {
	Scenario    string             `json:"scenario"`
	Candidate   vizeval.Candidate  `json:"candidate"`
	CandidateID string             `json:"candidateId"`
	Build       string             `json:"build"`
	BatchDigest string             `json:"batchDigest"`
	Rows        int64              `json:"rows"`
	Status      StatusE            `json:"status"`
	Reason      string             `json:"reason,omitempty"`
	Dir         string             `json:"dir,omitempty"`
	Area        [4]float64         `json:"area,omitempty"`
	Metrics     map[string]float64 `json:"metrics,omitempty"`
	Gates       map[string]bool    `json:"gates,omitempty"`
	At          string             `json:"at"`
}

// Options configures a scoring run.
type Options struct {
	// OutDir receives everything; a scenario gets OutDir/<scenario>.
	OutDir string
	// RepoRoot, HostBinary, ClientBinary and Timeout are the scene
	// launcher's; an empty HostBinary is this executable.
	RepoRoot     string
	HostBinary   string
	ClientBinary string
	Timeout      time.Duration
	// ClickHouseURL is where the harness runs the dataset and the answers;
	// empty means BOXER's configured endpoint. play reaches it the same way.
	ClickHouseURL string
	Logger        zerolog.Logger
}

// Dataset is a scenario's batch as the harness sees it.
type Dataset struct {
	Rows   int64
	Digest string
}

// DefaultCandidates is every sink the scenario admits, at its defaults.
func DefaultCandidates(sc *vizeval.Scenario) (cands []vizeval.Candidate, err error) {
	cands = make([]vizeval.Candidate, 0, len(sc.Spec.Sinks))
	for _, s := range sc.Spec.Sinks {
		var c vizeval.Candidate
		if c, err = vizeval.NewCandidate(s, nil); err != nil {
			return nil, err
		}
		cands = append(cands, c)
	}
	return cands, nil
}

// Score renders and measures every candidate over the scenario. A candidate
// that fails is recorded as failed and the run goes on; the error is for what
// stops the whole scenario — no dataset, no output directory.
func Score(sc *vizeval.Scenario, cands []vizeval.Candidate, opts Options) (cards []Scorecard, answers []Answer, err error) {
	dir := filepath.Join(opts.OutDir, sc.Name)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, eb.Build().Str("dir", dir).Errorf("unable to create the scenario directory: %w", err)
	}
	ds, err := RunDataset(sc, opts)
	if err != nil {
		return nil, nil, err
	}
	if answers, err = RunAnswers(sc, opts); err != nil {
		return nil, nil, err
	}
	build := buildID()
	cards = make([]Scorecard, 0, len(cands))
	for _, cand := range cands {
		card := Scorecard{
			Scenario: sc.Name, Candidate: cand, CandidateID: cand.ID(), Build: build,
			BatchDigest: ds.Digest, Rows: ds.Rows,
		}
		scoreOne(sc, &card, ds, opts)
		card.At = time.Now().UTC().Format(time.RFC3339)
		opts.Logger.Info().Str("scenario", sc.Name).Str("candidate", string(cand.Canonical())).
			Str("status", string(card.Status)).Str("reason", card.Reason).Msg("candidate scored")
		cards = append(cards, card)
	}
	if err = writeScorecards(opts.OutDir, dir, cards); err != nil {
		return cards, answers, err
	}
	if err = writeGallery(dir, sc, ds, answers, cards); err != nil {
		return cards, answers, err
	}
	return cards, answers, nil
}

func scoreOne(sc *vizeval.Scenario, card *Scorecard, ds Dataset, opts Options) {
	spec, _ := vizeval.SinkByID(card.Candidate.Sink)
	switch {
	case !sc.Admits(card.Candidate.Sink):
		card.Status, card.Reason = StatusInadmissible, "the scenario does not admit this sink"
		return
	case ds.Rows > spec.RowCap:
		card.Status = StatusInadmissible
		card.Reason = "the batch has " + strconv.FormatInt(ds.Rows, 10) + " rows, over the sink's cap of " +
			strconv.FormatInt(spec.RowCap, 10)
		return
	}
	cdir := filepath.Join(opts.OutDir, sc.Name, card.CandidateID)
	card.Dir = filepath.Join(sc.Name, card.CandidateID)
	res := scene.RunDoc(candidateScene(sc, card.Candidate), scene.Options{
		OutDir: cdir, RepoRoot: opts.RepoRoot, HostBinary: opts.HostBinary, ClientBinary: opts.ClientBinary,
		Timeout: opts.Timeout, Out: io.Discard, Logger: opts.Logger,
	})
	if res.Status != scene.StatusPass {
		card.Status = StatusFailed
		card.Reason = string(res.Status) + ": " + res.Reason
		if res.Err != nil {
			card.Reason += " " + scene.PlainError(res.Err)
		}
		return
	}
	area, metrics, err := measureCapture(cdir)
	if err != nil {
		card.Status, card.Reason = StatusFailed, err.Error()
		return
	}
	card.Area = [4]float64{area.X0, area.Y0, area.X1, area.Y1}
	card.Metrics = metrics
	card.Status = StatusScored
	card.Gates = make(map[string]bool, len(sc.Spec.Gates))
	for name, g := range sc.Spec.Gates {
		v, present := metrics[name]
		pass := g.Pass(v, present)
		card.Gates[name] = pass
		if !pass {
			card.Status = StatusGated
		}
	}
}

// candidateScene is the scene that renders one candidate: play with every tab
// in one leaf and Experiments raised, the dataset as the buffer, run on mount,
// and the pane seeded with the candidate over the result.
func candidateScene(sc *vizeval.Scenario, cand vizeval.Candidate) *scene.Doc {
	seed, _ := json.Marshal(struct {
		Source  string         `json:"source"`
		Sink    string         `json:"sink"`
		Options vizeval.Values `json:"options"`
	}{"result", cand.Sink, cand.Options}, json.Deterministic(true))
	settle := sc.Spec.SettleMs
	if settle <= 0 {
		settle = vizeval.DefaultSettleMs
	}
	w, h, _ := (scene.Spec{Size: sc.Spec.Size}).Dimensions()
	return &scene.Doc{
		Name: captureName,
		Spec: scene.Spec{
			Launch:   "play",
			Size:     sc.Spec.Size,
			Requires: []string{scene.RequireClickHouse},
			Env: map[string]string{
				// The window a little inside the viewport, as the tour's
				// scenes open it, so its frame is in the capture.
				"BOXER_PLAY_WINDOW_SIZE":       strconv.Itoa(w-32) + "x" + strconv.Itoa(h-60),
				"BOXER_PLAY_AUTORUN":           "1",
				"BOXER_PLAY_TAB_ZONES":         "*=body",
				"BOXER_PLAY_FOCUS_EXPERIMENTS": "1",
				"BOXER_PLAY_EXPERIMENTS":       string(seed),
			},
		},
		SQL: sc.DatasetSQL(),
		Steps: []carrierclient.Step{
			{Do: "wait", Name: "Run"},
			{Do: "sleep", SettleMs: settle},
			{Do: "capture", Text: captureName, Sidecars: []string{carrierclient.SidecarSVG, carrierclient.SidecarTree}},
		},
	}
}

// measureCapture finds the artifact in the tree, reads the SVG, measures, and
// writes the artifact's crop of the PNG beside the capture.
func measureCapture(dir string) (area geometry.Rect, m map[string]float64, err error) {
	node, err := findNode(filepath.Join(dir, carrierclient.SidecarFile(captureName, carrierclient.SidecarTree)), ArtifactNode)
	if err != nil {
		return area, nil, err
	}
	f, err := os.Open(filepath.Join(dir, carrierclient.SidecarFile(captureName, carrierclient.SidecarSVG)))
	if err != nil {
		return area, nil, eh.Errorf("unable to open the svg sidecar: %w", err)
	}
	d, err := geometry.ReadSVG(f)
	_ = f.Close()
	if err != nil {
		return area, nil, err
	}
	area = geometry.VisibleArea(d, node)
	if area.Empty() {
		return area, nil, eh.Errorf("the artifact is not on screen")
	}
	img, err := readPNG(filepath.Join(dir, carrierclient.SidecarFile(captureName, "")))
	if err != nil {
		return area, nil, err
	}
	m = geometry.Measure(d, area, img)
	if err = writeCrop(img, d.Viewport, area, filepath.Join(dir, "artifact.png")); err != nil {
		return area, nil, err
	}
	return area, m, nil
}

func findNode(path string, name string) (r geometry.Rect, err error) {
	f, err := os.Open(path)
	if err != nil {
		return r, eh.Errorf("unable to open the tree sidecar: %w", err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var n struct {
			Name string  `json:"name"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
			W    float64 `json:"w"`
			H    float64 `json:"h"`
		}
		if err = json.Unmarshal(sc.Bytes(), &n); err != nil {
			return r, eh.Errorf("unable to decode a tree node: %w", err)
		}
		if n.Name == name {
			return geometry.Rect{X0: n.X, Y0: n.Y, X1: n.X + n.W, Y1: n.Y + n.H}, nil
		}
	}
	if err = sc.Err(); err != nil {
		return r, eh.Errorf("unable to read the tree sidecar: %w", err)
	}
	return r, eb.Build().Str("node", name).Errorf("the artifact node is not in the tree")
}

func readPNG(path string) (img image.Image, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eh.Errorf("unable to open the capture: %w", err)
	}
	defer func() { _ = f.Close() }()
	if img, err = png.Decode(f); err != nil {
		return nil, eh.Errorf("unable to decode the capture: %w", err)
	}
	return img, nil
}

// writeCrop saves the artifact's pixels alone — what a judge is shown in a
// later milestone, and what the gallery puts side by side.
func writeCrop(img image.Image, viewport geometry.Rect, area geometry.Rect, path string) (err error) {
	b := img.Bounds()
	sx, sy := float64(b.Dx())/viewport.W(), float64(b.Dy())/viewport.H()
	r := image.Rect(
		b.Min.X+int((area.X0-viewport.X0)*sx), b.Min.Y+int((area.Y0-viewport.Y0)*sy),
		b.Min.X+int((area.X1-viewport.X0)*sx+0.999), b.Min.Y+int((area.Y1-viewport.Y0)*sy+0.999),
	).Intersect(b)
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return eh.Errorf("the capture's image type cannot be cropped")
	}
	f, err := os.Create(path)
	if err != nil {
		return eh.Errorf("unable to create the artifact crop: %w", err)
	}
	if err = png.Encode(f, sub.SubImage(r)); err != nil {
		_ = f.Close()
		return eh.Errorf("unable to encode the artifact crop: %w", err)
	}
	return f.Close()
}

// RunDataset runs the scenario's dataset through the same constructor
// expansion play applies, and returns its row count and a digest of the
// result as ClickHouse serialises it. Two scorecards with different digests
// were drawn from different data.
func RunDataset(sc *vizeval.Scenario, opts Options) (ds Dataset, err error) {
	body, err := query(opts, sc.DatasetSQL(), "TSVWithNamesAndTypes")
	if err != nil {
		return ds, eb.Build().Str("scenario", sc.Name).Errorf("unable to run the dataset: %w", err)
	}
	sum := blake3.Sum256(body)
	ds.Digest = hex.EncodeToString(sum[:16])
	lines := bytes.Count(body, []byte("\n"))
	ds.Rows = int64(max(lines-2, 0))
	return ds, nil
}

// RunAnswers computes every question's answer from its SQL.
func RunAnswers(sc *vizeval.Scenario, opts Options) (answers []Answer, err error) {
	answers = make([]Answer, 0, len(sc.Spec.Questions))
	for _, q := range sc.Spec.Questions {
		body, e := query(opts, sc.AnswerSQL(q), "TSV")
		if e != nil {
			return nil, eb.Build().Str("question", q.ID).Errorf("unable to compute the answer: %w", e)
		}
		a := Answer{ID: q.ID, Prompt: q.Prompt, Compare: q.Compare, Tol: q.Tol}
		for line := range strings.SplitSeq(strings.TrimRight(string(body), "\n"), "\n") {
			if line != "" {
				a.Rows = append(a.Rows, strings.Split(line, "\t"))
			}
		}
		answers = append(answers, a)
	}
	return answers, nil
}

func query(opts Options, sql string, format string) (body []byte, err error) {
	expanded, err := constructsql.ExpandPass.Run(sql)
	if err != nil {
		return nil, eh.Errorf("unable to expand leeway constructors: %w", err)
	}
	expanded = strings.TrimRight(strings.TrimSpace(expanded), ";")
	url := opts.ClickHouseURL
	if url == "" {
		url = clickhouseenv.URL.Get()
	}
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "text/plain", strings.NewReader(expanded+"\nFORMAT "+format))
	if err != nil {
		return nil, eb.Build().Str("url", url).Errorf("unable to reach ClickHouse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, eh.Errorf("unable to read the ClickHouse response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, eb.Build().Int("status", resp.StatusCode).Str("response", string(body)).
			Errorf("ClickHouse refused the query")
	}
	return body, nil
}

// buildID is the revision this binary was built from, marked when the tree was
// dirty; scorecards from different builds are different measurements.
func buildID() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				dirty = "+dirty"
			}
		}
	}
	if rev == "" {
		return "unknown"
	}
	return rev[:min(12, len(rev))] + dirty
}

// writeScorecards writes each candidate's scorecard into its directory (or the
// scenario's, when nothing was rendered) and appends all of them to
// OutDir/scorecards.jsonl.
func writeScorecards(outDir string, scenarioDir string, cards []Scorecard) (err error) {
	all, err := os.OpenFile(filepath.Join(outDir, "scorecards.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return eh.Errorf("unable to open scorecards.jsonl: %w", err)
	}
	defer func() { _ = all.Close() }()
	for _, c := range cards {
		b, e := json.Marshal(c, json.Deterministic(true))
		if e != nil {
			return eh.Errorf("unable to encode a scorecard: %w", e)
		}
		if _, err = all.Write(append(b, '\n')); err != nil {
			return eh.Errorf("unable to append a scorecard: %w", err)
		}
		dir := scenarioDir
		if c.Dir != "" {
			dir = filepath.Join(outDir, c.Dir)
		}
		if err = os.MkdirAll(dir, 0o755); err != nil {
			return eh.Errorf("unable to create a candidate directory: %w", err)
		}
		if err = os.WriteFile(filepath.Join(dir, "scorecard."+c.CandidateID+".json"), b, 0o644); err != nil {
			return eh.Errorf("unable to write a scorecard: %w", err)
		}
	}
	return nil
}

// metricOrder is the gallery's column order: the gate-worthy counts first.
var metricOrder = []string{
	geometry.MetricTextOverlapPairs, geometry.MetricTextClipped, geometry.MetricTextElided,
	geometry.MetricTextCutAtEdge, geometry.MetricTextLowContrast, geometry.MetricTextMinContrast,
	geometry.MetricTextMinSize, geometry.MetricTextRuns, geometry.MetricInkRatio,
	geometry.MetricColorDistinct, geometry.MetricColorMinDeltaE,
	geometry.MetricTableNumericColumns, geometry.MetricTableNumericRightAligned,
	geometry.MetricTextRowPitchCV, geometry.MetricMarks,
}

// writeGallery writes the scenario's contact sheet: the scenario, its data and
// answers, and each candidate's artifact beside its gates and metrics.
func writeGallery(dir string, sc *vizeval.Scenario, ds Dataset, answers []Answer, cards []Scorecard) (err error) {
	var b strings.Builder
	b.WriteString("# " + sc.Name + "\n\nGenerated by `imzero2 vizeval score` (ADR-0257) — " +
		time.Now().Format(time.RFC3339) + ".\n\n")
	if sc.Spec.Intent != "" {
		b.WriteString("**Intent.** " + sc.Spec.Intent + "\n\n")
	}
	b.WriteString("Batch: " + strconv.FormatInt(ds.Rows, 10) + " rows, digest `" + ds.Digest + "`.\n\n")
	if len(answers) > 0 {
		b.WriteString("| question | answer |\n| --- | --- |\n")
		for _, a := range answers {
			rows := make([]string, 0, len(a.Rows))
			for _, r := range a.Rows {
				rows = append(rows, strings.Join(r, " · "))
			}
			b.WriteString("| " + a.Prompt + " | " + strings.Join(rows, "; ") + " |\n")
		}
		b.WriteString("\n")
	}
	for _, c := range cards {
		b.WriteString("## " + c.Candidate.Sink + " — " + string(c.Status) + "\n\n")
		b.WriteString("`" + string(c.Candidate.Canonical()) + "` · id `" + c.CandidateID + "`\n\n")
		if c.Reason != "" {
			b.WriteString("_" + c.Reason + "_\n\n")
		}
		if c.Dir != "" && c.Metrics != nil {
			b.WriteString("![" + c.CandidateID + "](" + c.CandidateID + "/artifact.png)\n\n")
		}
		if len(c.Gates) > 0 {
			names := make([]string, 0, len(c.Gates))
			for n := range c.Gates {
				names = append(names, n)
			}
			slices.Sort(names)
			parts := make([]string, 0, len(names))
			for _, n := range names {
				mark := "pass"
				if !c.Gates[n] {
					mark = "FAIL"
				}
				parts = append(parts, n+" "+mark)
			}
			b.WriteString("Gates: " + strings.Join(parts, " · ") + "\n\n")
		}
		if c.Metrics != nil {
			b.WriteString("| metric | value |\n| --- | --- |\n")
			for _, n := range metricOrder {
				if v, ok := c.Metrics[n]; ok {
					b.WriteString("| " + n + " | " + strconv.FormatFloat(v, 'g', 4, 64) + " |\n")
				}
			}
			b.WriteString("\n")
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "index.md"), []byte(b.String()), 0o644); err != nil {
		return eh.Errorf("unable to write the gallery: %w", err)
	}
	return nil
}
