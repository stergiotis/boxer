package agentrunstore

// BodyRow is one vault row (ADR-0320 §SD1): the content of one entry and
// the opening the entry's commitments open under. ID is the run id and the
// envelope's ordinal the entry's, as on the ledger, so a run's bodies read
// as one range and a deletion names rows by the ordinal. The fields sit on
// the boxer.facts data sections (plan002 B0).
type BodyRow struct {
	_       struct{} `kind:"agentrunBody"`
	ID      string   `lw:",id"`
	Opening string   `lw:"agentrunBodyOpening,stringArray,unit"`
	Body    string   `lw:"agentrunBodyText,textArray,unit"`
	Calls   string   `lw:"agentrunBodyCalls,stringArray,unit"`
}
