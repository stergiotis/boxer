package play

// The buffer and a notebook's records are one statement in two bindings
// (ADR-0371 §SD6): live here, where each WITH query is a node that runs, and
// sealed in a notebook, where each is a record with a stored result. This
// file is the two directions of that reading and nothing else of a
// notebook: play neither collects one nor draws one.
//
//   - get_notebook, and the toolbar's "Copy as notebook": the buffer as a
//     notebook's text, every WITH query a record cell and the buffer's own
//     SELECT the last (mdnotebook.FromBuffer).
//   - Load: a picked file that holds record cells is read as the buffer of
//     its records (mdnotebook.Buffer).
//
// The two are inverse, so a notebook's records can be opened here, worked
// on against the server, and written back as cells.

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

const opGetNotebook = "get_notebook"

// notebookMaxBytes bounds get_notebook's reading. A notebook cut short is
// not the buffer's, so a longer one is refused, not truncated.
const notebookMaxBytes = 64 << 10

// NotebookArgs is get_notebook's argument.
type NotebookArgs struct {
	Sink string `json:",omitzero" desc:"the record name for the buffer's own SELECT; main when left out. A buffer with a WITH query of that name needs another"`
}

// BufferNotebook is get_notebook's result.
type BufferNotebook struct {
	Markdown string   `desc:"the notebook's text: a sql fence with an id per record, then one table cell over the last record. It lists no stored result; boxer markdown notebook collect computes and stores them"`
	Records  []string `desc:"the records in the buffer's order: its WITH queries, then its own SELECT"`
}

// notebookOfBuffer is the buffer as a notebook's text, or why no notebook
// holds it.
func notebookOfBuffer(sql string, sink string) (out BufferNotebook, err error) {
	// What only play knows of a buffer: a node it computes itself has no
	// statement a server answers, so it cannot be a record.
	if split, sErr := splitGraph(sql); sErr == nil {
		for i := range split.Nodes {
			if split.Nodes[i].Client != nil {
				return out, app.RefuseOperation("the node " + string(split.Nodes[i].ID) + " is computed in play, and a record is computed on the server at collection")
			}
		}
	}
	text, err := mdnotebook.FromBuffer(sql, sink)
	if err != nil {
		return out, app.RefuseOperation(err.Error())
	}
	nb, err := mdnotebook.Read(text)
	if err != nil {
		return out, err
	}
	for i := range nb.Cells {
		if nb.Cells[i].Kind == mdnotebook.CellKindRecord {
			out.Records = append(out.Records, nb.Cells[i].Name)
		}
	}
	out.Markdown = string(text)
	return out, nil
}

// bufferOfNotebook reads a loaded file as a notebook's records. isNotebook
// is false for a file that holds no record cell: SQL, to be loaded as it is.
func bufferOfNotebook(body []byte) (sql string, isNotebook bool, err error) {
	nb, rErr := mdnotebook.Read(body)
	if rErr != nil {
		return "", false, nil
	}
	for i := range nb.Cells {
		if nb.Cells[i].Kind == mdnotebook.CellKindRecord {
			isNotebook = true
			break
		}
	}
	if !isNotebook {
		return "", false, nil
	}
	sql, err = mdnotebook.Buffer(nb)
	return sql, true, err
}

// personCopyNotebook is the toolbar's "Copy as notebook".
func (inst *PlayApp) personCopyNotebook() {
	out, err := notebookOfBuffer(inst.sql, "")
	if err != nil {
		inst.setActionNote("copy as notebook: " + err.Error())
		return
	}
	inst.copyToClipboard("notebook", out.Markdown)
}

func addNotebookOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: the text quotes the person's buffer.
	appops.Query(s, app.OperationSpec{Name: opGetNotebook, Version: 1,
		Summary: "read the buffer as a notebook's text (ADR-0371): each WITH query a record cell, the buffer's own SELECT the last record, and one table cell over it",
		Reads:   []string{opsResSql}, Agents: true, Untrusted: true,
		Follows: []string{"the text is not sealed: write it to a file and run boxer markdown notebook collect on it, then check",
			"a notebook loaded with Load, or its buffer from boxer markdown notebook buffer, is this reading the other way"}},
		func(sn opsSnap, in NotebookArgs) (out BufferNotebook, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			out, err = notebookOfBuffer(sn.state.Sql, in.Sink)
			if err != nil {
				return BufferNotebook{}, err
			}
			if len(out.Markdown) > notebookMaxBytes {
				return BufferNotebook{}, app.RefuseOperation("the buffer's notebook is " + strconv.Itoa(len(out.Markdown)) + " bytes, over the " + strconv.Itoa(notebookMaxBytes) + " this reading returns; copy it from the window")
			}
			return out, nil
		})
}
