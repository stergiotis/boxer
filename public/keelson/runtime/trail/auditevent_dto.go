package trail

import (
	"strconv"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// AuditEvent is one event in a consumer's own records-of-processing
// vocabulary (ADR-0296 §SD1): what was done, to whose data, by whom, with
// what outcome. Everything a consumer varies — domain and action names,
// reference types, retention classes — is a value here, so a new action is
// a new constant and no membership. Who wrote it is the row's [Origin],
// which for this component the writing process states itself; the task,
// when the event is agent-caused, its [Delegation].
//
// No field carries a personal value: Principal is a pseudonymous reference,
// references are ids or hashes, attributes are short codes. The row's
// timestamp is when the recorded thing happened.
type AuditEvent struct {
	_ struct{} `kind:"auditEvent"`

	Id uint64 `lw:",id"`
	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"runtimeKindAuditEvent,symbol"`
	// Domain and Action are the event type: two lower-spinal names, the
	// consumer's namespace and what happened in it.
	Domain string `lw:"auditEventDomain,symbol"`
	Action string `lw:"auditEventAction,symbol"`
	// Outcome is one of the Outcome constants.
	Outcome string `lw:"auditEventOutcome,symbol"`
	// Principal is who acted, from the claims on the context the event was
	// recorded under; absent when the context carried none. PrincipalBy is
	// one of the PrincipalBy constants: how much a reader may trust it.
	Principal   option.Option[string] `lw:"auditEventPrincipal,stringArray,unit"`
	PrincipalBy string                `lw:"auditEventPrincipalBy,symbol"`
	// Purpose is the opaque purpose code the claims stated, when one was.
	Purpose option.Option[string] `lw:"auditEventPurpose,symbol"`
	// Node is the cell or node that wrote the event; absent on a
	// single-node host.
	Node option.Option[string] `lw:"auditEventNode,symbol"`
	// Subject is the event's primary data subject, 0 when there is none.
	// One scalar, so a storage table can sort by it (ADR-0296 §SD9).
	Subject uint64 `lw:"auditEventSubject,u64Array,unit"`
	// Retention is the retention class label, a lower-spinal name whose
	// period the consumer decides. One scalar, so a storage table can
	// partition by it.
	Retention string `lw:"auditEventRetention,symbol"`
	// RefTypes and RefValues are the typed object references, parallel:
	// RefTypes[i] names what RefValues[i] is an id or a hash of.
	RefTypes  []string `lw:"auditEventRefTypes,stringArray"`
	RefValues []string `lw:"auditEventRefValues,stringArray"`
	// AttrKeys and AttrValues are bounded short codes, parallel.
	AttrKeys   []string `lw:"auditEventAttrKeys,stringArray"`
	AttrValues []string `lw:"auditEventAttrValues,stringArray"`
}

// The outcomes, the values of AuditEvent.Outcome.
const (
	OutcomeOk     = "ok"
	OutcomeDenied = "denied"
	OutcomeFailed = "failed"
)

// Who vouched for a principal, the values of AuditEvent.PrincipalBy.
const (
	// PrincipalBySystem is an authenticated principal from an API layer.
	PrincipalBySystem = "system"
	// PrincipalByEnv is a principal a deployment variable asserted.
	PrincipalByEnv = "env"
	// PrincipalByOs is the OS user of the process.
	PrincipalByOs = "os"
	// PrincipalByNone is no principal: the context carried none.
	PrincipalByNone = "none"
)

// The recorder's own domain and its actions (ADR-0296 §SD4): rows the
// recorder writes about itself, in the retention class RetentionTrail.
const (
	TrailDomain        = "trail"
	ActionAuditGap     = "audit-gap"
	ActionAuditInvalid = "audit-invalid"
	RetentionTrail     = "trail"
)

// The bounds Validate applies.
const (
	// MaxNameRunes bounds a domain, an action, a retention class, a
	// reference type and an attribute key.
	MaxNameRunes = 64
	// MaxValueRunes bounds a principal, a purpose, a node, a reference
	// value and an attribute value.
	MaxValueRunes = 256
	// MaxRefs and MaxAttrs bound the parallel lists.
	MaxRefs  = 32
	MaxAttrs = 16
)

// Validate says whether the row is within ADR-0296 §SD1's bounds: names
// are lower-spinal [naming.StylableName]s, enumerations hold one of their
// constants, values are non-empty valid UTF-8 under their bound, the
// parallel lists agree in length, and a principal and its PrincipalBy
// agree. The recorder replaces a row that fails with an audit-invalid row.
func (inst AuditEvent) Validate() (err error) {
	if err = checkName("domain", inst.Domain); err != nil {
		return
	}
	if err = checkName("action", inst.Action); err != nil {
		return
	}
	if err = checkName("retention", inst.Retention); err != nil {
		return
	}
	switch inst.Outcome {
	case OutcomeOk, OutcomeDenied, OutcomeFailed:
	default:
		return eh.Errorf("outcome %q is not ok, denied or failed", inst.Outcome)
	}
	switch inst.PrincipalBy {
	case PrincipalBySystem, PrincipalByEnv, PrincipalByOs:
		if !inst.Principal.Has {
			return eh.Errorf("principal-by %q with no principal", inst.PrincipalBy)
		}
	case PrincipalByNone:
		if inst.Principal.Has {
			return eh.Errorf("principal-by none with a principal")
		}
	default:
		return eh.Errorf("principal-by %q is not system, env, os or none", inst.PrincipalBy)
	}
	if inst.Principal.Has {
		if err = checkValue("principal", inst.Principal.Val); err != nil {
			return
		}
	}
	if inst.Purpose.Has {
		if err = checkValue("purpose", inst.Purpose.Val); err != nil {
			return
		}
	}
	if inst.Node.Has {
		if err = checkValue("node", inst.Node.Val); err != nil {
			return
		}
	}
	if len(inst.RefTypes) != len(inst.RefValues) {
		return eh.Errorf("%d reference types for %d reference values", len(inst.RefTypes), len(inst.RefValues))
	}
	if len(inst.RefTypes) > MaxRefs {
		return eh.Errorf("%d references, more than %d", len(inst.RefTypes), MaxRefs)
	}
	for i, ty := range inst.RefTypes {
		if err = checkName("reference type "+strconv.Itoa(i), ty); err != nil {
			return
		}
		if err = checkValue("reference value "+strconv.Itoa(i), inst.RefValues[i]); err != nil {
			return
		}
	}
	if len(inst.AttrKeys) != len(inst.AttrValues) {
		return eh.Errorf("%d attribute keys for %d attribute values", len(inst.AttrKeys), len(inst.AttrValues))
	}
	if len(inst.AttrKeys) > MaxAttrs {
		return eh.Errorf("%d attributes, more than %d", len(inst.AttrKeys), MaxAttrs)
	}
	for i, k := range inst.AttrKeys {
		if err = checkName("attribute key "+strconv.Itoa(i), k); err != nil {
			return
		}
		if err = checkValue("attribute value "+strconv.Itoa(i), inst.AttrValues[i]); err != nil {
			return
		}
	}
	return nil
}

// checkName is a lower-spinal StylableName under MaxNameRunes.
func checkName(what string, s string) (err error) {
	if err = checkValue(what, s); err != nil {
		return
	}
	if utf8.RuneCountInString(s) > MaxNameRunes {
		return eh.Errorf("%s %q is longer than %d runes", what, s, MaxNameRunes)
	}
	n := naming.StylableName(s)
	if verr := naming.ValidateStylableName(n); verr != nil {
		return eh.Errorf("%s %q is not a stylable name: %w", what, s, verr)
	}
	if !n.IsUsingStyle(naming.DefaultNamingStyle) {
		return eh.Errorf("%s %q is not in %s", what, s, naming.DefaultNamingStyle.String())
	}
	return nil
}

// checkValue is non-empty valid UTF-8 under MaxValueRunes.
func checkValue(what string, s string) (err error) {
	if s == "" {
		return eh.Errorf("%s is empty", what)
	}
	if !utf8.ValidString(s) {
		return eh.Errorf("%s is not valid UTF-8", what)
	}
	if utf8.RuneCountInString(s) > MaxValueRunes {
		return eh.Errorf("%s is longer than %d runes", what, MaxValueRunes)
	}
	return nil
}
