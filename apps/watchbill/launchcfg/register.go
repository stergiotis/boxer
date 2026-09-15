package launchcfg

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

// The kindcheck registration lives beside the DTO declaration (not in the
// generated .out.go) so the generator stays untouched.
func init() {
	kindcheck.Register(Kind, func(b []byte) (err error) {
		_, err = buscodec.Decode[WatchbillLaunch](b)
		return
	})
}
