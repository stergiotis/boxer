package probe

import "time"

// Probe is a component fixture for the storegen tests. It is deliberately
// small but binds three different boxer.facts sections — a symbol, a
// unit-valued integer and a unit-valued float — so a generated store over it
// exercises the section machinery rather than one degenerate lane.
//
// Its package clause must match the generated package name — the emitted codec
// takes its package from the DTO source, not from Input.PackageName.
//
// It lives under testdata/ because the generator parses it as source (AST
// only, no type-check) and nothing should compile or link it.
type Probe struct {
	_ struct{} `kind:"storegenProbe"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Host  string  `lw:"storegenProbeHost,symbol"`
	Count uint64  `lw:"storegenProbeCount,u64Array,unit"`
	Ratio float32 `lw:"storegenProbeRatio,f32Array,unit"`
}
