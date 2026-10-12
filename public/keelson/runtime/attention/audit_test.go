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
