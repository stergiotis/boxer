package attention

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The rows the queue writes are within the audit event's bounds: a row
// outside them is replaced by an audit-invalid row (ADR-0296).
func TestAuditRowsValidate(t *testing.T) {
	it := Item{Key: "agent/req/req-1", Kind: KindWidening, App: "chat", Conversation: "c1"}
	rows := []trail.AuditEvent{actRow(ActAcknowledge, it), actRow(ActShelve, it), actRow(ActUnshelve, it),
		levelRow(Change{Conversation: "c1", Key: Key{Kind: KindQuestion}, Level: LevelCritical, Author: AuthorPerson}),
		levelRow(Change{Key: Key{Kind: "asked", Effect: app.OperationEffectRun, App: "play"}, Author: AuthorPerson})}
	for _, row := range rows {
		// What the recorder fills at write.
		row.PrincipalBy, row.OriginBy = trail.PrincipalByNone, evidence.OriginByHost.String()
		assert.NoError(t, row.Validate(), row.Action)
	}
}

// A level row carries the level the key had before the change as from,
// and the table it came from as from-by: the default for a key nothing
// overrode, else the person's or the conversation's override (ADR-0296,
// the Update of 2026-10-31).
func TestLevelRowCarriesFrom(t *testing.T) {
	var l Levels
	var got []Change
	l.SetOnWaitingChange(func(ch Change) { got = append(got, ch) })
	k := Key{Kind: KindQuestion}
	assert.NoError(t, l.Set(AuthorPerson, "", "", k, LevelCritical))
	assert.NoError(t, l.Set(AuthorPerson, "", "", k, LevelYourTurn))
	assert.NoError(t, l.Set(AuthorPerson, "", "c1", k, LevelCritical))
	assert.NoError(t, l.Clear(AuthorPerson, "", "c1", k))
	want := []struct{ from, fromBy, ref string }{
		{"your turn", "default", "person||question|*|*"},
		{"critical", "person", "person||question|*|*"},
		{"your turn", "person", "conversation|c1|question|*|*"},
		{"critical", "conversation", "conversation|c1|question|*|*"},
	}
	if !assert.Len(t, got, len(want)) {
		return
	}
	for i, ch := range got {
		row := levelRow(ch)
		attrs := map[string]string{}
		for j, key := range row.AttrKeys {
			attrs[key] = row.AttrValues[j]
		}
		assert.Equal(t, want[i].from, attrs[trail.AttrFrom], i)
		assert.Equal(t, want[i].fromBy, attrs[trail.AttrFromBy], i)
		assert.Equal(t, auditRefLevelKey, row.RefTypes[0], i, "the override is the first reference")
		assert.Equal(t, want[i].ref, row.RefValues[0], i)
		row.PrincipalBy, row.OriginBy = trail.PrincipalByNone, evidence.OriginByHost.String()
		assert.NoError(t, row.Validate(), i)
	}
}
