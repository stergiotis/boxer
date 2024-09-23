//go:build !bootstrap

package demo

import (
	"github.com/stergiotis/boxer/public/imzero/dto"
	"github.com/stergiotis/boxer/public/imzero/imgui"
)

func MakeParagraphDemo() func() {
	return func() {
		imgui.PushIsParagraphText(dto.IsParagraphTextAlways)
		imgui.TextUnformatted("this is a paragraph text")
		imgui.PopIsParagraphText()
	}
}
