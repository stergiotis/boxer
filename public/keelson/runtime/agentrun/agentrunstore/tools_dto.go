package agentrunstore

// ToolCatalog is a tools entry's record (ADR-0325 §SD1, ledger): the names
// of the tools offered, in order.
type ToolCatalog struct {
	_     struct{} `kind:"agentrunTools"`
	ID    uint64   `lw:",id"`
	Names []string `lw:"agentrunToolsNames,stringArray"`
}
