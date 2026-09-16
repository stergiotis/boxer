package introspect

import (
	"bytes"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sealedStub is the smallest EncryptedDatasetI: what the registry and its
// sealed predicate see of a dataset, with none of the capability behind it.
type sealedStub struct {
	name      string
	schema    *arrow.Schema
	structure string
	revision  uint64
	plaintext []byte
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

func (s *sealedStub) Name() string              { return s.name }
func (s *sealedStub) Freshness() FreshnessClass { return FreshnessLive }
func (s *sealedStub) Schema() *arrow.Schema     { return s.schema }
func (s *sealedStub) Structure() string         { return s.structure }
func (s *sealedStub) Revision() uint64          { return s.revision }
func (s *sealedStub) Snapshot(Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (s *sealedStub) Open() (io.ReadSeekCloser, uint64, error) {
	return nopCloser{bytes.NewReader(s.plaintext)}, s.revision, nil
}

var _ EncryptedDatasetI = (*sealedStub)(nil)

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	require.NoError(t, r.Register(&sealedStub{name: "adhoc_x", schema: schema, structure: "id Int64", revision: 1}))
	_, ok := r.Lookup("adhoc_x")
	assert.True(t, ok)

	assert.True(t, r.Unregister("adhoc_x"))
	_, ok = r.Lookup("adhoc_x")
	assert.False(t, ok)
	assert.False(t, r.Unregister("adhoc_x"), "second unregister is a no-op")
}

// TestRegistryIsSealed is the derivation's ground truth: a name either
// resolves to a sealed provider in this registry or it does not. Everything
// upstream — the dispatch label, the refusals — rests on this answer, so it
// must not be a guess about the name's shape.
func TestRegistryIsSealed(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&sealedStub{name: "adhoc_secret",
		schema:    arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil),
		structure: "v Int64", revision: 1}))

	assert.True(t, reg.IsSealed("adhoc_secret"))
	assert.False(t, reg.IsSealed("nothing_registered"),
		"an unknown name is not sealed — it is simply not here")
	assert.False(t, reg.IsSealed(""), "and neither is nothing")
}

// TestLocalSealedPredicate covers the discovery hook a co-resident
// dispatcher reads, including the case where no plane runs.
func TestLocalSealedPredicate(t *testing.T) {
	t.Cleanup(func() { SetLocalSealedPredicate(nil) })

	SetLocalSealedPredicate(nil)
	assert.False(t, IsLocalSealed("adhoc_secret"),
		"with no plane there is no sealed data to confine")

	SetLocalSealedPredicate(func(name string) (yes bool) { return name == "adhoc_secret" })
	assert.True(t, IsLocalSealed("adhoc_secret"))
	assert.False(t, IsLocalSealed("env"))

	SetLocalSealedPredicate(nil)
	assert.False(t, IsLocalSealed("adhoc_secret"), "clearing it is how a stopped plane says so")
}
