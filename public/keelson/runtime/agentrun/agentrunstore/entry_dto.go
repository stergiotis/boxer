package agentrunstore

import "time"

// Entry is one ledger entry (ADR-0308 §SD2): appended, never rewritten; a
// tombstone appends the entry at its next revision, which the reads keep
// over the original (§SD8). ID is the run id; the envelope's ordinal is
// the entry's. The fields sit on the boxer.facts data sections in the
// trail's spelling (plan002 B0), told apart by their membership ids. The
// body and calls are not here: they are the vault's (ADR-0320 §SD1), and
// BodyDigest and CallsDigest are the commitments its rows open.
type Entry struct {
	_             struct{}  `kind:"agentrunEntry"`
	ID            string    `lw:",id"`
	At            time.Time `lw:"agentrunEntryAt,timeArray,unit"`
	Kind          string    `lw:"agentrunEntryKind,symbol"`
	Key           string    `lw:"agentrunEntryKey,stringArray,unit"`
	Role          string    `lw:"agentrunEntryRole,symbol"`
	BodyDigest    string    `lw:"agentrunEntryBodyDigest,stringArray,unit"`
	CallsDigest   string    `lw:"agentrunEntryCallsDigest,stringArray,unit"`
	Name          string    `lw:"agentrunEntryName,stringArray,unit"`
	Book          string    `lw:"agentrunEntryBook,symbol"`
	Form          string    `lw:"agentrunEntryForm,symbol"`
	Target        uint32    `lw:"agentrunEntryTarget,u32Array,unit"`
	From          uint32    `lw:"agentrunEntryFrom,u32Array,unit"`
	Effect        string    `lw:"agentrunEntryEffect,symbol"`
	Turn          string    `lw:"agentrunEntryTurn,symbol"`
	LabelLevel    uint8     `lw:"agentrunEntryLabelLevel,u8Array,unit"`
	LabelTier     uint8     `lw:"agentrunEntryLabelTier,u8Array,unit"`
	LabelExposure []string  `lw:"agentrunEntryLabelExposure,symbolArray"`
	LabelOrigin   []string  `lw:"agentrunEntryLabelOrigin,symbolArray"`
	WriterRun     string    `lw:"agentrunEntryWriterRun,symbol"`
	WriterTurn    string    `lw:"agentrunEntryWriterTurn,symbol"`
	WriterOrdinal uint32    `lw:"agentrunEntryWriterOrdinal,u32Array,unit"`
	Prev          uint32    `lw:"agentrunEntryPrev,u32Array,unit"`
	Digest        string    `lw:"agentrunEntryDigest,stringArray,unit"`
	KeyId         string    `lw:"agentrunEntryKeyId,symbol"`
	Parent        uint32    `lw:"agentrunEntryParent,u32Array,unit"`
	Attachments   string    `lw:"agentrunEntryAttachments,stringArray,unit"`
}
