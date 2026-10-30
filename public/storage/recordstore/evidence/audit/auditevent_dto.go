package audit

import (
	"strconv"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// AuditEvent is one event in a consumer's own records-of-processing
// vocabulary (ADR-0296 §SD1): what was done, to whose data, by whom, with
// what outcome. Everything a consumer varies — domain and action names,
// reference types, retention classes — is a value here, so a new action is
// a new constant and no membership. Who wrote it is the row's origin
// component; OriginBy says how that origin was established (ADR-0327
// (proposed) §SD6), as PrincipalBy does for the principal.
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
	// PrincipalVia is the mechanism that established Principal, a
	// lower-spinal name the emitter chooses from an open vocabulary (the
	// PrincipalVia constants, or its own); absent when the emitter does
	// not say, and always when there is no principal.
	PrincipalVia option.Option[string] `lw:"auditEventPrincipalVia,symbol"`
	// OriginBy is how the row's origin was established, one of
	// evidence.OriginByE's names: the host's own service wrote it, the host
	// took it from a bus envelope's sender, or the writing process stated
	// it (ADR-0327 (proposed) §SD6).
	OriginBy string `lw:"auditEventOriginBy,symbol"`
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

// The outcomes, the values of AuditEvent.Outcome. Timeout and error are
// failures of a known shape — waited past a deadline, failed otherwise —
// for an emitter that tells them apart; failed is any failure else.
const (
	OutcomeOk      = "ok"
	OutcomeDenied  = "denied"
	OutcomeFailed  = "failed"
	OutcomeTimeout = "timeout"
	OutcomeError   = "error"
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

// Mechanisms that establish a principal, values of AuditEvent.PrincipalVia.
// The vocabulary is open: an emitter may name its own mechanism.
const (
	// PrincipalViaPeer is the kernel's credential of the connecting process
	// on a Unix socket.
	PrincipalViaPeer = "peer"
	// PrincipalViaMtls is a client certificate that verified against the
	// listener's authorities.
	PrincipalViaMtls = "mtls"
	// PrincipalViaToken is a bearer token the service holds and compared.
	PrincipalViaToken = "token"
	// PrincipalViaSession is a signed session the service verified.
	PrincipalViaSession = "session"
	// PrincipalViaUpstream is a trusted proxy or API that asserted the
	// principal; the request came from its address.
	PrincipalViaUpstream = "upstream"
)

// The recorder's own domain and its actions (ADR-0296 §SD4): rows a
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
// parallel lists agree in length, a principal and its PrincipalBy agree,
// and a PrincipalVia is a name and only beside a principal. A recorder replaces a row that fails with an audit-invalid row.
func (inst AuditEvent) Validate() (err error) {
	if err = CheckName("domain", inst.Domain); err != nil {
		return
	}
	if err = CheckName("action", inst.Action); err != nil {
		return
	}
	if err = CheckName("retention", inst.Retention); err != nil {
		return
	}
	switch inst.Outcome {
	case OutcomeOk, OutcomeDenied, OutcomeFailed, OutcomeTimeout, OutcomeError:
	default:
		return eh.Errorf("outcome %q is not ok, denied, failed, timeout or error", inst.Outcome)
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
	if inst.PrincipalVia.Has {
		if !inst.Principal.Has {
			return eh.Errorf("principal-via %q with no principal", inst.PrincipalVia.Val)
		}
		if err = CheckName("principal-via", inst.PrincipalVia.Val); err != nil {
			return
		}
	}
	if _, ok := evidence.ParseOriginBy(inst.OriginBy); !ok {
		return eh.Errorf("origin-by %q is not self, envelope or host", inst.OriginBy)
	}
	if inst.Principal.Has {
		if err = CheckValue("principal", inst.Principal.Val); err != nil {
			return
		}
	}
	if inst.Purpose.Has {
		if err = CheckValue("purpose", inst.Purpose.Val); err != nil {
			return
		}
	}
	if inst.Node.Has {
		if err = CheckValue("node", inst.Node.Val); err != nil {
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
		if err = CheckName("reference type "+strconv.Itoa(i), ty); err != nil {
			return
		}
		if err = CheckValue("reference value "+strconv.Itoa(i), inst.RefValues[i]); err != nil {
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
		if err = CheckName("attribute key "+strconv.Itoa(i), k); err != nil {
			return
		}
		if err = CheckValue("attribute value "+strconv.Itoa(i), inst.AttrValues[i]); err != nil {
			return
		}
	}
	return nil
}

// CheckName says s is a lower-spinal StylableName under MaxNameRunes;
// what names it in the error.
func CheckName(what string, s string) (err error) {
	if err = CheckValue(what, s); err != nil {
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

// CheckValue says s is non-empty valid UTF-8 under MaxValueRunes; what
// names it in the error.
func CheckValue(what string, s string) (err error) {
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

// Bounded is s as an attribute value: valid UTF-8, non-empty, under the
// bound — what a recorder writes about a row it could not take as it was.
func Bounded(s string) (out string) {
	out = toValidUTF8(s)
	if out == "" {
		return "-"
	}
	if utf8.RuneCountInString(out) > MaxValueRunes {
		r := []rune(out)
		out = string(r[:MaxValueRunes])
	}
	return
}

func toValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		out = append(out, r)
	}
	return string(out)
}
