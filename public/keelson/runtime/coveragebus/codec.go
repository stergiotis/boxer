package coveragebus

import (
	"github.com/RoaringBitmap/roaring"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/coverage/covsnap"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// Codec is the wire seam of the coverage plane. The interim CBOR codec is
// the sysmetricsbus position (ADR-0090 §SD3 precedent): the facts bus codec
// is the planned replacement, and this interface is the swap point
// (ADR-0089 keeps the bus wire distinct from the ingest wire either way).
type Codec interface {
	Encode(upd *covsnap.Update) (payload []byte, err error)
	Decode(payload []byte) (upd *covsnap.Update, err error)
}

// wireUpdate is the CBOR shape of one Update. The covered set travels as
// the roaring serialization (the format-spec'd portable bytes), not as an
// expanded array.
type wireUpdate struct {
	MetaHash        [16]byte
	Seq             uint64
	SampledAtUnixMs int64
	Full            bool
	Units           []byte
	Status          covsnap.RunStatus
	Pkgs            []covsnap.PkgSample
	Funcs           []covsnap.FuncSample
}

type CBORCodec struct{}

func NewCBORCodec() (c CBORCodec) {
	return CBORCodec{}
}

func (c CBORCodec) Encode(upd *covsnap.Update) (payload []byte, err error) {
	w := wireUpdate{
		MetaHash:        upd.MetaHash,
		Seq:             upd.Seq,
		SampledAtUnixMs: upd.SampledAtUnixMs,
		Full:            upd.Full,
		Status:          upd.Status,
		Pkgs:            upd.Pkgs,
		Funcs:           upd.Funcs,
	}
	if upd.Units != nil {
		w.Units, err = upd.Units.ToBytes()
		if err != nil {
			return nil, eh.Errorf("coveragebus: unable to serialize covered set: %w", err)
		}
	}
	payload, err = cbor.Marshal(&w)
	if err != nil {
		return nil, eh.Errorf("coveragebus: unable to encode update: %w", err)
	}
	return
}

func (c CBORCodec) Decode(payload []byte) (upd *covsnap.Update, err error) {
	var w wireUpdate
	err = cbor.Unmarshal(payload, &w)
	if err != nil {
		return nil, eh.Errorf("coveragebus: unable to decode update: %w", err)
	}
	units := roaring.New()
	if len(w.Units) > 0 {
		err = units.UnmarshalBinary(w.Units)
		if err != nil {
			return nil, eh.Errorf("coveragebus: unable to deserialize covered set: %w", err)
		}
	}
	upd = &covsnap.Update{
		MetaHash:        w.MetaHash,
		Seq:             w.Seq,
		SampledAtUnixMs: w.SampledAtUnixMs,
		Full:            w.Full,
		Units:           units,
		Status:          w.Status,
		Pkgs:            w.Pkgs,
		Funcs:           w.Funcs,
	}
	return
}
