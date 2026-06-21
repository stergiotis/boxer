package sysmetricsbus

import (
	"errors"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Codec encodes a BundleSnapshot to bus bytes and back. The Producer and
// Consumer hold a Codec so the wire format is swappable without touching
// them: P2 ships CBORCodec; ADR-0090 SD3's leeway-facts codec is the
// planned replacement.
type Codec interface {
	Encode(snap *sysmsnap.BundleSnapshot) (payload []byte, err error)
	Decode(payload []byte) (snap *sysmsnap.BundleSnapshot, err error)
}

// CBORCodec is the P2 interim codec (ADR-0090 SD3). It uses fxamacker/cbor
// — already a project dependency, so it adds none. The chosen production
// wire is the runtime.facts SoA codec; CBORCodec gets the bisection working
// first and the Codec seam keeps the swap local.
type CBORCodec struct{}

var _ Codec = CBORCodec{}

// NewCBORCodec returns the interim CBOR codec.
func NewCBORCodec() (c CBORCodec) { return }

// wireBundle carries a BundleSnapshot over the wire. It embeds the snapshot
// by value but moves Errors out as strings: CBOR cannot encode the error
// interface (an errors.New value has no exported fields and would serialise
// to an empty map, dropping the message). Snap.Errors is nil on the wire;
// Errors carries the messages.
type wireBundle struct {
	Snap   sysmsnap.BundleSnapshot    `cbor:"1,keyasint"`
	Errors map[sysmsnap.Domain]string `cbor:"2,keyasint"`
}

func (CBORCodec) Encode(snap *sysmsnap.BundleSnapshot) (payload []byte, err error) {
	if snap == nil {
		err = eh.Errorf("sysmetricsbus: encode nil snapshot")
		return
	}
	w := wireBundle{Snap: *snap} // shallow copy; we only blank Errors on the copy
	if len(snap.Errors) > 0 {
		w.Errors = make(map[sysmsnap.Domain]string, len(snap.Errors))
		for k, v := range snap.Errors {
			if v != nil {
				w.Errors[k] = v.Error()
			}
		}
	}
	w.Snap.Errors = nil // never encode the error-interface map
	payload, err = cbor.Marshal(w)
	if err != nil {
		err = eh.Errorf("sysmetricsbus: cbor marshal: %w", err)
	}
	return
}

func (CBORCodec) Decode(payload []byte) (snap *sysmsnap.BundleSnapshot, err error) {
	var w wireBundle
	err = cbor.Unmarshal(payload, &w)
	if err != nil {
		err = eh.Errorf("sysmetricsbus: cbor unmarshal: %w", err)
		return
	}
	s := w.Snap
	if len(w.Errors) > 0 {
		s.Errors = make(map[sysmsnap.Domain]error, len(w.Errors))
		for k, v := range w.Errors {
			s.Errors[k] = errors.New(v)
		}
	}
	snap = &s
	return
}
