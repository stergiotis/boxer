package arrowrowcbor

// JoinRecords concatenates the sparse-CBOR bytes carried by the
// records into a single byte slice (a CBOR-Seq per RFC 8742). The
// generated dml_cbor package's TransferRecords typically returns one
// Record per drain call; this helper is the convenient flatten for
// callers that want one io.Writer-ready byte stream rather than
// per-record handling.
//
// Caller-owned; safe to retain past the records' Release.
func JoinRecords(records []*Record) (out []byte) {
	var total int
	for _, r := range records {
		total += len(r.CBOR())
	}
	out = make([]byte, 0, total)
	for _, r := range records {
		out = append(out, r.CBOR()...)
	}
	return
}
