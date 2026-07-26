package chlocalbroker

import (
	"io"
	"os"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// OpenDatasetPlaintext streams the decryption of a sealed dataset
// (ADR-0134 §SD3 revised, ADR-0145 §SD1): the broker is the decrypt
// executor, so it resolves the key from its own KeyStore by handle, opens
// the ciphertext, and returns a reader over the plaintext Arrow stream. The
// key never leaves the process.
//
// This is how the introspection /table endpoint serves a sealed dataset
// over loopback HTTP, and since ADR-0145 §SD2 it is the ONLY decrypt path.
// The caller must Close the returned reader.
func (inst *Service) OpenDatasetPlaintext(ref adhocdata.Ref) (rc io.ReadCloser, err error) {
	key, ok := inst.keys.LookupDatasetKey(ref.Handle)
	if !ok {
		return nil, eh.Errorf("chlocalbroker: no key registered for dataset %q", ref.Handle)
	}
	f, err := os.Open(ref.Path)
	if err != nil {
		return nil, eh.Errorf("chlocalbroker: open dataset %q: %w", ref.Handle, err)
	}
	ar, err := adhocdata.NewReader(f, key)
	if err != nil {
		_ = f.Close()
		return nil, eh.Errorf("chlocalbroker: decrypt reader %q: %w", ref.Handle, err)
	}
	return &datasetReadCloser{r: ar, c: f}, nil
}

// The broker is ADR-0134 §SD2's decrypt executor, and this is the seam that
// says so.
var _ adhocdata.DecryptorI = (*Service)(nil)

// datasetReadCloser reads decrypted plaintext from the AEAD reader and
// closes the underlying ciphertext file.
type datasetReadCloser struct {
	r io.Reader
	c io.Closer
}

func (inst *datasetReadCloser) Read(p []byte) (n int, err error) { return inst.r.Read(p) }
func (inst *datasetReadCloser) Close() (err error)               { return inst.c.Close() }
