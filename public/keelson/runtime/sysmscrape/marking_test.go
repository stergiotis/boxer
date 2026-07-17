package sysmscrape_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
)

// TestComponentEnvVarContract pins the cross-layer literal: the proc
// collector sits below keelson and cannot import topo, so it carries the
// ADR-0126 mark variable name as its own default. This package imports
// both sides; the two must never drift.
func TestComponentEnvVarContract(t *testing.T) {
	if proc.DefaultComponentEnvVar != topo.EnvVarName {
		t.Fatalf("proc.DefaultComponentEnvVar (%q) != topo.EnvVarName (%q)",
			proc.DefaultComponentEnvVar, topo.EnvVarName)
	}
}
