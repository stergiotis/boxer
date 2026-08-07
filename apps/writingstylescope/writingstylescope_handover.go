package writingstylescope

// Hand the sweep to play as an ad-hoc dataset (ADR-0175 §SD7, over ADR-0134
// and ADR-0135). The Matrix tab's table is the top of a ranking; the dataset
// carries the whole cross-matrix, one row per pair, so the reader who wants to
// filter, aggregate, or join it has the real thing rather than the 25 rows the
// panel had room for. The launched buffer reproduces the on-screen table so
// what opens matches what was clicked from.

import (
	"bytes"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// datasetAlias is the stable alias this app publishes under. The launched
// window inherits no alias binding, so the seeded SQL names the handle
// directly — the alias is what the publish gate and the ad-hoc catalog show.
const datasetAlias = "pairs"

// handoverLimit is the LIMIT the seeded buffer carries, matching the ranked
// table on the Matrix tab. The dataset itself is unfiltered.
const handoverLimit = maxRankedPairs

// pairsSchema is the published table. Section titles ride along denormalised
// on every row: a pair is only meaningful with both titles attached, and the
// alternative — three datasets to join — is a worse trade at this size.
//
// Every type here is in ADR-0134 §SD1's bounded publish-gate set; a type
// outside it is refused at publish rather than at query.
var pairsSchema = arrow.NewSchema([]arrow.Field{
	{Name: "a_idx", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "b_idx", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "a_section", Type: arrow.BinaryTypes.String},
	{Name: "b_section", Type: arrow.BinaryTypes.String},
	{Name: "a_level", Type: arrow.PrimitiveTypes.Uint8},
	{Name: "b_level", Type: arrow.PrimitiveTypes.Uint8},
	{Name: "a_bytes", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "b_bytes", Type: arrow.PrimitiveTypes.Uint32},
	{Name: "ncd", Type: arrow.PrimitiveTypes.Float64},
	{Name: "quantile", Type: arrow.PrimitiveTypes.Float64},
}, nil)

// pairsArrow renders the whole cross-matrix as an Arrow IPC stream, one row
// per pair in row-major order. `quantile` is the fraction of all pairs at or
// below that cell — the app's own reading of where a pair sits in its
// background, carried into SQL so a query can reproduce the panel's "beats"
// column without recomputing the distribution.
func pairsArrow(res *Analysis) (stream []byte, err error) {
	if res == nil || res.Rows() == 0 || res.Cols() == 0 {
		err = eh.Errorf("writingstylescope: nothing to publish")
		return
	}
	rb := array.NewRecordBuilder(memory.DefaultAllocator, pairsSchema)
	defer rb.Release()

	aIdx := rb.Field(0).(*array.Uint32Builder)
	bIdx := rb.Field(1).(*array.Uint32Builder)
	aSec := rb.Field(2).(*array.StringBuilder)
	bSec := rb.Field(3).(*array.StringBuilder)
	aLvl := rb.Field(4).(*array.Uint8Builder)
	bLvl := rb.Field(5).(*array.Uint8Builder)
	aLen := rb.Field(6).(*array.Uint32Builder)
	bLen := rb.Field(7).(*array.Uint32Builder)
	ncd := rb.Field(8).(*array.Float64Builder)
	quant := rb.Field(9).(*array.Float64Builder)

	for i, sa := range res.SecA {
		for j, sb := range res.SecB {
			v := res.At(i, j)
			aIdx.Append(uint32(i))
			bIdx.Append(uint32(j))
			aSec.Append(sa.Label())
			bSec.Append(sb.Label())
			aLvl.Append(sa.Level)
			bLvl.Append(sb.Level)
			aLen.Append(uint32(sa.Bytes()))
			bLen.Append(uint32(sb.Bytes()))
			ncd.Append(v)
			quant.Append(res.Quantile(v))
		}
	}

	rec := rb.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(pairsSchema))
	if err = w.Write(rec); err != nil {
		err = eh.Errorf("writingstylescope: write arrow record: %w", err)
		return
	}
	if err = w.Close(); err != nil {
		err = eh.Errorf("writingstylescope: close arrow stream: %w", err)
		return
	}
	stream = buf.Bytes()
	return
}

