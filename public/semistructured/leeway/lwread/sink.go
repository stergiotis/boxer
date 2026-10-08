package lwread

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// Options bounds and names the reading.
type Options struct {
	// Renderer names memberships; nil takes membership.DefaultRenderer, which
	// spells a ref as its hex id. A registry-backed renderer names it.
	Renderer *membership.Renderer
	// MaxItems bounds a list's or set's items; 0 takes DefaultMaxItems.
	MaxItems int
	// MaxValueBytes cuts an item's text; 0 does not cut.
	MaxValueBytes int
}

// DefaultMaxItems is the list and set bound when Options.MaxItems is 0.
const DefaultMaxItems = 64

// Sink collects a Model from a streamreadaccess drive. It is not
// goroutine-safe; a drive's BeginBatch resets it.
type Sink struct {
	opts  Options
	model Model

	rec      *Record
	attr     *Attribute
	plain    bool
	plainSec string
	section  string
	coGroup  string
	allSec   bool
	nCols    int

	col      string
	colType  canonicaltypes.PrimitiveAstNodeI
	colIdx   int
	colShown bool
	shape    ShapeE
	items    []Item
	more     int
	text     strings.Builder
	inTags   bool
}

var (
	_ streamreadaccess.SinkI           = (*Sink)(nil)
	_ streamreadaccess.MembershipSinkI = (*Sink)(nil)
)

// NewSink returns an empty collector.
func NewSink(opts Options) *Sink {
	if opts.Renderer == nil {
		opts.Renderer = membership.DefaultRenderer()
	}
	if opts.MaxItems <= 0 {
		opts.MaxItems = DefaultMaxItems
	}
	return &Sink{opts: opts}
}

// Model is what the last drive read.
func (inst *Sink) Model() *Model { return &inst.model }

func (inst *Sink) BeginBatch()           { inst.model = Model{} }
func (inst *Sink) EndBatch() (err error) { return nil }
func (inst *Sink) BeginEntity() {
	inst.model.Records = append(inst.model.Records, Record{})
	inst.rec = &inst.model.Records[len(inst.model.Records)-1]
}
func (inst *Sink) EndEntity() (err error) {
	inst.rec.Label = recordLabel(inst.rec)
	inst.rec = nil
	return nil
}

func (inst *Sink) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.plain, inst.plainSec, inst.nCols = true, itemType.String(), len(valueNames)
}
func (inst *Sink) EndPlainSection() (err error) { inst.plain = false; return nil }
func (inst *Sink) BeginPlainValue()             {}
func (inst *Sink) EndPlainValue() (err error)   { return nil }

func (inst *Sink) BeginTaggedSections()             {}
func (inst *Sink) EndTaggedSections() (err error)   { return nil }
func (inst *Sink) BeginCoSectionGroup(k naming.Key) { inst.coGroup = k.String() }
func (inst *Sink) EndCoSectionGroup() (err error)   { inst.coGroup = ""; return nil }

func (inst *Sink) BeginSection(name naming.StylableName, valueNames []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, aspects useaspects.AspectSet, _ int) {
	inst.section, inst.nCols = name.String(), len(valueNames)
	inst.allSec = aspects.Contains(useaspects.AspectSectionMembershipsAllSecondary)
}
func (inst *Sink) EndSection() (err error) { inst.section = ""; return nil }

func (inst *Sink) BeginTaggedValue() {
	if inst.rec == nil {
		return
	}
	inst.rec.Attributes = append(inst.rec.Attributes, Attribute{Section: inst.section, CoGroup: inst.coGroup, SectionColumns: inst.nCols})
	inst.attr = &inst.rec.Attributes[len(inst.rec.Attributes)-1]
}

func (inst *Sink) EndTaggedValue() (err error) {
	if a := inst.attr; a != nil {
		if a.Named != nil {
			a.Name = a.Named.Text
		} else {
			a.Name = a.Section
		}
		inst.rec.Hidden += a.Hidden
	}
	inst.attr = nil
	return nil
}

func (inst *Sink) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, ct canonicaltypes.PrimitiveAstNodeI, aspects valueaspects.AspectSet) {
	inst.col, inst.colType, inst.colIdx = name.String(), ct, colAddr.Index
	inst.colShown = !(aspects.Contains(valueaspects.AspectMachineReadable) && !aspects.Contains(valueaspects.AspectHumanReadable))
	inst.shape, inst.items, inst.more = ShapeScalar, nil, 0
	inst.text.Reset()
}

func (inst *Sink) EndColumn() {
	if inst.rec == nil {
		return
	}
	if inst.plain {
		a := Attribute{Section: inst.plainSec, Plain: true, Name: inst.col, SectionColumns: inst.nCols}
		inst.addValue(&a)
		inst.rec.Hidden += a.Hidden
		inst.rec.Attributes = append(inst.rec.Attributes, a)
		return
	}
	if inst.attr != nil {
		inst.addValue(inst.attr)
	}
}

