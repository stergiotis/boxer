package vizeval

// Sink ids as the Experiments pane names them — the labels of its sink
// selector, and what a seed or a candidate names.
const (
	SinkCard         = "card"
	SinkTopology     = "topology"
	SinkJSON         = "json"
	SinkUnicode      = "unicode"
	SinkTopoSpark    = "topo"
	SinkBrailleSpark = "braille"
	SinkTreemapSpark = "treemap"
	SinkChart        = "chart"
	SinkGraph        = "graph"
	SinkHierarchy    = "hierarchy"
	SinkLens         = "lens"
)

// Option names shared by more than one caller.
const (
	OptionPalette        = "palette"
	OptionWidth          = "width"
	OptionMaxColumnWidth = "maxColumnWidth"
	OptionMark           = "mark"
	OptionSeriesBy       = "seriesBy"
	OptionSort           = "sort"
	OptionLegend         = "legend"
	OptionColormap       = "colormap"
	OptionLayout         = "layout"
	OptionOrientation    = "orientation"
	OptionSpacing        = "spacing"
	OptionLabelsAlways   = "labelsAlways"
	OptionDirected       = "directed"
	OptionColorGroups    = "colorGroups"
	OptionForm           = "form"
	OptionSizeBy         = "sizeBy"
	OptionSeparator      = "separator"
	OptionMaxDepth       = "maxDepth"
	OptionColorBy        = "colorBy"
	OptionValues         = "values"
	OptionStable         = "stable"
	OptionRow            = "row"
)

// Chart option choices, in the order the pane offers them.
var (
	ChartMarks     = []string{"bar", "line", "scatter", "heatmap"}
	ChartSeriesBy  = []string{"membership", "entity"}
	ChartSorts     = []string{"none", "ascending", "descending"}
	ChartColormaps = []string{"viridis", "inferno", "magma", "plasma", "cividis", "turbo"}
)

// Graph option choices, in the order the pane offers them.
var (
	GraphLayouts      = []string{"force", "force_gravity", "hierarchical", "radial"}
	GraphOrientations = []string{"top_down", "left_right"}
)

// Lens option choices, in the order the pane offers them.
var LensForms = []string{"rows", "archetypes", "focus"}

// Hierarchy option choices, in the order the pane offers them.
var (
	HierarchyForms      = []string{"treemap", "icicle", "sankey"}
	HierarchySizeBy     = []string{"value", "count"}
	HierarchySeparators = []string{"/", ".", ":", "-"}
	HierarchyColorBy    = []string{"branch", "depth"}
)

// SinkSpec declares one sink: what it is called, how many rows of a batch its
// picture can carry, and its option space.
//
// RowCap is a property of the picture, not of the machine: a sink that reads
// shape needs few rows because shape repeats, one that draws every row needs
// them all. The pane drives at most RowCap rows and says so when it cuts; the
// harness does not score a candidate whose batch exceeds it (ADR-0257 §SD1).
type SinkSpec struct {
	ID     string
	Title  string
	RowCap int64
	Space  Space
}

// palettes are the section-accent ladders of leewaywidgets.ColorPaletteE, by
// the name the pane shows.
var palettes = []string{"inferno", "viridis", "magma", "plasma"}

