// Package marshallgen is the generic Go DTO → leeway codec generator. It
// parses an annotated Go DTO source file (via go/ast) into a
// mappingplan.Plan and emits a sibling `.out.go` carrying the schema-
// agnostic core: <Kind>Columns SoA storage, Append/Row adapters, derived
// per-section / per-membership interfaces, and the generic
// <Kind>BuildEntities + <Kind>FillFromArrow helpers that bind to any
// leeway DML / RA via Go type inference at the call site.
//
// The Plan IR (the parsed DTO model) lives in the mappingplan package; the
// lw: tag grammar, validation, section grouping, and field-shape
// classification — the Go-DTO construction machinery shared with the
// reflect sibling — live in the sibling goplan package. marshallgen is the
// go/ast front-end plus the emitter on top of them.
//
// Anything schema-specific — kind-id resolution, dml backend pool,
// per-kind active-fields hints, Marshal/Unmarshal methods, codec bridge
// — lives behind WrapperEmitterI hooks the caller passes in. NoOpWrapper
// produces the schema-agnostic surface only; consumers layer their own
// wrapper for full-stack emit.
package marshallgen
