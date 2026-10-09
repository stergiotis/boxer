package trail_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
)

func validEvent() trail.AuditEvent {
	return trail.AuditEvent{
		Domain: "dmdm", Action: "vault-resolve", Outcome: trail.OutcomeOk,
		Principal: option.Some("p-1"), PrincipalBy: trail.PrincipalByEnv,
		Purpose: option.Some("art-15"), Node: option.Some("cell-a"),
		Subject: 42, Retention: "disclosure",
		RefTypes: []string{"vaultref", "slot"}, RefValues: []string{"v-1", "s-1"},
		AttrKeys: []string{"count"}, AttrValues: []string{"3"},
	}
}

// Validate holds ADR-0296 §SD1's bounds: names, enumerations, values,
// parallel lists, and the agreement of a principal with its PrincipalBy.
func TestAuditEventValidation(t *testing.T) {
	require.NoError(t, validEvent().Validate())
	cases := map[string]func(e *trail.AuditEvent){
		"empty domain":          func(e *trail.AuditEvent) { e.Domain = "" },
		"camel action":          func(e *trail.AuditEvent) { e.Action = "vaultResolve" },
		"snake retention":       func(e *trail.AuditEvent) { e.Retention = "long_term" },
		"long name":             func(e *trail.AuditEvent) { e.Action = strings.Repeat("a", trail.MaxNameRunes+1) },
		"unknown outcome":       func(e *trail.AuditEvent) { e.Outcome = "maybe" },
		"unknown principal-by":  func(e *trail.AuditEvent) { e.PrincipalBy = "friend" },
		"none with principal":   func(e *trail.AuditEvent) { e.PrincipalBy = trail.PrincipalByNone },
		"env without principal": func(e *trail.AuditEvent) { e.Principal = option.None[string]() },
		"empty principal":       func(e *trail.AuditEvent) { e.Principal = option.Some("") },
		"long value":            func(e *trail.AuditEvent) { e.RefValues[0] = strings.Repeat("x", trail.MaxValueRunes+1) },
		"bad utf8":              func(e *trail.AuditEvent) { e.AttrValues[0] = "\xff" },
		"ragged refs":           func(e *trail.AuditEvent) { e.RefValues = e.RefValues[:1] },
		"ragged attrs":          func(e *trail.AuditEvent) { e.AttrKeys = nil },
		"bad ref type":          func(e *trail.AuditEvent) { e.RefTypes[1] = "Slot" },
		"too many attrs": func(e *trail.AuditEvent) {
			for range trail.MaxAttrs {
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
	// Optional values may be absent, and lists empty.
	e := validEvent()
	e.Principal, e.PrincipalBy = option.None[string](), trail.PrincipalByNone
	e.Purpose, e.Node = option.None[string](), option.None[string]()
	e.RefTypes, e.RefValues, e.AttrKeys, e.AttrValues = nil, nil, nil, nil
	assert.NoError(t, e.Validate())
}
