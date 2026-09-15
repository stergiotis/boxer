package card

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// ItemKindE says what an Item stands for, which is what decides how it is
// spelled as a predicate over the physical columns (ADR-0235 §SD6).
type ItemKindE uint8

const (
	// ItemKindSection: the entity has the tagged section. Column is the
	// section's first value column, whose non-empty array is the presence.
	ItemKindSection ItemKindE = iota
	// ItemKindCoGroup: the entity has the co-section group.
	ItemKindCoGroup
	// ItemKindTaggedValue: a value column of a tagged section carries
	// Value for the entity (an element of the column's array).
	ItemKindTaggedValue
	// ItemKindTagRef: a low-cardinality membership ref Ref is attached to
	// an attribute of the section.
	ItemKindTagRef
	// ItemKindTagVerbatim: a low-cardinality verbatim membership Value is
	// attached to an attribute of the section.
	ItemKindTagVerbatim
	// ItemKindComponent: the entity carries the registered component kind
	// named by Value. Not emitted by the sink — a consumer that can detect
	// components adds these to the sets it built.
	ItemKindComponent
)

// Item is one member of the vocabulary an ItemExtractor builds: a fact an
// entity either has or lacks.
type Item struct {
	Name string
	Kind ItemKindE
	// Section is the section's styled name; PhysicalSection the name the
	// physical columns carry, read off the first value column seen in the
	// section, which is what a membership column's name is found by.
	Section         string
	PhysicalSection string
	// Column is the physical column the item reads, where the sink saw
	// one: the value column for values, the section's first value column
	// for a section, empty for co-groups and memberships. Handle is the
	// same column as the authoring surface names it, `section:column`
	// (ADR-0116), which is how a predicate over the item should be spelled;
	// for a membership it names the section's low-cardinality ref or
	// verbatim column, `section:lr` or `section:lv`.
	Column string
	Handle string
	// Value is the value text, or the verbatim membership; Ref the
	// membership ref. Quoted says whether Value is spelled as a string.
	Value  string
	Ref    uint64
	Quoted bool
}

// ItemSets is the per-entity item sets over a vocabulary.
type ItemSets struct {
	Items []Item
	// Rows holds, per entity, its items' ids in ascending order.
	Rows [][]int32
	// Support is the number of entities holding each item.
	Support []int32
}

// Select keeps the given entities, in the given order, over the same
// vocabulary, and recounts the support over them.
func (inst *ItemSets) Select(rows []int) (out ItemSets) {
	out.Items = inst.Items
	out.Support = make([]int32, len(inst.Items))
	out.Rows = make([][]int32, len(rows))
	for i, r := range rows {
		out.Rows[i] = inst.Rows[r]
		for _, it := range inst.Rows[r] {
			out.Support[it]++
		}
	}
	return
}

// Prune keeps the items whose support lies in [minSupport, maxSupport],
// at most maxItems of them by support then name, and renumbers the rows.
// Items everyone has and items almost no one has say nothing about a
// group, and the cap bounds the search that follows.
func (inst *ItemSets) Prune(minSupport, maxSupport int32, maxItems int) (out ItemSets) {
	keep := make([]int32, 0, len(inst.Items))
	for i := range inst.Items {
		if s := inst.Support[i]; s >= minSupport && s <= maxSupport {
			keep = append(keep, int32(i))
		}
	}
	slices.SortFunc(keep, func(a, b int32) int {
		if c := cmp.Compare(inst.Support[b], inst.Support[a]); c != 0 {
			return c
		}
		return cmp.Compare(inst.Items[a].Name, inst.Items[b].Name)
	})
	if maxItems > 0 && len(keep) > maxItems {
		keep = keep[:maxItems]
	}
	remap := make(map[int32]int32, len(keep))
	out.Items = make([]Item, len(keep))
	out.Support = make([]int32, len(keep))
	for j, i := range keep {
		remap[i] = int32(j)
		out.Items[j] = inst.Items[i]
		out.Support[j] = inst.Support[i]
	}
	out.Rows = make([][]int32, len(inst.Rows))
	for r, row := range inst.Rows {
		ids := make([]int32, 0, len(row))
		for _, i := range row {
			if j, ok := remap[i]; ok {
				ids = append(ids, j)
			}
		}
		slices.Sort(ids)
		out.Rows[r] = ids
	}
	return
}

