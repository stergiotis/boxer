//go:build llm_generated_opus47

// Package marshallreflect is the runtime-reflection sibling of
// boxer/public/semistructured/leeway/marshall/go/marshallgen. Both build on
// the shared Plan IR (mappingplan) and the Go-DTO construction machinery
// (goplan): marshallreflect parses the same `lw:` tag vocabulary at runtime
// via reflect.StructTag (no go/ast), produces the same mappingplan.Plan
// value, and drives a Go DTO ↔ leeway-DML chain through reflect.Value
// method dispatch.
//
// Use cases (per the slow-path / config-store rationale):
//
//   - Marshalling DTOs whose code is not pre-known to a generator
//     pass (config files, ad-hoc tooling, dynamic schemas).
//   - Tests that exercise multiple DML implementations against the
//     same DTO without regenerating per case.
//
// Hot-path uses must continue to call marshallgen-generated
// BuildEntities — the reflect path pays per-row reflection costs
// (method lookup, value boxing).
//
// Wire compatibility is the load-bearing invariant: the bytes emitted
// by marshallreflect.Marshal(rows) followed by dml.TransferRecords
// must equal the bytes emitted by marshallgen's generated
// <Kind>BuildEntities(dml, columns) followed by dml.TransferRecords,
// for the same DTO. Verified via round-trip tests against a
// recording mock DML and (transitively) against the typed DMLs that
// already round-trip in the per-kind keelson codec test suites.
//
// The membership-id resolver is pluggable via LookupI — pebble's
// facts target wraps vdd.KeelsonHrNkRegistry; an anchor or schema-
// agnostic target can use NoLookup if every membership in its DTOs
// carries `,verbatim`.
package marshallreflect
