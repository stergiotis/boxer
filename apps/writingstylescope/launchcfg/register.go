package launchcfg

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// The kindcheck registration lives beside the DTO declaration (not in the
// generated .out.go) so the generator stays untouched; the probe is one call
// through the module's own generated decoder. The window host runs it before
// an argument-carrying open reaches Mount.
func init() {
	kindcheck.Register(Kind, func(b []byte) (err error) {
		_, err = buscodec.Decode[WritingstylescopeLaunch](b)
		return
	})
}
