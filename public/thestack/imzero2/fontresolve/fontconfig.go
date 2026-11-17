package fontresolve

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Fontconfig asks the host's fontconfig through fc-match and fc-query.
type Fontconfig struct {
	Ctx context.Context
}

var _ FontconfigI = Fontconfig{}

func (inst Fontconfig) ctx() context.Context {
	if inst.Ctx == nil {
		return context.Background()
	}
	return inst.Ctx
}

// Match runs fc-match for pattern.
func (inst Fontconfig) Match(pattern string) (file string, family string, err error) {
	if _, ok := extbin.FcMatch.Resolve(); !ok {
		err = ErrUnavailable
		return
	}
	out, err := extbin.FcMatch.Output(inst.ctx(), extbin.Opts{}, "-f", "%{file}\t%{family}\n", pattern)
	if err != nil {
		err = eb.Build().Str("pattern", pattern).Errorf("fc-match: %w", err)
		return
	}
	file, family, ok := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if !ok {
		err = eb.Build().Str("pattern", pattern).Str("answer", string(out)).Errorf("fc-match gave no file and family")
	}
	return
}

// Charset runs fc-query for file's charset.
func (inst Fontconfig) Charset(file string) (charset string, err error) {
	if _, ok := extbin.FcQuery.Resolve(); !ok {
		err = ErrUnavailable
		return
	}
	out, err := extbin.FcQuery.Output(inst.ctx(), extbin.Opts{}, "--format=%{charset}", file)
	if err != nil {
		err = eb.Build().Str("file", file).Errorf("fc-query: %w", err)
		return
	}
	charset = string(out)
	return
}
