//go:build binary_log
// +build binary_log

package logging

import (
	cbor2 "github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func UnmarshallZerologMsg(msg []byte) (v interface{}, err error) {
	err = cbor2.Unmarshal(msg, &v)
	if err != nil {
		err = eb.Build().Bytes("msg", msg).Errorf("unable to unmarshall cbor zerolog msg: %w", err)
		return
	}
	return
}
func convertToCBOR(msg []byte) (retr []byte, err error) {
	retr = msg
	return
}

var zerologCborMessages = true
