package factsstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryFactsStore_WriteGrant_AssignsId(t *testing.T) {
	s := NewInMemoryFactsStore()
	id1, err := s.WriteGrant(GrantRow{AppId: "a", Pattern: "x"})
	require.NoError(t, err)
	id2, err := s.WriteGrant(GrantRow{AppId: "b", Pattern: "y"})
	require.NoError(t, err)
	assert.NotZero(t, id1)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, s.Grants(), 2)
}

func TestInMemoryFactsStore_WriteAudit(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteAudit(AuditRow{AppId: "a", Subject: "ch.query.boxer", Result: "ok", LatencyMs: 5})
	require.NoError(t, err)
	rows := s.AuditRows()
	require.Len(t, rows, 1)
	assert.Equal(t, "ch.query.boxer", rows[0].Subject)
}

// Workingset trail (ADR-0148 §SD6) — persist-state semantics: append-only,
// reverse-scan latest, tombstone delete, isolated per (app, name).

// ListWorkingsets (ADR-0148 §SD7) — the stored set, not the trail.

// Column-width overrides (ADR-0151). The behaviours asserted here are the
// contract chstore's implementation must match; the CH-side tests mirror
// them so the two backends cannot drift silently.
