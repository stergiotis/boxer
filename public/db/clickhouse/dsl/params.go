package dsl

import (
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/exp/maps"
	"iter"
)

type ParamSet struct {
	typesLu          map[string][]*chparser.QueryParam
	paramOccurrences int
}

func NewParamSet() *ParamSet {
	return &ParamSet{typesLu: nil}
}

var ErrIncompatibleParam = eh.Errorf("a param with an incompatible type is already contained in param set")

func (inst *ParamSet) Add(param *chparser.QueryParam) (err error) {
	if param == nil {
		return
	}
	if inst.typesLu == nil {
		inst.typesLu = make(map[string][]*chparser.QueryParam, 128)
	}
	others := inst.typesLu[param.Name.Name]
	if others == nil {
		inst.typesLu[param.Name.Name] = []*chparser.QueryParam{param}
		inst.paramOccurrences++
		return
	}
	for _, o := range others {
		if !isParamTypeCompatible(param.Type.Type(), o.Type.Type()) {
			err = eb.Build().Str("other", o.String()).Str("this", param.String()).Errorf("unable to add param to paramset: incompatible with existing param: %w", ErrIncompatibleParam)
			return
		}
	}
	inst.typesLu[param.Name.Name] = append(others, param)
	inst.paramOccurrences++
	return
}
func (inst *ParamSet) UnionMod(other *ParamSet) (err error) {
	for _, ps := range other.All() {
		for _, p := range ps {
			err = inst.Add(p)
			if err != nil {
				err = eh.Errorf("unable to union param sets: %w", err)
				return
			}
		}
	}
	return
}
func (inst *ParamSet) TotalParamOccurrences() int {
	return inst.paramOccurrences
}
func (inst *ParamSet) TotalDistinctParams() int {
	return len(inst.typesLu)
}
func (inst *ParamSet) IsEmpty() bool {
	return len(inst.typesLu) == 0
}
func (inst *ParamSet) All() iter.Seq2[string, []*chparser.QueryParam] {
	return func(yield func(string, []*chparser.QueryParam) bool) {
		for k, vs := range inst.typesLu {
			if !yield(k, vs) {
				return
			}
		}
	}
}
func (inst *ParamSet) Clear() {
	if len(inst.typesLu) > 0 {
		maps.Clear(inst.typesLu)
	}
	inst.paramOccurrences = 0
}
func isParamTypeCompatible(t1 string, t2 string) (compatible bool) {
	return t1 == t2
}

type paramsDiscoverer struct {
	chparser.DefaultASTVisitor
	params *ParamSet
}

func newParamsDiscoverer() *paramsDiscoverer {
	return &paramsDiscoverer{
		DefaultASTVisitor: chparser.DefaultASTVisitor{},
		params:            nil,
	}
}
func (inst *paramsDiscoverer) VisitQueryParam(expr *chparser.QueryParam) error {
	return inst.params.Add(expr)
}
func (inst *paramsDiscoverer) discover(ast chparser.Expr, params *ParamSet) (err error) {
	if params == nil {
		return eh.Errorf("paramset is nil")
	}
	inst.params = params
	err = ast.Accept(inst)
	inst.params = nil
	if err != nil {
		err = eh.Errorf("unable to discover query params: %w", err)
		return
	}
	return
}
