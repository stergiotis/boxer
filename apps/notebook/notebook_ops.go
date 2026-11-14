package notebook

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
)

// The catalog (ADR-0269, ADR-0371 §SD10): the operations over the document.
// The ones over the buffer as records are play's (get_notebook,
// set_notebook); an agent works a notebook's statements in a play window
// and hands the buffer here with set_records.

const (
	resDocument = "document"
	resFace     = "face"

	opGetDocument   = "get_document"
	opCheckNotebook = "check_notebook"
	opSetRecords    = "set_records"
	opShowFace      = "show_face"
	opOpenSample    = "open_sample"
	// opCollectNotebook writes outside the window, to the lading store:
	// consequential, so the person confirms an agent's call every time.
	opCollectNotebook = "collect_notebook"
)

// documentMaxBytes bounds get_document's text.
const documentMaxBytes = 256 << 10

// snap is what queries read: a copy taken after the command stage.
type snap struct {
	open     bool
	isNb     bool
	docErr   string
	name     string
	origin   string
	text     string
	edited   bool
	records  []string
	face     string
	checked  bool
	check    notebookview.Check
	revision string
	// collecting says a collection is running.
	collecting bool
}

// Document is get_document's result.
type Document struct {
	Name      string   `desc:"the notebook's file name"`
	Origin    string   `desc:"where it was opened from: a sample, or a lading snapshot as mount@snapshot:path"`
	Text      string   `desc:"the notebook's markdown: prose, record cells (a sql fence followed by an id), presentation cells (a sql fence with a renderer) and its frontmatter listing the stored results"`
	Length    int      `desc:"the whole text's length in bytes"`
	Truncated bool     `json:",omitzero" desc:"the text is cut at 256 KiB; Length and Revision still describe the whole"`
	Revision  string   `desc:"names this exact text"`
	Edited    bool     `json:",omitzero" desc:"a buffer was taken back into the text since it was opened"`
	Records   []string `desc:"the records in document order; the last is the buffer's own SELECT when the records are read as a play buffer"`
	Face      string   `desc:"the face the window shows: read or records"`
}

// CheckReport is check_notebook's result.
type CheckReport struct {
	Sealed   bool     `desc:"the notebook shows what it stores: no problem, no stale record, every presentation cell answered"`
	Problems []string `json:",omitzero" desc:"what the text, its frontmatter and its files disagree on: a record with no stored result, a blob that does not match its name, a figure whose label is not its value"`
	Stale    []string `json:",omitzero" desc:"records whose statement was edited since collection; what is stored is still shown"`
	Derived  []string `json:",omitzero" desc:"records that read a stale record, as 'record reads edited'"`
	Refused  []string `json:",omitzero" desc:"presentation cells with no answer, as 'sql renderer: what was refused': a cell picks and names columns of one record, and anything else belongs in a record"`
}

// SetRecordsArgs is set_records' argument.
type SetRecordsArgs struct {
	Sql  string `desc:"a play buffer: each query of its WITH list is written into the record cell of its name, its own SELECT into the record Sink names. A part no record is named for becomes a record cell after the one before it"`
	Sink string `json:",omitzero" desc:"the record the buffer's own SELECT is; the notebook's last record when left out"`
}

// ShowFaceArgs is show_face's argument.
type ShowFaceArgs struct {
	Face string `desc:"read, the cell stack over what is stored; or records, play over the notebook's records"`
}

// CollectArgs is collect_notebook's argument.
type CollectArgs struct {
	Tier string `desc:"how confidential what the records read is, as a TLP tier: CLEAR, GREEN, AMBER, AMBER+STRICT or RED. It is the caller's statement and is recorded with the publish; the store refuses a tier above its ceiling"`
}

// CollectOutcome is collect_notebook's result.
type CollectOutcome struct {
	Snapshot  string   `desc:"the snapshot that was written, as mount@snapshot:path; the window opens it"`
	Records   []string `desc:"the records that were computed and stored"`
	Files     uint64   `desc:"the files of the snapshot: the text, the stored results and what the notebook names beside them"`
	Bytes     uint64   `desc:"their bytes"`
	KeptUntil string   `desc:"the day the snapshot expires"`
}

// OpenSampleArgs is open_sample's argument.
type OpenSampleArgs struct {
	Name string `desc:"minimal, report or renderers"`
}