// ItemExtractor is a streamreadaccess sink that turns each entity into the
// set of items it holds: its tagged sections and co-groups, its values
// where short, and its low-cardinality memberships. High-cardinality
// memberships are identifiers and are not items; a value longer than
// ItemMaxValueLen is content, not a category, and is not one either; a
// value that is not printable text is a blob; and the plain section's
// values are the entity's identity, which no group shares.
type ItemExtractor struct {
	items   []Item
	byName  map[string]int32
	support []int32
	rows    [][]int32

	cur        map[int32]struct{}
	curSection string
	curSecPhys string
	curSecItem int32
	inPlain    bool
	curColumn  string
	curColName string
	curQuoted  bool
	// MaxItems caps the vocabulary: past it new items are dropped and the
	// entity keeps only what is already known.
	MaxItems int
}

const (
	// ItemMaxValueLen is the longest value text that becomes an item.
	ItemMaxValueLen = 64
	// ItemDefaultMaxItems is the default vocabulary cap.
	ItemDefaultMaxItems = 200_000
)

// NewItemExtractor returns an empty extractor.
func NewItemExtractor() *ItemExtractor {
	return &ItemExtractor{
		byName:   map[string]int32{},
		cur:      map[int32]struct{}{},
		MaxItems: ItemDefaultMaxItems,
	}
}

// Results returns what was extracted so far.
func (inst *ItemExtractor) Results() ItemSets {
	return ItemSets{Items: inst.items, Rows: inst.rows, Support: inst.support}
}

func (inst *ItemExtractor) add(it Item) (id int32) {
	id, ok := inst.byName[it.Name]
	if !ok {
		if len(inst.items) >= inst.MaxItems {
			return -1
		}
		id = int32(len(inst.items))
		inst.items = append(inst.items, it)
		inst.support = append(inst.support, 0)
		inst.byName[it.Name] = id
	}
	if _, seen := inst.cur[id]; !seen {
		inst.cur[id] = struct{}{}
		inst.support[id]++
	}
	return
}

// --- Batch / entity ---

func (inst *ItemExtractor) BeginBatch()     {}
func (inst *ItemExtractor) EndBatch() error { return nil }

func (inst *ItemExtractor) BeginEntity() {
	clear(inst.cur)
}

func (inst *ItemExtractor) EndEntity() error {
	ids := make([]int32, 0, len(inst.cur))
	for id := range inst.cur {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	inst.rows = append(inst.rows, ids)
	return nil
}

// --- Plain section ---

func (inst *ItemExtractor) BeginPlainSection(_ common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.inPlain = true
	inst.curSection = ""
	inst.curSecItem = -1
}
func (inst *ItemExtractor) EndPlainSection() error { inst.inPlain = false; return nil }
func (inst *ItemExtractor) BeginPlainValue()       {}
func (inst *ItemExtractor) EndPlainValue() error   { return nil }

// --- Tagged sections ---

func (inst *ItemExtractor) BeginTaggedSections()     {}
func (inst *ItemExtractor) EndTaggedSections() error { return nil }

func (inst *ItemExtractor) BeginCoSectionGroup(name naming.Key) {
	inst.add(Item{Name: "cogroup:" + name.String(), Kind: ItemKindCoGroup, Section: name.String()})
}
func (inst *ItemExtractor) EndCoSectionGroup() error { return nil }

func (inst *ItemExtractor) BeginSection(name naming.StylableName, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, _ int) {
	inst.inPlain = false
	inst.curSection = name.String()
	inst.curSecItem = inst.add(Item{Name: "section:" + inst.curSection, Kind: ItemKindSection, Section: inst.curSection})
}
func (inst *ItemExtractor) EndSection() error {
	inst.curSection, inst.curSecPhys, inst.curSecItem = "", "", -1
	return nil
}

func (inst *ItemExtractor) BeginTaggedValue()     {}
func (inst *ItemExtractor) EndTaggedValue() error { return nil }

// --- Column ---

func (inst *ItemExtractor) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.curColumn = colAddr.FullColumnName
	inst.curColName = name.String()
	inst.curQuoted = canonicalType == nil || !canonicalType.IsMachineNumericNode()
	if inst.curSecPhys == "" && !inst.inPlain {
		inst.curSecPhys = physicalSection(inst.curColumn)
	}
	if inst.curSecItem >= 0 && inst.items[inst.curSecItem].Column == "" {
		inst.items[inst.curSecItem].Column = inst.curColumn
		inst.items[inst.curSecItem].Handle = inst.curSection + ":" + inst.curColName
		inst.items[inst.curSecItem].PhysicalSection = inst.curSecPhys
	}
}

