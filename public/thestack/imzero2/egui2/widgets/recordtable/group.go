package recordtable

import (
	"slices"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Row grouping (ADR-0336) is a **view layer over the sort permutation**, as
// the sort is a view layer over the record: the record is untouched, a leaf
// keeps its record row as its identity, and the host's permutation — the
// header sort, or the query's own order — is the order of leaves *within*
// every group. What the layer adds is a second kind of line, the group, and
// a line sequence the grid draws instead of the permutation.
//
// A [GroupState] holds the spec (an ordered list of [GroupLevel]) and the
// expansion, keyed by a group's key path so it survives a new record, a sort
// and a page turn. [GroupState.LayoutFor] turns a record and a permutation
// into a [Layout]: the lines, the groups, and the aggregates of every
// [Column] whose Agg is set. It is pure — nothing here imports the bindings
// — and it is cached: the partition is redone only when the record, the
// permutation, the spec or the aggregate declarations change, and a change
// of expansion alone re-flattens.
//
// A level is one of two kinds. A column level yields one [Key] per row and
// one group per distinct key. A path level yields a row's segments and opens
// one nested group per proper prefix — the last segment is the leaf's own
// name — so its depth varies from row to row, a leaf may sit beside a
// subgroup under one node, and a prefix no row ends at is a group all the
// same. The segmenter is the host's; the layout never parses a path.

// Key is a group's identity and face. Two rows with equal Value are one
// group, whatever their Label; the first row's Label is the group's. A
// Value must not contain U+001E, which joins the segments of a path.
type Key struct {
	Value string
	// Label is what the group line shows; empty means Value.
	Label string
	// Null marks the one null group of a level: a null cell, or a row
	// whose segments are empty.
	Null bool
}

// pathSep joins the elements of a group's path. It is a control character
// no key value carries.
const pathSep = "\x1e"

// GroupOrder is how the sibling groups a level produces order among
// themselves. The zero value orders by key, ascending, the null group last.
type GroupOrder struct {
	// Desc reverses the order.
	Desc bool
	// Appearance orders groups by their first row in the incoming
	// permutation, so a sorted record groups in its sorted order.
	Appearance bool
	// ByAgg orders groups by the aggregate of Input.Columns[AggColumn],
	// whose Agg must be set; a group with no value sorts last.
	ByAgg     bool
	AggColumn int
	// NullsFirst puts the null group first rather than last; Desc does not
	// move it.
	NullsFirst bool
}

// GroupLevel is one level of the spec.
type GroupLevel struct {
	// Arrow is the record column a column level keys on. Ignored by a path
	// level, and by a column level with a Key of its own, which may name -1.
	Arrow int
	// Key, when set, derives a column level's key from the row — a bucket
	// (a date to its month), a label beside the value. Nil keys on the
	// column's value, compared as the column's type compares.
	Key func(row int64) Key
	// Segments, when set, makes this a path level: the row's segments, of
	// which every proper prefix is a group and the last is the leaf's name.
	// An empty result is the level's null group.
	Segments func(row int64) []Key
	// Order is how the level's sibling groups order.
	Order GroupOrder
	// KeyIsRow says the key is a record row number of the record the leaves
	// came from — the source row of an exploded record — so the group line
	// stands for a record: the renderer lets a click on it select
	// (ADR-0336 §SD6). The layout does not read it.
	KeyIsRow bool
}

// IsPath reports whether the level is a path level.
func (lvl GroupLevel) IsPath() bool { return lvl.Segments != nil }

// AggE is a built-in aggregate of a [Column] over a group's leaves.
type AggE uint8

const (
	AggNone AggE = iota
	// AggCount is the leaf count, for a column of any type.
	AggCount
	// AggSum, AggMin, AggMax and AggMean fold the non-null numeric values;
	// the mean is the merged sum over the merged count at every level.
	AggSum
	AggMin
	AggMax
	AggMean
)

// AllAggs lists the aggregates in declaration order.
var AllAggs = []AggE{AggNone, AggCount, AggSum, AggMin, AggMax, AggMean}

func (inst AggE) String() string {
	switch inst {
	case AggNone:
		return "none"
	case AggCount:
		return "count"
	case AggSum:
		return "sum"
	case AggMin:
		return "min"
	case AggMax:
		return "max"
	case AggMean:
		return "mean"
	}
	return "invalid"
}

// AggValue is a group's accumulator for one column: everything the
// built-ins need, so a parent merges its children rather than re-reading
// the leaves.
type AggValue struct {
	// Count is the leaf rows beneath the group.
	Count int64
	// Num is the numeric, non-null values among them; Sum, Min and Max are
	// over those and mean nothing while Num is zero.
	Num int64
	Sum float64
	Min float64
	Max float64
}

func (inst *AggValue) addLeaf(v float64, ok bool) {
	inst.Count++
	if !ok {
		return
	}
	if inst.Num == 0 || v < inst.Min {
		inst.Min = v
	}
	if inst.Num == 0 || v > inst.Max {
		inst.Max = v
	}
	inst.Num++
	inst.Sum += v
}

func (inst *AggValue) merge(o AggValue) {
	inst.Count += o.Count
	if o.Num == 0 {
		return
	}
	if inst.Num == 0 || o.Min < inst.Min {
		inst.Min = o.Min
	}
	if inst.Num == 0 || o.Max > inst.Max {
		inst.Max = o.Max
	}
	inst.Num += o.Num
	inst.Sum += o.Sum
}

// Mean is Sum over Num; ok is false while no numeric value was seen.
func (inst AggValue) Mean() (v float64, ok bool) {
	if inst.Num == 0 {
		return 0, false
	}
	return inst.Sum / float64(inst.Num), true
}

// Of is the number one aggregate reads from the accumulator; ok is false
// for AggNone and for a numeric aggregate over no numeric value.
func (inst AggValue) Of(agg AggE) (v float64, ok bool) {
	switch agg {
	case AggCount:
		return float64(inst.Count), true
	case AggSum:
		return inst.Sum, inst.Num > 0
	case AggMin:
		return inst.Min, inst.Num > 0
	case AggMax:
		return inst.Max, inst.Num > 0
	case AggMean:
		return inst.Mean()
	}
	return 0, false
}

// Line is one line of a grouped grid: a leaf, drawn as its record row, or a
// group line.
type Line struct {
	// Row is the record row of a leaf, -1 on a group line.
	Row int64
	// Group indexes Layout.Groups: the group itself on a group line, the
	// parent group of a leaf (0, the root, for a leaf under no group).
	Group int32
}

// IsGroup reports whether the line is a group line.
func (inst Line) IsGroup() bool { return inst.Row < 0 }

// Group is one node of the layout. Groups[0] is the root — every leaf's
// ancestor, never drawn, whose aggregates are the grand totals.
type Group struct {
	// Path identifies the group across records, sorts and page turns: the
	// level and key of every ancestor and its own, joined. Opaque.
	Path string
	// Level indexes the spec's levels; -1 on the root.
	Level int32
	// Depth is 0 for a root-level group, one more than its parent's
	// otherwise; -1 on the root. A path level adds a depth per prefix.
	Depth int32
	Key   Key
	// Parent indexes Groups; -1 on the root.
	Parent int32
	// Leaves is the record rows beneath the group, whatever its expansion.
	Leaves int64
	// Open is whether the group's children follow its line.
	Open bool
	// Line is the line the group is drawn on, -1 while an ancestor is
	// closed (and on the root).
	Line int64
	// rep is the first leaf row filed under the group, for typed key
	// comparison; seen is its ordinal in the incoming permutation.
	rep  int64
	seen int64
}

// KeyRow reads the key as a record row number, for a level with KeyIsRow.
func (inst Group) KeyRow() (row int64, ok bool) {
	if inst.Key.Null {
		return -1, false
	}
	row, err := strconv.ParseInt(inst.Key.Value, 10, 64)
	return row, err == nil
}

// item is one child of a node in its ordered sibling set: a subgroup or a
// direct leaf.
type item struct {
	group int32 // -1 for a leaf
	leaf  int32 // index into the leaf arrays
}

// Layout is what [GroupState.LayoutFor] produces: the state's own, valid
// until the next call. Lines and Groups are read by the renderer and the
// host; the rest answers the queries below.
type Layout struct {
	Lines  []Line
	Groups []Group
	// Revealed is the line of a row [GroupState.Reveal] asked for, -1
	// otherwise; the renderer scrolls to it once.
	Revealed int64

	// Per-node ordered children, as a CSR over items.
	items     []item
	itemStart []int32
	// Per-leaf arrays, in incoming order: the node, the row, the name a
	// path level gave it, the position in the incoming permutation.
	leafNode []int32
	leafRow  []int64
	leafName []string
	leafSeen []int64
	// fill is orderItems' scratch.
	fill []int32
	// rowLine maps a record row to its line, -1 while folded away or absent.
	rowLine []int64
	// aggregates: aggCols are the column indices with an Agg; aggs is
	// [group*len(aggCols)+k].
	aggCols []int
	aggs    []AggValue
	// byPath finds a group by its path while partitioning.
	byPath map[string]int32
}

// Len is the number of lines.
func (inst *Layout) Len() int64 { return int64(len(inst.Lines)) }

// PosOf is the line a record row is drawn on, -1 while it is folded away
// or the layout does not hold it.
func (inst *Layout) PosOf(row int64) (line int64) {
	if row < 0 || row >= int64(len(inst.rowLine)) {
		return -1
	}
	return inst.rowLine[row]
}

// PathOf is the keys of a group's ancestors and its own, root-level first:
// what a host prints to name the group, and reads back to find it.
func (inst *Layout) PathOf(group int32) (keys []Key) {
	for g := group; g > 0 && int(g) < len(inst.Groups); g = inst.Groups[g].Parent {
		keys = append(keys, inst.Groups[g].Key)
	}
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return
}

// GroupAt is the group a line draws, -1 on a leaf line.
func (inst *Layout) GroupAt(line int64) (group int32) {
	if line < 0 || line >= int64(len(inst.Lines)) || !inst.Lines[line].IsGroup() {
		return -1
	}
	return inst.Lines[line].Group
}

// Aggregate is a group's accumulator for Input.Columns[col]; ok is false
// when the column declares no Agg.
func (inst *Layout) Aggregate(group int32, col int) (v AggValue, ok bool) {
	k := inst.aggSlot(col)
	if k < 0 || group < 0 || int(group) >= len(inst.Groups) {
		return
	}
	return inst.aggs[int(group)*len(inst.aggCols)+k], true
}

// Total is the root's accumulator for Input.Columns[col]: the grand total
// over every leaf the layout holds.
func (inst *Layout) Total(col int) (v AggValue, ok bool) {
	return inst.Aggregate(0, col)
}

func (inst *Layout) aggSlot(col int) int {
	for k, c := range inst.aggCols {
		if c == col {
			return k
		}
	}
	return -1
}

// GroupState is a grid's grouping: the spec and the expansion, kept by the
// host across frames at one address. The zero value groups nothing.
//
// Expansion is three-valued, as tree.State's is (ADR-0176): the map holds
// only the groups whose state differs from the default, so "the reader
// closed this" and "this group is new" are told apart when the record
// changes, and a change of default still moves every untouched group.
type GroupState struct {
	levels []GroupLevel
	// collapsedFromP1 is one more than the depth from which groups start
	// closed, so the zero value — every group open — is the default.
	collapsedFromP1 int32
	expanded        map[string]bool
	// specGen moves on a change of levels or orders; expGen on a change of
	// expansion. Both join the cells key through Gen.
	specGen uint32
	expGen  uint32
	// revealP1 is a pending Reveal, the row plus one so zero means none.
	revealP1 int64

	layout Layout
	// The partition's inputs, to skip it while they hold.
	forRec     arrow.RecordBatch
	forOrder   *int64
	forOrderN  int
	forN       int64
	forSpecGen uint32
	forAggs    []AggE
	forExpGen  uint32
	partValid  bool
}

// Active reports whether the state groups: at least one level is set.
func (inst *GroupState) Active() bool { return len(inst.levels) > 0 }

// Levels is the spec, borrowed: set it through SetLevels.
func (inst *GroupState) Levels() []GroupLevel { return inst.levels }

// SetLevels replaces the spec. Expansion entries keep their paths, so a
// spec that keeps a level's position keeps its groups' states.
func (inst *GroupState) SetLevels(levels []GroupLevel) {
	inst.levels = append(inst.levels[:0], levels...)
	inst.specGen++
}

// SetOrder replaces one level's order; a level out of range is ignored.
func (inst *GroupState) SetOrder(level int, o GroupOrder) {
	if level < 0 || level >= len(inst.levels) {
		return
	}
	inst.levels[level].Order = o
	inst.specGen++
}

// ToggleDesc reverses one level's order — the header gesture on a grouped
// column (ADR-0336 §SD7).
func (inst *GroupState) ToggleDesc(level int) {
	if level < 0 || level >= len(inst.levels) {
		return
	}
	inst.levels[level].Order.Desc = !inst.levels[level].Order.Desc
	inst.specGen++
}

// Layout is the last layout computed, borrowed: what a Result's
// GroupToggled indexes.
func (inst *GroupState) Layout() *Layout { return &inst.layout }

// Apply applies a grid frame's group gestures: a band or chevron click
// folds or unfolds its group, a grouped column's header click reverses its
// level. A host that routes the gestures elsewhere first reads the Result
// instead.
func (inst *GroupState) Apply(res Result) {
	if g := res.GroupToggled; g >= 0 && int(g) < len(inst.layout.Groups) {
		inst.Toggle(inst.layout.Groups[g])
	}
	if res.GroupOrderClicked >= 0 {
		inst.ToggleDesc(res.GroupOrderClicked)
	}
}

// Glyph is the header suffix marking a grouped column's level direction.
func (inst *GroupState) Glyph(level int) (s string) {
	if level < 0 || level >= len(inst.levels) {
		return
	}
	if inst.levels[level].Order.Desc {
		return " ▼"
	}
	return " ▲"
}

// Gen moves on every change a layout depends on besides its record and
// permutation; a cells key folds it in.
func (inst *GroupState) Gen() uint64 {
	return uint64(inst.specGen)<<32 | uint64(inst.expGen)
}

// defaultOpen is how a group at depth is drawn without an entry.
func (inst *GroupState) defaultOpen(depth int32) bool {
	return inst.collapsedFromP1 == 0 || depth < inst.collapsedFromP1-1
}

// IsOpen is whether g's children follow its line.
func (inst *GroupState) IsOpen(g Group) bool {
	if v, ok := inst.expanded[g.Path]; ok {
		return v
	}
	return inst.defaultOpen(g.Depth)
}

// SetOpen opens or closes g. An entry that agrees with the default is
// dropped, so the map stays bounded by what the reader changed.
func (inst *GroupState) SetOpen(g Group, open bool) {
	if inst.IsOpen(g) == open {
		return
	}
	if open == inst.defaultOpen(g.Depth) {
		delete(inst.expanded, g.Path)
	} else {
		if inst.expanded == nil {
			inst.expanded = make(map[string]bool, 16)
		}
		inst.expanded[g.Path] = open
	}
	inst.expGen++
}

// Toggle flips g and reports its new state.
func (inst *GroupState) Toggle(g Group) (open bool) {
	open = !inst.IsOpen(g)
	inst.SetOpen(g, open)
	return
}

// ExpandAll opens every group and forgets every entry.
func (inst *GroupState) ExpandAll() { inst.setCollapsedFrom(-1) }

// CollapseAll closes every group and forgets every entry.
func (inst *GroupState) CollapseAll() { inst.setCollapsedFrom(0) }

// CollapseBelow leaves groups at depth and above open, closes the rest, and
// forgets every entry — Excel's outline-level buttons.
func (inst *GroupState) CollapseBelow(depth int32) { inst.setCollapsedFrom(max(depth, 0) + 1) }

// ResetExpansion forgets every entry and keeps the default.
func (inst *GroupState) ResetExpansion() {
	if len(inst.expanded) == 0 {
		return
	}
	clear(inst.expanded)
	inst.expGen++
}

// setCollapsedFrom closes groups at depth d and deeper, -1 for none.
func (inst *GroupState) setCollapsedFrom(d int32) {
	inst.collapsedFromP1 = d + 1
	clear(inst.expanded)
	inst.expGen++
}

// Reveal asks the next LayoutFor to open the ancestors of row and report
// its line as Layout.Revealed — a selection made in another pane.
func (inst *GroupState) Reveal(row int64) {
	if row < 0 {
		return
	}
	inst.revealP1 = row + 1
	inst.expGen++
}

// LayoutFor is the lines of the first n rows of order over rec — nil order
// for record order — under the state's spec and expansion, with the
// aggregates cols declare. The result is the state's own and valid until
// the next call. A changed permutation must be a different slice or n: the
// partition is skipped while rec, order's identity, n, the spec and the
// aggregate declarations hold, and expansion alone re-flattens.
//
// With no levels the lines are the leaves in order, under the root alone.
func (inst *GroupState) LayoutFor(rec arrow.RecordBatch, order []int64, n int64, cols []Column) (lay *Layout, err error) {
	lay = &inst.layout
	if rec == nil {
		n = 0
	} else {
		n = min(max(n, 0), rec.NumRows())
		if order != nil {
			n = min(n, int64(len(order)))
		}
	}
	var orderP *int64
	if len(order) > 0 {
		orderP = &order[0]
	}
	aggsSame := len(inst.forAggs) == len(cols)
	for i := range cols {
		if !aggsSame {
			break
		}
		aggsSame = inst.forAggs[i] == cols[i].Agg
	}
	same := inst.partValid && inst.forRec == rec && inst.forOrder == orderP && inst.forOrderN == len(order) &&
		inst.forN == n && inst.forSpecGen == inst.specGen && aggsSame
	if !same {
		err = inst.partition(rec, order, n, cols)
		inst.forRec, inst.forOrder, inst.forOrderN, inst.forN, inst.forSpecGen = rec, orderP, len(order), n, inst.specGen
		inst.forAggs = inst.forAggs[:0]
		for i := range cols {
			inst.forAggs = append(inst.forAggs, cols[i].Agg)
		}
		inst.partValid = err == nil
		if err != nil {
			lay.Lines, lay.Groups = lay.Lines[:0], lay.Groups[:0]
			lay.Revealed = -1
			return
		}
		inst.forExpGen = inst.expGen - 1 // force the flatten
	}
	if inst.forExpGen != inst.expGen || inst.revealP1 != 0 {
		inst.flatten(rec)
		inst.forExpGen = inst.expGen
	} else {
		lay.Revealed = -1 // a reveal is served by the flatten that opened it, once
	}
	return
}

func (inst *GroupState) validate(rec arrow.RecordBatch, cols []Column) (err error) {
	ncols := -1 // no record: nothing to read, so no column to miss
	if rec != nil {
		ncols = int(rec.NumCols())
	}
	for i := range inst.levels {
		lvl := &inst.levels[i]
		switch {
		case ncols >= 0 && !lvl.IsPath() && lvl.Key == nil && (lvl.Arrow < 0 || lvl.Arrow >= ncols):
			return eb.Build().Int("level", i).Int("arrow", lvl.Arrow).Int("columns", ncols).
				Errorf("group level keys on a column the record does not have")
		case lvl.Order.ByAgg && (lvl.Order.AggColumn < 0 || lvl.Order.AggColumn >= len(cols)):
			return eb.Build().Int("level", i).Int("aggColumn", lvl.Order.AggColumn).Int("columns", len(cols)).
				Errorf("group level orders by an aggregate of a column the grid does not have")
		case lvl.Order.ByAgg && cols[lvl.Order.AggColumn].Agg == AggNone:
			return eb.Build().Int("level", i).Int("aggColumn", lvl.Order.AggColumn).
				Errorf("group level orders by an aggregate of a column that declares none")
		}
	}
	return
}

// partition files every row under its groups, accumulates the aggregates,
// and orders each node's children. It does not read the expansion.
func (inst *GroupState) partition(rec arrow.RecordBatch, order []int64, n int64, cols []Column) (err error) {
	lay := &inst.layout
	if err = inst.validate(rec, cols); err != nil {
		return
	}
	lay.Groups = append(lay.Groups[:0], Group{Level: -1, Depth: -1, Parent: -1, Line: -1, rep: -1, seen: -1})
	lay.leafNode, lay.leafRow, lay.leafName, lay.leafSeen = lay.leafNode[:0], lay.leafRow[:0], lay.leafName[:0], lay.leafSeen[:0]
	if lay.byPath == nil {
		lay.byPath = make(map[string]int32, 64)
	} else {
		clear(lay.byPath)
	}
	lay.aggCols = lay.aggCols[:0]
	for i := range cols {
		if cols[i].Agg != AggNone {
			lay.aggCols = append(lay.aggCols, i)
		}
	}
	nAgg := len(lay.aggCols)
	lay.aggs = growZero(lay.aggs, nAgg)

	var pathBuf strings.Builder
	for pos := range n {
		row := pos
		if order != nil {
			row = order[pos]
		}
		if row < 0 || row >= rec.NumRows() {
			continue
		}
		node := int32(0)
		name := ""
		for li := range inst.levels {
			lvl := &inst.levels[li]
			if !lvl.IsPath() {
				node = inst.child(node, int32(li), inst.keyOf(rec, lvl, row), row, pos, &pathBuf)
				continue
			}
			segs := lvl.Segments(row)
			if len(segs) == 0 {
				node = inst.child(node, int32(li), Key{Null: true}, row, pos, &pathBuf)
				continue
			}
			for _, seg := range segs[:len(segs)-1] {
				node = inst.child(node, int32(li), seg, row, pos, &pathBuf)
			}
			name = segs[len(segs)-1].Value
			if segs[len(segs)-1].Label != "" {
				name = segs[len(segs)-1].Label
			}
		}
		lay.leafNode = append(lay.leafNode, node)
		lay.leafRow = append(lay.leafRow, row)
		lay.leafName = append(lay.leafName, name)
		lay.leafSeen = append(lay.leafSeen, pos)
		// Leaves count up the ancestry; the accumulators only at the node,
		// merged upward below.
		for g := node; g >= 0; g = lay.Groups[g].Parent {
			lay.Groups[g].Leaves++
		}
		if nAgg > 0 {
			base := int(node) * nAgg
			for k, col := range lay.aggCols {
				var v float64
				ok := false
				if a := cols[col].Arrow; a >= 0 && cols[col].Agg != AggCount {
					v, ok = chrows.Float64(rec.Column(a), int(row))
				}
				lay.aggs[base+k].addLeaf(v, ok)
			}
		}
	}
	// Children are created after their parents, so a reverse walk merges
	// every node into its parent before the parent is merged.
	if nAgg > 0 {
		for g := len(lay.Groups) - 1; g > 0; g-- {
			p := int(lay.Groups[g].Parent)
			for k := range nAgg {
				lay.aggs[p*nAgg+k].merge(lay.aggs[g*nAgg+k])
			}
		}
	}
	inst.orderItems(rec, cols)
	return
}

// keyOf is a column level's key for row.
func (inst *GroupState) keyOf(rec arrow.RecordBatch, lvl *GroupLevel, row int64) (k Key) {
	if lvl.Key != nil {
		return lvl.Key(row)
	}
	arr := rec.Column(lvl.Arrow)
	if arr.IsNull(int(row)) {
		return Key{Null: true}
	}
	return Key{Value: arr.ValueStr(int(row))}
}

// child is the group for key under parent at level, created on first use.
// A group's path is its parent's and its own element; the slot grows the
// aggregates by one node.
func (inst *GroupState) child(parent int32, level int32, k Key, row int64, pos int64, buf *strings.Builder) (g int32) {
	lay := &inst.layout
	buf.Reset()
	buf.WriteString(lay.Groups[parent].Path)
	buf.WriteString(pathSep)
	buf.WriteString(strconv.FormatInt(int64(level), 10))
	if k.Null {
		buf.WriteString(":~")
	} else {
		buf.WriteString(":=")
		buf.WriteString(k.Value)
	}
	path := buf.String()
	if g, ok := lay.byPath[path]; ok {
		return g
	}
	g = int32(len(lay.Groups))
	lay.Groups = append(lay.Groups, Group{
		Path: path, Level: level, Depth: lay.Groups[parent].Depth + 1, Key: k,
		Parent: parent, Line: -1, rep: row, seen: pos,
	})
	lay.byPath[path] = g
	for range lay.aggCols {
		lay.aggs = append(lay.aggs, AggValue{})
	}
	return
}

// growZero returns s at length n, its backing array reused when it holds n,
// every element zero.
func growZero[T any](s []T, n int) []T {
	if cap(s) >= n {
		s = s[:n]
		clear(s)
		return s
	}
	return make([]T, n)
}

// orderItems builds every node's ordered sibling set: its subgroups and
// its direct leaves, as a CSR over items, sorted by [GroupState.less].
func (inst *GroupState) orderItems(rec arrow.RecordBatch, cols []Column) {
	lay := &inst.layout
	ng := len(lay.Groups)
	lay.itemStart = growZero(lay.itemStart, ng+1)
	for g := 1; g < ng; g++ {
		lay.itemStart[lay.Groups[g].Parent+1]++
	}
	for _, node := range lay.leafNode {
		lay.itemStart[node+1]++
	}
	for g := range ng {
		lay.itemStart[g+1] += lay.itemStart[g]
	}
	lay.items = growZero(lay.items, int(lay.itemStart[ng]))
	lay.fill = growZero(lay.fill, ng)
	fill := lay.fill
	copy(fill, lay.itemStart[:ng])
	for g := 1; g < ng; g++ {
		p := lay.Groups[g].Parent
		lay.items[fill[p]] = item{group: int32(g), leaf: -1}
		fill[p]++
	}
	for li, node := range lay.leafNode {
		lay.items[fill[node]] = item{group: -1, leaf: int32(li)}
		fill[node]++
	}
	for g := range ng {
		set := lay.items[lay.itemStart[g]:lay.itemStart[g+1]]
		if len(set) < 2 {
			continue
		}
		slices.SortStableFunc(set, func(a, b item) int { return inst.cmpItems(rec, cols, a, b) })
	}
}

// cmpItems orders two siblings. A subgroup of a lower level comes first;
// two groups of one level order by its GroupOrder with the null group at
// its edge; a leaf against a group, or two leaves, order by the last level's
// GroupOrder — by name under a path level, else by arrival, which is the
// host's permutation. Ties fall back to arrival, so the sort is a
// refinement of the permutation.
func (inst *GroupState) cmpItems(rec arrow.RecordBatch, cols []Column, a, b item) int {
	lay := &inst.layout
	if len(inst.levels) == 0 {
		// No spec: the root's leaves in the host's order.
		return cmpInt(inst.seenOf(a), inst.seenOf(b))
	}
	last := int32(len(inst.levels) - 1)
	la, lb := last, last
	if a.group >= 0 {
		la = lay.Groups[a.group].Level
	}
	if b.group >= 0 {
		lb = lay.Groups[b.group].Level
	}
	if la != lb {
		return cmpInt(int64(la), int64(lb))
	}
	lvl := &inst.levels[la]
	o := lvl.Order
	ga, gb := a.group >= 0, b.group >= 0
	// The null group sits at its edge against anything, group or leaf, so
	// the order is consistent whatever the sibling set holds.
	nullA, nullB := ga && lay.Groups[a.group].Key.Null, gb && lay.Groups[b.group].Key.Null
	if nullA != nullB {
		if nullA == o.NullsFirst {
			return -1
		}
		return 1
	}
	if !ga && !gb && !lvl.IsPath() {
		// Two leaves under a column level keep the host's order, whatever
		// the level's order says: a sort refines the permutation.
		return cmpInt(inst.seenOf(a), inst.seenOf(b))
	}
	c := 0
	switch {
	case o.Appearance:
		c = cmpInt(inst.seenOf(a), inst.seenOf(b))
	case o.ByAgg:
		if ga != gb {
			// A leaf carries no aggregate: groups first, in either direction.
			if ga {
				return -1
			}
			return 1
		}
		if !ga {
			c = cmpInt(inst.seenOf(a), inst.seenOf(b))
			break
		}
		agg := cols[o.AggColumn].Agg
		k := lay.aggSlot(o.AggColumn)
		va, oka := lay.aggs[int(a.group)*len(lay.aggCols)+k].Of(agg)
		vb, okb := lay.aggs[int(b.group)*len(lay.aggCols)+k].Of(agg)
		switch {
		case oka != okb:
			// A group with no value sorts last, in either direction.
			if oka {
				return -1
			}
			return 1
		case va < vb:
			c = -1
		case va > vb:
			c = 1
		}
	default:
		switch {
		case lvl.IsPath():
			c = strings.Compare(inst.nameOf(a), inst.nameOf(b))
		case ga && gb && lvl.Key == nil:
			c = compareCells(rec.Column(lvl.Arrow), lay.Groups[a.group].rep, lay.Groups[b.group].rep)
		default:
			// Both are groups: a column level with a Key of its own.
			c = strings.Compare(lay.Groups[a.group].Key.Value, lay.Groups[b.group].Key.Value)
		}
	}
	if c == 0 {
		return cmpInt(inst.seenOf(a), inst.seenOf(b))
	}
	if o.Desc {
		return -c
	}
	return c
}

func (inst *GroupState) seenOf(it item) int64 {
	if it.group >= 0 {
		return inst.layout.Groups[it.group].seen
	}
	return inst.layout.leafSeen[it.leaf]
}

func (inst *GroupState) nameOf(it item) string {
	if it.group >= 0 {
		return inst.layout.Groups[it.group].Key.Value
	}
	return inst.layout.leafName[it.leaf]
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// flatten walks the ordered items from the root and emits the lines under
// the current expansion, serving a pending Reveal first.
func (inst *GroupState) flatten(rec arrow.RecordBatch) {
	lay := &inst.layout
	lay.Revealed = -1
	revealLeaf := int32(-1)
	if inst.revealP1 != 0 {
		want := inst.revealP1 - 1
		inst.revealP1 = 0
		for li, row := range lay.leafRow {
			if row == want {
				revealLeaf = int32(li)
				break
			}
		}
		if revealLeaf >= 0 {
			for g := lay.leafNode[revealLeaf]; g > 0; g = lay.Groups[g].Parent {
				inst.SetOpen(lay.Groups[g], true)
			}
		}
	}
	nrows := 0
	if rec != nil {
		nrows = int(rec.NumRows())
	}
	lay.rowLine = growZero(lay.rowLine, nrows)
	for i := range lay.rowLine {
		lay.rowLine[i] = -1
	}
	lay.Lines = lay.Lines[:0]
	for g := range lay.Groups {
		lay.Groups[g].Line = -1
		lay.Groups[g].Open = inst.IsOpen(lay.Groups[g])
	}
	inst.emit(0)
	if revealLeaf >= 0 {
		lay.Revealed = lay.rowLine[lay.leafRow[revealLeaf]]
	}
}

func (inst *GroupState) emit(node int32) {
	lay := &inst.layout
	for _, it := range lay.items[lay.itemStart[node]:lay.itemStart[node+1]] {
		if it.group < 0 {
			row := lay.leafRow[it.leaf]
			lay.rowLine[row] = int64(len(lay.Lines))
			lay.Lines = append(lay.Lines, Line{Row: row, Group: node})
			continue
		}
		g := &lay.Groups[it.group]
		g.Line = int64(len(lay.Lines))
		lay.Lines = append(lay.Lines, Line{Row: -1, Group: it.group})
		if g.Open {
			inst.emit(it.group)
		}
	}
}