// handoverSql is the buffer the opened play window is seeded with: the ranked
// table the reader clicked from, over the full dataset. Handle-form rather
// than alias-form because the opened window inherits no alias binding
// (ADR-0134 §SD4).
func handoverSql(handle string) (sql string) {
	return fmt.Sprintf(`-- Section pairs from writingstylescope, closest first.
-- The dataset holds every pair; this is the panel's ranked view of it.
SELECT
    a_section,
    b_section,
    ncd,
    quantile        AS beats_fraction,
    a_bytes,
    b_bytes
FROM keelson('%s')
ORDER BY ncd ASC
LIMIT %d`, handle, handoverLimit)
}

// requestHandover publishes the current sweep and opens a play window on it.
// Both halves are synchronous bus round-trips, so this must run off the render
// thread; the outcome lands in the handover fields for the button to surface.
// A re-click while a request is in flight is dropped.
func (inst *App) requestHandover() {
	inst.handoverMu.Lock()
	if inst.handoverBusy {
		inst.handoverMu.Unlock()
		return
	}
	inst.handoverBusy = true
	inst.handoverErr = ""
	inst.handoverNote = ""
	handle := inst.handle
	inst.handoverMu.Unlock()

	handle, note, err := inst.handover(handle, inst.res)

	inst.handoverMu.Lock()
	inst.handoverBusy = false
	if err != nil {
		inst.handoverErr = err.Error()
		inst.logger.Warn().Err(err).Msg("writingstylescope: handover to play failed")
	} else {
		inst.handle = handle
		inst.handoverNote = note
	}
	inst.handoverMu.Unlock()
}

// handover is the blocking body: publish (republishing under the same handle
// when one is already held, so repeated presses do not leak datasets) and then
// ask the window host for a play window bound to the introspection endpoint,
// which is where `keelson('<handle>')` resolves.
func (inst *App) handover(handle string, res *Analysis) (outHandle string, note string, err error) {
	if inst.bus == nil {
		err = eh.Errorf("no bus wired — the handover needs the app runtime")
		return
	}
	stream, err := pairsArrow(res)
	if err != nil {
		return
	}
	pub, err := adhocdata.PublishRequest(inst.bus, adhocdata.PublishInput{
		Alias:          datasetAlias,
		Handle:         handle,
		ArrowIPCStream: stream,
	})
	if err != nil {
		err = eh.Errorf("publish pairs dataset: %w", err)
		return
	}
	outHandle = pub.Handle

	cfg := launchcfg.PlayLaunch{
		Sql:      handoverSql(pub.Handle),
		AutoRun:  true,
		Endpoint: launchcfg.EndpointIntrospection,
	}
	cfgBytes, err := buscodec.Encode(cfg)
	if err != nil {
		err = eh.Errorf("encode play launch config: %w", err)
		return
	}
	if _, err = windowhost.RequestOpen(inst.bus, launchcfg.AppId, launchcfg.Kind, cfgBytes); err != nil {
		err = eh.Errorf("open play: %w", err)
		return
	}
	note = fmt.Sprintf("opened in play — %d pairs as %s (rev %d)", pub.Rows, pub.Handle, pub.Revision)
	return
}

// retractHandover drops the published dataset. Called from Unmount; a failure
// is logged rather than surfaced, because by then there is no window to
// surface it in and the store sweeps unreferenced datasets on restart anyway.
func (inst *App) retractHandover() {
	inst.handoverMu.Lock()
	handle := inst.handle
	inst.handle = ""
	inst.handoverMu.Unlock()
	if handle == "" || inst.bus == nil {
		return
	}
	if err := adhocdata.RetractRequest(inst.bus, handle); err != nil {
		inst.logger.Debug().Err(err).Msg("writingstylescope: retract on unmount")
	}
}

// handoverState snapshots the fields the button and its status line read.
func (inst *App) handoverState() (busy bool, note string, errText string) {
	inst.handoverMu.Lock()
	defer inst.handoverMu.Unlock()
	return inst.handoverBusy, inst.handoverNote, inst.handoverErr
}