// physicalSection is the section component of a tagged section's physical
// column name, `tv:<section>:…`, or empty when the name is not that shape.
func physicalSection(fullColumnName string) string {
	parts := strings.SplitN(fullColumnName, ":", 3)
	if len(parts) < 3 || parts[0] != "tv" {
		return ""
	}
	return parts[1]
}
func (inst *ItemExtractor) EndColumn() {}

// --- Value shapes ---

func (inst *ItemExtractor) BeginScalarValue()             {}
func (inst *ItemExtractor) EndScalarValue() error         { return nil }
func (inst *ItemExtractor) BeginHomogenousArrayValue(int) {}
func (inst *ItemExtractor) EndHomogenousArrayValue()      {}
func (inst *ItemExtractor) BeginSetValue(int)             {}
func (inst *ItemExtractor) EndSetValue()                  {}
func (inst *ItemExtractor) BeginValueItem(int)            {}
func (inst *ItemExtractor) EndValueItem()                 {}

// --- Write ---

func (inst *ItemExtractor) Write(p []byte) (n int, err error) {
	inst.value(string(p))
	return len(p), nil
}

func (inst *ItemExtractor) WriteString(s string) (n int, err error) {
	inst.value(s)
	return len(s), nil
}

func (inst *ItemExtractor) value(s string) {
	if inst.inPlain || inst.curColumn == "" || !itemText(s) {
		return
	}
	inst.add(Item{
		Name: "value:" + inst.curSection + "." + inst.curColName + "=" + s,
		Kind: ItemKindTaggedValue, Column: inst.curColumn, Handle: inst.curSection + ":" + inst.curColName,
		Value: s, Quoted: inst.curQuoted, Section: inst.curSection, PhysicalSection: inst.curSecPhys,
	})
}

// itemText reports whether s can be an item's value: short enough, valid
// UTF-8, and without control characters.
func itemText(s string) bool {
	if len(s) > ItemMaxValueLen || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// --- Memberships ---

func (inst *ItemExtractor) BeginTags(int) {}
func (inst *ItemExtractor) EndTags()      {}

func (inst *ItemExtractor) AddMembershipRef(lowCard bool, ref uint64) {
	if !lowCard || inst.curSection == "" {
		return
	}
	inst.add(Item{
		Name: "tag:" + inst.curSection + "#" + strconv.FormatUint(ref, 10),
		Kind: ItemKindTagRef, Section: inst.curSection, PhysicalSection: inst.curSecPhys, Ref: ref,
		Handle: inst.curSection + ":lr",
	})
}

func (inst *ItemExtractor) AddMembershipVerbatim(lowCard bool, verbatim string) {
	if !lowCard || inst.curSection == "" || !itemText(verbatim) {
		return
	}
	inst.add(Item{
		Name: "tag:" + inst.curSection + "=" + verbatim,
		Kind: ItemKindTagVerbatim, Section: inst.curSection, PhysicalSection: inst.curSecPhys, Value: verbatim, Quoted: true,
		Handle: inst.curSection + ":lv",
	})
}

func (inst *ItemExtractor) AddMembershipRefParametrized(lowCard bool, ref uint64, _ string) {
	inst.AddMembershipRef(lowCard, ref)
}

func (inst *ItemExtractor) AddMembershipMixedLowCardRefHighCardParam(ref uint64, _ string) {
	inst.AddMembershipRef(true, ref)
}

func (inst *ItemExtractor) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, _ string) {
	inst.AddMembershipVerbatim(true, verbatim)
}

var (
	_ streamreadaccess.SinkI           = (*ItemExtractor)(nil)
	_ streamreadaccess.MembershipSinkI = (*ItemExtractor)(nil)
)
