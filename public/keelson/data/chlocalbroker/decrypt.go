package chlocalbroker

import (
	"io"
	"os"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// OpenDatasetPlaintext serves the decryption of a sealed dataset
// (ADR-0134 §SD3 revised, ADR-0145 §SD1): the broker is the decrypt
// executor, so it resolves the key from its own KeyStore by handle, opens
// the ciphertext, and returns a seekable reader over the plaintext Arrow
// stream (random access from the chunk geometry — what lets the /table
// endpoint honor HTTP ranges). The key never leaves the process.
//
// This is how the introspection /table endpoint serves a sealed dataset
// over loopback HTTP, and since ADR-0145 §SD2 it is the ONLY decrypt path.
// The caller must Close the returned reader.
func (inst *Service) OpenDatasetPlaintext(ref adhocdata.Ref) (rc adhocdata.PlaintextI, err error) {
	key, ok := inst.keys.LookupDatasetKey(ref.Handle)
	if !ok {
		return nil, eb.Build().Str("handle", ref.Handle).Errorf("chlocalbroker: no key registered for dataset")
	}
	f, err := os.Open(ref.Path)
	if err != nil {
		return nil, eb.Build().Str("handle", ref.Handle).Errorf("chlocalbroker: open dataset: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, eb.Build().Str("handle", ref.Handle).Errorf("chlocalbroker: stat dataset: %w", err)
	}
	ar, err := adhocdata.NewSeekableReader(f, st.Size(), key)
	if err != nil {
		_ = f.Close()
		return nil, eb.Build().Str("handle", ref.Handle).Errorf("chlocalbroker: decrypt reader: %w", err)
	}
	return &datasetReadCloser{SeekableReader: ar, c: f}, nil
}

// The broker is ADR-0134 §SD2's decrypt executor, and this is the seam that
// says so.
var _ adhocdata.DecryptorI = (*Service)(nil)

// datasetReadCloser reads decrypted plaintext from the seekable AEAD
// reader and closes the underlying ciphertext file.
type datasetReadCloser struct {
	*adhocdata.SeekableReader
	c io.Closer
}

func (inst *datasetReadCloser) Close() (err error) { return inst.c.Close() }
