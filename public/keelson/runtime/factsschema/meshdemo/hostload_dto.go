package meshdemo

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// HostLoad is domain B: the component capacity planning formulates *after* the
// agent's rows exist.
//
// Everything about it is deliberately unremarkable, which is the point. It is
// a Go struct in a package that generates nothing; its ids are resolved from
// [NkRegistry] at run time; its plan is built from these tags by
// `marshallreflect.PlanFor`; and the rows it reads were written by code that
// has never heard of it.
//
// Two choices in it are worth reading twice:
//
//   - It does **not** claim `meshKindFleetSample`. A component is satisfied by
//     the slots a row carries, not by a marker the writer thought to set, and
//     gating on the marker would mean no row written before the marker existed
//     could ever satisfy a later component. Assertions accelerate; slots
//     decide.
//   - `DrainedAt` names a membership nobody writes. That is not a defect to
//     handle — an absent optional slot is a legal reading, and a component
//     whose every slot had to be present could not be formulated over data it
//     did not commission.
type HostLoad struct {
	_ struct{} `kind:"hostLoad"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Host       string `lw:"meshHost,symbol"`
	CpuPercent uint8  `lw:"meshCpuPercent,u8Array,unit"`

	DrainedAt option.Option[uint64] `lw:"meshDrainedAt,u64Array,unit"`
}
