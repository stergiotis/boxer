package stevedorefacts

import "time"

// DeadLetter is one message a lander gave up on (ADR-0252 §SD3): the
// envelope's identity where it decoded, the failure's class and text, the
// message's place on its topic, and its bytes so the failure can be replayed.
type DeadLetter struct {
	_ struct{} `kind:"stevedoreDeadLetter"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"stevedoreKindDeadLetter,symbol"`

	// Ref, Origin and Ordinal are the envelope's, or zero when the envelope
	// itself did not decode.
	Ref     uint64 `lw:"stevedoreDeadRef,u64Array,unit"`
	Origin  string `lw:"stevedoreDeadOrigin,stringArray,unit"`
	Ordinal uint64 `lw:"stevedoreDeadOrdinal,u64Array,unit"`

	// Class is permanent, or incomplete for a split body whose last part
	// never arrived.
	Class string `lw:"stevedoreDeadClass,symbol"`
	Error string `lw:"stevedoreDeadError,stringArray,unit"`

	Topic     string `lw:"stevedoreDeadTopic,symbol"`
	Partition int32  `lw:"stevedoreDeadPartition,i32Array,unit"`
	Offset    int64  `lw:"stevedoreDeadOffset,i64Array,unit"`

	Message []byte `lw:"stevedoreDeadMessage,blobArray,unit"`
}
