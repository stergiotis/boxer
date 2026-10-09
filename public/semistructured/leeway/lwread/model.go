package lwread

// ShapeE is how a value column holds its value.
type ShapeE uint8

const (
	ShapeScalar ShapeE = iota
	ShapeList
	ShapeSet
)

func (inst ShapeE) String() string {
	switch inst {
	case ShapeList:
		return "list"
	case ShapeSet:
		return "set"
	}
	return "scalar"
}

// Item is one value — a scalar, or one item of a list or set.
type Item struct {
	// Text is the reading's spelling of it.
	Text string
	// Raw is the driver's text, which a gloss reads.
	Raw string
	// Cut is set when Text was cut at Options.MaxValueBytes.
	Cut bool
}

// Value is one value column of an attribute.
type Value struct {
	// Column is the value column's name.
	Column string
	// HandleColumn is the column as its physical name spells it, which a
	// handle names it by; Column when the name does not say.
	HandleColumn string
	// Type is the canonical type of the column, of one item for a list or
	// set.
	Type  string
	Shape ShapeE
	// Items holds the scalar, or the list's or set's items in reading
	// order: a list's own, a set's by value.
	Items []Item
	// More counts the items Options.MaxItems left out.
	More int
	// ArrowIdx is the physical column, for a gloss keyed by it.
	ArrowIdx int
}

// Membership is one membership of an attribute, as read.
type Membership struct {
	// Text is the membership rendered: a ref through the caller's renderer,
	// a verbatim name as it is, parameters spelled in brackets.
	Text string
	// Ref and IsRef are the registry id of a ref membership; a handle names
	// a ref by its id, which no renderer can make ambiguous.
	Ref   uint64
	IsRef bool
	// Params is the rendered parameters, empty when there are none.
	Params string
	// Name is the rendered name without its parameters.
	Name string
}

// Attribute is one attribute of a record.
type Attribute struct {
	// Section is the section the attribute is in; for a plain column, the
	// plain item type.
	Section string
	// HandleSection is the section as the physical column names spell it —
	// u32Array where Section is u32-array — which is what a handle and
	// LW_GET name it by, as leeway.columns and the Table's headers do;
	// Section when the names do not say.
	HandleSection string
	// CoGroup is the co-section group the section belongs to.
	CoGroup string
	// Plain is set for a plain column, which every record carries once.
	Plain bool
	// Name is what the attribute is called: its first membership, the
	// section when no membership names it, or the plain column.
	Name string
	// Qualified is set when Model.Qualify prefixed Name with the section.
	Qualified bool
	// Named is the membership that names it; nil when none does.
	Named *Membership
	// Labels are its further memberships.
	Labels []Membership
	// Values are its value columns the readability aspect shows.
	Values []Value
	// Hidden counts its value columns the readability aspect hides.
	Hidden int
	// SectionColumns is how many value columns the section declares, which
	// decides whether a handle names the column.
	SectionColumns int
}

// Record is one entity.
type Record struct {
	// Label is how the record is known: its natural key, else its id.
	Label      string
	Attributes []Attribute
	// Hidden counts value columns the readability aspect hid; Cut counts
	// items cut at Options.MaxValueBytes; More counts list and set items
	// left out.
	Hidden, Cut, More int
}

// Model is a batch as read.
type Model struct {
	Records []Record
}
