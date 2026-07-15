package callsites

import "github.com/stergiotis/boxer/public/packageprops"

// PackageProps records this package's curated properties (ADR-0080).
// Seeded by `wasmsurvey props generate` (WASM*) and `capsurvey generate`
// (Caps*); curate by hand, then run the matching verify.
var PackageProps = packageprops.Props{
	WASMWASI:         packageprops.WASMBlocked,
	WASMJS:           packageprops.WASMBlocked,
	WASMFreestanding: packageprops.WASMBlocked,
	CapsDirect:       packageprops.Caps(packageprops.CapabilityFiles, packageprops.CapabilityNetwork, packageprops.CapabilityReadSystemState, packageprops.CapabilityExec),
	CapsReachable:    packageprops.Caps(packageprops.CapabilityFiles, packageprops.CapabilityNetwork, packageprops.CapabilityRuntime, packageprops.CapabilityReadSystemState, packageprops.CapabilityModifySystemState, packageprops.CapabilityOperatingSystem, packageprops.CapabilitySystemCalls, packageprops.CapabilityArbitraryExecution, packageprops.CapabilityCgo, packageprops.CapabilityUnsafePointer, packageprops.CapabilityReflect, packageprops.CapabilityExec),
}

func init() { packageprops.Register("github.com/stergiotis/boxer/public/gov/callsites", PackageProps) }
