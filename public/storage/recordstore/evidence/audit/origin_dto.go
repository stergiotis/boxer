package audit

// Origin is who a row came from: the run, the app and the app instance
// (ADR-0277 §SD1), on the memberships every runtime-written row spells
// them with. A store binds it beside its own components; the trail's rows
// and an audit recorder's carry it. On an audit event a reader weighs it
// by the event's OriginBy: the host stamped it, took it from a bus
// envelope, or the writing process stated it.
//
// Run names the seal stream the row belongs to. Instance 0 is
// unattributed: a host service's own work, or a transport that carries no
// window. The pair (Run, Instance) names a window.
type Origin struct {
	_ struct{} `kind:"origin"`

	Id       uint64 `lw:",id"`
	Run      string `lw:"runtimeRun,symbol"`
	App      string `lw:"runtimeApp,symbol"`
	Instance uint64 `lw:"runtimeLifecycleTileKey,u64Array,unit"`
}
