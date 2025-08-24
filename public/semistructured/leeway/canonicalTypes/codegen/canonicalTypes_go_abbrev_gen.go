package codegen

import (
	"fmt"
	"io"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicalTypes/sample"
	"github.com/stoewer/go-strcase"
)

func GenerateGoAbbrev(packageName string, imp string, astPackage string, w io.Writer, accept func(ct canonicalTypes.PrimitiveAstNodeI) (keep bool)) (err error) {
	_, err = golang.AddCodeGenComment(w, CodeGeneratorName)
	if err != nil {
		return
	}
	if imp != "" {
		imp = "import \"" + imp + "\""
	}
	if astPackage != "" {
		astPackage = astPackage + "."
	}
	_, err = fmt.Fprintf(w, "package %s\n%s\n", packageName, imp)
	if err != nil {
		return
	}
	for n := uint64(0); n < sample.SampleTypeMaxExcl; n++ {
		typ := sample.GenerateSampleType(n)
		if !typ.IsValid() {
			log.Debug().Str("debug", fmt.Sprintf("%#v", typ)).Stringer("typ", typ).Msg("skipping invalid type")
			continue
		}
		if typ.IsStringNode() && strings.ContainsAny(typ.String(), "012345678910") {
			// skipping fixed length string type
			continue
		}
		if accept == nil || accept(typ) {
			_, err = fmt.Fprintf(w, "var %s = %s", strcase.UpperCamelCase(typ.String()), astPackage)
			if err != nil {
				return
			}
			err = typ.GenerateGoCode(w)
			if err != nil {
				return
			}
			_, err = fmt.Fprint(w, "\n")
			if err != nil {
				return
			}
		} else {
			log.Info().Stringer("type", typ).Msg("skipping type")
		}
	}
	return
}
