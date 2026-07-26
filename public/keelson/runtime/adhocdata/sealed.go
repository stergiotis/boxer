package adhocdata

import "io"

// sealed.go — the vocabulary of a sealed dataset at the point where it is
// read ([ADR-0145] §SD1). One reference type, one decrypt seam.
//
// # Why this is not everything the concept touches
//
// [introspect.EncryptedDatasetI] stays where it is and keeps its accessors,
// rather than reporting a [Ref]. That is a layering fact, not a preference:
// this package imports `introspect` (the capability registers its datasets
// there), so `introspect` cannot import back. The two are also genuinely
// different statements — the registry's interface says "this provider is
// sealed", while a Ref is a request to decrypt one — and the party that
// holds both, the loopback `/table` handler, converts.
//
// Custody stays split for a stronger reason than layering. ADR-0134 §SD2
// divides key roles: the capability service is the POLICY OWNER and holds
// [KeyRegistrarI] (register at publish, forget at retract), while the broker
// is the DECRYPT EXECUTOR and is the only party that looks a key up. Fusing
// those into one interface would hand the policy owner an ability the split
// exists to deny it.
//
// [ADR-0145]: https://github.com/stergiotis/boxer/blob/main/doc/adr/0145-sealed-app-data.md
// [introspect.EncryptedDatasetI]: https://pkg.go.dev/github.com/stergiotis/boxer/public/keelson/runtime/introspect#EncryptedDatasetI

// Ref names one sealed dataset for reading. It carries what a decryptor
// needs and, deliberately, nothing else: the key is resolved on the
// executor's side by handle and never travels — not on a bus, not on a
// wire, and not in this struct.
type Ref struct {
	// Handle is the dataset's unguessable name, and the key under which
	// custody holds its key. It is also a valid keelson table name, which
	// is what lets a query name it.
	Handle string
	// Path locates the chunk-encrypted Arrow file. Ciphertext, so its
	// exposure is the disk exposure the scheme already assumes.
	Path string
	// Structure is the explicit ClickHouse structure string. Schema
	// inference cannot be used on this read — it consumes the stream and
	// cannot re-read it — so the publish gate computes this once and every
	// reader is handed it.
	Structure string
	// Revision increments on republish. It is what makes a same-revision
	// requery a legitimate cache hit and a republish a miss.
	Revision uint64
}

// DecryptorI streams a sealed dataset's plaintext.
//
// The implementation resolves the key in-process by [Ref.Handle] and
// returns a reader over the decrypted Arrow stream; the caller closes it.
// A mid-stream authentication or truncation failure surfaces as a read
// error rather than as a short result, which is the property the whole
// scheme rests on — a truncated dataset must fail the query, not shorten
// it.
//
// Taking an interface here is what keeps the loopback endpoint from
// importing the broker.
type DecryptorI interface {
	OpenDatasetPlaintext(ref Ref) (rc io.ReadCloser, err error)
}
