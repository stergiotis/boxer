package agentrunstore

import "time"

// GrantRecord is a grant answer's record (ADR-0325 §SD1, vault): the task
// and handle, the terms when there are any, and whether it runs
// unattended.
type GrantRecord struct {
	_            struct{}  `kind:"agentrunGrant"`
	ID           uint64    `lw:",id"`
	Task         string    `lw:"agentrunGrantTask,textArray,unit"`
	Handle       string    `lw:"agentrunGrantHandle,stringArray,unit"`
	HasTerms     bool      `lw:"agentrunGrantHasTerms,bool"`
	CallsBudget  uint32    `lw:"agentrunGrantCallsBudget,u32Array,unit"`
	CallsUsed    uint32    `lw:"agentrunGrantCallsUsed,u32Array,unit"`
	Deadline     time.Time `lw:"agentrunGrantDeadline,timeArray,unit"`
	Destinations []string  `lw:"agentrunGrantDestinations,stringArray"`
	Unattended   bool      `lw:"agentrunGrantUnattended,bool"`
}
