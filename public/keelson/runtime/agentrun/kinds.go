package agentrun

// KindE is an entry's kind (ADR-0308 §SD2): a closed enum whose ordinals
// never retire, pinned by a golden. A kind not in this list is not
// written; an extension-private entry is KindIgnorable and the projection
// skips it.
type KindE uint8

const (
	KindUnspecified KindE = 0
	// KindUser is a message the person sent.
	KindUser KindE = 1
	// KindRequest is a model call about to leave: its call key, the
	// request's digest, and what is new since the last request.
	KindRequest KindE = 2
	// KindReply is the model's answer: the call id and the assistant
	// message with its tool calls.
	KindReply KindE = 3
	// KindIntent is a tool call about to be dispatched: its key, tool,
	// arguments and effect class.
	KindIntent KindE = 4
	// KindOutcome is what the model was told of a tool call.
	KindOutcome KindE = 5
	// KindNote is context the host put before the person's message: a
	// snapshot or a notice, with its source.
	KindNote KindE = 6
	// KindSection sets or clears a named prompt section, with its book and
	// slug; the latest per name wins.
	KindSection KindE = 7
	// KindTools declares the tool set: names and schema digests.
	KindTools KindE = 8
	// KindCompact is a compaction: a cut ordinal, the span replaced, a
	// summary, the summariser's call key and the summary's label
	// (ADR-0316).
	KindCompact KindE = 9
	// KindEdit replaces an earlier entry's body, or drops it; the latest
	// edit per target wins.
	KindEdit KindE = 10
	// KindProfile records the profile id and fingerprint at the first
	// request and on change (ADR-0310).
	KindProfile KindE = 11
	// KindTurn ends a turn with its terminal state.
	KindTurn KindE = 12
	// KindAsked is a question to the person: ask_user, a grant request, a
	// widening, a confirmation.
	KindAsked KindE = 13
	// KindAnswered is the person's decision and who made it.
	KindAnswered KindE = 14
	// KindCandidate is one sample of a request under fan-out.
	KindCandidate KindE = 15
	// KindVerdict is a verifier's result on a candidate.
	KindVerdict KindE = 16
	// KindSelect is the chosen candidate with its rule.
	KindSelect KindE = 17
	// KindCompleted is the completion of asynchronous work a command
	// started, apart from its outcome (ADR-0315 §SD6).
	KindCompleted KindE = 18
	// KindEndorse lifts one body for named act classes: who, the digest as
	// shown, the display epoch, the diff since the last endorsement
	// (ADR-0311 §SD3).
	KindEndorse KindE = 19
	// KindApproval is an exit above a channel's free tier: channel, tier,
	// approver, justification (ADR-0312 §SD3).
	KindApproval KindE = 20
	// KindHead is a chain head shipped to the anchor store: the ordinal
	// covered, the digest, the receipt (ADR-0314 §SD2).
	KindHead KindE = 21
	// KindCopy is a clipboard write of labelled text with its digest
	// (ADR-0317).
	KindCopy KindE = 22
	// KindIssued is a watermark payload issued for a frame (ADR-0317 §SD5).
	KindIssued KindE = 23
	// KindDeleted is the tombstone of a deleted run (ADR-0308 §SD8).
	KindDeleted KindE = 24
	// KindKeyUse is reserved while per-run encryption is deferred.
	KindKeyUse KindE = 25
	// KindIgnorable is an extension-private entry the projection skips.
	KindIgnorable KindE = 26
	// KindErased records the erasure of one earlier entry's content: who
	// in its name, the ordinal as its target; the vault row is destroyed
	// with it (ADR-0320 §SD4).
	KindErased KindE = 27
)

// AllKinds lists every kind in ordinal order; the golden pins it.
var AllKinds = []KindE{KindUser, KindRequest, KindReply, KindIntent, KindOutcome, KindNote, KindSection, KindTools,
	KindCompact, KindEdit, KindProfile, KindTurn, KindAsked, KindAnswered, KindCandidate, KindVerdict, KindSelect,
	KindCompleted, KindEndorse, KindApproval, KindHead, KindCopy, KindIssued, KindDeleted, KindKeyUse, KindIgnorable, KindErased}

var kindNames = map[KindE]string{
	KindUser: "user", KindRequest: "request", KindReply: "reply", KindIntent: "intent", KindOutcome: "outcome",
	KindNote: "note", KindSection: "section", KindTools: "tools", KindCompact: "compact", KindEdit: "edit",
	KindProfile: "profile", KindTurn: "turn", KindAsked: "asked", KindAnswered: "answered", KindCandidate: "candidate",
	KindVerdict: "verdict", KindSelect: "select", KindCompleted: "completed", KindEndorse: "endorse",
	KindApproval: "approval", KindHead: "head", KindCopy: "copy", KindIssued: "issued", KindDeleted: "deleted",
	KindKeyUse: "keyuse", KindIgnorable: "ignorable", KindErased: "erased",
}

func (inst KindE) String() (s string) {
	s, ok := kindNames[inst]
	if !ok {
		s = "unspecified"
	}
	return
}

// ParseKind is the kind a name spells; false for none.
func ParseKind(s string) (k KindE, ok bool) {
	for kind, name := range kindNames {
		if name == s {
			return kind, true
		}
	}
	return
}

// IsValid says the kind is one of the enum's.
func (inst KindE) IsValid() bool { _, ok := kindNames[inst]; return ok }

// MovesProjection says an entry of the kind changes what the model
// receives (ADR-0308 §SD2's third column).
func (inst KindE) MovesProjection() bool {
	switch inst {
	case KindUser, KindReply, KindOutcome, KindNote, KindSection, KindTools, KindCompact, KindEdit:
		return true
	}
	return false
}

// Rewrites says an entry of the kind may change earlier output of the
// projection rather than extend it (ADR-0309 §SD2): the priced
// exceptions to append stability.
func (inst KindE) Rewrites() bool {
	switch inst {
	case KindSection, KindTools, KindCompact, KindEdit:
		return true
	}
	return false
}