// addValue files the column just read under a, or counts it hidden.
func (inst *Sink) addValue(a *Attribute) {
	if !inst.colShown {
		a.Hidden++
		return
	}
	if inst.shape == ShapeScalar {
		inst.items = append(inst.items[:0], inst.item(inst.text.String(), itemType(inst.colType)))
	}
	if inst.shape == ShapeSet {
		sortItems(inst.items, itemType(inst.colType))
	}
	v := Value{Column: inst.col, Shape: inst.shape, Items: inst.items, More: inst.more, ArrowIdx: inst.colIdx}
	if t := itemType(inst.colType); t != nil {
		v.Type = t.String()
	}
	for _, it := range v.Items {
		if it.Cut {
			inst.rec.Cut++
		}
	}
	inst.rec.More += v.More
	a.Values = append(a.Values, v)
	inst.items = nil
}

// item spells one value read as raw.
func (inst *Sink) item(raw string, ct canonicaltypes.PrimitiveAstNodeI) (it Item) {
	it.Raw = raw
	it.Text = spell(raw, ct)
	if m := inst.opts.MaxValueBytes; m > 0 && len(it.Text) > m {
		it.Text, it.Cut = cutUTF8(it.Text, m)+"…", true
	}
	return
}

func (inst *Sink) BeginScalarValue()             { inst.shape = ShapeScalar; inst.text.Reset() }
func (inst *Sink) EndScalarValue() (err error)   { return nil }
func (inst *Sink) BeginHomogenousArrayValue(int) { inst.shape = ShapeList }
func (inst *Sink) EndHomogenousArrayValue()      {}
func (inst *Sink) BeginSetValue(int)             { inst.shape = ShapeSet }
func (inst *Sink) EndSetValue()                  {}
func (inst *Sink) BeginValueItem(int)            { inst.text.Reset() }
func (inst *Sink) EndValueItem() {
	if len(inst.items) >= inst.opts.MaxItems {
		inst.more++
		return
	}
	inst.items = append(inst.items, inst.item(inst.text.String(), itemType(inst.colType)))
}

func (inst *Sink) Write(p []byte) (n int, err error) {
	inst.text.Write(p)
	return len(p), nil
}
func (inst *Sink) WriteString(s string) (n int, err error) {
	inst.text.WriteString(s)
	return len(s), nil
}

func (inst *Sink) BeginTags(int) { inst.inTags = true }
func (inst *Sink) EndTags()      { inst.inTags = false }

func (inst *Sink) AddMembershipRef(lowCard bool, ref uint64) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityRef, LowCard: lowCard, Ref: ref})
}
func (inst *Sink) AddMembershipVerbatim(lowCard bool, verbatim string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityVerbatim, LowCard: lowCard, Verbatim: verbatim})
}
func (inst *Sink) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityPerRowBlob, LowCard: lowCard, Ref: ref, Params: params})
}
func (inst *Sink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityPerRowId, Ref: ref, Params: params})
}
func (inst *Sink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityPerRowName, Verbatim: verbatim, Params: params})
}

// addMembership names the attribute by its first membership, unless the
// section declares every membership secondary, and labels it with the rest.
func (inst *Sink) addMembership(mv membership.MembershipValue) {
	if inst.attr == nil || membership.IsPlaceholder(mv) {
		return
	}
	m := Membership{Name: inst.opts.Renderer.Render(mv)}
	switch mv.Kind {
	case membership.IdentityRef, membership.IdentityPerRowBlob, membership.IdentityPerRowId:
		m.Ref, m.IsRef = mv.Ref, true
	}
	m.Text = m.Name
	if mv.Params != "" {
		m.Params = inst.opts.Renderer.RenderParams(mv.Params)
		m.Text += "[" + m.Params + "]"
	}
	if inst.attr.Named == nil && !inst.allSec {
		inst.attr.Named = &m
		return
	}
	inst.attr.Labels = append(inst.attr.Labels, m)
}

// recordLabel is how a record is known: a plain column whose name says it is
// a natural key, else one named like a key or a name, else the id.
func recordLabel(r *Record) string {
	best, rank := "", 0
	for _, a := range r.Attributes {
		if !a.Plain || len(a.Values) == 0 || len(a.Values[0].Items) == 0 {
			continue
		}
		lc := strings.ToLower(a.Name)
		k := 0
		switch {
		case strings.Contains(lc, "natural"):
			k = 3
		case strings.Contains(lc, "name") || strings.Contains(lc, "key") || strings.Contains(lc, "label"):
			k = 2
		case lc == "id":
			k = 1
		}
		if t := a.Values[0].Items[0].Text; k > rank && t != "" {
			best, rank = t, k
		}
	}
	return best
}
