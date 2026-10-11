// Package audit holds the audit event below the keelson runtime (ADR-0327
// (proposed) §SD8): the component a domain writes its records of
// processing with (ADR-0296 §SD1), its bounds, the recorder's own domain,
// and the forwarder seam a carrier adapter implements. A process that
// links no runtime package builds a recorder over this package, its own
// generated store and a forwarder; the host's trail recorder re-exports
// the type and keeps its verbs.
//
// The component binds the facts data sections by name and the runtime
// vocabulary's membership names (`runtimeKindAuditEvent`,
// `auditEvent*`); a store outside the host registers the same names in
// the vocabulary its table is written against.
package audit
