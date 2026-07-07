package nested

import "github.com/stergiotis/boxer/public/functional/option"

// OptNoteDoc exercises an OPTIONAL nested section: anchor's `symbol` section
// (static membership "note") present-or-absent per row, via option.Option[S].
// An absent value contributes zero attributes; a present one exactly one. (Only
// option.Option[S] is accepted — a `*S` nested field is rejected, matching the
// codegen front-end's scalar pointer policy.)
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/nested/optnotedoc.go
type OptNoteDoc struct {
	_ struct{} `kind:"optNoteDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Note option.Option[noteAttr] `lw:"note,symbol"`
}

// noteAttr is one symbol attribute — a single scalar sub-column (column "value",
// tag-free).
type noteAttr struct {
	Val string
}
