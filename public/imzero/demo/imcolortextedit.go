//go:build !bootstrap

package demo

import "github.com/stergiotis/boxer/public/imzero/imcolortextedit/demo"

func MakeRenderImColorTextDemo() func() {
	return demo.MakeColorTextEditDemo()
}
