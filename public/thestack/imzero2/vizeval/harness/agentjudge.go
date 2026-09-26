package harness

import (
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/judge"
	"golang.org/x/image/draw"
	"lukechampine.com/blake3"
)

// The agent judge (ADR-0257, proposed, §SD10): a reader — an agent or a
// person — answers the scenario's questions from judge sheets, and the harness
// scores the replies as it scores a model's. What the design guards against is
// a reader who already knows the answers, so the sheets carry nothing but the
// picture, the intent and the questions, and the replies of a reader who shows
// knowledge are kept apart (task.informed.*) instead of mixed into accuracy.

// JudgeDirName is the directory under a scenario's output that holds its
// sheets, their images and the key.
const JudgeDirName = "judge"

// judgeKeyName maps sheet ids to what they show. The sheets do not refer to
// it; a reader is not given it.
const judgeKeyName = "key.json"

// controlBlurFactor is how far the control's picture is shrunk before being
// grown back: enough that no label survives, not so far that the layout goes.
const controlBlurFactor = 14

// Metrics a reader's replies add when the reader is informed: the task.*
// metrics under another name, which no gate and no ranking reads.
const (
	MetricTaskInformedAccuracy   = "task.informed.accuracy"
	MetricTaskInformedUnreadable = "task.informed.unreadable"
	MetricTaskInformedErrors     = "task.informed.errors"
)

// SheetEntry is one sheet in the key: the drawing it shows and the
// candidates that drew it, or, for the control, the drawing it was blurred
// from.
type SheetEntry struct {
	Sheet      string   `json:"sheet"`
	Drawing    string   `json:"drawing"`
	Candidates []string `json:"candidates,omitempty"`
	Control    bool     `json:"control,omitempty"`
}

type sheetKey struct {
	Scenario string       `json:"scenario"`
	Sheets   []SheetEntry `json:"sheets"`
}

// SheetID is a sheet's id: opaque to a reader, and the same for the same
// drawing on every run, so a re-render keeps its replies and two candidates
// that drew one picture share one sheet.
func SheetID(scenario string, role string, drawing string) string {
	h := blake3.New(32, nil)
	for _, p := range []string{"vizeval-sheet", scenario, role, drawing} {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:5])
}

func readSheetKey(jdir string) (k sheetKey, err error) {
	b, err := os.ReadFile(filepath.Join(jdir, judgeKeyName))
	if errors.Is(err, fs.ErrNotExist) {
		return k, nil
	}
	if err != nil {
		return k, eh.Errorf("unable to read the judge key: %w", err)
	}
	if err = json.Unmarshal(b, &k); err != nil {
		return k, eh.Errorf("unable to decode the judge key: %w", err)
	}
	return k, nil
}

