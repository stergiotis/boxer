package persiststore

// ColumnWidth is one table column-width override (ADR-0151), keyed
// ColumnWidthKey(appId, tier, scope, columnKey): the width a user dragged a
// column to, and the font size it was captured under so a resolver can
// rescale it. Tier is the override's scope class (instance / shape /
// column), Scope the table tag or shape hash it applies to (empty for the
// column tier), ColumnKey the blake3short of the column's name and type.
//
// The row moved here from `boxer.facts` with ADR-0105's Update of
// 2026-08-15, for the reason Workingset did.
type ColumnWidth struct {
	_         struct{} `kind:"columnWidth"`
	ID        string   `lw:",id"`
	Tier      string   `lw:"runtimeColWidthTier,symbol"`
	Scope     string   `lw:"runtimeColWidthScope,string"`
	ColumnKey string   `lw:"runtimeColWidthColumnKey,string"`
	Points    float64  `lw:"runtimeColWidthPoints,f64"`
	FontSize  float64  `lw:"runtimeColWidthFontSize,f64"`
}