var ops = func() (s *appops.Set[*App, snap]) {
	s = appops.NewSet(func(inst *App) (sn snap) {
		sn.face = inst.face.String()
		sn.docErr = inst.docErr
		if inst.doc == nil {
			return
		}
		sn.open, sn.isNb = true, inst.nb != nil
		sn.name, sn.origin, sn.edited = inst.doc.name, inst.doc.origin, inst.doc.edited
		sn.text = string(inst.doc.text)
		sn.revision = mdnotebook.BlobHash(inst.doc.text)
		sn.records = inst.records()
		sn.checked, sn.check = inst.checked, inst.check
		sn.collecting = inst.collect.job.Running()
		return
	})
	s.Resource(resDocument, "the notebook's text", func(inst *App) any {
		if inst.doc == nil {
			return ""
		}
		return mdnotebook.BlobHash(inst.doc.text)
	})
	s.Resource(resFace, "which face the window shows", func(inst *App) any { return inst.face.String() })

	// Untrusted: the text is whatever its author wrote.
	appops.Query(s, app.OperationSpec{Name: opGetDocument, Version: 1,
		Summary: "read the notebook's text, where it was opened from and its records",
		Reads:   []string{resDocument, resFace}, Agents: true, Untrusted: true,
		Follows: []string{"play's set_notebook takes the text and makes its records the buffer of a play window, to work on the statements against live data",
			"set_records writes a buffer back into the text"}},
		func(sn snap, in appops.None) (out Document, err error) {
			if !sn.open {
				return out, app.RefuseOperation("no notebook is open; open_sample opens one of the samples")
			}
			out = Document{Name: sn.name, Origin: sn.origin, Text: sn.text, Length: len(sn.text), Revision: sn.revision,
				Edited: sn.edited, Records: sn.records, Face: sn.face}
			if len(out.Text) > documentMaxBytes {
				cut := documentMaxBytes
				for cut > 0 && out.Text[cut]&0xC0 == 0x80 {
					cut--
				}
				out.Text, out.Truncated = out.Text[:cut], true
			}
			return out, nil
		})
	appops.Query(s, app.OperationSpec{Name: opCheckNotebook, Version: 1,
		Summary: "what stands between the notebook and being sealed, with no server asked: problems, stale records and presentation cells with no answer",
		Reads:   []string{resDocument}, Agents: true, Untrusted: true,
		Follows: []string{"a stale record or one with no stored result is cured by collection; a refused cell by moving what it computes into a record"}},
		func(sn snap, in appops.None) (out CheckReport, err error) {
			switch {
			case !sn.open:
				return out, app.RefuseOperation("no notebook is open; open_sample opens one of the samples")
			case !sn.isNb:
				return out, app.RefuseOperation("the text is not a notebook: " + sn.docErr)
			case !sn.checked:
				return out, app.RefuseOperation("the notebook's stored results are still being read; ask again")
			}
			ck := sn.check
			return CheckReport{Sealed: ck.Sealed(), Problems: ck.Problems, Stale: ck.Stale, Derived: ck.Derived, Refused: ck.Refused}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opSetRecords, Version: 1,
		Summary: "write a play buffer into the notebook's record cells by name; prose, presentation cells and unchanged records keep their text",
		Effect:  app.OperationEffectDocument, Writes: []string{resDocument}, Agents: true, Gesture: "Take the buffer back, on the Records face",
		Follows: []string{"a record whose statement changed is stale, and a new one has no stored result, until the notebook is collected",
			"a buffer that drops or reorders the notebook's records is refused, naming the record"}},
		func(inst *App, call app.OperationCall, in SetRecordsArgs) (appops.None, error) {
			changed, err := inst.setRecords(in.Sql, in.Sink)
			inst.noteTakeBack(changed, err)
			return appops.None{}, err
		})
	appops.Command(s, app.OperationSpec{Name: opShowFace, Version: 1,
		Summary: "show the read face or the records face",
		Effect:  app.OperationEffectView, Writes: []string{resFace}, Agents: true, Gesture: "the Read and Records buttons"},
		func(inst *App, call app.OperationCall, in ShowFaceArgs) (appops.None, error) {
			switch in.Face {
			case faceNameRead:
				inst.face = faceRead
			case faceNameRecords:
				inst.face = faceRecords
			default:
				return appops.None{}, app.RefuseOperation("a face is read or records")
			}
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opOpenSample, Version: 1,
		Summary: "open one of the sample notebooks in place of the window's document",
		Effect:  app.OperationEffectDocument, Writes: []string{resDocument}, Agents: true, Gesture: "the Open sample buttons of an empty window"},
		func(inst *App, call app.OperationCall, in OpenSampleArgs) (appops.None, error) {
			if err := inst.openSample(in.Name); err != nil {
				return appops.None{}, app.RefuseOperation(err.Error())
			}
			return appops.None{}, nil
		})
	// Collection runs off the frame and settles when the snapshot was
	// written or the run ended without one (ADR-0358).
	appops.Command(s, app.OperationSpec{Name: opCollectNotebook, Version: 1,
		Summary: "run every record once on the server and publish the notebook with its stored results to the lading store as a new snapshot, under the confidentiality tier the caller states; settles with the snapshot",
		Effect:  app.OperationEffectConsequential, Reads: []string{resDocument}, Writes: []string{resDocument}, Agents: true, Async: true,
		Gesture: "Collect…, then Collect and publish",
		Follows: []string{"the window opens the snapshot that was written; check_notebook then says whether it is sealed",
			"a notebook whose records read sealed data is refused before anything runs"}},
		func(inst *App, call app.OperationCall, in CollectArgs) (appops.None, error) {
			tier, ok := agentrun.ParseTier(in.Tier)
			if !ok {
				return appops.None{}, app.RefuseOperation("a tier is CLEAR, GREEN, AMBER, AMBER+STRICT or RED")
			}
			return appops.None{}, inst.startCollect(tier)
		})
	appops.Settles(s, opCollectNotebook, func(inst *App, call app.OperationCall) (out CollectOutcome, settled bool, err error) {
		cs := &inst.collect
		if cs.done < cs.seq {
			return
		}
		if cs.last == nil {
			return out, true, app.RefuseOperation("not collected: " + cs.lastErr)
		}
		return CollectOutcome{Snapshot: cs.last.ref.String(), Records: cs.last.records, Files: cs.last.files, Bytes: cs.last.bytes, KeptUntil: cs.last.expires}, true, nil
	})
	s.Available(opCollectNotebook, func(sn snap) (ok bool, reason string) {
		if sn.collecting {
			return false, "a collection is running; wait for it to end"
		}
		return true, ""
	})
	return
}()
