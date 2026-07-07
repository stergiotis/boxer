// Package nested holds codecdemo DTOs written in the NESTED attribute-struct
// model (Slice C) — a section is a Go struct field whose fields are the
// sub-columns, instead of flat `membership,section:column` fields. Each must
// marshal byte-identically to its flat spelling and to the reflect front-end.
// Kept in a sibling package so its membership kind consts (kindProse, …) do not
// collide with codecdemo's flat DTOs, which reuse the same membership names.
package nested

// TextDocNested is the nested spelling of codecdemo.TextDoc: anchor's `text`
// section (static membership "prose") as a One-cardinality nested attribute
// struct. The whole text attribute is one struct value per row; its fields are
// the section's sub-columns (scalar `text`, co-containers `wordLength` /
// `wordBag`).
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/nested/textdocnested.go
type TextDocNested struct {
	_ struct{} `kind:"textDocNested"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Body proseAttrs `lw:"prose,text"`
}

// proseAttrs is the text section's sub-column tuple: one scalar (`text`) plus
// two zipped co-containers (`wordLength` / `wordBag`, equal length per row).
type proseAttrs struct {
	Text       string   `lw:"text"`
	WordLength []uint32 `lw:"wordLength"`
	WordBag    []string `lw:"wordBag"`
}
