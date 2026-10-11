package canonwire

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// GoClassBuilder emits the canonical-wire classes of one table into a code
// builder. It is the peer of the readaccess and dml builders, but it walks the
// slot table of ADR-0210 SD2 rather than the table's sections: the wire is
// organised by slot, and a co-section group is one slot with several sections
// inside it.
type GoClassBuilder struct {
	builder *strings.Builder
	// encoderOnly leaves out the dispatcher and the decoder.
	encoderOnly bool
}

// EncoderOnly makes the builder emit the slot keys, the slot enum, the
// tagger and the encoder, and leave out the dispatcher and the decoder. A
// consumer that only hashes rows needs no decoder, and a table whose dml
// builder keeps its control surface unexported (the record store's
// internal scaffolding, ADR-0100 SD6) cannot host one.
func (inst *GoClassBuilder) EncoderOnly() {
	inst.encoderOnly = true
}

// GeneratorDriver turns a table description into the source of one Go file.
// Its shape mirrors the readaccess and dml drivers so the CLI subcommand can be
// the same three lines.
type GeneratorDriver struct {
	builder          *GoClassBuilder
	validator        *common.TableValidator
	namingConvention common.NamingConventionI
	tech             common.TechnologySpecificGeneratorI
}
