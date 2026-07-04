package example

import "github.com/stergiotis/boxer/public/functional/option"

// Identity, Battery (battery_dto.go), Tagged (tagged_dto.go) and Located
// (located_dto.go) are the device components: flat lw:-tagged DTOs, each
// carrying the entity id plus its own tagged section, mirroring the
// ecsdemo component split. One kind lives per source file because
// marshallgen.ParsePlan reads a single kind per input. Membership ids are
// per-section and each component owns a distinct section, so the
// generated per-kind ids cannot collide in storage.
//
// Nick is an Option scalar (ZeroToOne): absent Nick and present Identity
// coexist on one row — the shape the presence-gated ReadRow decode covers.
type Identity struct {
	_      struct{}              `kind:"identity"`
	ID     uint64                `lw:",id"`
	Status string                `lw:"deviceStatus,symbol"`
	Nick   option.Option[string] `lw:"deviceNick,symbol"`
}
