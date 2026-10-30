package stevedorefacts

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// Origin is who a dead letter came from (ADR-0277 §SD1): the audit
// package's origin component, bound beside the dead-letter kind so a reader
// selects these rows with every other writer's by one predicate (ADR-0330
// §SD3). Run is the lander's run; a lander has no window, so App is empty
// and Instance zero — unattributed (ADR-0191 §SD3). It is not
// [DeadLetter.Origin], which is the stevedore envelope's source name. An
// alias, against CS008, for the reason trail.Origin is one: the generated
// store names its component type unqualified in this package.
type Origin = audit.Origin
