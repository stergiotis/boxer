// Package audit holds the audit event below the keelson runtime (ADR-0327
// §SD8): the component a domain writes its records of processing with
// (ADR-0296 §SD1), its bounds, the origin component every row of a
// recorder carries, the rows a recorder writes about itself ([Invalid],
// [Gap]), the identity rule every recorder applies ([Stamp], [OriginOf]),
// and the forwarder seam a carrier adapter implements. The recorder is
// package auditrec; the host's trail recorder re-exports the types and
// builds its rows with the same functions, so a gap or an invalid row
// means the same whichever process wrote it.
//
// The components bind the facts data sections by name and the runtime
// vocabulary's membership names (`runtimeKindAuditEvent`, `auditEvent*`,
// `runtimeRun`, `runtimeApp`, `runtimeLifecycleTileKey`); a store outside
// the host registers the same names in the vocabulary its table is
// written against.
package audit
