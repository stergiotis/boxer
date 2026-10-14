package trail_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// The row digests and the seal digest of fixed trail rows are pinned: a
// change to a component's DTO, its binding or the canonical wire that
// moves them moves every seal stream already written, and must be a
// decision, not a side effect. The values were taken before the origin
// component moved to the audit package (2026-10-13) and held after it.
func TestTrailDigestsArePinned(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	st := trail.NewTrailStore(nil, nil, trail.TrailStoreConfig{})
	defer st.Close()
	var sealed []evidence.SealedRow
	digest := func(name string, id uint64, f func(b *trail.TrailEntityBuilder)) string {
		b := st.Begin(id, at, trail.TrailEnvelope{NaturalKey: []byte(name)})
		b.AddOrigin(trail.Origin{Id: id, Run: "run-1", App: "app-x", Instance: 7})
		f(b)
		ent, _ := b.Entity()
		require.NoError(t, b.Commit())
		item, err := st.CanonicalItem(&ent)
		require.NoError(t, err)
		d := trail.TrailSealScheme.RowDigest(item)
		sealed = append(sealed, evidence.SealedRow{Id: id, Digest: d})
		return d
	}
	assert.Equal(t, "372180d368092e6ce466f9681b9a0f2dd564d6a11d01a2b7a1174a82d2193e8e", digest("origin-only", 1, func(b *trail.TrailEntityBuilder) {}))
	assert.Equal(t, "d6f0570e680ac57982bad1e41ef82e808db2abda68d2b416822d8a5883813938", digest("event", 2, func(b *trail.TrailEntityBuilder) {
		b.AddAuditEvent(trail.AuditEvent{Id: 2, Kind: "auditEvent", Domain: "d", Action: "a", Outcome: "ok", Principal: option.Some("p"), PrincipalBy: "system",
			OriginBy: "self", Subject: 9, Retention: "r", RefTypes: []string{"x"}, RefValues: []string{"y"}})
	}))
	assert.Equal(t, "a6cb700290c4ff24680c738efb8e8c57b231e9e9de3c92f1864dfc51d85e9509", digest("conv", 3, func(b *trail.TrailEntityBuilder) {
		b.AddConversation(trail.Conversation{Id: 3, Conversation: "c", Turn: option.Some("t")})
		b.AddDelegation(trail.Delegation{Id: 3, Task: "task", Epoch: 2})
	}))
	assert.Equal(t, "eb1c4f11bcb53daaba5a0a74fc3e055ba7ea2bdca003ebb0de3cab77368b26e5", trail.SealOf(trail.TrailSealScheme, "run-1", 0, "", sealed).Digest)
	st.DiscardPending()
}
