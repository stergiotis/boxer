package ladingpolicy

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// Origin is who declared a mount's policy (ADR-0277 §SD1): the audit
// package's origin component, bound beside the mount kind so a reader
// selects these rows with every other writer's by one predicate (ADR-0330
// §SD3). The writer states it: the run of the process that records, and
// the app and window a published tree came from, else "" and 0 —
// unattributed (ADR-0191 §SD3). An alias, against CS008, for the reason
// trail.Origin is one: the generated store names its component type
// unqualified in this package.
type Origin = audit.Origin
