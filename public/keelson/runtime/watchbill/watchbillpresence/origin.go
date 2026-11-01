package watchbillpresence

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// Origin is who a presence row came from (ADR-0277 §SD1): the audit
// package's origin component, bound beside the store's own so a reader
// selects this kind with every other writer's by one predicate (ADR-0330
// §SD3). Run is the worker's run; a worker has no window, so App is empty
// and Instance zero — unattributed (ADR-0191 §SD3). An alias, against
// CS008, for the reason trail.Origin is one: the generated store names its
// component type unqualified in this package.
type Origin = audit.Origin
