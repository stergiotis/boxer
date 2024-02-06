//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/application"
	"github.com/stergiotis/boxer/public/imzero/nerdfont/widget"
)

func MakeNerdfontDemo(app *application.Application) func() {
	return widget.MakeIconPickRender(app.IconFont)
}
