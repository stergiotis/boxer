package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// The notebook launch config (ADR-0371 §SD10): a notebook another app hands
// over — its markdown and a name to show — and, when it is a document of a
// lading snapshot, the mount, the snapshot instant and its path there, so
// the window reads the stored results beside it from the same snapshot. The
// ordinals continue after the external-edit launch config's.
var (
	MembNotebookLaunchText = KeelsonHrNkRegistry.MustBegin("notebookLaunchText", 275).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembNotebookLaunchName = KeelsonHrNkRegistry.MustBegin("notebookLaunchName", 276).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembNotebookLaunchMount = KeelsonHrNkRegistry.MustBegin("notebookLaunchMount", 277).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembNotebookLaunchSnapshot = KeelsonHrNkRegistry.MustBegin("notebookLaunchSnapshot", 278).
					MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembNotebookLaunchPath = KeelsonHrNkRegistry.MustBegin("notebookLaunchPath", 279).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
)