var sinks = []SinkSpec{
	{ID: SinkCard, Title: "card table", RowCap: 32, Space: Space{
		{Name: OptionPalette, Kind: OptionKindEnum, Choices: palettes, Default: "viridis",
			Description: "section-accent colour ladder"},
	}},
	{ID: SinkTopology, Title: "topology treemap", RowCap: 16},
	{ID: SinkJSON, Title: "card-JSON", RowCap: 16},
	{ID: SinkUnicode, Title: "box-drawn tables", RowCap: 16, Space: Space{
		{Name: OptionWidth, Kind: OptionKindInt, Min: 40, Max: 240, Default: int64(160),
			Description: "table budget, in characters: columns shrink when a table is wider"},
		{Name: OptionMaxColumnWidth, Kind: OptionKindInt, Min: 8, Max: 240, Default: int64(60),
			Description: "cap on one column, in characters; a longer cell is cut with an ellipsis"},
	}},
	{ID: SinkTopoSpark, Title: "topology spark", RowCap: 64},
	{ID: SinkBrailleSpark, Title: "braille spark", RowCap: 64},
	{ID: SinkTreemapSpark, Title: "treemap spark", RowCap: 64},
	// The chart projects the first tagged section with a numeric value: one
	// category per entity, one series per membership (leewaywidgets.ChartModel).
	// Every row is drawn, and 64 entities is past where category labels stay
	// readable at any width the pane has.
	{ID: SinkChart, Title: "chart", RowCap: 64, Space: Space{
		{Name: OptionMark, Kind: OptionKindEnum, Choices: ChartMarks, Default: "bar",
			Description: "how values are drawn"},
		{Name: OptionSeriesBy, Kind: OptionKindEnum, Choices: ChartSeriesBy, Default: "membership",
			Description: "what a series is: a membership, with entities along x, or an entity, with memberships along x"},
		{Name: OptionSort, Kind: OptionKindEnum, Choices: ChartSorts, Default: "none",
			Description: "category order: the batch's, or by the first series' value"},
		{Name: OptionLegend, Kind: OptionKindBool, Default: true, Description: "show the series legend"},
		{Name: OptionColormap, Kind: OptionKindEnum, Choices: ChartColormaps, Default: "viridis",
			Description: "the heatmap's colour ramp"},
	}},
	// The graph projects entities onto nodes and the section whose values
	// name other entities onto edges (leewaywidgets.GraphModel). 96 nodes is
	// where labels on every node stop fitting a pane.
	{ID: SinkGraph, Title: "graph", RowCap: 96, Space: Space{
		{Name: OptionLayout, Kind: OptionKindEnum, Choices: GraphLayouts, Default: "force_gravity",
			Description: "node placement"},
		{Name: OptionOrientation, Kind: OptionKindEnum, Choices: GraphOrientations, Default: "top_down",
			Description: "the hierarchical layout's growth direction"},
		{Name: OptionSpacing, Kind: OptionKindFloat, Min: 0.5, Max: 3, Default: 1.0,
			Description: "scale on every layout's distances"},
		{Name: OptionLabelsAlways, Kind: OptionKindBool, Default: true, Description: "label every node, not only the hovered one"},
		{Name: OptionDirected, Kind: OptionKindBool, Default: true, Description: "draw arrow heads"},
		{Name: OptionColorGroups, Kind: OptionKindBool, Default: true, Description: "tone nodes by group"},
	}},
	// The hierarchy splits each entity's label into a path and weighs it by
	// its values (leewaywidgets.Hierarchy). 96 entities is where a sankey's
	// leaf column stops having room for its labels.
	{ID: SinkHierarchy, Title: "hierarchy", RowCap: 96, Space: Space{
		{Name: OptionForm, Kind: OptionKindEnum, Choices: HierarchyForms, Default: "treemap",
			Description: "how the tree is drawn"},
		{Name: OptionSizeBy, Kind: OptionKindEnum, Choices: HierarchySizeBy, Default: "value",
			Description: "a leaf's size: its values' sum, or one per entity"},
		{Name: OptionSeparator, Kind: OptionKindEnum, Choices: HierarchySeparators, Default: "/",
			Description: "what splits a label into its path"},
		{Name: OptionMaxDepth, Kind: OptionKindInt, Min: 0, Max: 8, Default: int64(0),
			Description: "levels drawn, deeper ones folded into their ancestor; 0 draws all"},
		{Name: OptionColorBy, Kind: OptionKindEnum, Choices: HierarchyColorBy, Default: "branch",
			Description: "colour: a hue per top-level branch, or a ramp by depth"},
	}},
	// The lens reads rows as slots — (section, primary membership) — and
	// draws them by two reader intents (lwlens.Intent). Every row is drawn;
	// past 128 even the presence-only rows stop fitting a pane.
	{ID: SinkLens, Title: "lens", RowCap: 128, Space: Space{
		{Name: OptionValues, Kind: OptionKindFloat, Min: 0, Max: 1, Default: 0.5,
			Description: "structure (0: which slots a row has) to values (1: what they hold)"},
		{Name: OptionStable, Kind: OptionKindFloat, Min: 0, Max: 1, Default: 0.5,
			Description: "local (0: each row on its own terms) to stable (1: one frame for every row)"},
		{Name: OptionForm, Kind: OptionKindEnum, Choices: LensForms, Default: "rows",
			Description: "every row in its cluster, each cluster as its template and its exceptions, or one row among its peers"},
		{Name: OptionRow, Kind: OptionKindInt, Min: 0, Max: 127, Default: int64(0),
			Description: "the row the focus form draws"},
	}},
}

// Sinks is the catalogue, in the order the pane offers the sinks. The slice is
// shared; do not modify it.
func Sinks() []SinkSpec {
	return sinks
}

// SinkByID finds a sink by id.
func SinkByID(id string) (spec SinkSpec, ok bool) {
	for _, s := range sinks {
		if s.ID == id {
			return s, true
		}
	}
	return SinkSpec{}, false
}
