package trail

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// AuditEvent is the audit package's component (ADR-0327 §SD8): the trail
// store binds it as one of its kinds, and the recorder's Event writes it.
// It is an alias, against CS008, because the generated store names its
// component type unqualified in this package while the DTO lives in the
// audit package; the alias goes when the generator emits a qualified type
// for a component from another package.
type AuditEvent = audit.AuditEvent

const (
	OutcomeOk     = audit.OutcomeOk
	OutcomeDenied = audit.OutcomeDenied
	OutcomeFailed = audit.OutcomeFailed

	PrincipalBySystem = audit.PrincipalBySystem
	PrincipalByEnv    = audit.PrincipalByEnv
	PrincipalByOs     = audit.PrincipalByOs
	PrincipalByNone   = audit.PrincipalByNone

	TrailDomain        = audit.TrailDomain
	ActionAuditGap     = audit.ActionAuditGap
	ActionAuditInvalid = audit.ActionAuditInvalid
	RetentionTrail     = audit.RetentionTrail

	MaxNameRunes  = audit.MaxNameRunes
	MaxValueRunes = audit.MaxValueRunes
	MaxRefs       = audit.MaxRefs
	MaxAttrs      = audit.MaxAttrs
)
