package meshdemo

import "time"

// FleetSample is what the fleet agent writes: one host's reading at one
// moment, as a `boxer.facts` row.
//
// This is domain A. It has a generated store (fleet_store.out.go, from
// gen_test.go) whose membership ids come from [NkRegistry] rather than from
// this struct's field order, which is what lets a reader that has never seen
// this file resolve the same ids from the same names.
type FleetSample struct {
	_ struct{} `kind:"fleetSample"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label a human reads; its membership id is what a
	// query filters on. The agent asserts it about its own rows.
	Kind string `lw:"meshKindFleetSample,symbol"`

	Host   string `lw:"meshHost,symbol"`
	Region string `lw:"meshRegion,symbol"`

	CpuPercent    uint8  `lw:"meshCpuPercent,u8Array,unit"`
	UptimeSeconds uint64 `lw:"meshUptimeSeconds,u64Array,unit"`
}
