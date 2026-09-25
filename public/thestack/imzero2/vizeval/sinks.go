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
)

// Option names shared by more than one caller.
const (
	OptionPalette        = "palette"
	OptionWidth          = "width"
	OptionMaxColumnWidth = "maxColumnWidth"
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
