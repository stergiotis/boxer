package audit_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/audit"
)

func validEvent() audit.AuditEvent {
	return audit.AuditEvent{
		Domain: "dmdm", Action: "vault-resolve", Outcome: audit.OutcomeOk,
		Principal: option.Some("p-1"), PrincipalBy: audit.PrincipalByEnv, OriginBy: "envelope",
		PrincipalVia: option.Some(audit.PrincipalViaToken),
		Purpose:      option.Some("art-15"), Node: option.Some("cell-a"),
		Subject: 42, Retention: "disclosure",
		RefTypes: []string{"vaultref", "slot"}, RefValues: []string{"v-1", "s-1"},
		AttrKeys: []string{"count"}, AttrValues: []string{"3"},
	}
}

// Validate holds ADR-0296 §SD1's bounds: names, enumerations, values,
// parallel lists, the agreement of a principal with its PrincipalBy and
// PrincipalVia, and the origin's attestation (ADR-0327 §SD6).
func TestAuditEventValidation(t *testing.T) {
	require.NoError(t, validEvent().Validate())
	cases := map[string]func(e *audit.AuditEvent){
		"empty domain":          func(e *audit.AuditEvent) { e.Domain = "" },
		"camel action":          func(e *audit.AuditEvent) { e.Action = "vaultResolve" },
		"snake retention":       func(e *audit.AuditEvent) { e.Retention = "long_term" },
		"long name":             func(e *audit.AuditEvent) { e.Action = strings.Repeat("a", audit.MaxNameRunes+1) },
		"unknown outcome":       func(e *audit.AuditEvent) { e.Outcome = "maybe" },
		"unknown principal-by":  func(e *audit.AuditEvent) { e.PrincipalBy = "friend" },
		"unknown origin-by":     func(e *audit.AuditEvent) { e.OriginBy = "friend" },
		"empty origin-by":       func(e *audit.AuditEvent) { e.OriginBy = "" },
		"none with principal":   func(e *audit.AuditEvent) { e.PrincipalBy = audit.PrincipalByNone },
		"env without principal": func(e *audit.AuditEvent) { e.Principal = option.None[string]() },
		"empty principal":       func(e *audit.AuditEvent) { e.Principal = option.Some("") },
		"camel principal-via":   func(e *audit.AuditEvent) { e.PrincipalVia = option.Some("bearerToken") },
		"empty principal-via":   func(e *audit.AuditEvent) { e.PrincipalVia = option.Some("") },
		"via without principal": func(e *audit.AuditEvent) {
			e.Principal, e.PrincipalBy = option.None[string](), audit.PrincipalByNone
		},
		"long value":   func(e *audit.AuditEvent) { e.RefValues[0] = strings.Repeat("x", audit.MaxValueRunes+1) },
		"bad utf8":     func(e *audit.AuditEvent) { e.AttrValues[0] = "\xff" },
		"ragged refs":  func(e *audit.AuditEvent) { e.RefValues = e.RefValues[:1] },
		"ragged attrs": func(e *audit.AuditEvent) { e.AttrKeys = nil },
		"bad ref type": func(e *audit.AuditEvent) { e.RefTypes[1] = "Slot" },
		"too many attrs": func(e *audit.AuditEvent) {
			for range audit.MaxAttrs {
				e.AttrKeys = append(e.AttrKeys, "k")
				e.AttrValues = append(e.AttrValues, "v")
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := validEvent()
			mutate(&e)
			assert.Error(t, e.Validate())
		})
	}
	// Every outcome of the vocabulary is accepted.
	for _, o := range []string{audit.OutcomeOk, audit.OutcomeDenied, audit.OutcomeFailed, audit.OutcomeTimeout, audit.OutcomeError} {
		e := validEvent()
		e.Outcome = o
		assert.NoError(t, e.Validate(), o)
	}
	// An emitter may name its own mechanism.
	e := validEvent()
	e.PrincipalVia = option.Some("oidc")
	assert.NoError(t, e.Validate())
	// Optional values may be absent, and lists empty.
	e = validEvent()
	e.Principal, e.PrincipalBy, e.PrincipalVia = option.None[string](), audit.PrincipalByNone, option.None[string]()
	e.Purpose, e.Node = option.None[string](), option.None[string]()
	e.RefTypes, e.RefValues, e.AttrKeys, e.AttrValues = nil, nil, nil, nil
	assert.NoError(t, e.Validate())
}

// Bounded makes any text an attribute value.
func TestBounded(t *testing.T) {
	assert.Equal(t, "-", audit.Bounded(""))
	assert.Equal(t, "ok", audit.Bounded("ok"))
	assert.Len(t, []rune(audit.Bounded(strings.Repeat("y", audit.MaxValueRunes+5))), audit.MaxValueRunes)
	assert.NoError(t, audit.CheckValue("x", audit.Bounded("\xffbad")))
}
