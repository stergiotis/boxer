package play

import (
	"encoding/hex"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// play_table_identity.go is the Table pane's side of ADR-0219 (SD7): an
// options-bar toggle reveals two synthetic trailing columns in the per-DB-row
// grid — the canonform digest and the canonwire fingerprint of every row —
// filled from a per-result cache that one background job per result
// computes by driving the whole record once through both encoders. Cells
// read "…" until the job lands; a new result abandons the old job by
// ResultID; an error is shown in the cells.

// identityColKindE is which synthetic column a cell or header belongs to.
type identityColKindE uint8

const (
	identityColCanonform identityColKindE = iota
	identityColCanonwire
	identityColCount
)

// identityColName is the header caption and the column-width identity's name
// (ADR-0151 §SD1): fixed, never sampled.
func (k identityColKindE) name() string {
	switch k {
	case identityColCanonform:
		return "canonform"
	case identityColCanonwire:
		return "canonwire"
	}
	return "identity"
}

func (k identityColKindE) hover() string {
	switch k {
	case identityColCanonform:
		return "canonform digest (ADR-0201): the content identity — equal for two rows whose content is the same whatever their widths, aspects, section names or order. Hover a cell for the full value; the Detail pane copies it."
	case identityColCanonwire:
		return "canonwire fingerprint (ADR-0210, ADR-0219): keyed BLAKE3 over the lossless wire item — equal for two rows that are the same record, widths and channels included. Hover a cell for the full value; the Detail pane copies it."
	}
	return ""
}

// identityColumnKey is the width-tier identity of a synthetic column: its
// caption as the name and a fixed format tag as the type, so it never
// borrows a width from an Arrow column and the Arrow columns never borrow
// one from it.
func identityColumnKey(k identityColKindE) colwidth.Column {
	return colwidth.Column{Name: k.name(), Type: "identity" + tableViewTagRow}
}

// identityCellRunes is how many hex characters a cell shows; the whole
// value is on hover. Sixteen characters are 64 bits: enough to tell rows
// apart by eye, narrow enough to leave the data columns their room.
const identityCellRunes = 16

// identityColWidth is the synthetic columns' seed width.
func identityColWidth(cellPadX float32) float32 {
	return float32(identityCellRunes)*colCharPx + 16.0 + 2*cellPadX
}

// identitySynthCols returns the sentinel column indices the per-DB-row grid
// appends after the Arrow columns — schema.NumFields()+k for kind k — or nil
// when the toggle is off or the result is not leeway-shaped. A sentinel is
// never a valid Arrow index, which is what keeps every Arrow-indexed cache
// (widths, labels, glosses) out of its way.
func (inst *PlayApp) identitySynthCols(schema *arrow.Schema) (synth []int) {
	if !inst.tableOpts.showHashes || schema == nil || inst.leewayColumnClasses(schema) == nil {
		return nil
	}
	base := schema.NumFields()
	synth = make([]int, 0, int(identityColCount))
	for k := range int(identityColCount) {
		synth = append(synth, base+k)
	}
	return
}

// identityJob computes both identities for every row of one result, off the
// render thread. It owns its driver and encoders (built on the render thread
// by ensureIdentityJob, then handed over) and a retained reference to the
// record, released when the drive ends.
type identityJob struct {
	result ResultID

	mu     sync.Mutex
	done   bool
	err    error
	n      int
	canon  []byte // n × canonform.Blake3DigestSize
	wire   []byte // n × cwruntime.FingerprintSize
	broken []bool // per row: VerifyCanonical refused the wire item
}

// ensureIdentityJob returns the job for result, starting one if the current
// job is for another result. A job that cannot be built (no driver for the
// schema) is recorded as done with its error, so the cells say why.
func (inst *PlayApp) ensureIdentityJob(rec arrow.RecordBatch, result ResultID) *identityJob {
	if inst.identityJob != nil && inst.identityJob.result == result {
		return inst.identityJob
	}
	inst.identityJob.stop()
	job := &identityJob{result: result}
	inst.identityJob = job
	driver, err := inst.cards.NewDetachedDriver()
	if err == nil && driver == nil {
		err = eh.Errorf("play: result is not leeway-shaped")
	}
	var comp *identityComputer
	if err == nil {
		comp, err = newIdentityComputer(inst.cards.TableDesc(), inst.cards.IR(), driver)
	}
	if err != nil {
		job.finish(nil, err)
		return job
	}
	rec.Retain()
	go func() {
		defer rec.Release()
		derr := comp.drive(rec)
		job.finish(comp, derr)
	}()
	return job
}

// finish publishes the drive's outcome. comp is read here, once, on the
// job's goroutine; nothing reads it afterwards.
func (inst *identityJob) finish(comp *identityComputer, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.done = true
	inst.err = err
	if err != nil || comp == nil {
		if err != nil {
			log.Debug().Err(err).Msg("play: identity job failed")
		}
		return
	}
	n := comp.count()
	inst.n = n
	inst.canon = make([]byte, 0, n*canonform.Blake3DigestSize)
	inst.wire = make([]byte, 0, n*cwruntime.FingerprintSize)
	inst.broken = make([]bool, n)
	for i := range n {
		v := comp.values(i)
		inst.canon = append(inst.canon, v.canon[:]...)
		inst.wire = append(inst.wire, v.wire[:]...)
		inst.broken[i] = v.wireErr != nil
	}
}

// stop abandons the job: its goroutine still finishes the drive it is in
// (a driver cannot be interrupted mid-batch) and publishes into a job
// nothing reads any more. nil-safe.
func (inst *identityJob) stop() {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	inst.done = true
	inst.mu.Unlock()
}

// cell returns the text a synthetic cell shows for row and the hover text
// behind it: the short hex once the job has landed, "…" while it runs, the
// error when it failed.
func (inst *identityJob) cell(k identityColKindE, row int64) (text string, hover string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.done {
		return "…", "computing"
	}
	if inst.err != nil {
		return "error", inst.err.Error()
	}
	if row < 0 || int(row) >= inst.n {
		return "", ""
	}
	var full string
	switch k {
	case identityColCanonform:
		full = hex.EncodeToString(inst.canon[int(row)*canonform.Blake3DigestSize : (int(row)+1)*canonform.Blake3DigestSize])
	case identityColCanonwire:
		full = hex.EncodeToString(inst.wire[int(row)*cwruntime.FingerprintSize : (int(row)+1)*cwruntime.FingerprintSize])
		if inst.broken[row] {
			full += " — the wire item is not canonical"
		}
	}
	text = full
	if len(text) > identityCellRunes {
		text = text[:identityCellRunes]
	}
	return text, full
}
