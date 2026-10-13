package example

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// AuditEvent and Origin are the audit package's components, bound by the
// store through these aliases: the generated store names its component
// types unqualified in its own package.
type (
	AuditEvent = audit.AuditEvent
	Origin     = audit.Origin
)
