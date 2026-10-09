package actor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/identity/identgen/mem"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

func newRecorder(t *testing.T) *Recorder {
	t.Helper()
	gen, err := mem.NewIdInternalizer(2, 1024)
	require.NoError(t, err)
	return NewRecorder(gen, NewStoreSink(NewActorStore(nil, nil, ActorStoreConfig{})))
}

func current(s recordstore.ReferenceStamper, ctx context.Context) (ids []identifier.TaggedId, err error) {
	for id, cerr := range s.Current(ctx) {
		if cerr != nil {
			return nil, cerr
		}
		ids = append(ids, id)
	}
	return
}

func TestStamperRefusesWithoutPrincipal(t *testing.T) {
	s := newRecorder(t).Stamper()
	_, err := current(s, context.Background())
	require.ErrorIs(t, err, ErrNoPrincipal)
	_, err = current(s, callident.WithCallIdentity(context.Background(), callident.CallIdentity{
		Origin: callident.Origin{App: "a"}, Claims: callident.Claims{Purpose: "x"},
	}))
	require.ErrorIs(t, err, ErrNoPrincipal, "an origin and a purpose without a principal attribute nothing")
}

func TestStamperInternsTheClaimedTriple(t *testing.T) {
	r := newRecorder(t)
	s := r.Stamper()
	with := func(run, principal, purpose string) context.Context {
		return callident.WithCallIdentity(context.Background(), callident.CallIdentity{
			Origin: callident.Origin{Run: run, App: "example.vault"},
			Claims: callident.Claims{Principal: principal, Purpose: purpose},
		})
	}
	a, err := current(s, with("r1", "p:1", "dsar"))
	require.NoError(t, err)
	require.Len(t, a, 1)
	b, err := current(s, with("r2", "p:1", "dsar"))
	require.NoError(t, err)
	assert.Equal(t, a, b, "the run is not part of the key")
	c, err := current(s, with("r1", "p:1", "billing"))
	require.NoError(t, err)
	assert.NotEqual(t, a, c)

	got, found, err := r.Resolve(context.Background(), a[0])
	require.NoError(t, err)
	require.True(t, found, "a locally interned descriptor resolves before flush")
	assert.Equal(t, Actor{ID: uint64(a[0]), Principal: "p:1", Purpose: "dsar", App: "example.vault"}, got)
}

func TestKeyIsUnambiguous(t *testing.T) {
	assert.NotEqual(t, key(Actor{Principal: "ab", Purpose: "c"}), key(Actor{Principal: "a", Purpose: "bc"}))
}

func TestStampersRefusedOnDescriptorStore(t *testing.T) {
	require.Panics(t, func() {
		NewActorStore(nil, nil, ActorStoreConfig{Stampers: []recordstore.ReferenceStamper{nil}})
	})
}
