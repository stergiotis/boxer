package golang

import (
	"fmt"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
)

var ErrInvalidCodeGeneratorName = eh.Errorf("code generator name is invalid")

func ValidateCodeGeneratorName(generator string) (err error) {
	l := len(generator)
	if l == 0 || l > 256 || strings.ContainsAny(generator, "\n\r") {
		err = ErrInvalidCodeGeneratorName
	}
	return
}

func AddCodeGenComment(out io.Writer, generator string) (n int, err error) {
	err = ValidateCodeGeneratorName(generator)
	if err != nil {
		return
	}
	return fmt.Fprintf(out, "// Code generated; %s DO NOT EDIT.\n\n", generator)
}
