package trail

// CoverageFunc resolves a unit id to source (ADR-0343 §SD3): one row per
// instrumented function of a build, written once per meta hash. The
// function owns the global unit ids [UnitBase, UnitBase+Units); the three
// parallel arrays are indexed by a unit's offset from the base, so a unit
// id resolves to a line span without the meta blob. File is spelled as the
// meta spells it: the import path and the base name. The run that first saw
// the hash is the row's [Origin].
type CoverageFunc struct {
	_ struct{} `kind:"coverageFunc"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind     string `lw:"runtimeKindCoverageFunc,symbol"`
	MetaHash string `lw:"coverageFuncMetaHash,symbol"`
	Pkg      string `lw:"coverageFuncPkg,symbol"`
	Name     string `lw:"coverageFuncName,stringArray,unit"`
	File     string `lw:"coverageFuncFile,symbol"`
	// Lit marks a function literal.
	Lit      bool   `lw:"coverageFuncLit,bool"`
	UnitBase uint32 `lw:"coverageFuncUnitBase,u32Array,unit"`
	Units    uint32 `lw:"coverageFuncUnits,u32Array,unit"`
	Stmts    uint32 `lw:"coverageFuncStmts,u32Array,unit"`
	// Per unit, by offset from UnitBase: its first and last line and the
	// statements it holds.
	UnitStLine []uint32 `lw:"coverageFuncUnitStLine,u32Array"`
	UnitEnLine []uint32 `lw:"coverageFuncUnitEnLine,u32Array"`
	UnitStmts  []uint32 `lw:"coverageFuncUnitStmts,u32Array"`
}
