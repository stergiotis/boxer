package nested

import "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"

// LineageNested exercises a REPEATED ref membership ([]lw.Ref): one attribute
// carries several ref memberships on one channel (ADR-0109 D3), the ids carried
// directly. The `symbol` section's scalar sub-column (column "value") holds the
// attribute's own value.
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/nested/lineagenested.go
type LineageNested struct {
	_ struct{} `kind:"lineageNested"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Types []lineageAttr `lw:"symbol"`
}

type lineageAttr struct {
	Ancestors []lw.Ref // repeated ref membership — several ids per attribute
	Kind      string   // value sub-column (column "value", tag-free)
}
