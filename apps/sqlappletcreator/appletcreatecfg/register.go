package appletcreatecfg

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

// The kindcheck registration lives beside the DTO declaration (not in the
// generated .out.go) so the generator stays untouched; the probe is one call
// through the module's own generated decoder. The window host runs it before
// an argument-carrying open of the creator reaches Mount.
func init() {
	kindcheck.Register(Kind, func(b []byte) (err error) {
		_, err = buscodec.Decode[AppletCreate](b)
		return
	})
}
