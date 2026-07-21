// Package capabilitygrant is the ADR-0042 M4 retrofit of the
// rowmarshall.CapabilityGrant hand-coded writer. The generator
// produces a byte-equivalent `Marshal` over a SoA Columns buffer; the
// existing `rowmarshall.BenchmarkRowBinaryMarshal` (51 ns/op) is the
// regression gate.
//
// The DTO uses the codec grammar: scalar T for ExactlyOne, option.Option[T]
// for ZeroToOne, and the `u32Range:<col>` sub-column suffix for the
// validity range's two physical columns. NaturalKey is a plain byte
// slice; Ts and ExpiresAt are time.Time, matching the facts entity
// builder's SetTimestamp / SetLifecycle signatures verbatim (strict 1:1
// — the seconds-resolution DateTime landing happens inside the builder).
package capabilitygrant

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// CapabilityGrant is one row of a capability grant in boxer.facts.
// Mirrors rowmarshall.CapabilityGrant on the wire; differences:
//   - `*int64` → `option.Option[int64]` for ExpiresAt
//   - `*uint64` → `option.Option[uint64]` for GranterFact
//   - Same plain columns, same tagged sections, same kind ids
//     (now resolved from vdd at init instead of hardcoded constants).
type CapabilityGrant struct {
	_ struct{} `kind:"capabilityGrant"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"` // truncated to seconds on the wire

	// ExpiresAt is a time.Time; the zero value (the Go default when not
	// set) lands as the "no TTL" sentinel the way the facts builder maps
	// a zero time. (option.Option is reserved for ZeroToOne *tagged*
	// values; plain columns are mandatory and rely on Go's zero value.)
	ExpiresAt time.Time `lw:",expiresAt"`

	Subject       string                `lw:"cgSubject,stringArray"` // free text — high-card
	Capability    string                `lw:"cgCapability,symbol"`   // enum — LowCardinality(String) on disk
	ValidityBegin uint32                `lw:"cgValidity,u32Range:beginIncl"`
	ValidityEnd   uint32                `lw:"cgValidity,u32Range:endExcl"`
	Active        bool                  `lw:"cgActive,bool"`
	GranterFact   option.Option[uint64] `lw:"cgGranter,foreignKey"` // None ⇒ no granter link
}
