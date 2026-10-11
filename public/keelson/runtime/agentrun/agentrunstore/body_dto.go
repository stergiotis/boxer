package agentrunstore

// BodyRow is one vault row (ADR-0320 §SD1): the content of one entry and
// the opening the entry's commitment opens under. ID is the run id and the
// envelope's ordinal the entry's, as on the ledger, so a run's bodies read
// as one range and a deletion names rows by the ordinal. The fields sit on
// the boxer.facts data sections (plan002 B0). The records that hold
// content are components beside it (ADR-0325 §SD1).
type BodyRow struct {
	_       struct{} `kind:"agentrunBody"`
	ID      uint64   `lw:",id"`
	Opening string   `lw:"agentrunBodyOpening,stringArray,unit"`
	Body    string   `lw:"agentrunBodyText,textArray,unit"`
	// ArgumentsText is a tool call's arguments when they do not parse as
	// JSON; parsed arguments and a reply's calls are shredded into the
	// row's sections instead (ADR-0325 §SD1).
	ArgumentsText string `lw:"agentrunBodyArgumentsText,stringArray,unit"`
}
