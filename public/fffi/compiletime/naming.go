package compiletime

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"strconv"
	"strings"
	"unicode"
)

type Namer struct {
	titleCase cases.Caser
	cfg       *NamerConfig
}

type NamerConfig struct {
	RuneCppType         string
	nValidationMessages int
	validated           bool
}

func (inst *NamerConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  nameTransf("runeCppType"),
			Value: inst.RuneCppType,
		},
	}
}

func (inst *NamerConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.RuneCppType = ctx.String(nameTransf("runeCppType"))
	return inst.Validate(true)
}

func (inst *NamerConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.Configer = (*NamerConfig)(nil)

func NewNamer(cfg *NamerConfig) *Namer {
	caser := cases.Title(language.English, cases.Compact)
	return &Namer{
		titleCase: caser,
		cfg:       cfg,
	}
}

func (inst *Namer) GoTypeNameToCppTypeName(name string) (r string, err error) {
	switch name {
	case "bool", "int":
		r = name
		break
	case "uint":
		r = "unsigned"
		break
	case "float32":
		r = "float"
		break
	case "float64":
		r = "double"
		break
	case "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "uintptr":
		r = name + "_t"
		break
	case "[]uint8", "[]uint16", "[]uint32", "[]uint64", "[]int8", "[]int16", "[]int32":
		r = strings.TrimPrefix(name, "[]") + "_t*"
		break
	case "[]string":
		r = "const char* const"
		break
	case "byte":
		r = "uint8_t"
		break
	case "rune":
		r = inst.cfg.RuneCppType
		break
	case "string", "[]byte":
		r = "const char *"
		break
	case "complex64":
		r = "float*"
		break
	case "complex128":
		r = "double*"
		break
	default:
		if isArrayType(name) || isSliceType(name) {
			var rest string
			_, rest, err = splitArrayOrSliceType(name)
			if err != nil {
				err = eh.Errorf("unable to split go array or slice type: %w", err)
				return
			}
			var restCpp string
			restCpp, err = inst.GoTypeNameToCppTypeName(rest)
			if err != nil {
				err = eb.Build().Str("name", name).Str("rest", rest).Errorf("unable to convert rest of slice or array to cpp typename: %w", err)
				return
			}
			r = restCpp + "*"
		} else {
			err = eb.Build().Str("name", name).Errorf("unable to convert go type to cpp type name")
			return
		}
	}
	return
}
func (inst *Namer) GoTypeNameToSendRecvFuncNameSuffix(name string) (r string, err error) {
	switch name {
	case "string", "bool", "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "float32", "float64", "int", "complex64", "complex128", "uintptr", "rune", "uint":
		r = inst.titleCase.String(name)
		break
	case "byte":
		r = "Uint8"
		break
	case "[]byte":
		r = "Bytes"
		break
	case "[]string":
		r = "Strings"
		break
	default:
		if isArrayType(name) || isSliceType(name) {
			var rest string
			var l int
			l, rest, err = splitArrayOrSliceType(name)
			if err != nil {
				err = eh.Errorf("unable to split go array or slice type: %w", err)
				return
			}
			var r2 string
			r2, err = inst.GoTypeNameToSendRecvFuncNameSuffix(rest)
			if err != nil {
				err = eb.Build().Str("name", name).Str("rest", rest).Errorf("unable to convert rest of slice or array to function name suffix: %w", err)
				return
			}
			if l < 0 {
				r = fmt.Sprintf("%sSlice", r2)
			} else {
				r = fmt.Sprintf("%sArray%d", r2, l)
			}
		} else {
			err = eb.Build().Str("name", name).Errorf("unable to convert go type to cpp receiver function name")
			return
		}
	}
	return
}
func (inst *Namer) GoTypeNameToRecvExprCpp(name string) (r string, err error) {
	switch name {
	case "[]string":
		return "receiveStrings()", nil
	}
	return inst.goTypeNameToExprCpp(name, "", "receive", false)
}
func (inst *Namer) GoTypeNameToSendExprCpp(name string, varname string) (r string, err error) {
	return inst.goTypeNameToExprCpp(name, varname, "send", true)
}
func (inst *Namer) goTypeNameToExprCpp(name string, varname string, prefix string, send bool) (r string, err error) {
	if isStringType(name) {
		r = fmt.Sprintf("%sString(%s)", prefix, varname)
	} else if isPointerType(name) && send {
		r = fmt.Sprintf("%sValue(%s)", prefix, varname)
	} else if isSupportedValueType(name) {
		switch name {
		case "int", "int8", "int16", "int32", "int64":
			{
				var tn string
				tn, err = inst.GoTypeNameToCppTypeName(name)
				if err != nil {
					err = eh.Errorf("unable to compose ValueSignMagnitude template param: %w", err)
					return
				}
				r = fmt.Sprintf("%sValueSignMagnitude<%s>(%s)", prefix, tn, varname)
			}
			return
		case "complex64":
			r = fmt.Sprintf("%sArray<%s,%d>(%s)", prefix, "float", 2, varname)
			return
		case "complex128":
			r = fmt.Sprintf("%sArray<%s,%d>(%s)", prefix, "double", 2, varname)
			return
		}
		var tn string
		tn, err = inst.GoTypeNameToCppTypeName(name)
		if err != nil {
			err = eh.Errorf("unable to get cpp type name for go type: %w", err)
			return
		}
		r = fmt.Sprintf("%sValue<%s>(%s)", prefix, tn, varname)
	} else if isSupportedSliceType(name) {
		var tn string
		tn, err = inst.GoTypeNameToCppTypeName(strings.TrimPrefix(name, "[]"))
		if err != nil {
			err = eh.Errorf("unable to get cpp type name for go type: %w", err)
			return
		}
		tn = strings.Trim(tn, "*")
		if varname == "" {
			r = fmt.Sprintf("%sSlice<%s>()", prefix, tn)
		} else {
			r = fmt.Sprintf("%sSlice<%s>(%s,%s_len)", prefix, tn, varname, varname)
		}
	} else if isSupportedArrayType(name) {
		var l int
		var rest string
		l, rest, err = splitArrayOrSliceType(name)
		if err != nil {
			err = eh.Errorf("unable to split go array or slice type: %w", err)
			return
		}
		var restCpp string
		restCpp, err = inst.GoTypeNameToCppTypeName(rest)
		if err != nil {
			err = eb.Build().Str("name", name).Str("rest", rest).Errorf("unable to translate supported array type to cpp: %w", err)
			return
		}
		r = fmt.Sprintf("%sArray<%s,%d>(%s)", prefix, restCpp, l, varname)
	} else {
		err = eb.Build().Str("name", name).Errorf("unable to convert go type to cpp expr")
		return
	}
	return
}
func isStringType(name string) bool {
	return name == "string"
}
func isPointerType(name string) bool {
	return name == "uintptr"
}
func isSupportedValueType(name string) bool {
	switch name {
	case "byte", "bool", "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "float32", "float64", "int", "complex64", "complex128", "uintptr", "rune", "uint":
		return true
	}
	return false
}
func isSupportedSliceType(name string) bool {
	if !isSliceType(name) {
		return false
	}
	var rest string
	var err error
	_, rest, err = splitArrayOrSliceType(name)
	if err != nil {
		log.Fatal().Err(err).Msg("found inconsistent type")
		return false
	}
	return isSupportedValueType(rest) || rest == "string"
}
func isSupportedArrayType(name string) bool {
	if !isArrayType(name) {
		return false
	}
	var rest string
	var err error
	_, rest, err = splitArrayOrSliceType(name)
	if err != nil {
		log.Fatal().Err(err).Msg("found inconsistent type")
		return false
	}
	return isSupportedValueType(rest)
}
func isArrayType(name string) bool {
	return len(name) > 3 && name[0] == '[' && unicode.IsDigit(rune(name[1]))
}
func isSliceType(name string) bool {
	return len(name) > 2 && name[0] == '[' && name[1] == ']'
}
func splitArrayOrSliceType(name string) (len int, rest string, err error) {
	idx := strings.IndexByte(name, ']')
	if idx < 0 {
		err = eb.Build().Str("name", name).Errorf("go type name is not a go array type of the form [n]T: unable to find ]")
		return
	}
	rest = name[idx+1:]
	if idx == 1 {
		// is slice type
		len = -1
		return
	}
	var l uint64
	l, err = strconv.ParseUint(name[1:idx], 10, 32)
	if err != nil {
		err = eb.Build().Str("name", name).Errorf("go type name is not a go array type of the form [n]T: unable to parse n")
		return
	}
	len = int(l)
	return
}
