//go:build llm_generated_opus47

// Package capabilitygrant is the ADR-0042 M4 retrofit of the
// rowmarshall.CapabilityGrant hand-coded writer. The generator
// produces a byte-equivalent `Marshal` over a SoA Columns buffer; the
// existing `rowmarshall.BenchmarkRowBinaryMarshal` (51 ns/op) is the
// regression gate.
//
// The DTO uses the codec grammar: scalar T for ExactlyOne, option.Option[T]
// for ZeroToOne, and the `u32Range:<col>` sub-column suffix for the
// validity range's two physical columns. NaturalKey is a plain byte
// slice; Ts and ExpiresAt are int64 nanoseconds (matching the legacy
// wire — divided by 1e9 at emit time to land as DateTime UInt32).
package capabilitygrant

import (
	"github.com/stergiotis/boxer/public/functional/option"
)

// CapabilityGrant is one row of a capability grant in runtime.facts.
// Mirrors rowmarshall.CapabilityGrant on the wire; differences:
//   - `*int64` → `option.Option[int64]` for ExpiresAt
//   - `*uint64` → `option.Option[uint64]` for GranterFact
//   - Same plain columns, same tagged sections, same kind ids
//     (now resolved from vdd at init instead of hardcoded constants).
type CapabilityGrant struct {
	_ struct{} `kind:"capabilityGrant"`

	Id         uint64 `lw:",id"`
	NaturalKey []byte `lw:",naturalKey"`
	Ts         int64  `lw:",ts"` // unix nanoseconds; truncated to seconds on the wire

	// ExpiresAt is int64 nanoseconds matching the legacy wire convention.
	// A zero value (the Go default when not set) lands as `0` on the wire
	// — the same "no TTL" sentinel the legacy rowmarshall writer used.
	// (option.Option is reserved for ZeroToOne *tagged* values; plain
	// columns rely on Go's zero-value semantics.)
	ExpiresAt int64 `lw:",expiresAt"`

	Subject       string `lw:"cgSubject,stringArray"`           // free text — high-card
	Capability    string `lw:"cgCapability,symbol"`        // enum — LowCardinality(String) on disk
	ValidityBegin uint32 `lw:"cgValidity,u32Range:beginIncl"`
	ValidityEnd   uint32 `lw:"cgValidity,u32Range:endExcl"`
	Active        bool   `lw:"cgActive,bool"`
	GranterFact   option.Option[uint64] `lw:"cgGranter,foreignKey"` // None ⇒ no granter link
}
