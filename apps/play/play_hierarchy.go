package play

import (
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// play_hierarchy.go is the column contract every hierarchy panel resolves
// against, and the builder that turns an accepted result into a flat tree
// (ADR-0166 §SD1). The Icicle tab (ADR-0160 §SD9) defined it and was its only
// reader; the Treemap tab is the second, and one contract read by two panels is
// the point — a query that draws as an icicle draws as a treemap, with the same
// columns and no restructuring.
//
// TWO CONTRACTS, discriminated by the columns present, because the hierarchies
// that reach a SQL result arrive in two shapes and neither converts to the other
// in a line of SQL:
//
//   - FOLDED — `stack` (an Array) + `value`: one row per root-to-leaf path, the
//     path carried as an array. The rows are interned into a trie and the
//     interior nodes synthesised. This is what a pprof capture already is, and
//     what any delimited path reaches with one splitByChar.
//   - NODES — `id` + `parent` + `value`: one row per node. What a recursive CTE
//     or a self-join emits, and the only one of the two in which an INTERIOR
//     node can carry a value of its own.
//
// A folded `stack` wins when a schema satisfies both, being the more specific
// claim.
//
// The optional `color` column is the second data channel (§SD2). It is read as
// a quantity or as a category depending on its Arrow type, and a type that is
// neither is IGNORED rather than rejected: colour is an enrichment, and a panel
// that refused to draw over it would trade a picture for a pedantry.

const (
	// The folded contract. `stack` must be list-typed; the elements are
	// stringified, so an Array(UInt64) of ids is as good a path as an
	// Array(String) of names.
	hierStackCol = "stack"

	// The node contract. `parent` is the discriminator and `id` is what it
	// refers to; an empty (or NULL) parent marks a root.
	hierIDCol     = "id"
	hierParentCol = "parent"
	// label overrides the drawn text in node mode. Folded mode has no use for
	// it — a node's label IS its path element.
	hierLabelCol = "label"

	// Shared by both contracts. value is required and is the node's OWN
	// quantity, excluding its children; unit is optional and only labels the
	// quantity. color is optional and drives the second channel (§SD2).
	hierValueCol = "value"
	hierUnitCol  = "unit"
	hierColorCol = "color"

	// hierMaxNodes bounds the tree. This is a cost limit, not a readability
	// one: what each panel actually draws is bounded by its own culling (the
	// icicle's visible range) or nesting depth (the treemap's frontier), so a
	// big tree stays cheap to DRAW, but interning and laying it out are paid
	// per node on every rebuild.
	hierMaxNodes = 20000
	// hierMaxDepth caps a single path. Deeper than this and no space-filling
	// form has pixels left to say anything with; reaching it means a path
	// column that is not really a hierarchy.
	hierMaxDepth = 256
)

// hierModeE is which contract a schema satisfied.
type hierModeE uint8

const (
	hierModeNone hierModeE = iota
	hierModeFolded
	hierModeNodes
)

func (m hierModeE) String() string {
	switch m {
	case hierModeFolded:
		return "folded"
	case hierModeNodes:
		return "nodes"
	}
	return "none"
}

// hierColorKindE is what the optional `color` column turned out to carry.
type hierColorKindE uint8

const (
	hierColorNone hierColorKindE = iota
	// hierColorNumeric — a quantity, driving a continuous colormap.
	hierColorNumeric
	// hierColorCategorical — a name, driving the qualitative cycle.
	hierColorCategorical
)

// hierForm is the panel-specific vocabulary a reject message is written in, so
// one resolver can speak as the pane the reader is looking at.
type hierForm struct {
	// noun names the view: "flame view", "treemap".
	noun string
	// elem names one thing in it: "frame", "cell".
	elem string
}

// hierClaim is the resolved contract a schema yielded: the mode plus the column
// indices the builder consumes. -1 marks an absent optional column.
type hierClaim struct {
	mode                       hierModeE
	stackCol                   int
	idCol, parentCol, labelCol int
	valueCol, unitCol          int
	colorCol                   int
	colorKind                  hierColorKindE
}

// hierTree is the flat, columnar hierarchy both panels consume: one entry per
// node, parents by index, -1 marking a root. It is icicle.Tree's shape plus the
// colour channel, and converts to it by slice header.
//
// Self is a node's OWN quantity, excluding its children — the reading both
// forms need, and the one that lets an interior node carry a value.
type hierTree struct {
	Labels  []string
	Parents []int32
	Self    []float64
	// ColorNum and ColorKey carry the `color` cell of the row a node's value
	// came from, in whichever form the column resolved to. Each is either empty
	// (no usable column) or exactly Len() long. NaN and "" mark a node with no
	// colour of its own — every synthesised interior node in folded mode, since
	// no row described it.
	ColorNum  []float64
	ColorKey  []string
	ColorKind hierColorKindE
}

// Len returns the node count.
func (t hierTree) Len() int { return len(t.Labels) }

// hierStats is what one build noticed: the tree it produced and everything it
// had to drop, demote or truncate to get there. Reported in a panel's status
// line — a picture scaled against a total must say when rows did not reach it.
type hierStats struct {
	mode hierModeE
	// nodes is how many tree nodes the build produced, before any layout-time
	// pruning a panel applies afterwards.
	nodes int
	// droppedValue counts rows whose value was missing, unreadable, negative or
	// not finite. droppedPath counts rows carrying no usable path (folded) or no
	// id (nodes).
	droppedValue int
	droppedPath  int
	// droppedDup counts node-mode rows repeating an id already taken; the first
	// row wins and the rest are a real loss, unlike two folded rows sharing a
	// path, whose values simply sum.
	droppedDup int
	// reparented counts node-mode rows whose `parent` names no row in the
	// result, or names themselves. They are laid out as ROOTS rather than
	// dropped: a forest is a shape both widgets draw, so a subtree whose own
	// root a WHERE clause filtered away still shows the value it carries.
	reparented int
	// truncated counts folded paths cut at hierMaxDepth.
	truncated int
	// capped reports the node cap. Past it a folded path's value is attributed
	// to the deepest ancestor already interned rather than dropped, so the
	// picture understates DEPTH instead of quantity; a node-mode result stops
	// reading rows, which does drop value, and says so.
	capped bool
	// droppedCapped counts the one case the cap DOES cost quantity: a folded row
	// whose very first element could not be interned, so there is no ancestor to
	// attribute it to. Counted apart from droppedPath, which is about the row
	// rather than about the cap.
	droppedCapped int
	// colorConflicts counts nodes two rows described with DIFFERENT colours —
	// only reachable in folded mode, where rows sharing a path sum into one
	// node. The first answer wins, as it does for unit; the count is what keeps
	// that from being silent.
	colorConflicts int
	// unit is the first non-empty `unit` cell, labelling the quantity.
	unit string
}

// hierNodeKey identifies a trie node by its parent and its own label — the
// interning key that turns one row per path into one node per distinct prefix.
type hierNodeKey struct {
	parent int32
	label  string
}

// hierIsPathColumn reports whether a column can carry a path: any list of
// anything, since the elements go through formatArrayElem. The three list
// families are the ones play's formatter already cases.
func hierIsPathColumn(dt arrow.DataType) bool {
	switch dt.(type) {
	case *arrow.ListType, *arrow.LargeListType, *arrow.FixedSizeListType:
		return true
	}
	return false
}

// hierColorKindOf classifies a `color` column by its Arrow type. Anything that
// is neither a quantity nor a name returns hierColorNone and the column is
// ignored — see the file comment on why an unusable colour is not a rejection.
//
// A dictionary column is classified by its VALUE type, which is how ClickHouse
// LowCardinality(String) arrives and is the most natural spelling of a category
// column there is.
func hierColorKindOf(dt arrow.DataType) hierColorKindE {
	switch t := dt.(type) {
	case *arrow.DictionaryType:
		return hierColorKindOf(t.ValueType)
	case *arrow.Int8Type, *arrow.Int16Type, *arrow.Int32Type, *arrow.Int64Type,
		*arrow.Uint8Type, *arrow.Uint16Type, *arrow.Uint32Type, *arrow.Uint64Type,
		*arrow.Float16Type, *arrow.Float32Type, *arrow.Float64Type,
		*arrow.Decimal128Type, *arrow.Decimal256Type:
		return hierColorNumeric
	case *arrow.StringType, *arrow.LargeStringType, *arrow.StringViewType:
		return hierColorCategorical
	}
	return hierColorNone
}

// hierStackAt appends the non-null elements of the list cell at row to dst.
// EMPTY elements are skipped: splitByChar('/', '/usr/bin') yields an empty
// leading element, and an unnamed node is not a node. Skipping conserves the
// total — the value still lands on the deepest element that survived — where
// drawing an unlabelled rectangle would read as a rendering fault.
//
// ok is false for a null cell or a column that is not list-typed.
func hierStackAt(arr arrow.Array, row int, dst []string) (out []string, ok bool) {
	out = dst
	if arr == nil || row < 0 || row >= arr.Len() || arr.IsNull(row) {
		return out, false
	}
	var inner arrow.Array
	var beg, end int64
	switch a := arr.(type) {
	case *array.List:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	case *array.LargeList:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	case *array.FixedSizeList:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	default:
		return out, false
	}
	for i := beg; i < end; i++ {
		if inner.IsNull(int(i)) {
			continue
		}
		if s := formatArrayElem(inner, i); s != "" {
			out = append(out, s)
		}
	}
	return out, true
}

// resolveHierarchy applies both contracts to a schema and reports which one it
// satisfied. Pure and schema-only, so it can run every frame.
//
// The precedence is deliberate. A list-typed `stack` takes folded mode; failing
// that, `id`+`parent` take node mode — which is also the fallback for a column
// NAMED `stack` that is not a list, so a result that carries both a scalar
// `stack` label and a real parent column still draws. Only when neither shape is
// available does a mistyped `stack` become the message, because "wrap it in an
// array" is a more useful thing to say than "add a stack column" to a query that
// visibly has one.
func resolveHierarchy(schema *arrow.Schema, form hierForm) (cl hierClaim, reason string) {
	cl = hierClaim{stackCol: -1, idCol: -1, parentCol: -1, labelCol: -1, valueCol: -1, unitCol: -1, colorCol: -1}
	stackIsPath := false
	for ci, f := range schema.Fields() {
		switch f.Name {
		case hierStackCol:
			cl.stackCol = ci
			stackIsPath = hierIsPathColumn(f.Type)
		case hierIDCol:
			cl.idCol = ci
		case hierParentCol:
			cl.parentCol = ci
		case hierLabelCol:
			cl.labelCol = ci
		case hierValueCol:
			cl.valueCol = ci
		case hierUnitCol:
			cl.unitCol = ci
		case hierColorCol:
			if k := hierColorKindOf(f.Type); k != hierColorNone {
				cl.colorCol, cl.colorKind = ci, k
			}
		}
	}
	switch {
	case cl.stackCol >= 0 && stackIsPath:
		cl.mode = hierModeFolded
	case cl.idCol >= 0 && cl.parentCol >= 0:
		cl.mode = hierModeNodes
	case cl.stackCol >= 0:
		reason = "The " + form.noun + "'s `stack` column must be an array — the path from the root, " +
			"outermost first. Wrap it, e.g. `splitByChar('/', path) AS stack`."
		return
	default:
		reason = "Run a query with a hierarchy to see a " + form.noun + ": either a `stack` array plus a `value` " +
			"— e.g. WITH s AS (SELECT splitByChar('/', path) AS stack, sum(bytes) AS value FROM t GROUP BY 1) " +
			"SELECT * FROM s — or one row per node carrying `id`, `parent` and `value`."
		return
	}
	if cl.valueCol < 0 {
		cl.mode = hierModeNone
		reason = "The " + form.noun + " needs a `value` column carrying each " + form.elem + "'s own quantity — " +
			"add one, e.g. `sum(bytes) AS value`."
	}
	return
}

// buildHierarchy maps the result to the flat tree, per the mode the claim
// resolved.
func buildHierarchy(rec arrow.RecordBatch, cl hierClaim) (t hierTree, st hierStats) {
	switch cl.mode {
	case hierModeFolded:
		t, st = buildHierarchyFolded(rec, cl)
	case hierModeNodes:
		t, st = buildHierarchyNodes(rec, cl)
	default:
		return
	}
	st.unit = hierUnitOf(rec, cl)
	return
}

// hierUnitOf reads the first non-empty `unit` cell. One unit labels a whole
// picture, so a result disagreeing with itself row to row is read by its first
// answer rather than rejected — the column is a label, not a quantity.
func hierUnitOf(rec arrow.RecordBatch, cl hierClaim) string {
	if rec == nil || cl.unitCol < 0 {
		return ""
	}
	for row := range rec.NumRows() {
		if u := formatCell(rec, cl.unitCol, row); u != "" {
			return truncateRunes(u, 24)
		}
	}
	return ""
}

// hierColorAt reads one row's colour cell in whichever form the column
// resolved to. Absent, unreadable or non-finite reads as "no colour", which the
// panels render as their neutral fill rather than as an arbitrary one.
func hierColorAt(rec arrow.RecordBatch, cl hierClaim, row int64) (num float64, key string) {
	num = math.NaN()
	if cl.colorCol < 0 {
		return
	}
	switch cl.colorKind {
	case hierColorNumeric:
		if v, ok := quantityCellValue(rec, cl.colorCol, row); ok && !math.IsInf(v, 0) && !math.IsNaN(v) {
			num = v
		}
	case hierColorCategorical:
		key = truncateRunes(formatCell(rec, cl.colorCol, row), 64)
	}
	return
}

// hierInitColors sizes the colour slices for a tree of n nodes, when there is a
// colour to carry. Called once the node count is known, so the build's hot loop
// appends labels and parents without also growing two slices it may not need.
func (t *hierTree) initColors(kind hierColorKindE) {
	t.ColorKind = kind
	switch kind {
	case hierColorNumeric:
		t.ColorNum = make([]float64, t.Len())
		for i := range t.ColorNum {
			t.ColorNum[i] = math.NaN()
		}
	case hierColorCategorical:
		t.ColorKey = make([]string, t.Len())
	}
}

// setColor records a node's colour, keeping the FIRST answer and reporting a
// later, different one as a conflict. Two rows summing into one node is the
// defined reading of a folded result; two rows giving that node two colours is
// not something the query said how to resolve, so the picture takes one and the
// status line says how often it had to.
func (t *hierTree) setColor(at int32, num float64, key string, st *hierStats) {
	i := int(at)
	switch t.ColorKind {
	case hierColorNumeric:
		if i >= len(t.ColorNum) {
			return
		}
		if math.IsNaN(num) {
			return
		}
		if math.IsNaN(t.ColorNum[i]) {
			t.ColorNum[i] = num
			return
		}
		if t.ColorNum[i] != num {
			st.colorConflicts++
		}
	case hierColorCategorical:
		if i >= len(t.ColorKey) || key == "" {
			return
		}
		if t.ColorKey[i] == "" {
			t.ColorKey[i] = key
			return
		}
		if t.ColorKey[i] != key {
			st.colorConflicts++
		}
	}
}

// buildHierarchyFolded interns one row per root-to-leaf path into a trie. Two
// rows carrying the same path SUM, which is the defined reading rather than an
// anomaly: they are two measurements of one node.
//
// Deterministic given the record — the interning walks rows in order — so the
// tree is stable frame to frame.
//
// A row's colour lands on the node its VALUE lands on, i.e. the deepest element
// of its path. Interior nodes a row merely passed through keep no colour, since
// nothing in the result described them; a path that is a PREFIX of another does
// colour its interior node, because there the row terminates.
func buildHierarchyFolded(rec arrow.RecordBatch, cl hierClaim) (t hierTree, st hierStats) {
	st.mode = hierModeFolded
	if rec == nil {
		return
	}
	stackArr := rec.Column(cl.stackCol)
	intern := make(map[hierNodeKey]int32, 1024)
	// Colours are collected per row and applied after the node count is final,
	// so the slices are sized once instead of growing alongside the trie.
	type colorAt struct {
		at  int32
		num float64
		key string
	}
	var colors []colorAt
	var path []string
	for row := range rec.NumRows() {
		var ok bool
		path, ok = hierStackAt(stackArr, int(row), path[:0])
		if !ok || len(path) == 0 {
			st.droppedPath++
			continue
		}
		v, valOK := quantityCellValue(rec, cl.valueCol, row)
		if !valOK || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			st.droppedValue++
			continue
		}
		if len(path) > hierMaxDepth {
			path = path[:hierMaxDepth]
			st.truncated++
		}
		cur := int32(-1)
		for _, lbl := range path {
			key := hierNodeKey{parent: cur, label: lbl}
			at, seen := intern[key]
			if !seen {
				if len(t.Labels) >= hierMaxNodes {
					st.capped = true
					break
				}
				at = int32(len(t.Labels))
				t.Labels = append(t.Labels, lbl)
				t.Parents = append(t.Parents, cur)
				t.Self = append(t.Self, 0)
				intern[key] = at
			}
			cur = at
		}
		if cur < 0 {
			// The cap was reached before even this path's first element could be
			// interned, so there is nothing to attribute the value to. This is
			// the only way a folded row loses its value to the cap.
			st.droppedCapped++
			continue
		}
		t.Self[cur] += v
		if cl.colorCol >= 0 {
			num, k := hierColorAt(rec, cl, row)
			colors = append(colors, colorAt{at: cur, num: num, key: k})
		}
	}
	st.nodes = t.Len()
	if cl.colorCol >= 0 {
		t.initColors(cl.colorKind)
		for _, c := range colors {
			t.setColor(c.at, c.num, c.key, &st)
		}
	}
	return
}

