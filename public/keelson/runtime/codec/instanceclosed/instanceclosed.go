// Package instanceclosed is the leeway-coded wire form of the bus's
// `runtime.instance.closed` announcement (ADR-0240 §SD5): a client that
// carried an instance key has closed, so whatever the instance owned on a
// service's books may go. The subject is app.SubjectInstanceClosed; the
// envelope's sender is the closed client's own identity.
//
// Vocabulary: the shared `appId` and `tileKey` — the host's window/tile
// identifier is what an instance key is.
package instanceclosed

import "time"

// InstanceClosed is the flat wire form of one announcement.
type InstanceClosed struct {
	_ struct{} `kind:"instanceClosed"`

	// FactId is the per-row event id.
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry none.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the closing instant.
	At time.Time `lw:",ts"`

	// AppId is the closed client's app id.
	AppId string `lw:"appId,stringArray"`

	// InstanceKey is the host-minted window or embed key the client carried.
	InstanceKey uint64 `lw:"tileKey,u64Array"`
}
