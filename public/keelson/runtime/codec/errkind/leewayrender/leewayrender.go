// Package leewayrender bridges a captured errkind.Error into the
// leeway widget pipeline (Table2CardEmitter and any other
// streamreadaccess.SinkI).
//
// Pipeline (per call):
//
//	errkind.Error → ErrorColumns.Marshal      (sparse-CBOR bytes)
//	              → cborarrow.Convert         (Arrow IPC stream)
//	              → ipc.NewReader              (arrow.RecordBatch)
//	              → Driver.DriveRecordBatch    (SinkI)
//
// The Driver / TableDesc / IR are shared singletons built lazily on
// first call — they describe the table schema, which is process-wide.
// Each Render allocates fresh RecordBatches because the error data is
// per-call.
//
// ADR-0042 M5+ retrofit of the original rowmarshall.leewayrender.
package leewayrender

import (
	"bytes"
	"sync"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"

	"github.com/stergiotis/boxer/public/keelson/runtime/codec/errkind"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/cborarrow"
)

// Render drives one errkind.Error through the full leeway widget
// pipeline and into `sink` (typically a leewaywidgets.Table2CardEmitter).
// An Error with no facts (Len(Messages) == 0) renders nothing — callers
// may invoke unconditionally.
//
// Suitable for per-frame use: the Driver and schema artefacts are
// cached; only the RowBinary payload + Arrow record batches are built
// fresh each call.
func Render(sink streamreadaccess.SinkI, e errkind.Error) (err error) {
	// Empty error tree — nothing to render.
	if len(e.Messages) == 0 && len(e.Funcs) == 0 && len(e.Sources) == 0 && len(e.FactIds) == 0 {
		return
	}
	var d *streamreadaccess.Driver
	d, err = driver()
	if err != nil {
		return
	}

	cols := &errkind.ErrorColumns{}
	cols.Append(e)
	var wire bytes.Buffer
	err = cols.Marshal(&wire)
	if err != nil {
		err = eh.Errorf("leewayrender: marshal cbor: %w", err)
		return
	}

	var arrowBuf bytes.Buffer
	err = cborarrow.Convert(&wire, &arrowBuf)
	if err != nil {
		err = eh.Errorf("leewayrender: cbor -> arrow: %w", err)
		return
	}

	var rd *ipc.Reader
	rd, err = ipc.NewReader(&arrowBuf)
	if err != nil {
		err = eh.Errorf("leewayrender: new ipc reader: %w", err)
		return
	}
	defer rd.Release()

	for rd.Next() {
		rec := rd.RecordBatch()
		err = d.DriveRecordBatch(sink, rec)
		rec.Release()
		if err != nil {
			err = eh.Errorf("leewayrender: drive record batch: %w", err)
			return
		}
	}
	if rdErr := rd.Err(); rdErr != nil {
		err = eh.Errorf("leewayrender: ipc reader: %w", rdErr)
	}
	return
}

var (
	driverOnce  sync.Once
	driverCache *streamreadaccess.Driver
	driverErr   error
)

// driver returns the lazily-built singleton Driver for runtime.facts.
// First call resolves the schema via factsschema.GetSchemaInManipulator,
// the IR via common.NewIntermediateTableRepresentation, and the Driver
// via streamreadaccess.NewDriver. Subsequent calls reuse the cached
// instance.
func driver() (out *streamreadaccess.Driver, err error) {
	driverOnce.Do(func() {
		manip, ferr := factsschema.GetSchemaInManipulator()
		if ferr != nil {
			driverErr = eh.Errorf("leewayrender: schema manipulator: %w", ferr)
			return
		}
		td, ferr := manip.BuildTableDesc()
		if ferr != nil {
			driverErr = eh.Errorf("leewayrender: build table desc: %w", ferr)
			return
		}
		tech := clickhouse.NewTechnologySpecificCodeGenerator()
		ir := common.NewIntermediateTableRepresentation()
		ferr = ir.LoadFromTable(&td, tech)
		if ferr != nil {
			driverErr = eh.Errorf("leewayrender: load IR: %w", ferr)
			return
		}
		d, ferr := streamreadaccess.NewDriver(&td, ir, streamreadaccess.DefaultFormatters())
		if ferr != nil {
			driverErr = eh.Errorf("leewayrender: new driver: %w", ferr)
			return
		}
		driverCache = d
	})
	if driverErr != nil {
		err = driverErr
		return
	}
	out = driverCache
	return
}
