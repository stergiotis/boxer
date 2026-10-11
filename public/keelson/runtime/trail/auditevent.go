package trail

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// AuditEvent is the audit package's component (ADR-0327 §SD8): the trail
// store binds it as one of its kinds, and the recorder's Event writes it.
// The bounds, the outcomes, the attestations and the recorder's own domain
// are the audit package's too, re-exported for the host's services.
type AuditEvent = audit.AuditEvent

// ForwarderI is the audit package's forwarder seam (ADR-0296 §SD6): the
// recorder hands it durable audit events as plain values.
type ForwarderI = audit.ForwarderI

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