// writeJudgeSheets writes a sheet and its picture for every drawing of a
// scored candidate, and one control, merging into the key already there. It
// returns the sheets' paths in id order, which is no order a reader can read
// anything into.
func writeJudgeSheets(outDir string, dir string, sc *vizeval.Scenario, answers []Answer, cards []Scorecard) (paths []string, err error) {
	if len(answers) == 0 {
		return nil, nil
	}
	jdir := filepath.Join(dir, JudgeDirName)
	if err = os.MkdirAll(jdir, 0o755); err != nil {
		return nil, eh.Errorf("unable to create the judge directory: %w", err)
	}
	key, err := readSheetKey(jdir)
	if err != nil {
		return nil, err
	}
	key.Scenario = sc.Name
	entry := func(id string) *SheetEntry {
		for i := range key.Sheets {
			if key.Sheets[i].Sheet == id {
				return &key.Sheets[i]
			}
		}
		key.Sheets = append(key.Sheets, SheetEntry{Sheet: id})
		return &key.Sheets[len(key.Sheets)-1]
	}
	qs := questionsOf(answers)
	eligible := make([]Scorecard, 0, len(cards))
	for _, c := range cards {
		if c.Status == StatusScored && c.DrawingDigest != "" && c.Dir != "" &&
			nonEmpty(filepath.Join(outDir, c.Dir, "artifact.png")) {
			eligible = append(eligible, c)
		}
	}
	slices.SortFunc(eligible, func(a, b Scorecard) int { return strings.Compare(a.DrawingDigest, b.DrawingDigest) })
	write := func(id string, img image.Image) error {
		if err := writePNG(filepath.Join(jdir, id+".png"), img); err != nil {
			return err
		}
		p := filepath.Join(jdir, id+".md")
		paths = append(paths, p)
		return os.WriteFile(p, []byte(judge.SheetMarkdown(id, id+".png", sc.Spec.Intent, qs)), 0o644)
	}
	for _, c := range eligible {
		id := SheetID(sc.Name, "candidate", c.DrawingDigest)
		e := entry(id)
		e.Drawing = c.DrawingDigest
		if !slices.Contains(e.Candidates, c.CandidateID) {
			e.Candidates = append(e.Candidates, c.CandidateID)
		}
		if slices.Contains(paths, filepath.Join(jdir, id+".md")) {
			continue
		}
		img, e2 := readPNG(filepath.Join(outDir, c.Dir, "artifact.png"))
		if e2 != nil {
			return nil, e2
		}
		if err = write(id, img); err != nil {
			return nil, err
		}
	}
	if len(eligible) > 0 {
		src := eligible[0]
		img, e2 := readPNG(filepath.Join(outDir, src.Dir, "artifact.png"))
		if e2 != nil {
			return nil, e2
		}
		id := SheetID(sc.Name, "control", src.DrawingDigest)
		e := entry(id)
		e.Drawing, e.Control = src.DrawingDigest, true
		if err = write(id, blurred(img)); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(key.Sheets, func(a, b SheetEntry) int { return strings.Compare(a.Sheet, b.Sheet) })
	b, err := json.Marshal(key, json.Deterministic(true))
	if err != nil {
		return nil, eh.Errorf("unable to encode the judge key: %w", err)
	}
	if err = os.WriteFile(filepath.Join(jdir, judgeKeyName), b, 0o644); err != nil {
		return nil, eh.Errorf("unable to write the judge key: %w", err)
	}
	slices.Sort(paths)
	return paths, nil
}

// blurred is img shrunk by controlBlurFactor and grown back: the layout of a
// real rendering with no label left to read.
func blurred(img image.Image) image.Image {
	b := img.Bounds()
	small := image.NewRGBA(image.Rect(0, 0, max(1, b.Dx()/controlBlurFactor), max(1, b.Dy()/controlBlurFactor)))
	draw.CatmullRom.Scale(small, small.Bounds(), img, b, draw.Src, nil)
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.BiLinear.Scale(out, out.Bounds(), small, small.Bounds(), draw.Src, nil)
	return out
}

// applyReaderReplies scores the replies to the scenario's sheets onto the
// cards that drew them. Replies to sheets not in this scenario's key belong
// to another scenario and are left alone.
//
// A reader is informed when it answered more than one sheet of the scenario
// — the easier picture's answers carry into the harder one — or answered a
// question of the control correctly, which shows no picture to answer from.
// An informed reader's replies land under task.informed.* and gate nothing.
func applyReaderReplies(sc *vizeval.Scenario, dir string, answers []Answer, replies []judge.ReaderReply, cards []Scorecard) (err error) {
	key, err := readSheetKey(filepath.Join(dir, JudgeDirName))
	if err != nil || len(key.Sheets) == 0 {
		return err
	}
	sheets := make(map[string]SheetEntry, len(key.Sheets))
	for _, s := range key.Sheets {
		sheets[s.Sheet] = s
	}
	qs := questionsOf(answers)
	qByID := make(map[string]judge.Question, len(qs))
	for _, q := range qs {
		qByID[q.ID] = q
	}
	type sq struct{ sheet, question string }
	got := make(map[sq]judge.ReaderReply)
	readerOf := make(map[string]string)
	sheetsOf := make(map[string][]string)
	informed := make(map[string]bool)
	answeredSheet := make(map[string]bool)
	for _, r := range replies {
		s, ok := sheets[r.Sheet]
		if !ok {
			continue
		}
		q, ok := qByID[r.Question]
		if !ok {
			return eb.Build().Str("sheet", r.Sheet).Str("question", r.Question).Errorf("a reply names a question the scenario does not ask")
		}
		if _, dup := got[sq{r.Sheet, r.Question}]; dup {
			return eb.Build().Str("sheet", r.Sheet).Str("question", r.Question).Errorf("a question of a sheet is answered twice")
		}
		if prev, ok := readerOf[r.Sheet]; ok && prev != r.Reader {
			return eb.Build().Str("sheet", r.Sheet).Str("readers", prev+", "+r.Reader).
				Errorf("a sheet is answered by two readers; give each its own answers file")
		}
		got[sq{r.Sheet, r.Question}] = r
		readerOf[r.Sheet] = r.Reader
		answeredSheet[r.Sheet] = true
		if !slices.Contains(sheetsOf[r.Reader], r.Sheet) {
			sheetsOf[r.Reader] = append(sheetsOf[r.Reader], r.Sheet)
		}
		if s.Control && judge.VerdictOf(q, r.Answer, r.Unreadable).Correct {
			informed[r.Reader] = true
		}
	}
	for reader, ss := range sheetsOf {
		if len(ss) > 1 {
			informed[reader] = true
		}
	}
	for i := range cards {
		c := &cards[i]
		if c.Status != StatusScored || c.DrawingDigest == "" {
			continue
		}
		id := SheetID(sc.Name, "candidate", c.DrawingDigest)
		reader, answered := readerOf[id]
		if !answered {
			if stale := staleSheet(key, c, answeredSheet); stale != "" {
				c.Verdicts = []judge.Verdict{{ID: "*", Error: "stale: sheet " + stale + " showed another drawing of this candidate"}}
			}
			continue
		}
		c.Verdicts = make([]judge.Verdict, 0, len(qs))
		for _, q := range qs {
			r, ok := got[sq{id, q.ID}]
			if !ok {
				c.Verdicts = append(c.Verdicts, judge.Verdict{ID: q.ID, Expected: q.Expected, Error: "no reply"})
				continue
			}
			c.Verdicts = append(c.Verdicts, judge.VerdictOf(q, r.Answer, r.Unreadable))
		}
		c.TaskJudge = "reader:" + reader
		if c.Metrics == nil {
			c.Metrics = make(map[string]float64, 3)
		}
		if informed[reader] {
			taskMetrics(c, MetricTaskInformedAccuracy, MetricTaskInformedUnreadable, MetricTaskInformedErrors)
		} else {
			taskMetrics(c, MetricTaskAccuracy, MetricTaskUnreadable, MetricTaskErrors)
			if c.Gates == nil {
				c.Gates = make(map[string]bool)
			}
			applyGates(sc, c, true)
		}
		if c.ReusedFrom != "" {
			// The measurement was read back; the judgement is new, so the
			// card is filed again under its key with both.
			c.ReusedFrom = ""
			c.At = time.Now().UTC().Format(time.RFC3339)
		}
	}
	return nil
}

// staleSheet names a sheet that was answered for this candidate but shows a
// drawing it no longer draws, or "".
func staleSheet(key sheetKey, c *Scorecard, answered map[string]bool) string {
	for _, s := range key.Sheets {
		if !s.Control && s.Drawing != c.DrawingDigest && answered[s.Sheet] && slices.Contains(s.Candidates, c.CandidateID) {
			return s.Sheet
		}
	}
	return ""
}

// taskMetrics records a card's verdicts under the three names given:
// accuracy only when every question got a reply, as for the model.
func taskMetrics(c *Scorecard, accuracy, unreadable, errs string) {
	var correct, unread, failed int
	for _, v := range c.Verdicts {
		switch {
		case v.Error != "":
			failed++
		case v.Correct:
			correct++
		case v.Unreadable:
			unread++
		}
	}
	c.Metrics[errs] = float64(failed)
	if failed == 0 && len(c.Verdicts) > 0 {
		c.Metrics[accuracy] = float64(correct) / float64(len(c.Verdicts))
		c.Metrics[unreadable] = float64(unread)
	}
}
