package vocab

import "github.com/stergiotis/boxer/public/identity/tagmint"

// The runtime's tagged id streams (ADR-0319 §SD2): one tag value per table
// whose rows other rows point at, its bodies leased in blocks from the lease
// row on the deployment's one server (identgen/leased/rowalloc) and minted
// locally. An id identifies a row; it is never an order (§SD5).
//
// Each value is the one the fibonacci planner advises for the declared
// cardinality, counted with the gaps a block lease leaves (§SD4), and among
// that class one whose code carries many leading zeros, so the high bits of a
// stored id compress. The class minimum, which carries the most, goes to the
// persisted stream. The in-memory twins (§SD6) mint from a process counter
// under a value of their own, so an id minted without a lease row can never
// be mistaken for a persisted one.
//
// A claim is a decision: a value changed later changes every id minted under
// it. testdata/idstream-claims.golden pins them.
var (
	// IdStreamAgentRun identifies journal runs (ADR-0308 §SD1).
	IdStreamAgentRun = tagmint.MustClaim("agentrunRun", 317811, MaxExpectedAgentRuns)
	// IdStreamAgentRunEntry identifies journal entries (ADR-0308 §SD2).
	IdStreamAgentRunEntry = tagmint.MustClaim("agentrunEntry", 46368, MaxExpectedAgentRunEntries)
	// IdStreamAgentRunMem and IdStreamAgentRunEntryMem are the in-memory
	// lane's runs and entries (ADR-0319 §SD6).
	IdStreamAgentRunMem      = tagmint.MustClaim("agentrunRunMem", 439204, MaxExpectedAgentRuns)
	IdStreamAgentRunEntryMem = tagmint.MustClaim("agentrunEntryMem", 64079, MaxExpectedAgentRunEntries)
)

// MaxExpectedAgentRuns and MaxExpectedAgentRunEntries are what the journal's
// streams tell the mint they will need, gaps included: width 28 holds 2^36
// bodies, width 24 holds 2^40.
const (
	MaxExpectedAgentRuns       = 1 << 35
	MaxExpectedAgentRunEntries = 1 << 39
)

// IdStreams lists the streams this package claims, for the golden.
var IdStreams = []tagmint.ClaimedTagValue{IdStreamAgentRun, IdStreamAgentRunEntry, IdStreamAgentRunMem, IdStreamAgentRunEntryMem}
