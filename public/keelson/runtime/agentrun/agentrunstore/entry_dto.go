package agentrunstore

import "time"

// Entry is one ledger entry (ADR-0308 §SD2): appended, never rewritten. ID
// is the run id (ADR-0325 §SD5); the envelope's ordinal is the entry's.
// The fields sit on the boxer.facts data sections in the trail's spelling
// (plan002 B0), told apart by their membership ids. The content is not
// here: it is the vault's (ADR-0320 §SD1), and the commitments its rows
// open are on the row's chain component with the chain digest and the
// mark (ADR-0327 §SD2, §SD4). EntryId is the entry's tagged id (ADR-0325
// §SD5); the attachments are references, a media type and the artefact
// store's digest, index for index. The records an entry carries beside are
// components of their own.
type Entry struct {
	_                    struct{}  `kind:"agentrunEntry"`
	ID                   uint64    `lw:",id"`
	At                   time.Time `lw:"agentrunEntryAt,timeArray,unit"`
	Kind                 string    `lw:"agentrunEntryKind,symbol"`
	Key                  string    `lw:"agentrunEntryKey,stringArray,unit"`
	Role                 string    `lw:"agentrunEntryRole,symbol"`
	EntryId              uint64    `lw:"agentrunEntryId,u64Array,unit"`
	Name                 string    `lw:"agentrunEntryName,stringArray,unit"`
	Book                 string    `lw:"agentrunEntryBook,symbol"`
	Form                 string    `lw:"agentrunEntryForm,symbol"`
	Target               uint32    `lw:"agentrunEntryTarget,u32Array,unit"`
	From                 uint32    `lw:"agentrunEntryFrom,u32Array,unit"`
	Effect               string    `lw:"agentrunEntryEffect,symbol"`
	Turn                 string    `lw:"agentrunEntryTurn,symbol"`
	LabelLevel           uint8     `lw:"agentrunEntryLabelLevel,u8Array,unit"`
	LabelTier            uint8     `lw:"agentrunEntryLabelTier,u8Array,unit"`
	LabelExposure        []string  `lw:"agentrunEntryLabelExposure,symbolArray"`
	LabelOrigin          []string  `lw:"agentrunEntryLabelOrigin,symbolArray"`
	WriterRun            uint64    `lw:"agentrunEntryWriterRun,u64Array,unit"`
	WriterTurn           string    `lw:"agentrunEntryWriterTurn,symbol"`
	WriterOrdinal        uint32    `lw:"agentrunEntryWriterOrdinal,u32Array,unit"`
	Parent               uint32    `lw:"agentrunEntryParent,u32Array,unit"`
	AttachmentMediaTypes []string  `lw:"agentrunEntryAttachmentMediaTypes,symbolArray"`
	AttachmentDigests    []string  `lw:"agentrunEntryAttachmentDigests,stringArray"`
}
