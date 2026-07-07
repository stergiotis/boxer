package nested

import "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"

// LabeledTextNested is the nested-model spelling of a dynamic-membership tuple
// (the codecdemo.LabeledTextDoc `@membership,verbatim` shape): the per-attribute
// membership is an lw.Verbatim FIELD instead of an `@membership` tag. `[]S` with
// a marker field is a dynamic nested tuple — one attribute per element, each
// carrying its own verbatim membership. Byte-identical to the flat spelling.
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/nested/labeledtextnested.go
type LabeledTextNested struct {
	_ struct{} `kind:"labeledTextNested"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Texts []labeledTextAttr `lw:"text"`
}

// labeledTextAttr is one text attribute: a verbatim membership marker (Label)
// plus the section's sub-columns.
type labeledTextAttr struct {
	Label      lw.Verbatim // per-attribute membership (verbatim channel)
	Text       string      `lw:"text"`
	WordLength []uint32    `lw:"wordLength"`
	WordBag    []string    `lw:"wordBag"`
}
