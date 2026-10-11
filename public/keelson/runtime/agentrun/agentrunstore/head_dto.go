package agentrunstore

import "time"

// HeadRecord is a head entry's record (ADR-0325 §SD1, ledger; ADR-0314): the
// ordinal and digest the receipt covers, the receipt, its cadence and
// whether it is durable.
type HeadRecord struct {
	_         struct{}  `kind:"agentrunHead"`
	ID        uint64    `lw:",id"`
	Ordinal   uint32    `lw:"agentrunHeadOrdinal,u32Array,unit"`
	Digest    string    `lw:"agentrunHeadDigest,stringArray,unit"`
	ReceiptId string    `lw:"agentrunHeadReceiptId,stringArray,unit"`
	ReceiptAt time.Time `lw:"agentrunHeadReceiptAt,timeArray,unit"`
	Cadence   string    `lw:"agentrunHeadCadence,symbol"`
	Durable   bool      `lw:"agentrunHeadDurable,bool"`
}
