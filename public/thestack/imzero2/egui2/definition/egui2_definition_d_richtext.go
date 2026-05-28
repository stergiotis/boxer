package definition

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
)

func definitionsText() (widgets []*ir.BuilderFactoryNode) {
	// PushRichText and PushRichTextColored have been replaced by the
	// Atoms().RichText(text).Strong().EndRichText() inline sub-protocol
	// defined in egui2_definition_d_evaluated.go.
	return nil
}
