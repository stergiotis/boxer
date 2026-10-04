package trail

// Origin is who a trail row came from (ADR-0277 §SD1): the run, the app and
// its window, on the memberships every runtime-written row spells them with
// (ADR-0191 §SD1). The host stamps it — the run from the process, app and
// window from the bus envelope — so an app cannot claim another's.
//
// Instance 0 is unattributed: a host service's own work, or a transport that
// carries no window. The pair (Run, Instance) names a window; Instance alone
// is a per-run counter.
type Origin struct {
	_ struct{} `kind:"origin"`

	Id       uint64 `lw:",id"`
	Run      string `lw:"runtimeRun,symbol"`
	App      string `lw:"runtimeApp,symbol"`
	Instance uint64 `lw:"runtimeLifecycleTileKey,u64Array,unit"`
}
