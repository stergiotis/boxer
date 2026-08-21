package launchcfg

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

// The kind probe the window host runs before it delivers or stores a config
// claiming this kind (ADR-0135 §SD4, ADR-0148 §SD4): the bytes must decode.
func init() {
	kindcheck.Register(Kind, func(b []byte) (err error) {
		_, err = buscodec.Decode[TallyLaunch](b)
		return
	})
}
