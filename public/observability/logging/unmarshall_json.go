//go:build !binary_log

package logging

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func UnmarshallZerologMsg(msg []byte) (v any, err error) {
	err = json.UnmarshalDecode(jsontext.NewDecoder(bytes.NewReader(msg)),
		&v,
		json.DefaultOptionsV2())
	if err != nil {
		err = eb.Build().Bytes("msg", msg).Errorf("unable to unmarshall json zerolog msg: %w", err)
		return
	}
	return
}
func convertToCBOR(msg []byte) (retr []byte, err error) {
	// FIXME use zerolog's streaming implementation
	var v any
	v, err = UnmarshallZerologMsg(msg)
	if err != nil {
		err = eh.Errorf("unable to convert zerolog message to cbor: %w", err)
		return
	}
	retr, err = cbor.Marshal(v)
	if err != nil {
		err = eh.Errorf("unable to convert zerolog message to cbor: %w", err)
		return
	}
	return
}
