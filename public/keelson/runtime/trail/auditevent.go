package trail

import "github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"

// AuditEvent is the audit package's component (ADR-0327 §SD8): the trail
// store binds it as one of its kinds, and the recorder's Event writes it.
// It is an alias, against CS008, because the generated store names its
// component type unqualified in this package while the DTO lives in the
// audit package; the alias goes when the generator emits a qualified type
// for a component from another package.
type AuditEvent = audit.AuditEvent

// Origin is who a trail row came from (ADR-0277 §SD1): the audit
// package's origin component, an alias for the reason AuditEvent is one.
// On every verb but Event the host stamps it — the run from the process,
// app and window from the bus envelope — so an app cannot claim another's.
type Origin = audit.Origin

const (
	OutcomeOk     = audit.OutcomeOk
	OutcomeDenied = audit.OutcomeDenied
	OutcomeFailed = audit.OutcomeFailed

	PrincipalBySystem = audit.PrincipalBySystem
	PrincipalByEnv    = audit.PrincipalByEnv
	PrincipalByOs     = audit.PrincipalByOs
	PrincipalByNone   = audit.PrincipalByNone

	PrincipalViaPeer     = audit.PrincipalViaPeer
	PrincipalViaMtls     = audit.PrincipalViaMtls
	PrincipalViaToken    = audit.PrincipalViaToken
	PrincipalViaSession  = audit.PrincipalViaSession
	PrincipalViaUpstream = audit.PrincipalViaUpstream

	TrailDomain        = audit.TrailDomain
	ActionAuditGap     = audit.ActionAuditGap
	ActionAuditInvalid = audit.ActionAuditInvalid
	RetentionTrail     = audit.RetentionTrail

	MaxNameRunes  = audit.MaxNameRunes
	MaxValueRunes = audit.MaxValueRunes
	MaxRefs       = audit.MaxRefs
	MaxAttrs      = audit.MaxAttrs
)
