// sensorreading.go is a second codecdemo DTO that exercises the Cut-2
// carrier (mixed-channel) emit paths of keelsoncodec --target=anchor —
// the paths dronemission.go does not reach. dronemission covers the
// scalar / unit-scalar write+read; SensorReading adds a
// marshalltypes.MixedLowCardRef carrier so the generated golden (and the
// round-trip test beside it) compile and run the AddMembershipMixedLowCardRefP
// write path and the GetMembValueLowCardRefHighCardParams carrier-decode
// read path against anchor's real DML / RA.
//
// sensorreading.out.go is regenerated from sensorreading.go by the same
// generate.sh step 3 as dronemission:
//
//	go run -tags "$(cat tags)" ./public/app keelsoncodec --target=anchor \
//	    public/semistructured/leeway/anchor/codecdemo/sensorreading.go
package codecdemo

import "github.com/stergiotis/boxer/public/semistructured/leeway/marshall/marshalltypes"

// SensorReading pairs a value field with a marshalltypes.MixedLowCardRef
// carrier on one (membership, section, channel) triple — anchor's `symbol`
// section, mixed-low-card-ref channel. The membership identity (id + params)
// is per-row carrier data, so no lookup is consulted: the codec emits
// AddMembershipMixedLowCardRefP(ReadingC.Id, ReadingC.Params) on write and
// rebuilds the carrier from the symbol section's Seq2 combined accessor on
// read. mixed-ref is the only Cut-2 channel anchor declares an RA reader for
// (MixedLowCardRefHighCardParameters), so it is the one with a clean wire
// round-trip in-tree.
type SensorReading struct {
	_ struct{} `kind:"sensorReading"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	// Reading is the section value; ReadingC carries the per-row membership
	// id + params. Both target anchor's `symbol` section on the
	// mixedLowCardRef channel — paired by goplan.PlanBuilder on the shared
	// (membership, section, channel) triple.
	Reading  string                        `lw:"sensor,symbol,mixedLowCardRef"`
	ReadingC marshalltypes.MixedLowCardRef `lw:"sensor,symbol,mixedLowCardRef"`
}
