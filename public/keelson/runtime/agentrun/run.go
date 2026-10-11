package agentrun

import "time"

// StateE is a run's state (ADR-0308 §SD5 for stranded, §SD8 for deleted;
// ADR-0303 §SD8, superseded by ADR-0308).
type StateE uint8

const (
	StateUnspecified StateE = iota
	StateOpen
	StateClosed
	// StateStranded is a run whose fingerprint differs from the code now
	// running: readable, not continuing (ADR-0308 §SD5).
	StateStranded
	// StateDeleted is a tombstoned run: bodies emptied, the chain kept
	// (ADR-0308 §SD8).
	StateDeleted
)

var stateNames = []string{"unspecified", "open", "closed", "stranded", "deleted"}

func (inst StateE) String() (s string) {
	if int(inst) < len(stateNames) {
		return stateNames[inst]
	}
	return "unknown"
}

// ParseState is the state a name spells.
func ParseState(s string) (st StateE, ok bool) {
	for i, n := range stateNames {
		if n == s && i > 0 {
			return StateE(i), true
		}
	}
	return
}

// Run is the run row (ADR-0308 §SD1, §SD6): one per run, its identity,
// owner and state, the fingerprint it is pinned to, the settings fixed at
// the first send, and its current label.
type Run struct {
	// Id is the cell-qualified run id; Cell names the cell that minted
	// it, so a synced run keeps its writer's ordinals (§SD1).
	Id   string
	Cell string
	// Conversation is the owner app's id for the conversation; OwnerApp
	// the app and Coordinator the window that writes the run.
	Conversation string
	OwnerApp     string
	Coordinator  string
	State        StateE
	// Fingerprint pins the run to the code it started with (§SD5); Build
	// is recorded and shown, not compared. Profile names the harness
	// profile (ADR-0310 §SD7).
	Fingerprint string
	Build       string
	Profile     string
	// Settings are the conversation's settings fixed at the first send,
	// as JSON: Keep, Apps, Questions, the artefact, the ceiling.
	Settings string
	// Label is the run's current label, floating as it reads (ADR-0311
	// §SD2): the join of every entry's label, raised by the store as
	// entries append, never lowered by an update.
	Label Label
	// Created is the first write; LastOrdinal the last entry's ordinal;
	// Closed when the run left the open state.
	Created     time.Time
	LastOrdinal uint32
	Closed      time.Time
	// Digest is the run row's chain seed, the first entry's predecessor;
	// Head is the chain's head, the last entry's digest (the seed while
	// there is none), advanced by the store as entries append.
	Digest string
	Head   string
	// Version counts the row's writes: the latest-wins order of the
	// record, so a clock tie or step cannot resurrect an older row.
	Version uint32
	// DeletedBy and DeletedAt record a tombstone.
	DeletedBy string
	DeletedAt time.Time
}
