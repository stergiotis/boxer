package play

import (
	"bytes"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/streamenc"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// play_identity.go computes a leeway record's two canonical identities
// (ADR-0219 SD1–SD4) from the result batch: the canonform digest — the
// content identity of ADR-0201 — and the canonwire fingerprint — the
// representation identity over the lossless wire of ADR-0210. Both are
// streamreadaccess sinks driven over the CardDriver's reconstructed table, so
// they and the card agree on every column. The Detail strip drives one-row
// slices on the render thread; the Table job drives the whole record off it.
// The two agree by construction — the parity suite under canonwire/streamenc
// pins a one-row slice to the whole-batch bytes.

// identityValues is what the panes show for one row.
type identityValues struct {
	canon   [canonform.Blake3DigestSize]byte
	wire    [cwruntime.FingerprintSize]byte
	wireLen int
	// wireErr is the runtime's table-free verdict over the wire item: nil
	// when VerifyCanonical accepts it. It is shown beside the fingerprint so
	// a developer sees an item the checker refuses in the same glance.
	wireErr error
}

// identityCanonformOptions fixes the canonform parameters play digests under
// (ADR-0219 SD2): the classifier the card emitter uses, so the primary /
// secondary split the card draws is the one the hash counted; the entity id
// excluded, so "same content, different id" compares equal; the default
// plains mask and digester. The encoder's FormPin names all of it.
func identityCanonformOptions() canonform.Options {
	return canonform.Options{Classifier: membershiprole.PathPrefixClassifier{}}
}

// identityComputer holds the two encoders and the driver over one table.
// It is not goroutine-safe; the Table job owns one, the Detail strip another.
type identityComputer struct {
	pin    string
	driver *streamreadaccess.Driver
	canon  *canonform.Encoder
	wire   *streamenc.Encoder
	fp     *cwruntime.Fingerprinter
}

// newIdentityComputer binds the encoders to tbl / ir, which must be the pair
// driver was built from.
func newIdentityComputer(tbl *common.TableDesc, ir *common.IntermediateTableRepresentation, driver *streamreadaccess.Driver) (inst *identityComputer, err error) {
	if tbl == nil || ir == nil || driver == nil {
		err = eh.Errorf("play: identity needs the reconstructed table, its IR and a driver")
		return
	}
	inst = &identityComputer{driver: driver, fp: cwruntime.NewFingerprinter()}
	inst.canon, err = canonform.NewEncoder(ir, identityCanonformOptions())
	if err != nil {
		err = eh.Errorf("play: canonform encoder: %w", err)
		return nil, err
	}
	inst.pin, err = inst.canon.FormPin()
	if err != nil {
		err = eh.Errorf("play: canonform pin: %w", err)
		return nil, err
	}
	inst.wire, err = streamenc.NewEncoder(tbl, ir)
	if err != nil {
		err = eh.Errorf("play: canonwire stream encoder: %w", err)
		return nil, err
	}
	return
}

// drive runs both encoders over rec. After it, count / values read the
// per-row results until the next drive.
func (inst *identityComputer) drive(rec arrow.RecordBatch) (err error) {
	if err = inst.driver.DriveRecordBatch(inst.canon, rec); err != nil {
		return eh.Errorf("play: canonform drive: %w", err)
	}
	if err = inst.canon.Err(); err != nil {
		return eh.Errorf("play: canonform: %w", err)
	}
	if err = inst.driver.DriveRecordBatch(inst.wire, rec); err != nil {
		return eh.Errorf("play: canonwire drive: %w", err)
	}
	if err = inst.wire.Err(); err != nil {
		return eh.Errorf("play: canonwire: %w", err)
	}
	if inst.canon.NumRecords() != inst.wire.NumEntities() {
		return eh.Errorf("play: the two encoders saw different entity counts")
	}
	return
}

// count is the number of rows the last drive covered.
func (inst *identityComputer) count() int { return inst.canon.NumRecords() }

// values returns the i-th row's identities from the last drive.
func (inst *identityComputer) values(i int) (v identityValues) {
	copy(v.canon[:], inst.canon.RecordDigest(i))
	item := inst.wire.Entity(i)
	v.wire = inst.fp.Sum(item)
	v.wireLen = len(item)
	v.wireErr = cwruntime.VerifyCanonical(item)
	return
}

// row drives a one-row slice of rec and returns that row's identities.
func (inst *identityComputer) row(rec arrow.RecordBatch, row int64) (v identityValues, err error) {
	if rec == nil || row < 0 || row >= rec.NumRows() {
		err = eh.Errorf("play: identity row out of range")
		return
	}
	slice := rec.NewSlice(row, row+1)
	defer slice.Release()
	if err = inst.drive(slice); err != nil {
		return
	}
	if inst.count() != 1 {
		err = eh.Errorf("play: a one-row slice yielded %d entities", inst.count())
		return
	}
	v = inst.values(0)
	return
}

// rowItems returns the CBOR items behind one row's identities (ADR-0219
// SD5): the canonform attribute items followed by its entity item, as a CBOR
// sequence — captured through the recording digester, the seam ADR-0201
// SD7 keeps for exactly this — and the canonwire entity item. The digests
// are unchanged by the capture. Both slices are the caller's.
func (inst *identityComputer) rowItems(ir *common.IntermediateTableRepresentation, rec arrow.RecordBatch, row int64) (canonItems []byte, wireItem []byte, err error) {
	if rec == nil || row < 0 || row >= rec.NumRows() {
		err = eh.Errorf("play: identity row out of range")
		return
	}
	var leaves, record bytes.Buffer
	opts := identityCanonformOptions()
	opts.Digester = canonform.NewRecordingDigester(canonform.NewBlake3Digester(), &leaves, &record)
	rec2, err := canonform.NewEncoder(ir, opts)
	if err != nil {
		err = eh.Errorf("play: canonform recording encoder: %w", err)
		return
	}
	slice := rec.NewSlice(row, row+1)
	defer slice.Release()
	if err = inst.driver.DriveRecordBatch(rec2, slice); err != nil {
		err = eh.Errorf("play: canonform drive: %w", err)
		return
	}
	if err = rec2.Err(); err != nil {
		return
	}
	canonItems = make([]byte, 0, leaves.Len()+record.Len())
	canonItems = append(canonItems, leaves.Bytes()...)
	canonItems = append(canonItems, record.Bytes()...)
	if err = inst.driver.DriveRecordBatch(inst.wire, slice); err != nil {
		err = eh.Errorf("play: canonwire drive: %w", err)
		return
	}
	if err = inst.wire.Err(); err != nil {
		return
	}
	if inst.wire.NumEntities() != 1 {
		err = eh.Errorf("play: a one-row slice yielded %d wire entities", inst.wire.NumEntities())
		return
	}
	wireItem = append([]byte(nil), inst.wire.Entity(0)...)
	return
}
