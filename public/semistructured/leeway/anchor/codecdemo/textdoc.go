package codecdemo

// TextDoc exercises the mixed-shape multi-sub-column path (ADR-0101):
// anchor's `text` section declares one scalar sub-column (`text` S) plus
// two zipped co-containers (`wordLength` U32h, `wordBag` Sh). The write
// path drives BeginAttribute(text) + one AddToCoContainersP(wordLength,
// wordBag) per element; the read path pairs the scalar accessor with the
// two iter.Seq container accessors. WordLength and WordBag must have
// equal length per row (co-containers advance in lockstep); both may be
// empty (the attribute still emits — the scalar tuple is the presence
// signal).
//
// textdoc.out.go is regenerated from textdoc.go by:
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/textdoc.go
type TextDoc struct {
	_ struct{} `kind:"textDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	// Text maps to the scalar sub-column of anchor's text section.
	Text string `lw:"prose,text:text"`
	// WordLength / WordBag map to the section's co-container sub-columns,
	// zipped element-wise (word i has length WordLength[i] and spelling
	// WordBag[i]).
	WordLength []uint32 `lw:"prose,text:wordLength"`
	WordBag    []string `lw:"prose,text:wordBag"`
}
