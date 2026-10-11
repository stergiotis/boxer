//go:build !unix

package agentrun

// processExists cannot ask the system here, so every holder counts as
// present: a run is refused rather than taken from a writer that may
// still be there.
func processExists(pid int) bool { return true }
