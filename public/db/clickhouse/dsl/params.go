package dsl

import (
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/exp/maps"
	"iter"
)

type ParamBindEnv struct {
	bind map[string]*chparser.SettingExprList
}

func NewParamBindEnv() *ParamBindEnv {
	return &ParamBindEnv{
		bind: make(map[string]*chparser.SettingExprList, 32),
	}
}
func (inst *ParamBindEnv) Has(name string) (has bool) {
	_, has = inst.bind[name]
	return
}

var ErrParamAlreadyBound = eh.Errorf("parameter is already bound to a value")

func (inst *ParamBindEnv) IsEmpty() bool {
	return len(inst.bind) == 0
}
func (inst *ParamBindEnv) AddDistinct(p *chparser.SettingExprList) (err error) {
	if p == nil {
		return
	}
	name := p.Name.Name
	if inst.Has(name) {
		err = eb.Build().Str("param", name).Errorf("unable to add: %w", ErrParamAlreadyBound)
		return
	}
	inst.bind[name] = p
	return
}
func (inst *ParamBindEnv) Set(p *chparser.SettingExprList) {
	if p == nil {
		return
	}
	name := p.Name.Name
	inst.bind[name] = p
	return
}
func (inst *ParamBindEnv) IterSql() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for _, p := range inst.bind {
			if !yield(p.Name.String(), p.Expr.String()) {
				return
			}
		}
	}
}

type ParamSlotSet struct {
	typesLu          map[string][]*chparser.QueryParam
	paramOccurrences int
}

func NewParamSlotsSet() *ParamSlotSet {
	return &ParamSlotSet{
		typesLu:          nil,
		paramOccurrences: 0,
	}
}

var ErrIncompatibleParam = eh.Errorf("a param with an incompatible type is already contained in param set")

func (inst *ParamSlotSet) Add(param *chparser.QueryParam) (err error) {
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
func (inst *ParamSlotSet) UnionMod(other *ParamSlotSet) (err error) {
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
func (inst *ParamSlotSet) TotalParamOccurrences() int {
	return inst.paramOccurrences
}
func (inst *ParamSlotSet) TotalDistinctParams() int {
	return len(inst.typesLu)
}
func (inst *ParamSlotSet) IsEmpty() bool {
	return len(inst.typesLu) == 0
}
func (inst *ParamSlotSet) All() iter.Seq2[string, []*chparser.QueryParam] {
	return func(yield func(string, []*chparser.QueryParam) bool) {
		for k, vs := range inst.typesLu {
			if !yield(k, vs) {
				return
			}
		}
	}
}
func (inst *ParamSlotSet) NamesAndTypes() iter.Seq2[string, *containers.HashSet[string]] {
	return func(yield func(string, *containers.HashSet[string]) bool) {
		types := containers.NewHashSet[string](32)
		defer types.Clear()
		for k, vs := range inst.typesLu {
			for _, v := range vs {
				types.Add(v.Type.String())
			}
			if !yield(k, types) {
				return
			}
			types.Clear()
		}
	}
}
func (inst *ParamSlotSet) Clear() {
	if len(inst.typesLu) > 0 {
		maps.Clear(inst.typesLu)
	}
	inst.paramOccurrences = 0
}
func isParamTypeCompatible(t1 string, t2 string) (compatible bool) {
	return t1 == t2
}

type paramSlotsDiscoverer struct {
	chparser.DefaultASTVisitor
	params *ParamSlotSet
}

func newParamSlotsDiscoverer() *paramSlotsDiscoverer {
	return &paramSlotsDiscoverer{
		DefaultASTVisitor: chparser.DefaultASTVisitor{},
		params:            nil,
	}
}
func (inst *paramSlotsDiscoverer) VisitQueryParam(expr *chparser.QueryParam) error {
	return inst.params.Add(expr)
}
func (inst *paramSlotsDiscoverer) discover(ast chparser.Expr, params *ParamSlotSet) (err error) {
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
