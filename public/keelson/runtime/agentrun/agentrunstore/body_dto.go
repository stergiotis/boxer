package agentrunstore

// BodyRow is one vault row (ADR-0320 §SD1): the content of one entry and
// the opening the entry's commitments open under. ID is the run id; the
// envelope ts is the ordinal as a synthetic instant (recordstore.SeqTs),
// as on the entry table, so a key-prefix scan reads a run's bodies in
// order and a deletion names rows by the order column.
type BodyRow struct {
	_       struct{} `kind:"agentrunBody"`
	ID      string   `lw:",id"`
	Ordinal uint32   `lw:"agentrunBodyOrdinal,bodyOrdinal"`
	Opening string   `lw:"agentrunBodyOpening,bodyOpening"`
	Body    string   `lw:"agentrunBodyText,bodyText"`
	Calls   string   `lw:"agentrunBodyCalls,bodyCalls"`
}
