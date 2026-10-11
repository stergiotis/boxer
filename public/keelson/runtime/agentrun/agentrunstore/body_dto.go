package agentrunstore

// BodyRow is a vault row's prose (ADR-0320 §SD1): one of the content
// components of an entry, behind its own commitment and opening on the
// vault row that holds it (ADR-0327 §SD4). ID is the run id and the
// envelope's ordinal the entry's, as on the ledger, so a run's bodies read
// as one range and a deletion names rows by the ordinal. The fields sit on
// the boxer.facts data sections (plan002 B0). The records that hold
// content are components beside it (ADR-0325 §SD1).
type BodyRow struct {
	_    struct{} `kind:"agentrunBody"`
	ID   uint64   `lw:",id"`
	Body string   `lw:"agentrunBodyText,textArray,unit"`
	// ArgumentsText is a tool call's arguments when they do not parse as
	// JSON; parsed arguments and a reply's calls are shredded into the
	// profile's shredded component instead (ADR-0325 §SD1, ADR-0326).
	ArgumentsText string `lw:"agentrunBodyArgumentsText,stringArray,unit"`
}
