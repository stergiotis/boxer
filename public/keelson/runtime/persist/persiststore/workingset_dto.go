package persiststore

// Workingset is one saved app workingset (ADR-0148 §SD6): the launch config
// a plain open restores, keyed WorkingsetKey(appId, name). Config is the
// app's Manifest.LaunchKind DTO in facts-CBOR, opaque to this layer; Kind
// names that DTO in its own column, because the bytes carry no kind marker
// and a reader that sniffed them would be guessing. Reason is why the
// window that wrote it closed.
//
// The row moved here from `boxer.facts` with ADR-0105's Update of
// 2026-08-15: a workingset is state — the newest one wins — and the facts
// table could only answer that through hand-written argMax SQL. The window
// that wrote it is Owner.InstanceKey, the run Owner.RunId.
type Workingset struct {
	_      struct{} `kind:"workingset"`
	ID     string   `lw:",id"`
	Name   string   `lw:"runtimeWorkingsetName,symbol"`
	Kind   string   `lw:"runtimeLaunchConfigKind,symbol"`
	Config []byte   `lw:"runtimeLaunchConfig,blob"`
	Reason string   `lw:"runtimeLifecycleStopReason,symbol"`
}
