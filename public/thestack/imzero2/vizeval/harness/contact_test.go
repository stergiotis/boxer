package harness

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval/geometry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantCards writes n candidates' artifacts under out and returns their cards;
// the last one failed and has no artifact.
func plantCards(t *testing.T, out string, n int) (cards []Scorecard) {
	for i := range n {
		cand, err := vizeval.NewCandidate(vizeval.SinkUnicode, map[string]any{"width": int64(60 + i)})
		require.NoError(t, err)
		c := Scorecard{Scenario: "s", Candidate: cand, CandidateID: cand.ID(), BatchDigest: "d", Status: StatusScored,
			Dir: filepath.Join("s", cand.ID()), Metrics: map[string]float64{geometry.MetricTextElided: float64(i)}}
		if i == n-1 {
			c.Status, c.Reason, c.Metrics = StatusFailed, "the artifact node is not in the tree", nil
		} else {
			require.NoError(t, os.MkdirAll(filepath.Join(out, c.Dir), 0o755))
			img := image.NewRGBA(image.Rect(0, 0, 1500, 550))
			for x := range 1500 {
				img.Set(x, 10, color.RGBA{200, 200, 200, 255})
			}
			require.NoError(t, writePNG(filepath.Join(out, c.Dir, "artifact.png"), img))
		}
		cards = append(cards, c)
	}
	return cards
}

func TestContactSheets(t *testing.T) {
	out := t.TempDir()
	dir := filepath.Join(out, "s")
	cards := plantCards(t, out, contactPerSheet+2)
	paths, err := writeContactSheets(out, dir, cards)
	require.NoError(t, err)
	require.Len(t, paths, 2, "past a sheet's worth of candidates, a second sheet")
	assert.Equal(t, filepath.Join(dir, ContactSheetName), paths[0])

	first, err := readPNG(paths[0])
	require.NoError(t, err)
	assert.Equal(t, contactWidth, first.Bounds().Dx())
	rows := (contactPerSheet + contactMaxCols - 1) / contactMaxCols
	cellW := (contactWidth - contactGutter*(contactMaxCols+1)) / contactMaxCols
	thumbH := int(550*float64(cellW-2*contactPad)/1500 + 0.5)
	assert.Greater(t, first.Bounds().Dy(), rows*thumbH, "every row holds a scaled artifact")

	second, err := readPNG(paths[1])
	require.NoError(t, err)
	assert.Less(t, second.Bounds().Dy(), first.Bounds().Dy(), "two candidates, one row")
}

func TestContactLabelWraps(t *testing.T) {
	face, err := contactFace()
	require.NoError(t, err)
	defer func() { _ = face.Close() }()
	cand, err := vizeval.NewCandidate(vizeval.SinkLens, nil)
	require.NoError(t, err)
	c := Scorecard{Candidate: cand, CandidateID: cand.ID(), Status: StatusGated,
		Gates: map[string]bool{"text.clipped": false, "text.overlap_pairs": true}}
	lines := contactLabel(c, 3, face, 200)
	require.Greater(t, len(lines), 2, "options wrap in a narrow cell")
	assert.True(t, strings.HasPrefix(lines[0].text, "#3 gated  lens"))
	assert.Equal(t, contactStatusC[StatusGated], lines[0].col)
	assert.Contains(t, lines[len(lines)-1].text, "text.clipped")
	for _, l := range wrapTokens([]string{strings.Repeat("x", 200)}, face, 200) {
		assert.True(t, strings.HasSuffix(l, "…"), "a token wider than the cell is cut, not overflowed")
	}
}

func TestWriteTable(t *testing.T) {
	a, err := vizeval.NewCandidate(vizeval.SinkUnicode, map[string]any{"width": int64(60)})
	require.NoError(t, err)
	b, err := vizeval.NewCandidate(vizeval.SinkCard, nil)
	require.NoError(t, err)
	card := func(c vizeval.Candidate, digest string, build string, elided float64) Scorecard {
		return Scorecard{Scenario: "s", Candidate: c, CandidateID: c.ID(), BatchDigest: digest, Build: build, Rows: 8,
			Status: StatusScored, Metrics: map[string]float64{geometry.MetricTextElided: elided}}
	}
	cards := []Scorecard{
		card(a, "d1", "r1", 5), card(b, "d1", "r1", 1), card(a, "d1", "r1", 3), // a re-scored
		card(a, "d2", "r1", 0),
	}
	var buf bytes.Buffer
	require.NoError(t, WriteTable(&buf, cards, TableOptions{Metrics: []string{geometry.MetricTextElided, "task.accuracy"}, Sort: "-" + geometry.MetricTextElided}))
	got := buf.String()
	assert.Equal(t, 2, strings.Count(got, "# s  batch "), "one group per batch digest")
	lines := strings.Split(got, "\n")
	assert.Contains(t, lines[0], "build r1")
	assert.Contains(t, lines[2], "width=60", "descending by elided: a's latest card, 3, before b's 1")
	assert.Contains(t, lines[2], " 3 ")
	assert.Contains(t, lines[3], "card")
	assert.True(t, strings.HasSuffix(strings.TrimRight(lines[3], " "), "-"), "a metric the card lacks prints as -")

	buf.Reset()
	cards[1].Build = "r2"
	require.NoError(t, WriteTable(&buf, cards, TableOptions{All: true, Scenario: "s"}))
	first := strings.Split(buf.String(), "\n# ")[0]
	assert.Contains(t, first, "\tbuild"[1:], "two builds in a group: a build column")
	assert.Equal(t, 5, strings.Count(first, "\n"), "header, column names and all three cards")
}

func TestNaturalKeys(t *testing.T) {
	body := "id:id:u64:::0:\tid:natural-key:s:::0:\ttv:x\nUInt64\tString\tArray(Float64)\n" +
		"0\thost-00\t[]\n1\ttab\\there\t[]\n2\thost-00\t[]\n"
	assert.Equal(t, []string{"host-00", "tab\there"}, naturalKeys([]byte(body)))
	assert.Nil(t, naturalKeys([]byte("a\tb\nUInt8\tUInt8\n1\t2\n")), "no natural-key column")
}
