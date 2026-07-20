package chlocalbroker

import (
	"io"
	"os"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// OpenDatasetPlaintext streams the decryption of an ad-hoc dataset file
// (ADR-0134 §SD3, revised): the broker is the decrypt executor (K2), so it
// resolves the dataset's key from its own KeyStore by handle, opens the
// ciphertext at path, and returns a reader over the plaintext Arrow
// stream. The key never leaves the process. This is how the introspection
// /table endpoint serves an ad-hoc dataset over loopback HTTP — the same
// AEAD reader the pipe path uses, without the named pipe. The caller must
// Close the returned reader.
func (inst *Service) OpenDatasetPlaintext(handle, path string) (rc io.ReadCloser, err error) {
	key, ok := inst.keys.LookupDatasetKey(handle)
	if !ok {
		return nil, eh.Errorf("chlocalbroker: no key registered for dataset %q", handle)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, eh.Errorf("chlocalbroker: open dataset %q: %w", handle, err)
	}
	ar, err := adhocdata.NewReader(f, key)
	if err != nil {
		_ = f.Close()
		return nil, eh.Errorf("chlocalbroker: decrypt reader %q: %w", handle, err)
	}
	return &datasetReadCloser{r: ar, c: f}, nil
}

// datasetReadCloser reads decrypted plaintext from the AEAD reader and
// closes the underlying ciphertext file.
type datasetReadCloser struct {
	r io.Reader
	c io.Closer
}

func (inst *datasetReadCloser) Read(p []byte) (n int, err error) { return inst.r.Read(p) }
func (inst *datasetReadCloser) Close() (err error)               { return inst.c.Close() }
