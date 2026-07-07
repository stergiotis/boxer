package nested

import "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"

// NamedTextNested exercises HETEROGENEOUS per-attribute memberships (ADR-0109
// D4): one attribute carries a verbatim membership (Name) AND a ref membership
// (Kind) on different channels. The ref id is carried directly (no lookup).
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/nested/namedtextnested.go
type NamedTextNested struct {
	_ struct{} `kind:"namedTextNested"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Notes []namedTextAttr `lw:"text"`
}

type namedTextAttr struct {
	Name       lw.Verbatim // membership #1 — verbatim channel
	Kind       lw.Ref      // membership #2 — ref channel (id carried directly)
	Text       string      `lw:"text"`
	WordLength []uint32    `lw:"wordLength"`
	WordBag    []string    `lw:"wordBag"`
}
