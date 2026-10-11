package agentrunstore

import "time"

// Run is the run row (ADR-0308 §SD1, §SD6): one per run, rewritten by a
// new row the state view folds latest-wins. ID is the cell-qualified run
// id. The memberships are the runtime vocabulary's, resolved at generation
// time; adding a field means minting its membership first (ADR-0183 D0).
type Run struct {
	_             struct{}  `kind:"agentrun"`
	ID            string    `lw:",id"`
	Cell          string    `lw:"agentrunCell,runCell"`
	Conversation  string    `lw:"agentrunConversation,runConversation"`
	OwnerApp      string    `lw:"runtimeApp,runOwnerApp"`
	Coordinator   string    `lw:"agentrunCoordinator,runCoordinator"`
	State         string    `lw:"agentrunState,runState"`
	Fingerprint   string    `lw:"agentrunFingerprint,runFingerprint"`
	Build         string    `lw:"agentrunBuild,runBuild"`
	Profile       string    `lw:"agentrunProfile,runProfile"`
	Settings      string    `lw:"agentrunSettings,runSettings"`
	LabelLevel    uint32    `lw:"agentrunLabelLevel,runLabelLevel"`
	LabelTier     uint32    `lw:"agentrunLabelTier,runLabelTier"`
	LabelExposure string    `lw:"agentrunLabelExposure,runLabelExposure"`
	LabelOrigin   string    `lw:"agentrunLabelOrigin,runLabelOrigin"`
	Created       time.Time `lw:"agentrunCreated,runCreated"`
	LastOrdinal   uint32    `lw:"agentrunLastOrdinal,runLastOrdinal"`
	Closed        time.Time `lw:"agentrunClosed,runClosed"`
	Digest        string    `lw:"agentrunDigest,runDigest"`
	Head          string    `lw:"agentrunHead,runHead"`
	Version       uint32    `lw:"agentrunVersion,runVersion"`
	DeletedBy     string    `lw:"agentrunDeletedBy,runDeletedBy"`
	DeletedAt     time.Time `lw:"agentrunDeletedAt,runDeletedAt"`
}
