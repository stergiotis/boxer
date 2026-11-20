package sysmetricsbus

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// The system-metrics bundle is the one bus payload that leaves the process:
// sysmetricsd publishes it over NATS and a host bridges it onto its bus
// (ADR-0090), so the two ends may be different builds. The struct fields
// carry no `cbor:` tags inside the snapshot, so a renamed Go field — or an
// added `json:` tag, which fxamacker/cbor falls back to — renames a key on
// the wire. These fixtures pin the bytes: encode must reproduce them, and
// decode must read them back to the value.
//
// The value is small on purpose and holds at most one map entry: the codec
// does not sort map keys, so a fixture with two would not be stable.
func wireFixtureValue() (snap *sysmsnap.BundleSnapshot) {
	return &sysmsnap.BundleSnapshot{
		SampledAtUnixMs: 1_700_000_000_123,
		CPU: &sysmsnap.CPUSnapshot{
			SampledAtUnixMs: 1_700_000_000_123,
			TotalPercent:    42,
			PerCorePercent:  []uint8{10, 20},
			LoadAvg1:        1.25,
			ModelName:       "Test CPU",
			LogicalCores:    2,
		},
		Mem:   &sysmsnap.MemSnapshot{TotalBytes: 16 << 30, UsedBytes: 8 << 30},
		Procs: []sysmsnap.ProcInfo{{PID: 4242, PPID: 1, Name: "imztop", CPUPercent: 12.25, RSSBytes: 1 << 20, Component: "imzero2-demo"}},
		Errors: map[sysmsnap.Domain]error{
			sysmsnap.DomainGPU: errors.New("no gpu present"),
		},
	}
}

const wireFixtureHex = "a201ae6f53616d706c65644174556e69784d731b0000018bcfe5687b63435055ac6f53616d706c65644174556e69784d" +
	"731b0000018bcfe5687b6c546f74616c50657263656e74182a6e506572436f726550657263656e74420a146e50657243" +
	"6f7265467265714d487af6684c6f616441766731fa3fa00000684c6f616441766735fa00000000694c6f616441766731" +
	"35fa000000006a55736167655761747473fa000000007355736167655761747473417661696c61626c65f46a41637469" +
	"766543505573f6694d6f64656c4e616d656854657374204350556c4c6f676963616c436f72657302634d656dac6f5361" +
	"6d706c65644174556e69784d73006a546f74616c42797465731b000000040000000069467265654279746573006e4176" +
	"61696c61626c654279746573006c427566666572734279746573006b4361636865644279746573006e53776170546f74" +
	"616c4279746573006d5377617046726565427974657300695573656442797465731b00000002000000006d5377617055" +
	"7365644279746573006c41524353697a654279746573006b4152434d696e427974657300644469736bf6634e6574f667" +
	"42617474657279f669436f6e7461696e6572f663475055f663505349f66550726f637381b56350494419109264505049" +
	"4401644e616d6566696d7a746f7063436d646065537461746500635549440063474944006455736572606f5374617274" +
	"65644174556e69784d73006a43505550657263656e74fa414400006852535342797465731a001000006b564d53697a65" +
	"42797465730071494f5265616442797465735065725365630072494f577269746542797465735065725365630067494f" +
	"4b6e6f776ef46a4e756d5468726561647300644e69636500685072696f72697479006c4b65726e656c546872656164f4" +
	"69436f6d706f6e656e746c696d7a65726f322d64656d6f6a4367726f7570556e6974606753656e736f7273f667536f63" +
	"6b657473f668546f706f6c6f6779f6664572726f7273f602a1636770756e6e6f206770752070726573656e74"

func TestWireFixture(t *testing.T) {
	codec := NewCBORCodec()
	payload, err := codec.Encode(wireFixtureValue())
	require.NoError(t, err)
	require.Equal(t, wireFixtureHex, hex.EncodeToString(payload),
		"the metric bundle's wire bytes changed; a build on the other side of NATS reads the old ones. "+
			"If the change is intended, update the fixture and say so in the commit")

	raw, err := hex.DecodeString(wireFixtureHex)
	require.NoError(t, err)
	got, err := codec.Decode(raw)
	require.NoError(t, err)
	want := wireFixtureValue()
	require.Equal(t, want.Errors[sysmsnap.DomainGPU].Error(), got.Errors[sysmsnap.DomainGPU].Error())
	want.Errors, got.Errors = nil, nil
	require.Equal(t, want, got)
}
