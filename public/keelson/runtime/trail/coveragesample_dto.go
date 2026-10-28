package trail

import "github.com/RoaringBitmap/roaring"

// CoverageSample is what the sampler's tick found newly covered (ADR-0343
// §SD4): the units that went from never executed to executed since the
// previous row of this run, and the run's cumulative totals after the
// fold. The union of a run's sample rows is its covered set; the sampler's
// periodic re-statements are not persisted, since nothing is lost between
// the sampler and this row. The run is the row's [Origin].
type CoverageSample struct {
	_ struct{} `kind:"coverageSample"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind     string `lw:"runtimeKindCoverageSample,symbol"`
	MetaHash string `lw:"coverageSampleMetaHash,symbol"`
	Seq      uint64 `lw:"coverageSampleSeq,u64Array,unit"`
	// Units are the newly covered global unit ids.
	Units        *roaring.Bitmap `lw:"coverageSampleUnits,u32Set"`
	CoveredUnits uint64          `lw:"coverageSampleCoveredUnits,u64Array,unit"`
	CoveredStmts uint64          `lw:"coverageSampleCoveredStmts,u64Array,unit"`
	CoveredFuncs uint64          `lw:"coverageSampleCoveredFuncs,u64Array,unit"`
}
