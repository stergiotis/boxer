package capmapfacts

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// Origin is who ingested a corpus (ADR-0277 §SD1): the audit package's
// origin component, written on every competence and relation row so a
// reader selects them with every other writer's rows by one predicate
// (ADR-0330 §SD3). The caller states it: the ingesting process's run, and
// "" and 0 for app and window when a CLI ingests — unattributed (ADR-0191
// §SD3). An alias, against CS008, for the reason trail.Origin is one: the
// generated codec names its component type unqualified in this package.
type Origin = audit.Origin
