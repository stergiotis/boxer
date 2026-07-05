package codecdemo

// LineageTag is the tuple element of LineageDoc's type-lineage mapping onto
// anchor's `symbol` section (ADR-0109 (a)+(b)): one attribute per element, its
// Kind the leaf kind name, its Ancestors the ancestor-closure kind node-ids as
// MANY ref memberships on that one attribute (`membership-card > 1`). Ancestors
// is a repeated `@membership` field on the `lowCardRef` channel — the id is
// carried directly as a uint64, so a supertype query is a membership filter (no
// lookup, no hierarchy walk).
type LineageTag struct {
	Ancestors []uint64 `lw:"@membership,lowCardRef"` // N ref memberships (person, legalEntity, thing, …)
	Kind      string   `lw:"symbol"`                 // the attribute value: the leaf kind name
}

// EdgeTag is the tuple element of LineageDoc's edge-aliasing mapping onto
// anchor's `foreignKey` section: a value under TWO fixed ref memberships on the
// `lowCardRef` channel — a typed predicate and a generic graph membership — so
// one edge answers both a typed-predicate query and a generic graph traversal
// (ADR-0109 (a) repeated fixed fields). foreignKey declares only the LowCardRef
// spec, so both memberships share that channel.
type EdgeTag struct {
	Predicate uint64 `lw:"@membership,lowCardRef"` // typed, e.g. `owner` node id
	Generic   uint64 `lw:"@membership,lowCardRef"` // generic, e.g. `pointsTo` node id
	Target    uint64 `lw:"foreignKey"`             // the edge target entity id
}

// NamedText is the tuple element of LineageDoc's property mapping onto anchor's
// mixed-shape `text` section (S = 1, C = 2): a value under a HETEROGENEOUS pair
// of memberships — a verbatim Name (the literal property label) and a ref Kind
// (the property's type node-id) — proving an element may mix channels
// (ADR-0109 D4). The AttrI then exposes AddMembershipLowCardVerbatimP AND
// AddMembershipLowCardRefP.
type NamedText struct {
	Name       string   `lw:"@membership,verbatim"`   // literal property name
	Kind       uint64   `lw:"@membership,lowCardRef"` // property type node-id
	Text       string   `lw:"text:text"`
	WordLength []uint32 `lw:"text:wordLength"`
	WordBag    []string `lw:"text:wordBag"`
}

// LineageDoc exercises the multi-membership + ref-channel tuple path (ADR-0109)
// across three shapes an ontology-integrated entity model needs at once: a
// slice of ref memberships (type lineage), two fixed ref memberships (edge
// aliasing), and a heterogeneous verbatim+ref pair over a mixed-shape section.
// It is the gen≡reflect byte-identity witness for the extended grammar.
//
// lineagedoc.out.go is regenerated from lineagedoc.go by:
//
//	./boxer.sh keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/lineagedoc.go
type LineageDoc struct {
	_ struct{} `kind:"lineageDoc"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Types []LineageTag `lw:"symbol"`     // type-lineage: N ref memberships per element
	Edges []EdgeTag    `lw:"foreignKey"` // edge aliasing: two fixed ref memberships
	Notes []NamedText  `lw:"text"`       // heterogeneous verbatim + ref memberships
}
