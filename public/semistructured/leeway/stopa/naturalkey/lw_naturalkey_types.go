package naturalkey

import (
	"bytes"

	"github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type ResolverI interface {
	GetNaturalKeyId() (id identifier.TaggedId)
	ResolveNaturalKey(naturalKey []byte) (id identifier.TaggedId, err error)
	MustResolveNaturalKey(naturalKey []byte) (id identifier.TaggedId)
}
type ResolverHRI interface {
	GetNaturalKeyId() (id identifier.TaggedId)
	ResolveNaturalKeyHR(naturalKey naming.StylableName) (id identifier.TaggedId, err error)
	CanonicalizeNaturalKeyHR(naturalKey naming.StylableName) (canonicalized naming.StylableName)
	MustResolveNaturalKeyHR(naturalKey naming.StylableName) (id identifier.TaggedId)
}

type Encoder struct {
	encCbor *cbor.Encoder
	encJson *jsontext.Encoder
	bufCbor *bytes.Buffer
	bufJson *bytes.Buffer
	errs    []error
	state   naturalKeyEncoderStateE
}
type SerializationFormatE uint8
type naturalKeyEncoderStateE uint8
