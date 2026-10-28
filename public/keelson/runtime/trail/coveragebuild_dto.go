package trail

// CoverageBuild anchors a coverage meta hash to the binary and the commit
// it was built from (ADR-0343 §SD2): one row per run of an instrumented
// build with the trail opt-in on. MetaHash is the key every other coverage
// row carries; BuildId, VcsRevision and VcsModified are the run row's,
// denormalised so the join from hash to binary is one lookup. A build is
// anchored to a commit when VcsRevision is set and VcsModified is false;
// otherwise its units resolve against the function table of its own hash
// and no further. The run is the row's [Origin].
type CoverageBuild struct {
	_ struct{} `kind:"coverageBuild"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind        string `lw:"runtimeKindCoverageBuild,symbol"`
	MetaHash    string `lw:"coverageBuildMetaHash,symbol"`
	Mode        string `lw:"coverageBuildMode,symbol"`
	Granularity string `lw:"coverageBuildGranularity,symbol"`
	Units       uint64 `lw:"coverageBuildUnits,u64Array,unit"`
	Stmts       uint64 `lw:"coverageBuildStmts,u64Array,unit"`
	Funcs       uint64 `lw:"coverageBuildFuncs,u64Array,unit"`
	Pkgs        uint64 `lw:"coverageBuildPkgs,u64Array,unit"`
	BuildId     string `lw:"coverageBuildId,symbol"`
	VcsRevision string `lw:"coverageBuildVcsRevision,symbol"`
	VcsModified bool   `lw:"coverageBuildVcsModified,bool"`
}
