//go:build llm_generated_opus47

package factsstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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

func TestInMemoryFactsStore_State_LatestWins(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: []byte("v1"), Ts: time.Now()})
	require.NoError(t, err)
	_, err = s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: []byte("v2"), Ts: time.Now()})
	require.NoError(t, err)
	got, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []byte("v2"), got)
}

func TestInMemoryFactsStore_State_MissingKey(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, found, err := s.LatestState("nope", "absent")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryFactsStore_DeleteState_Tombstones(t *testing.T) {
	s := NewInMemoryFactsStore()
	_, err := s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: []byte("v1"), Ts: time.Now()})
	require.NoError(t, err)
	err = s.DeleteState("play", "tabs")
	require.NoError(t, err)
	_, found, err := s.LatestState("play", "tabs")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestInMemoryFactsStore_WriteState_DefensiveCopy(t *testing.T) {
	s := NewInMemoryFactsStore()
	v := []byte("hello")
	_, err := s.WriteState(StateRow{AppId: "play", Key: "tabs", Value: v, Ts: time.Now()})
	require.NoError(t, err)
	v[0] = 'X'
	got, _, _ := s.LatestState("play", "tabs")
	assert.Equal(t, "hello", string(got))
}

func TestInMemoryFactsStore_StateSeparation_TwoApps(t *testing.T) {
	s := NewInMemoryFactsStore()
	require.NoError(t, mustWrite(s, "play", "tabs", []byte("p")))
	require.NoError(t, mustWrite(s, "imztop", "tabs", []byte("i")))
	got, _, _ := s.LatestState("play", "tabs")
	assert.Equal(t, "p", string(got))
	got, _, _ = s.LatestState("imztop", "tabs")
	assert.Equal(t, "i", string(got))
}

func mustWrite(s FactsStoreI, appId app.AppIdT, key string, value []byte) (err error) {
	_, err = s.WriteState(StateRow{AppId: appId, Key: key, Value: value, Ts: time.Now()})
	return
}
