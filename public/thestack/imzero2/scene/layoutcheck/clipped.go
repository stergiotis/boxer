package layoutcheck

import (
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// clipped.go finds text the host drew elided. The tree cannot: egui gives a
// label's AccessKit node the text it was asked to lay out, so a label cut to
// "SELECT * FR…" on screen reads "SELECT * FROM events" in the tree. An
// ellipsis that is in the tree is the app's own (a "Save as…" button, a
// "running…" status, an id the app shortened), and says nothing about room.
// The capture's svg sidecar has the drawn text, one <text> element per run,
// so an elided run is one that ends in "…" where the node under it has more
// text than that.

// TextRun is one <text> element of an svg sidecar: where its first glyph
// starts, its baseline, and what it drew.
type TextRun struct {
	X        float32
	Baseline float32
	FontSize float32
	Text     string
}

// ReadSVGText collects the text runs of an svg sidecar.
func ReadSVGText(r io.Reader) (runs []TextRun, err error) {
	dec := xml.NewDecoder(r)
	var cur *TextRun
	var text strings.Builder
	for {
		tok, e := dec.Token()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, eh.Errorf("unable to read the svg sidecar: %w", e)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "text" {
				continue
			}
			cur = &TextRun{}
			text.Reset()
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "x":
					first, _, _ := strings.Cut(strings.TrimSpace(a.Value), " ")
					cur.X = parseFloat(first)
				case "y":
					cur.Baseline = parseFloat(a.Value)
				case "font-size":
					cur.FontSize = parseFloat(a.Value)
				}
			}
		case xml.CharData:
			if cur != nil {
				text.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "text" && cur != nil {
				cur.Text = text.String()
				runs = append(runs, *cur)
				cur = nil
			}
		}
	}
	return runs, nil
}

func parseFloat(s string) float32 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 32)
	return float32(v)
}

// CheckElided reports InvariantClipped for every node with a run drawn over
// it that ends in "…" and is a strict part of the node's name or value. It
// applies to every node with text, labels included: a cut value is as lost
// as a cut button caption.
func CheckElided(nodes []Node, runs []TextRun) (findings []Finding) {
	reported := make(map[uint64]bool, 8)
	for _, r := range runs {
		drawn := strings.TrimRight(r.Text, " \t\n")
		kept, ok := strings.CutSuffix(drawn, "…")
		kept = strings.TrimSpace(kept)
		if !ok || kept == "" {
			continue
		}
		// A point inside the run's first glyph: the baseline sits below the
		// text's middle by about a third of the font size.
		px, py := r.X+1, r.Baseline-max(r.FontSize, 1)*0.35
		for _, n := range nodes {
			if reported[n.ID] || !n.hasBounds() ||
				px < n.X-2 || px > n.X+n.W+2 || py < n.Y-2 || py > n.Y+n.H+2 {
				continue
			}
			full := elidedFrom(kept, n.Name)
			if full == "" {
				full = elidedFrom(kept, n.Value)
			}
			if full == "" {
				continue
			}
			reported[n.ID] = true
			findings = append(findings, Finding{Invariant: InvariantClipped, Node: n,
				Detail: "drawn as " + strconv.Quote(drawn) + " of " + strconv.Itoa(len([]rune(full))) + " characters"})
		}
	}
	return findings
}

// elidedFrom returns text when kept is part of it and text does not itself
// carry the ellipsis after kept — the case where the host shortened it.
func elidedFrom(kept string, text string) string {
	if text == "" || !strings.Contains(text, kept) || strings.Contains(text, kept+"…") {
		return ""
	}
	if strings.TrimSpace(text) == kept {
		return ""
	}
	return text
}
