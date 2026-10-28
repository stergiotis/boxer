package trail

import "github.com/RoaringBitmap/roaring"

// CoverageAgentApply is the code one agent command's handler executed
// (ADR-0343 §SD5): the units whose counter moved between a read before the
// handler call and a read after it, on the render goroutine. The window
// the command addressed is the row's [Origin]; the task, epoch and call the
// dispatcher stamped on the command its [Delegation], so the row joins the
// action record on (task, call). The row claims the synchronous handler
// path only: work the handler started elsewhere, the task's queries and
// external reads, and the next frame's drawing are outside the bracket.
type CoverageAgentApply struct {
	_ struct{} `kind:"coverageAgentApply"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind     string `lw:"runtimeKindCoverageAgentApply,symbol"`
	MetaHash string `lw:"coverageAgentApplyMetaHash,symbol"`
	// Frame is the engine's frame the command applied in.
	Frame uint64 `lw:"coverageAgentApplyFrame,u64Array,unit"`
	// Units are the global unit ids whose counter changed inside the bracket.
	Units        *roaring.Bitmap `lw:"coverageAgentApplyUnits,u32Set"`
	ChangedUnits uint64          `lw:"coverageAgentApplyChangedUnits,u64Array,unit"`
	ChangedFuncs uint64          `lw:"coverageAgentApplyChangedFuncs,u64Array,unit"`
	// Operation is the catalog name the command ran; Phase the outcome of
	// the handler call (applied, failed, refused, conflict).
	Operation string `lw:"coverageAgentApplyOperation,symbol"`
	Phase     string `lw:"coverageAgentApplyPhase,symbol"`
	// CostUs is what the bracket itself cost: both counter reads, their
	// decode and the diff, in microseconds (ADR-0343 §SD7).
	CostUs uint64 `lw:"coverageAgentApplyCostUs,u64Array,unit"`
}
