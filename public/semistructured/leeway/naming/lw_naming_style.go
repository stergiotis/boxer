package naming

import (
	"bytes"
	"errors"
	"iter"
	"strings"
	"unicode"

	"github.com/ettle/strcase"
	"github.com/go-json-experiment/json"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const SnakeCaseSeparator = '_'
const SpinalCaseSeparator = '-'
const InvalidComponentRune = unicode.ReplacementChar
const (
	ShortestNamingStyle     = LowerCamelCase
	BestReadableNamingStyle = LowerSpinalCase
	DefaultNamingStyle      = BestReadableNamingStyle
)

func ConvertNameStyle[S ~string](name S, targetStyle NamingStyleE) (naming S) {
	switch targetStyle {
	case LowerCamelCase:
		return S(strcase.ToCamel(string(name)))
	case UpperCamelCase:
		return S(strcase.ToPascal(string(name)))
	case LowerSnakeCase:
		return S(strcase.ToSnake(string(name)))
	case UpperSnakeCase:
		return S(strcase.ToSNAKE(string(name)))
	case LowerSpinalCase:
		return S(strcase.ToKebab(string(name)))
	case UpperSpinalCase:
		return S(strcase.ToKEBAB(string(name)))
	default:
		log.Panic().Uint8("targetStyle", uint8(targetStyle)).Msg("unhandled target naming style")
	}
	return
}
func MustBeValidKey[S ~string](key S) (r Key) {
	err := ValidateKey(key)
	if err != nil {
		log.Panic().Err(err).Str("key", string(key)).Msg("key is not valid")
	}
	r = Key(key)
	return
}
func MustBeValidStylableName[S ~string](name S) (r StylableName) {
	err := ValidateStylableName(name)
	if err != nil {
		log.Panic().Err(err).Str("name", string(name)).Msg("name is not valid")
	}
	r = StylableName(name).Convert(DefaultNamingStyle)
	return
}
func MakeKey[S ~string](key S) (r Key, err error) {
	err = ValidateKey(key)
	if err != nil {
		return
	}
	r = Key(key)
	return
}
func MakeStylableName[S ~string](name S) (r StylableName, err error) {
	err = ValidateStylableName(name)
	if err != nil {
		return
	}
	r = StylableName(name).Convert(DefaultNamingStyle)
	return
}
func Compare[S ~string](a, b S) int {
	return strings.Compare(string(StylableName(a).Convert(DefaultNamingStyle)), string(StylableName(b).Convert(DefaultNamingStyle)))
}
func (inst StylableName) Compare(other StylableName) int {
	return Compare(inst, other)
}
func ValidateStylableName[S ~string](name S) (err error) {
	if name == "" {
		err = eh.Errorf("empty values are invalid")
		return
	}
	var errs []error
	for component := range StylableName(name).IterateComponents() {
		e := ValidateNameComponent(component)
		if e != nil {
			if errs == nil {
				errs = make([]error, 0, 8)
			}
			errs = append(errs, e)
		}
	}
	if errs != nil {
		err = errors.Join(errs...)
	}
	return
}
func ValidateNameComponent[S ~string](component S) (err error) {
	if component == "" {
		return eh.Errorf("empty components are not representable")
	}
	runes := []rune(component)
	if unicode.ToLower(runes[0]) == unicode.ToUpper(runes[0]) {
		return eb.Build().Str("initialRune", string(runes[0])).Str("component", string(component)).Errorf("first rune must not be the same in lower- and upper-case")
	}
	for r := range runes {
		switch r {
		case SpinalCaseSeparator:
			return eb.Build().Str("component", string(component)).Str("separator", string(SpinalCaseSeparator)).Errorf("component contains spinal case separator")
		case SnakeCaseSeparator:
			return eb.Build().Str("component", string(component)).Str("separator", string(SnakeCaseSeparator)).Errorf("component contains snake case separator")
		case InvalidComponentRune:
			return eb.Build().Str("component", string(component)).Errorf("component contains invalid component rune")
		}
	}
	err = checkJsonSerializableString(string(component))
	if err != nil {
		err = eb.Build().Str("component", string(component)).Errorf("component must be json serializable: %w", err)
		return
	}
	return
}
func JoinComponents[S ~string](components ...S) (name StylableName, err error) {
	if len(components) == 0 {
		err = eh.Errorf("empty names are not allowed")
		return
	}
	s := bytes.NewBuffer(make([]byte, 0, 128))
	for _, c := range components {
		err = ValidateNameComponent(c)
		if err != nil {
			return
		}
		if s.Len() > 0 {
			_, err = s.WriteRune(SpinalCaseSeparator)
			if err != nil {
				return
			}
		}
		_, err = s.WriteString(strings.ToLower(string(c)))
		if err != nil {
			return
		}
	}
	name = StylableName(s.String())
	return
}
func (inst StylableName) IterateComponents() iter.Seq[StylableName] {
	return func(yield func(StylableName) bool) {
		for component := range strings.SplitSeq(string(ConvertNameStyle(inst, LowerSpinalCase)), string(SpinalCaseSeparator)) {
			if !yield(StylableName(component)) {
				return
			}
		}
	}
}
func (inst StylableName) Validate() (err error) {
	if inst == "" {
		err = eh.Errorf("empty values are invalid")
		return
	}
	var errs []error
	for component := range inst.IterateComponents() {
		e := ValidateNameComponent(component)
		if e != nil {
			if errs == nil {
				errs = make([]error, 0, 8)
			}
			errs = append(errs, e)
		}
	}
	if errs != nil {
		err = errors.Join(errs...)
	}
	return
}
func (inst StylableName) IsEmpty() (empty bool) {
	return inst == ""
}
func (inst StylableName) IsValid() (valid bool) {
	if inst == "" {
		return
	}
	for component := range inst.IterateComponents() {
		if ValidateNameComponent(component) != nil {
			return
		}
	}
	valid = true
	return
}

func (inst StylableName) Convert(targetStyle NamingStyleE) StylableName {
	return ConvertNameStyle(inst, targetStyle)
}
func (inst StylableName) IsUsingStyle(style NamingStyleE) bool {
	return ConvertNameStyle(inst, style) == inst
}
func (inst StylableName) String() string {
	// NOTE: does _not_ enforce a style
	return string(inst)
}
func (inst StylableName) Bytes() []byte {
	return []byte(inst)
}
func (inst Key) String() string {
	return string(inst)
}
func (inst Key) IsValid() (valid bool) {
	valid = ValidateKey(inst) == nil
	return
}
func (inst Key) Validate() (err error) {
	err = ValidateKey(inst)
	return
}
func ValidateKey[S ~string](s S) (err error) {
	err = checkJsonSerializableString(string(s))
	if err != nil {
		err = eb.Build().Str("key", string(s)).Errorf("key must be json serializable: %w", err)
		return
	}
	return
}
func checkJsonSerializableString(s string) (err error) {
	_, err = json.Marshal(s)
	return
}
