package marshallreflect_test

import (
	"time"

	"github.com/RoaringBitmap/roaring"

	"github.com/stergiotis/boxer/public/functional/option"
)

// paritySimple covers the flat simple subset plus the frozen mixed-shape
// sub-column spelling in one accept case: the header roles, scalar / Option /
// slice / blob / fixed-byte / roaring value shapes, and a two-sub-column
// section (timeRange). Parsed AND compiled — see parity_corpus_test.go.
type paritySimple struct {
	_        struct{}              `kind:"paritySimple"`
	ID       uint64                `lw:",id"`
	Ts       time.Time             `lw:",ts"`
	Tracking []byte                `lw:",naturalKey"`
	Status   string                `lw:"status,symbol"`
	Note     option.Option[string] `lw:"note,text"`
	Scope    []string              `lw:"scope,stringArray"`
	Payload  []byte                `lw:"payload,blob"`
	Digest   [8]byte               `lw:"digest,blob"`
	Seen     *roaring.Bitmap       `lw:"seen,u32Array"`
	Begin    time.Time             `lw:"window,timeRange:beginIncl"`
	End      time.Time             `lw:"window,timeRange:endExcl"`
}