// buildHierarchyNodes maps one row per node. Two passes, because no ordering is
// required of the result: demanding parents before children would reject the
// shape a recursive CTE most naturally emits, and neither widget imposes such an
// order either.
func buildHierarchyNodes(rec arrow.RecordBatch, cl hierClaim) (t hierTree, st hierStats) {
	st.mode = hierModeNodes
	if rec == nil {
		return
	}
	index := make(map[string]int32, 256)
	parentOf := make([]string, 0, 256)
	var colorRows []int64
	for row := range rec.NumRows() {
		if len(t.Labels) >= hierMaxNodes {
			st.capped = true
			break
		}
		id := formatCell(rec, cl.idCol, row)
		if id == "" {
			st.droppedPath++
			continue
		}
		if _, dup := index[id]; dup {
			st.droppedDup++
			continue
		}
		v, ok := quantityCellValue(rec, cl.valueCol, row)
		if !ok || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
			st.droppedValue++
			continue
		}
		label := id
		if cl.labelCol >= 0 {
			if l := formatCell(rec, cl.labelCol, row); l != "" {
				label = l
			}
		}
		index[id] = int32(len(t.Labels))
		t.Labels = append(t.Labels, label)
		t.Parents = append(t.Parents, -1)
		t.Self = append(t.Self, v)
		parentOf = append(parentOf, formatCell(rec, cl.parentCol, row))
		if cl.colorCol >= 0 {
			colorRows = append(colorRows, row)
		}
	}
	for i, p := range parentOf {
		if p == "" {
			continue // a root, which is what an empty or NULL parent means
		}
		at, ok := index[p]
		if !ok || at == int32(i) {
			// An unknown parent, or a row naming itself — which a widget's own
			// validation would reject the whole tree over. Demote to a root and
			// count it.
			st.reparented++
			continue
		}
		t.Parents[i] = at
	}
	st.nodes = t.Len()
	if cl.colorCol >= 0 {
		t.initColors(cl.colorKind)
		// One node per accepted row, in order, so the i-th accepted row is the
		// i-th node. No conflict is reachable here — a duplicate id never became
		// a second node — but the same path is used so both modes agree.
		for i, row := range colorRows {
			num, k := hierColorAt(rec, cl, row)
			t.setColor(int32(i), num, k, &st)
		}
	}
	return
}
