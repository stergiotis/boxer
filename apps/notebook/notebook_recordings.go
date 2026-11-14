package notebook

// The notebook's recordings, played through the host's audio output
// (ADR-0372). The view draws a recording's waveform and transport and asks
// here for its state; this reads the file from where the notebook's files
// are, off the frame, and holds the one recording that plays.

import (
	"context"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// recordingsOf is the window as what plays its notebook's recordings.
type recordingsOf struct{ inst *App }

// recordings is the view's player, nil for a window that was not mounted:
// its recordings are drawn and not heard.
func (inst *App) recordings() notebookview.RecordingsI {
	if inst.player == nil {
		return nil
	}
	return recordingsOf{inst: inst}
}

// key names a recording of the document on screen. A recording of another
// document is not the player's: setDocument stops it.
func (r recordingsOf) key(p string) string {
	if r.inst.doc == nil {
		return p
	}
	return r.inst.doc.origin + "\x00" + p
}

func (r recordingsOf) State(p string) notebookview.RecordingState {
	st := r.inst.player.State(r.key(p))
	return notebookview.RecordingState{Active: st.Active, Opening: st.Opening, Playing: st.Playing,
		Fraction: st.Fraction, Position: st.Position, Note: st.Note}
}

func (r recordingsOf) Toggle(p string) {
	doc := r.inst.doc
	if doc == nil {
		return
	}
	fsys := doc.fsys
	r.inst.player.Toggle(r.key(p), func(context.Context) ([]byte, error) {
		// Checked against its name when it is named by its content, as the
		// reading of the notebook checked it.
		return mdnotebook.ReadAsset(fsys, p)
	})
}

func (r recordingsOf) Seek(p string, fraction float64) { r.inst.player.Seek(r.key(p), fraction) }

var _ notebookview.RecordingsI = recordingsOf{}
