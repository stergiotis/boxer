package definition

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime/rustclient"
	"github.com/stergiotis/boxer/public/thestack/fffi2/ir"
	"github.com/valyala/fasttemplate"
	"golang.org/x/exp/maps"
)

type templatedRustClientCode struct {
	str    string
	tmpl   *fasttemplate.Template
	caller string
}

func (inst *templatedRustClientCode) UseDefaultCode() bool {
	return false
}

var _ ir.VerbatimCodeI = (*templatedRustClientCode)(nil)

func (inst *templatedRustClientCode) GetVerbatimCode() string {
	if inst.tmpl == nil {
		return inst.str
	}
	return inst.caller + inst.tmpl.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		v, h := rustClientTemplateValues[tag]
		if !h {
			log.Panic().Str("tag", tag).
				Str("caller", inst.caller).
				Str("in", inst.str).
				Strs("available", maps.Keys(rustClientTemplateValues)).
				Msg("unable to find tag in template values")
		}
		return fmt.Fprint(w, v)
	})
}
func toMap(in any) (out map[string]any) {
	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		log.Panic().Type("in", in).Msg("unable to convert anything else than structs")
	}

	typ := v.Type()
	out = make(map[string]interface{}, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		fi := typ.Field(i)
		out[fi.Name] = v.Field(i).Interface()
	}
	return out
}

var rustClientTemplateValues = toMap(rustclient.BuilderFactoryCodeGenExprs)

func rustClientCode(s string) *templatedRustClientCode {
	c := ir.NewStackCapture(1, 1)
	var caller string
	if len(c.Files) > 0 {
		caller = fmt.Sprintf("// generating location: %s:%d %s(...)\n", c.Files[0], c.Lines[0], c.Funcs[0])
	}
	if strings.Contains(s, "{{") {
		t, err := fasttemplate.NewTemplate(s, "{{", "}}")
		if err != nil {
			log.Panic().Err(err).Str("template", s).Msg("invalid template")
		}
		return &templatedRustClientCode{
			str:    s,
			tmpl:   t,
			caller: caller,
		}
	}
	return &templatedRustClientCode{
		str:    s,
		tmpl:   nil,
		caller: caller,
	}
}
