package nested

// ManyTagsDoc exercises a static-membership MANY nested section: anchor's
// `symbol` section (static membership "tags") as `[]S`, one attribute per
// element — the nested spelling of a flat `,explode` field. Every element emits
// an attribute carrying the same static membership.
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/nested/manytagsdoc.go
type ManyTagsDoc struct {
	_ struct{} `kind:"manyTagsDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Blocks []symBlock `lw:"tags,symbol"`
}

// symBlock is one symbol attribute — a single scalar sub-column (column "value",
// tag-free).
type symBlock struct {
	Val string
}
