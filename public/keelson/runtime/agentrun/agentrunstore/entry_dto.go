package agentrunstore

import "time"

// Entry is one journal entry (ADR-0308 §SD2): appended, never rewritten;
// a tombstone appends a body-less twin that wins over the original
// (§SD8), the digests of body and calls staying so the chain verifies.
// ID is the run id; the envelope ts is the ordinal as a synthetic instant
// (recordstore.SeqTs), so a key-prefix scan reads a run in order.
type Entry struct {
	_             struct{}  `kind:"agentrunEntry"`
	ID            string    `lw:",id"`
	Ordinal       uint32    `lw:"agentrunEntryOrdinal,entryOrdinal"`
	At            time.Time `lw:"agentrunEntryAt,entryAt"`
	Kind          string    `lw:"agentrunEntryKind,entryKind"`
	Key           string    `lw:"agentrunEntryKey,entryKey"`
	Role          string    `lw:"agentrunEntryRole,entryRole"`
	Body          string    `lw:"agentrunEntryBody,entryBody"`
	Calls         string    `lw:"agentrunEntryCalls,entryCalls"`
	BodyDigest    string    `lw:"agentrunEntryBodyDigest,entryBodyDigest"`
	CallsDigest   string    `lw:"agentrunEntryCallsDigest,entryCallsDigest"`
	Name          string    `lw:"agentrunEntryName,entryName"`
	Book          string    `lw:"agentrunEntryBook,entryBook"`
	Form          string    `lw:"agentrunEntryForm,entryForm"`
	Target        uint32    `lw:"agentrunEntryTarget,entryTarget"`
	From          uint32    `lw:"agentrunEntryFrom,entryFrom"`
	Effect        string    `lw:"agentrunEntryEffect,entryEffect"`
	Turn          string    `lw:"agentrunEntryTurn,entryTurn"`
	LabelLevel    uint32    `lw:"agentrunEntryLabelLevel,entryLabelLevel"`
	LabelTier     uint32    `lw:"agentrunEntryLabelTier,entryLabelTier"`
	LabelExposure string    `lw:"agentrunEntryLabelExposure,entryLabelExposure"`
	LabelOrigin   string    `lw:"agentrunEntryLabelOrigin,entryLabelOrigin"`
	WriterRun     string    `lw:"agentrunEntryWriterRun,entryWriterRun"`
	WriterTurn    string    `lw:"agentrunEntryWriterTurn,entryWriterTurn"`
	WriterOrdinal uint32    `lw:"agentrunEntryWriterOrdinal,entryWriterOrdinal"`
	Prev          uint32    `lw:"agentrunEntryPrev,entryPrev"`
	Digest        string    `lw:"agentrunEntryDigest,entryDigest"`
	KeyId         string    `lw:"agentrunEntryKeyId,entryKeyId"`
	Parent        uint32    `lw:"agentrunEntryParent,entryParent"`
	Attachments   string    `lw:"agentrunEntryAttachments,entryAttachments"`
}
