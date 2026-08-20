package ladingingest

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// RecordPolicy writes a mount's *declared* policy to `boxer.facts`.
//
// It is deliberately not part of [Snapshot]. The two record different things:
// this is runtime state — edited by whoever registers or reconfigures a mount,
// outliving every snapshot taken under it — while a snapshot records the
// policy it *applied* on its own root row, so it stays interpretable after
// this record changes. Calling it on every walk would append a row per walk
// and turn a registry into a log.
//
// The store is append-only, so a change is a new row and the previous
// declaration stays readable; `Latest` is the current one.
//
// name is the mount's human name — resolving a name to an id belongs to the
// application, and this is the field a name-as-sugar macro would read.
// storeName is which set of tables the mount's rows live in, the unit a
// capability grant covers.
func RecordPolicy(ctx context.Context, st *ladingpolicy.PolicyStore, mount identifier.TaggedId, policy Policy, name string, storeName string) (err error) {
	err = policy.check()
	if err != nil {
		return
	}
	if st == nil {
		return eh.Errorf("ladingingest: no policy store")
	}
	if !mount.IsValid() {
		return eh.Errorf("ladingingest: mount id is not a valid tagged id")
	}
	err = st.Begin(mount.Value(), time.Now().UTC(), ladingpolicy.PolicyEnvelope{
		NaturalKey: []byte(name),
	}).AddLadingMount(ladingpolicy.LadingMount{
		Kind:      "mount",
		Name:      name,
		Store:     storeName,
		TtlClass:  policy.Ttl.String(),
		TextRule:  policy.Text.String(),
		InlineMax: policy.InlineMax,
	}).Commit()
	if err != nil {
		return eh.Errorf("ladingingest: buffer policy record: %w", err)
	}
	_, err = st.Flush(ctx)
	if err != nil {
		return eh.Errorf("ladingingest: flush policy record: %w", err)
	}
	return
}
