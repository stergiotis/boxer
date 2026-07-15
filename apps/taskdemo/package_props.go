package taskdemo

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMUnknown,
	WASMJS:           packageprops.WASMUnknown,
	WASMFreestanding: packageprops.WASMUnknown,
	CapsDirect:       packageprops.Caps(packageprops.CapabilitySafe),
	CapsReachable:    packageprops.Caps(packageprops.CapabilityFiles, packageprops.CapabilityNetwork, packageprops.CapabilityRuntime, packageprops.CapabilityReadSystemState, packageprops.CapabilityModifySystemState, packageprops.CapabilityOperatingSystem, packageprops.CapabilitySystemCalls, packageprops.CapabilityArbitraryExecution, packageprops.CapabilityCgo, packageprops.CapabilityUnsafePointer, packageprops.CapabilityReflect, packageprops.CapabilityExec),
}

func init() { packageprops.Register("github.com/stergiotis/boxer/apps/taskdemo", PackageProps) }
