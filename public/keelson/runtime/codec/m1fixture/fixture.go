//go:build llm_generated_opus47

// Package m1fixture is the ADR-0042 M1 demonstration fact kind: a flat
// DTO exercising every plain type the generator must support (string,
// uint{8,16,32,64}, float{32,64}, bool, time, [4]byte, [16]byte) under
// both `Membership` + `ExactlyOne` and `Membership` + `ZeroToOne`
// cardinalities.
//
// The DTO in this file is the **input** the generator will parse (the
// human-authored "schema source"); the file `fixture.out.go` is the
// generator's eventual output, hand-written here so the wire shape can
// be validated against clickhouse-local before the generator binary
// lands. See [keelson/vdd/EXPLANATION.md] for the model the generator
// respects.
//
// [keelson/vdd/EXPLANATION.md]: ../../../vdd/EXPLANATION.md
package m1fixture

import (
	"time"

	"github.com/RoaringBitmap/roaring"

	"github.com/stergiotis/boxer/public/functional/option"
)

// M1Sample is a synthetic fact kind: one row per "sample event"; the
// fields are chosen to exercise the M1 type matrix, not to model any
// real domain entity. Once the generator lands the first production
// fact kind, this fixture stays as a regression target.
//
// Plain columns:
//   - `id` (uint64) — caller-supplied row id
//   - `naturalKey` ([]byte) — entity natural key (facts SetId is 2-arg)
//   - `ts` (time.Time) — capture timestamp (UTC, nano precision via z64)
//
// Tagged values:
//   - Eight `Membership` + `ExactlyOne` fields covering symbol / u8 /
//     u16 / u32 / u64 / f32 / f64 / bool.
//   - Two `Membership` + `ExactlyOne` fixed-byte fields targeting the
//     blob section (peerV4 = [4]byte IPv4, peerV6 = [16]byte IPv6).
//   - Two `Membership` + `ZeroToOne` fields covering z64 (lastSuccess)
//     and string (operatorName) via option.Option[T].
type M1Sample struct {
	_ struct{} `kind:"m1Sample"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Source       string  `lw:"m1Source,symbol"`
	Severity     uint8   `lw:"m1Severity,u8Array"`
	MajorVer     uint16  `lw:"m1MajorVer,u16Array"`
	Sequence     uint32  `lw:"m1Sequence,u32Array"`
	LatencyNanos uint64  `lw:"m1LatencyNanos,u64Array"`
	CpuPct       float32 `lw:"m1CpuPct,f32Array"`
	LoadAvg1     float64 `lw:"m1LoadAvg1,f64Array"`
	Healthy      bool    `lw:"m1Healthy,bool"`

	PeerV4 [4]byte  `lw:"m1PeerV4,blobArray"`
	PeerV6 [16]byte `lw:"m1PeerV6,blobArray"`

	LastSuccess  option.Option[time.Time] `lw:"m1LastSuccess,timeArray"`
	OperatorName option.Option[string]    `lw:"m1OperatorName,stringArray"`
	Tags         []string                 `lw:"m1Tags,textArray"`
	CapBits      *roaring.Bitmap          `lw:"m1CapBits,u32Array"`
}
