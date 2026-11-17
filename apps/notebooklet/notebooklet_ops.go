package notebooklet

import (
	"strconv"

	"github.com/stergiotis/boxer/apps/notebook"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// The catalog (ADR-0269, ADR-0380 §SD6): reading the document, reading the
// pane's state and starting a run. Nothing changes the text; the notebook
// window is where it is worked on.

const (
	resDocument = "document"
	resReading  = "reading"
	resHandover = "handover"

	opGetDocument    = "get_document"
	opOpenInNotebook = "open_in_notebook"
)

// documentMaxBytes bounds get_document's text.
const documentMaxBytes = 256 << 10

// snap is what queries read: a copy taken after the command stage.
type snap struct {
	book, path, slug string
	text             string
	paneErr          string
	live             notebook.LiveState
	built            bool
}

// Document is get_document's result.
type Document struct {
	Book      string `desc:"the registered book the notebook is committed in"`
	Path      string `desc:"its path in the book"`
	Slug      string `desc:"its slug, the last part of this app's id"`
	Text      string `desc:"the notebook's markdown: prose, record cells (a sql fence followed by an id), presentation cells (a sql fence with a renderer) and its frontmatter"`
	Length    int    `desc:"the whole text's length in bytes"`
	Truncated bool   `json:",omitzero" desc:"the text is cut at 256 KiB; Length and Revision still describe the whole"`
	Revision  string `desc:"names this exact text"`
}

var ops = func() (s *appops.Set[*window, snap]) {
	s = appops.NewSet(func(inst *window) (sn snap) {
		sn.book, sn.path, sn.slug = inst.def.Book, inst.def.Path, inst.def.Slug
		sn.text = string(inst.def.Text)
		sn.paneErr = inst.paneErr
		if inst.pane != nil {
			sn.built, sn.live = true, inst.pane.State()
		}
		return
	})
	s.Resource(resDocument, "the notebook's text, which this window never changes", func(inst *window) any {
		return mdnotebook.BlobHash(inst.def.Text)
	})
	s.Resource(resHandover, "what the last hand-over to the notebook window came to", func(inst *window) any {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return inst.note
	})
	s.Resource(resReading, "which reading the window shows, and the run that computed a live one", func(inst *window) any {
		if inst.pane == nil {
			return ""
		}
		started, ended := inst.pane.Runs()
		return inst.pane.State().Reading + "\x00" + itoa(started) + "\x00" + itoa(ended)
	})

	// Untrusted: the text is whatever its author wrote.
	appops.Query(s, app.OperationSpec{Name: opGetDocument, Version: 1,
		Summary: "read the notebook's text and where it is committed",
		Reads:   []string{resDocument}, Agents: true, Untrusted: true,
		Follows: []string{"open_in_notebook opens the notebook window on it, where its records are worked on and it is collected"}},
		func(sn snap, in appops.None) (out Document, err error) {
			out = Document{Book: sn.book, Path: sn.path, Slug: sn.slug, Text: sn.text, Length: len(sn.text), Revision: mdnotebook.BlobHash([]byte(sn.text))}
			if len(out.Text) > documentMaxBytes {
				cut := documentMaxBytes
				for cut > 0 && out.Text[cut]&0xC0 == 0x80 {
					cut--
				}
				out.Text, out.Truncated = out.Text[:cut], true
			}
			return out, nil
		})
	appops.Query(s, app.OperationSpec{Name: notebook.OperationGetReading, Version: 1,
		Summary: "which reading the window shows — stored, or live with when it was computed — and how the last run ended",
		Reads:   []string{resReading}, Agents: true,
		Follows: []string{"run_live computes the records again; show_stored shows what the notebook stores"}},
		func(sn snap, in appops.None) (out notebook.LiveState, err error) {
			if !sn.built {
				return out, app.RefuseOperation("the notebook cannot be read live: " + sn.paneErr)
			}
			return sn.live, nil
		})
	// A run reads the server and stores nothing; it settles when every
	// record answered or the run ended without them (ADR-0380 §SD4).
	appops.Command(s, app.OperationSpec{Name: notebook.OperationRunLive, Version: 1,
		Summary: "compute every record on the server again, read-only, and show the notebook over the results; nothing is stored. Settles with the reading",
		Effect:  app.OperationEffectRun, Reads: []string{resDocument}, Writes: []string{resReading}, Agents: true, Async: true,
		Gesture: "Run live, or Run again",
		Follows: []string{"a run that fails leaves the earlier reading on screen and names the record"}},
		func(inst *window, call app.OperationCall, in appops.None) (appops.None, error) {
			if inst.pane == nil {
				return appops.None{}, app.RefuseOperation("the notebook cannot be read live: " + inst.paneErr)
			}
			return appops.None{}, inst.pane.Run()
		})
	appops.Settles(s, notebook.OperationRunLive, func(inst *window, call app.OperationCall) (out notebook.LiveState, settled bool, err error) {
		started, ended := inst.pane.Runs()
		if ended < started {
			return
		}
		st := inst.pane.State()
		if st.Reading != "live" || st.LastErr != "" {
			return out, true, app.RefuseOperation("the run did not answer: " + st.LastErr)
		}
		return st, true, nil
	})
	s.Available(notebook.OperationRunLive, func(sn snap) (ok bool, reason string) {
		switch {
		case !sn.built:
			return false, "the notebook cannot be read live: " + sn.paneErr
		case sn.live.Running:
			return false, "a run is under way; wait for it to end"
		}
		return true, ""
	})
	appops.Command(s, app.OperationSpec{Name: notebook.OperationShowStored, Version: 1,
		Summary: "show what the notebook stores again; the live reading is let go",
		Effect:  app.OperationEffectView, Writes: []string{resReading}, Agents: true, Gesture: "Show stored"},
		func(inst *window, call app.OperationCall, in appops.None) (appops.None, error) {
			if inst.pane == nil {
				return appops.None{}, app.RefuseOperation("the notebook cannot be read live: " + inst.paneErr)
			}
			return appops.None{}, inst.pane.ShowStored()
		})
	appops.Command(s, app.OperationSpec{Name: opOpenInNotebook, Version: 1,
		Summary: "open the notebook window on this notebook's text, where its records are worked on in play and it is collected",
		Effect:  app.OperationEffectView, Reads: []string{resDocument}, Writes: []string{resHandover}, Agents: true, Gesture: "Open in notebook",
		Follows: []string{"the new window's get_document reads the text; it has none of the stored results, and collecting it stores them"}},
		func(inst *window, call app.OperationCall, in appops.None) (appops.None, error) {
			return appops.None{}, inst.openInNotebook()
		})
	return
}()

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }
