package mdnotebook

// Problems are what stands between a notebook and being sealed, beside a
// stale record: what the text and its frontmatter disagree on, a blob that
// could not be read, and a figure whose label is no longer the rendering of
// its value (ADR-0371 §SD5, §SD8).
//
// A bound notebook ([Bind]) stores nothing, so two of these do not apply to
// it: a record the frontmatter does not list is a problem only when it is
// not bound either, and a figure's label is not held to its value, which a
// reader draws in its place ([Relabel]).
func Problems(nb *Notebook, ld *Loaded) (problems []Problem) {
	for _, p := range nb.Problems {
		if ld.Bound && p.Record != "" {
			if _, bound := ld.Records[p.Record]; bound {
				continue
			}
			p.Text = "the record " + p.Record + " is not bound"
		}
		problems = append(problems, p)
	}
	problems = append(problems, ld.Problems...)
	if len(nb.Vals) == 0 {
		return
	}
	cat := ld.Catalog()
	for _, ref := range nb.Vals {
		if _, loaded := ld.Records[ref.Record]; !loaded {
			// Already a problem of the frontmatter or of the blob.
			continue
		}
		v, err := ValueOf(cat, ld.Records, ref.Record, ref.Column, ref.Gloss)
		switch {
		case err != nil:
			problems = append(problems, Problem{Cell: ref.Cell, Text: "val:" + ref.Record + "." + ref.Column + ": " + err.Error()})
		case ld.Bound:
		case v.Face.Text != ref.Label:
			problems = append(problems, Problem{Cell: ref.Cell, Text: "val:" + ref.Record + "." + ref.Column + " reads “" + ref.Label + "” in the text and “" + v.Face.Text + "” in the record"})
		}
	}
	return
}

// StaleRecords names, in document order, the records whose statement was
// edited since collection.
func StaleRecords(nb *Notebook, ld *Loaded) (names []string) {
	for _, c := range nb.recordCells() {
		if ld.Stale[c.Name] {
			names = append(names, c.Name)
		}
	}
	return
}

// DerivedStale is one record computed from an edited record's earlier
// statement.
type DerivedStale struct {
	// Record is the record whose own statement is unchanged.
	Record string
	// Edited is the edited record it reads, directly or through others.
	Edited string
}

// DerivedStaleRecords names, in document order, the records that read an
// edited record: their statements are as collected, and what is stored for
// them is not what the text would compute.
func DerivedStaleRecords(nb *Notebook, ld *Loaded) (derived []DerivedStale) {
	for _, c := range nb.recordCells() {
		if edited, found := ld.StaleVia[c.Name]; found {
			derived = append(derived, DerivedStale{Record: c.Name, Edited: edited})
		}
	}
	return
}
