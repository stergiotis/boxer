package playground

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

// The kindcheck registration lives beside the DTO declaration (not in
// the generated .out.go) so the generator stays untouched; the probe is
// one call through the module's own generated decoder. The window host
// runs it before an argument-carrying open reaches the app's Mount
// (ADR-0135 §SD1).
func init() {
	kindcheck.Register(Kind, func(b []byte) (err error) {
		_, err = buscodec.Decode[Launch](b)
		return
	})
}
